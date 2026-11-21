package room

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"silo.agent/internal/app/chats"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
)

// The notices Silo writes in a room.
const (
	waitNotice     = "The Bots have said a lot among themselves: they wait for one of you before they wake each other again."
	nobodyNamed    = "Nobody was named: in this room only the Bots a message @mentions answer."
	couldNotAnswer = "%s ran into a problem: %s"
)

// --- what a Bot says ---

// say posts what a Bot wrote in its seat to the room, unless it stayed quiet
// (Pass) or was taken out of the room meanwhile. The Bots it names are woken.
func (s *Service) say(seat db.RoomBot, text string) {
	text = passed(text)
	if text == "" {
		return
	}
	var still db.RoomBot
	if s.db.Where("room_id = ? AND bot_id = ?", seat.RoomID, seat.BotID).Limit(1).Find(&still); still.ChatID != seat.ChatID {
		return
	}
	text = textx.CapRunes(s.host.KeptText(seat.BotID, text), textMax)
	m := db.RoomMessage{
		ID: ids.New(), RoomID: seat.RoomID, Kind: "message", BotID: seat.BotID, RunID: s.host.LiveRunID(seat.BotID, seat.ChatID),
		Author: s.botName(seat.BotID), Body: text, Mentions: strings.Join(mentionsIn(text, s.members(seat.RoomID)), ","), CreatedAt: time.Now(),
	}
	if err := s.db.Create(&m).Error; err != nil {
		log.Printf("room %s: post from %s: %v", seat.RoomID, seat.BotID, err)
		return
	}
	s.stamp(&m)
	s.touch(seat.RoomID)
	s.publish(seat.RoomID, change{msg: &m})
	go s.botSpoke(&m)
}

// botSpoke wakes the Bots a Bot's message names, unless the Bots have said
// enough among themselves since a person last spoke.
func (s *Service) botSpoke(m *db.RoomMessage) {
	var named []string
	for _, id := range splitIDs(m.Mentions) {
		if id != m.BotID {
			named = append(named, id)
		}
	}
	if len(named) == 0 {
		return
	}
	since := s.lastPersonSeq(m.RoomID)
	var n int64
	s.db.Model(&db.RoomMessage{}).Where("room_id = ? AND kind = ? AND bot_id <> '' AND seq > ?", m.RoomID, "message", since).Count(&n)
	if n > botStreak {
		var told int64
		s.db.Model(&db.RoomMessage{}).Where("room_id = ? AND kind = ? AND body = ? AND seq > ?", m.RoomID, "notice", waitNotice, since).Count(&told)
		if told == 0 {
			s.notice(m.RoomID, waitNotice)
		}
		return
	}
	for _, id := range named {
		s.hand(m.RoomID, id, true, nil)
	}
}

func (s *Service) lastPersonSeq(roomID string) int64 {
	var seq int64
	s.db.Model(&db.RoomMessage{}).Select("COALESCE(MAX(seq), 0)").Where("room_id = ? AND kind = ? AND user_id <> ''", roomID, "message").Scan(&seq)
	return seq
}

func splitIDs(s string) []string {
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// --- handing a Bot what the room said ---

// hand gives a Bot what the room said since it last heard it, in its seat:
// into the run working there, or a run of its own. It reports whether a new
// run started; only then is turned called, when its first turn ends. A Bot
// with nothing new from others is handed nothing.
func (s *Service) hand(roomID, botID string, mentioned bool, turned func()) bool {
	defer s.seatLock(roomID, botID)()
	var seat db.RoomBot
	if s.db.Where("room_id = ? AND bot_id = ?", roomID, botID).Limit(1).Find(&seat); seat.ChatID == "" {
		return false
	}
	r := s.room(roomID)
	if r == nil {
		return false
	}
	var msgs []db.RoomMessage
	s.db.Where("room_id = ? AND kind = ? AND seq > ?", roomID, "message", seat.SeenSeq).Order("seq").Find(&msgs)
	if len(msgs) == 0 {
		return false
	}
	top := msgs[len(msgs)-1].Seq
	var lines []line
	for _, m := range msgs {
		if m.BotID == botID {
			continue // its own words are its replies in the seat already
		}
		if slices.Contains(splitIDs(m.Mentions), botID) {
			mentioned = true
		}
		lines = append(lines, line{Author: m.Author, Bot: m.BotID != "", Body: m.Body, At: m.CreatedAt})
	}
	if len(lines) == 0 {
		s.seen(seat, top)
		return false
	}
	lines, omitted := fit(lines, handMaxLines, handMaxRunes)
	text := handed(lines, omitted, s.working(roomID, botID), zone.OfBot(s.db, botID))
	req := run.Request{
		BotID: botID, ChatID: seat.ChatID, Text: text, From: run.FromRoom,
		Extra:  run.Extra{Room: &run.RoomFrom{Title: r.Title, Mentioned: mentioned}},
		Origin: s.origin(seat, turned),
	}
	_, started, err := s.host.StartOrSteer(req)
	if err != nil {
		// Busy with a full inbox, or a Bot that may not run: what it missed
		// is handed with the next message.
		if !errors.Is(err, run.ErrBusy) {
			log.Printf("room %s: hand to %s: %v", roomID, botID, err)
		}
		return false
	}
	s.seen(seat, top)
	return started
}

func (s *Service) seen(seat db.RoomBot, seq int64) {
	s.db.Model(&db.RoomBot{}).Where("room_id = ? AND bot_id = ? AND seen_seq < ?", seat.RoomID, seat.BotID, seq).UpdateColumn("seen_seq", seq)
}

// working are the names of the room's other Bots with a run going in their
// seat: at work on a reply, which the Bot being handed the room should not
// repeat.
func (s *Service) working(roomID, except string) []string {
	var out []string
	for _, seat := range s.bots(roomID) {
		if seat.BotID == except {
			continue
		}
		if s.host.LiveRunID(seat.BotID, seat.ChatID) != "" {
			out = append(out, s.botName(seat.BotID))
		}
	}
	return out
}

// --- the floor ---

// round is who decides about one person's message, stage by stage: the Bots
// it names together, then each of the others alone. A stage ends when each
// of its Bots has had its first turn (answered, stayed quiet or started
// working), or after Floor.
type round struct {
	id     string
	stages [][]string
	at     int
	wait   map[string]bool
	named  map[string]bool
	timer  *time.Timer
}

// heard starts the round for a person's message: it replaces any round still
// going, whose Bots are handed this message with the rest.
func (s *Service) heard(m *db.RoomMessage) {
	r := s.room(m.RoomID)
	seats := s.bots(m.RoomID)
	if r == nil || len(seats) == 0 {
		return
	}
	named := map[string]bool{}
	for _, id := range splitIDs(m.Mentions) {
		named[id] = true
	}
	if len(seats) == 1 {
		named[seats[0].BotID] = true // the one Bot here is always asked
	}
	order := s.speakingOrder(m.RoomID, seats)
	var stages [][]string
	var first []string
	for _, id := range order {
		if named[id] {
			first = append(first, id)
		}
	}
	if len(first) > 0 {
		stages = append(stages, first)
	}
	if r.ReplyMode == ModeMentions {
		if len(first) == 0 {
			s.notice(m.RoomID, nobodyNamed)
		}
	} else {
		for _, id := range order {
			if !named[id] {
				stages = append(stages, []string{id})
			}
		}
	}
	rd := &round{id: ids.New(), stages: stages, named: named}
	s.mu.Lock()
	if old := s.rounds[m.RoomID]; old != nil && old.timer != nil {
		old.timer.Stop()
	}
	s.rounds[m.RoomID] = rd
	s.mu.Unlock()
	s.runStage(m.RoomID, rd.id)
}

// speakingOrder is the room's Bots, the one that spoke last first: a person
// answering a Bot most often answers the one that just spoke.
func (s *Service) speakingOrder(roomID string, seats []db.RoomBot) []string {
	var rows []struct {
		BotID string
		Seq   int64
	}
	s.db.Model(&db.RoomMessage{}).Select("bot_id, MAX(seq) AS seq").Where("room_id = ? AND bot_id <> ''", roomID).Group("bot_id").Scan(&rows)
	last := map[string]int64{}
	for _, r := range rows {
		last[r.BotID] = r.Seq
	}
	out := make([]string, 0, len(seats))
	for _, seat := range seats {
		out = append(out, seat.BotID)
	}
	sort.SliceStable(out, func(i, j int) bool { return last[out[i]] > last[out[j]] })
	return out
}

// runStage hands the room to the Bots of the round's current stage.
func (s *Service) runStage(roomID, id string) {
	s.mu.Lock()
	rd := s.rounds[roomID]
	if rd == nil || rd.id != id {
		s.mu.Unlock()
		return
	}
	if rd.at >= len(rd.stages) {
		delete(s.rounds, roomID)
		s.mu.Unlock()
		return
	}
	at, stage := rd.at, rd.stages[rd.at]
	rd.wait = map[string]bool{}
	named := map[string]bool{}
	for _, b := range stage {
		rd.wait[b] = true
		named[b] = rd.named[b]
	}
	rd.timer = time.AfterFunc(s.Floor, func() { s.next(roomID, id, at, "") })
	s.mu.Unlock()
	for _, b := range stage {
		if !s.hand(roomID, b, named[b], func() { s.next(roomID, id, at, b) }) {
			s.next(roomID, id, at, b)
		}
	}
}

// next is a Bot of the stage done with its turn (or, with no Bot, the stage's
// time up): once nobody in it is left, the next stage decides.
func (s *Service) next(roomID, id string, at int, botID string) {
	s.mu.Lock()
	rd := s.rounds[roomID]
	if rd == nil || rd.id != id || rd.at != at {
		s.mu.Unlock()
		return
	}
	if botID != "" {
		delete(rd.wait, botID)
		if len(rd.wait) > 0 {
			s.mu.Unlock()
			return
		}
	}
	if rd.timer != nil {
		rd.timer.Stop()
	}
	rd.at++
	s.mu.Unlock()
	// Off the caller: a Bot's run tells its turn from inside its loop.
	go s.runStage(roomID, id)
}

// endRound forgets the room's round: nobody else is handed its message.
func (s *Service) endRound(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rd := s.rounds[roomID]; rd != nil && rd.timer != nil {
		rd.timer.Stop()
	}
	delete(s.rounds, roomID)
}

// --- the runs in seats ---

// RunEnded is told when a run ends in any chat; one in a seat that failed
// says so in the room, since nothing else there would.
func (s *Service) RunEnded(botID, chatID, runID, st string) {
	if st != "error" {
		return
	}
	seat := s.seatOf(chatID)
	if seat == nil {
		return
	}
	var ev db.RunEvent
	s.db.Where("run_id = ? AND kind = ?", runID, "error").Order("seq desc").Limit(1).Find(&ev)
	why := oneLine(ev.Body, 300)
	if why == "" {
		why = "it stopped before answering"
	}
	s.notice(seat.RoomID, fmt.Sprintf(couldNotAnswer, s.botName(botID), why))
}

// stop ends every Bot's work in the room and its round.
func (s *Service) stop(roomID string) {
	s.endRound(roomID)
	for _, seat := range s.bots(roomID) {
		s.host.StopChatLive(seat.BotID, seat.ChatID)
	}
}

// dropSeat takes a Bot out of a room: its work there stops and its seat goes.
func (s *Service) dropSeat(seat db.RoomBot) {
	s.host.StopChatLive(seat.BotID, seat.ChatID)
	s.db.Where("room_id = ? AND bot_id = ?", seat.RoomID, seat.BotID).Delete(&db.RoomBot{})
	chats.Drop(s.db, seat.ChatID)
	s.seats.Delete(seat.RoomID + "/" + seat.BotID)
}

// drop deletes a room with everything in it.
func (s *Service) drop(roomID string) {
	s.endRound(roomID)
	for _, seat := range s.bots(roomID) {
		s.dropSeat(seat)
	}
	s.db.Where("room_id = ?", roomID).Delete(&db.RoomMessage{})
	s.db.Where("room_id = ?", roomID).Delete(&db.RoomPerson{})
	s.db.Where("id = ?", roomID).Delete(&db.Room{})
	s.publish(roomID, change{room: true})
}

// DropBot takes a deleted Bot out of its rooms.
func (s *Service) DropBot(botID string) {
	var seats []db.RoomBot
	s.db.Where("bot_id = ?", botID).Find(&seats)
	name := s.botName(botID)
	for _, seat := range seats {
		s.dropSeat(seat)
		s.notice(seat.RoomID, name+" was deleted.")
		s.publish(seat.RoomID, change{room: true})
	}
}

// DropSpace deletes a deleted Workspace's rooms.
func (s *Service) DropSpace(spaceID string) {
	var ids []string
	s.db.Model(&db.Room{}).Where("space_id = ?", spaceID).Pluck("id", &ids)
	for _, id := range ids {
		s.drop(id)
	}
}

// LostBot takes a person out of the rooms a Bot they are no longer on is in:
// everyone in a room is on every Bot in it.
func (s *Service) LostBot(botID, userID string) {
	var roomIDs []string
	s.db.Model(&db.RoomPerson{}).Where("user_id = ? AND room_id IN (?)", userID,
		s.db.Model(&db.RoomBot{}).Select("room_id").Where("bot_id = ?", botID)).Pluck("room_id", &roomIDs)
	if len(roomIDs) == 0 {
		return
	}
	var who db.User
	s.db.Select("email").Where("id = ?", userID).Limit(1).Find(&who)
	name := s.botName(botID)
	for _, id := range roomIDs {
		s.takeOut(id, userID, fmt.Sprintf("%s left: they are no longer on %s.", who.Email, name))
	}
}

// DropPerson takes a deleted account out of every room.
func (s *Service) DropPerson(userID string) {
	var roomIDs []string
	s.db.Model(&db.RoomPerson{}).Where("user_id = ?", userID).Pluck("room_id", &roomIDs)
	for _, id := range roomIDs {
		s.takeOut(id, userID, "")
	}
}

// takeOut removes a person from a room (the room goes with its last person),
// saying why when there is a why.
func (s *Service) takeOut(roomID, userID, why string) {
	s.db.Where("room_id = ? AND user_id = ?", roomID, userID).Delete(&db.RoomPerson{})
	var left int64
	if s.db.Model(&db.RoomPerson{}).Where("room_id = ?", roomID).Count(&left); left == 0 {
		s.drop(roomID)
		return
	}
	if why != "" {
		s.notice(roomID, why)
	}
	s.publish(roomID, change{room: true})
}

// Moved takes a Bot moved to another Workspace out of its old Workspace's
// rooms: a room is one Workspace's.
func (s *Service) Moved(botID, spaceID string) {
	var seats []db.RoomBot
	s.db.Where("bot_id = ? AND room_id IN (?)", botID, s.db.Model(&db.Room{}).Select("id").Where("space_id <> ?", spaceID)).Find(&seats)
	name := s.botName(botID)
	for _, seat := range seats {
		s.dropSeat(seat)
		s.notice(seat.RoomID, name+" moved to another workspace.")
		s.publish(seat.RoomID, change{room: true})
	}
}
