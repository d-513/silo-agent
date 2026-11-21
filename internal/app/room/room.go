// Package room is rooms: conversations several Bots and people of one
// Workspace share.
//
// The room's transcript is its own (db.RoomMessage). Each Bot hears it in its
// seat, a chat of its own (db.RoomBot.ChatID): what the room said since the
// Bot last heard it is handed to it as one message there, and what it writes
// there is posted back to the room. So each Bot keeps its own context (which
// only grows, as a provider's prompt cache needs), its own tools, rules,
// approvals and memory, and nobody else sees its tool calls.
//
// Who answers: a person's message is heard by every Bot. A Bot it names
// (@Name) must answer; the others each decide whether they have something to
// add, and say Pass when not. They decide one after another, the one that
// spoke last first, so a Bot sees what the ones before it answered or that
// they are at work on it (the floor, floor.go). A Bot's message wakes only
// the Bots it names, and only a few times before a person speaks again.
//
// Everyone in a room is on every Bot in it: what a Bot says is shared with
// everyone there, and the Bot is the unit of trust (access.Room).
package room

import (
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"silo.agent/internal/app/run"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

const (
	// Reply modes (db.Room.ReplyMode).
	ModeDecide   = "decide"
	ModeMentions = "mentions"

	botsMax   = 8
	peopleMax = 50
	textMax   = 20000
	titleMax  = 80
	// handMaxLines and handMaxRunes bound what one hand-over carries: a Bot
	// not woken for a long while is told how many older messages it missed.
	handMaxLines = 40
	handMaxRunes = 24000
	// botStreak is how many Bot messages in a row may wake other Bots: past
	// it, Bots wait for a person (two Bots naming each other would never
	// stop).
	botStreak = 8
	// floorWait is how long one Bot holds the floor before the next decides
	// anyway.
	floorWait = 45 * time.Second
	// replayMax is how many messages a stream opens with.
	replayMax = 200
)

// Host is what rooms need from the App.
type Host interface {
	run.Engine
	// RunStep is what a conversation's live run is doing: live, and the
	// tool it is in ("" while it thinks).
	RunStep(botID, chatID string) (live bool, step string)
	// NeedsYou reports whether a run in the chat waits on an approval the
	// person may answer.
	NeedsYou(botID, chatID, userID string) bool
	// KeptText is text as the Bot's run log keeps it: its secrets masked,
	// valid UTF-8.
	KeptText(botID, s string) string
}

// Service is rooms: their RPCs, the seats, and who answers what.
type Service struct {
	db   *gorm.DB
	host Host
	// Floor is how long one Bot holds the floor; tests shorten it.
	Floor time.Duration

	mu     sync.Mutex
	rounds map[string]*round
	seats  sync.Map // roomID/botID → *sync.Mutex

	subsMu sync.Mutex
	subs   map[string]map[*sub]struct{}
}

func New(gdb *gorm.DB, h Host) *Service {
	return &Service{db: gdb, host: h, Floor: floorWait, rounds: map[string]*round{}, subs: map[string]map[*sub]struct{}{}}
}

// --- the room's own stream ---

// change is one thing a room's viewers are told: a message, or that the
// room changed (each viewer reads it again as they see it).
type change struct {
	msg  *db.RoomMessage
	room bool
}

type sub struct {
	ch   chan change
	once sync.Once
}

func (s *sub) close() { s.once.Do(func() { close(s.ch) }) }

// subscribe follows a room until stop. A viewer that falls behind is cut
// off (its channel closes) rather than holding the room up.
func (s *Service) subscribe(roomID string) (<-chan change, func()) {
	x := &sub{ch: make(chan change, 128)}
	s.subsMu.Lock()
	if s.subs[roomID] == nil {
		s.subs[roomID] = map[*sub]struct{}{}
	}
	s.subs[roomID][x] = struct{}{}
	s.subsMu.Unlock()
	return x.ch, func() {
		s.subsMu.Lock()
		delete(s.subs[roomID], x)
		if len(s.subs[roomID]) == 0 {
			delete(s.subs, roomID)
		}
		s.subsMu.Unlock()
		x.close()
	}
}

func (s *Service) publish(roomID string, c change) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for x := range s.subs[roomID] {
		select {
		case x.ch <- c:
		default:
			delete(s.subs[roomID], x)
			x.close()
		}
	}
}

// --- seats ---

func (s *Service) seatLock(roomID, botID string) func() {
	m, _ := s.seats.LoadOrStore(roomID+"/"+botID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// seatOf is the room seat a chat is, nil for any other chat.
func (s *Service) seatOf(chatID string) *db.RoomBot {
	if chatID == "" {
		return nil
	}
	var seat db.RoomBot
	if s.db.Where("chat_id = ?", chatID).Limit(1).Find(&seat); seat.RoomID == "" {
		return nil
	}
	return &seat
}

// Origin is how a run in a seat is wired to its room: what the Bot writes is
// posted there, and turned (when set) is told when its first turn ends. A
// chat that is no seat has none (nil).
func (s *Service) Origin(chatID string) *run.Origin {
	seat := s.seatOf(chatID)
	if seat == nil {
		return nil
	}
	return s.origin(*seat, nil)
}

func (s *Service) origin(seat db.RoomBot, turned func()) *run.Origin {
	return &run.Origin{
		Room:   &seat,
		Turned: turned,
		Deliver: func(out channels.Outbound) error {
			s.say(seat, out.Text)
			return nil
		},
	}
}

// bots are the room's Bots, in the order they came in.
func (s *Service) bots(roomID string) []db.RoomBot {
	var out []db.RoomBot
	s.db.Where("room_id = ?", roomID).Order("added_at").Find(&out)
	return out
}

// members are the room's Bots by name, as mentions name them.
func (s *Service) members(roomID string) []member {
	var rows []struct {
		ID   string
		Name string
	}
	s.db.Table("room_bots AS rb").Select("b.id, b.name").Joins("JOIN bots b ON b.id = rb.bot_id").
		Where("rb.room_id = ?", roomID).Order("rb.added_at").Scan(&rows)
	out := make([]member, 0, len(rows))
	for _, r := range rows {
		out = append(out, member{ID: r.ID, Name: r.Name})
	}
	return out
}

func (s *Service) botName(botID string) string {
	var b db.Bot
	s.db.Select("name").Where("id = ?", botID).Limit(1).Find(&b)
	return b.Name
}

func (s *Service) room(roomID string) *db.Room {
	var r db.Room
	if s.db.Where("id = ?", roomID).Limit(1).Find(&r); r.ID == "" {
		return nil
	}
	return &r
}

// touch moves the room's last activity.
func (s *Service) touch(roomID string) {
	s.db.Model(&db.Room{}).Where("id = ?", roomID).UpdateColumn("updated_at", time.Now())
}

// notice writes a line Silo says in the room (who came, why the Bots wait):
// the people see it, no Bot is handed it.
func (s *Service) notice(roomID, text string) {
	m := db.RoomMessage{ID: ids.New(), RoomID: roomID, Kind: "notice", Body: text, CreatedAt: time.Now()}
	if err := s.db.Create(&m).Error; err != nil {
		log.Printf("room %s: notice: %v", roomID, err)
		return
	}
	s.stamp(&m)
	s.publish(roomID, change{msg: &m})
}

// Prompt is the note a run in a seat gets (prompts/ROOM.md as tmpl): who is
// in the room and how it answers. "" for a run that is in no seat.
func (s *Service) Prompt(seat *db.RoomBot, tmpl string) string {
	if seat == nil {
		return ""
	}
	r := s.room(seat.RoomID)
	if r == nil {
		return ""
	}
	var people []string
	s.db.Table("room_people AS p").Joins("JOIN users u ON u.id = p.user_id").Where("p.room_id = ?", r.ID).
		Order("p.added_at").Pluck("u.email", &people)
	var bots []string
	for _, m := range s.members(r.ID) {
		if m.ID != seat.BotID {
			bots = append(bots, m.Name)
		}
	}
	others := "none yet"
	if len(bots) > 0 {
		others = strings.Join(bots, ", ")
	}
	who := "nobody"
	if len(people) > 0 {
		who = strings.Join(people, ", ")
	}
	mode := "Every Bot here hears each message a person writes, and each decides whether to answer."
	if r.ReplyMode == ModeMentions {
		mode = "In this room a person's message wakes only the Bots it names; you still read the rest when you are next handed the room."
	}
	return strings.NewReplacer("{{NAME}}", s.botName(seat.BotID), "{{TITLE}}", r.Title, "{{PEOPLE}}", who, "{{BOTS}}", others, "{{MODE}}", mode).
		Replace(strings.TrimSpace(tmpl))
}
