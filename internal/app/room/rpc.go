package room

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
)

func invalid(msg string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(msg))
}

func precondition(msg string) error {
	return connect.NewError(connect.CodeFailedPrecondition, errors.New(msg))
}

// canDelete: who made the room, or an admin of its Workspace.
func (s *Service) canDelete(r *db.Room, userID string) bool {
	if r.CreatedBy == userID {
		return true
	}
	role := access.SpaceRole(s.db, r.SpaceID, userID)
	return role == access.RoleAdmin || role == access.RoleOwner
}

// botFor is a Bot the caller may put in a room of the Workspace: they manage
// it (a room widens what it hears), it is in that Workspace and it may run.
func (s *Service) botFor(ctx context.Context, spaceID, botID string) (*db.Bot, error) {
	b, err := access.Bot(ctx, s.db, botID, access.Manage)
	if err != nil {
		return nil, err
	}
	if b.SpaceID != spaceID {
		return nil, invalid(b.Name + " is in another workspace")
	}
	if access.Suspended(s.db, b.ID) {
		return nil, precondition(b.Name + " is suspended: it has no enabled manager")
	}
	return b, nil
}

// notOn says who of people is not on the Bot, "" when everyone is: everyone
// in a room is on every Bot in it.
func (s *Service) notOn(botID string, people []string) string {
	for _, p := range people {
		if access.BotRole(s.db, botID, p) == "" {
			return access.Email(s.db, p)
		}
	}
	return ""
}

func (s *Service) people(roomID string) []string {
	var ids []string
	s.db.Model(&db.RoomPerson{}).Where("room_id = ?", roomID).Order("added_at").Pluck("user_id", &ids)
	return ids
}

// seatChat is a Bot's chat in a room: closed, the Bot's own (no person
// writes in it).
func seatChat(botID, roomID, title string) db.Chat {
	now := time.Now()
	return db.Chat{ID: ids.New(), BotID: botID, RoomID: roomID, Title: seatTitle(title), Hidden: true, CreatedAt: now, UpdatedAt: now}
}

func seatTitle(roomTitle string) string {
	return textx.ClipRunes("Room: "+roomTitle, 120)
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// --- RPCs ---

func (s *Service) ListRooms(ctx context.Context, req *connect.Request[v1.ListRoomsRequest]) (*connect.Response[v1.ListRoomsResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.View)
	if err != nil {
		return nil, err
	}
	u := access.User(ctx)
	var rows []db.Room
	s.db.Where("space_id = ? AND id IN (?)", sp.ID, s.db.Model(&db.RoomPerson{}).Select("room_id").Where("user_id = ?", u.ID)).
		Order("updated_at desc").Find(&rows)
	out := &v1.ListRoomsResponse{}
	for i := range rows {
		out.Rooms = append(out.Rooms, s.proto(ctx, &rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateRoom(ctx context.Context, req *connect.Request[v1.CreateRoomRequest]) (*connect.Response[v1.Room], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.View)
	if err != nil {
		return nil, err
	}
	u := access.User(ctx)
	botIDs := dedupe(req.Msg.GetBotIds())
	switch {
	case len(botIDs) == 0:
		return nil, invalid("a room needs at least one Bot")
	case len(botIDs) > botsMax:
		return nil, invalid(fmt.Sprintf("at most %d Bots in a room", botsMax))
	}
	people := dedupe(append([]string{u.ID}, req.Msg.GetUserIds()...))
	if len(people) > peopleMax {
		return nil, invalid(fmt.Sprintf("at most %d people in a room", peopleMax))
	}
	for _, p := range people {
		if access.SpaceRole(s.db, sp.ID, p) == "" {
			return nil, invalid("everyone in a room must be in its workspace")
		}
	}
	var bots []*db.Bot
	var names []string
	for _, id := range botIDs {
		b, err := s.botFor(ctx, sp.ID, id)
		if err != nil {
			return nil, err
		}
		if who := s.notOn(b.ID, people); who != "" {
			return nil, precondition(fmt.Sprintf("%s is not on %s: add them to it first, or leave one of them out", who, b.Name))
		}
		bots = append(bots, b)
		names = append(names, b.Name)
	}
	title := textx.ClipRunes(strings.ReplaceAll(req.Msg.GetTitle(), "\n", " "), titleMax)
	if title == "" {
		title = textx.ClipRunes(strings.Join(names, ", "), titleMax)
	}
	now := time.Now()
	r := db.Room{ID: ids.New(), SpaceID: sp.ID, Title: title, CreatedBy: u.ID, ReplyMode: ModeDecide, CreatedAt: now, UpdatedAt: now}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		for _, p := range people {
			if err := tx.Create(&db.RoomPerson{RoomID: r.ID, UserID: p, AddedAt: now}).Error; err != nil {
				return err
			}
		}
		for _, b := range bots {
			c := seatChat(b.ID, r.ID, title)
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			if err := tx.Create(&db.RoomBot{RoomID: r.ID, BotID: b.ID, ChatID: c.ID, AddedBy: u.ID, AddedAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.proto(ctx, &r)), nil
}

func (s *Service) GetRoom(ctx context.Context, req *connect.Request[v1.GetRoomRequest]) (*connect.Response[v1.Room], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.proto(ctx, r)), nil
}

func (s *Service) UpdateRoom(ctx context.Context, req *connect.Request[v1.UpdateRoomRequest]) (*connect.Response[v1.Room], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	cols := map[string]any{}
	if req.Msg.Title != nil {
		t := textx.ClipRunes(strings.ReplaceAll(req.Msg.GetTitle(), "\n", " "), titleMax)
		if t == "" {
			return nil, invalid("a room needs a name")
		}
		cols["title"], r.Title = t, t
	}
	if req.Msg.ReplyMode != nil {
		m := req.Msg.GetReplyMode()
		if m != ModeDecide && m != ModeMentions {
			return nil, invalid("reply_mode is decide or mentions")
		}
		cols["reply_mode"], r.ReplyMode = m, m
	}
	if len(cols) > 0 {
		if err := s.db.Model(&db.Room{}).Where("id = ?", r.ID).Updates(cols).Error; err != nil {
			return nil, err
		}
		if _, ok := cols["title"]; ok {
			s.db.Model(&db.Chat{}).Where("room_id = ?", r.ID).UpdateColumn("title", seatTitle(r.Title))
		}
		s.publish(r.ID, change{room: true})
	}
	return connect.NewResponse(s.proto(ctx, r)), nil
}

func (s *Service) DeleteRoom(ctx context.Context, req *connect.Request[v1.DeleteRoomRequest]) (*connect.Response[v1.DeleteRoomResponse], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	if !s.canDelete(r, access.User(ctx).ID) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only whoever made the room or a workspace admin can delete it"))
	}
	s.drop(r.ID)
	return connect.NewResponse(&v1.DeleteRoomResponse{}), nil
}

func (s *Service) AddRoomBot(ctx context.Context, req *connect.Request[v1.RoomBotRequest]) (*connect.Response[v1.Room], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	b, err := s.botFor(ctx, r.SpaceID, req.Msg.GetBotId())
	if err != nil {
		return nil, err
	}
	seats := s.bots(r.ID)
	for _, seat := range seats {
		if seat.BotID == b.ID {
			return connect.NewResponse(s.proto(ctx, r)), nil
		}
	}
	if len(seats) >= botsMax {
		return nil, precondition(fmt.Sprintf("at most %d Bots in a room", botsMax))
	}
	if who := s.notOn(b.ID, s.people(r.ID)); who != "" {
		return nil, precondition(fmt.Sprintf("%s is in this room and not on %s: everyone in a room is on every Bot in it", who, b.Name))
	}
	u := access.User(ctx)
	c := seatChat(b.ID, r.ID, r.Title)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		// It hears what is said from now on; what came before is the room's.
		return tx.Create(&db.RoomBot{RoomID: r.ID, BotID: b.ID, ChatID: c.ID, SeenSeq: s.topSeq(r.ID), AddedBy: u.ID, AddedAt: time.Now()}).Error
	})
	if err != nil {
		return nil, err
	}
	s.notice(r.ID, fmt.Sprintf("%s joined, added by %s.", b.Name, u.Email))
	s.touch(r.ID)
	s.publish(r.ID, change{room: true})
	return connect.NewResponse(s.proto(ctx, r)), nil
}

func (s *Service) RemoveRoomBot(ctx context.Context, req *connect.Request[v1.RoomBotRequest]) (*connect.Response[v1.Room], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	var seat db.RoomBot
	if s.db.Where("room_id = ? AND bot_id = ?", r.ID, req.Msg.GetBotId()).Limit(1).Find(&seat); seat.ChatID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("that Bot is not in this room"))
	}
	name := s.botName(seat.BotID)
	s.dropSeat(seat)
	s.notice(r.ID, fmt.Sprintf("%s left, taken out by %s.", name, access.User(ctx).Email))
	s.publish(r.ID, change{room: true})
	return connect.NewResponse(s.proto(ctx, r)), nil
}

func (s *Service) AddRoomPerson(ctx context.Context, req *connect.Request[v1.RoomPersonRequest]) (*connect.Response[v1.Room], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	who := req.Msg.GetUserId()
	if who == "" || access.SpaceRole(s.db, r.SpaceID, who) == "" {
		return nil, invalid("they are not in this room's workspace")
	}
	people := s.people(r.ID)
	if slices.Contains(people, who) {
		return connect.NewResponse(s.proto(ctx, r)), nil
	}
	if len(people) >= peopleMax {
		return nil, precondition(fmt.Sprintf("at most %d people in a room", peopleMax))
	}
	email := access.Email(s.db, who)
	for _, seat := range s.bots(r.ID) {
		if access.BotRole(s.db, seat.BotID, who) == "" {
			return nil, precondition(fmt.Sprintf("%s is not on %s: everyone in a room is on every Bot in it", email, s.botName(seat.BotID)))
		}
	}
	// They read what was said before; it does not count as unread.
	if err := s.db.Create(&db.RoomPerson{RoomID: r.ID, UserID: who, ReadSeq: s.topSeq(r.ID), AddedAt: time.Now()}).Error; err != nil {
		return nil, err
	}
	s.notice(r.ID, fmt.Sprintf("%s joined, added by %s.", email, access.User(ctx).Email))
	s.publish(r.ID, change{room: true})
	return connect.NewResponse(s.proto(ctx, r)), nil
}

func (s *Service) RemoveRoomPerson(ctx context.Context, req *connect.Request[v1.RoomPersonRequest]) (*connect.Response[v1.RemoveRoomPersonResponse], error) {
	u := access.User(ctx)
	var r *db.Room
	who := req.Msg.GetUserId()
	if who == "" {
		who = u.ID
	}
	if who == u.ID {
		// Leaving needs only being in it, even when a Bot there is no
		// longer theirs: that is how they get out.
		var row db.Room
		if s.db.Where("id = ?", req.Msg.GetRoomId()).Limit(1).Find(&row); row.ID == "" || !access.InRoom(s.db, row.ID, u.ID) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New(access.NoRoom))
		}
		r = &row
	} else {
		var err error
		if r, err = access.Room(ctx, s.db, req.Msg.GetRoomId()); err != nil {
			return nil, err
		}
		if !s.canDelete(r, u.ID) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only whoever made the room or a workspace admin can take someone out"))
		}
	}
	if s.db.Where("room_id = ? AND user_id = ?", r.ID, who).Delete(&db.RoomPerson{}).RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("they are not in this room"))
	}
	if len(s.people(r.ID)) == 0 {
		s.drop(r.ID)
		return connect.NewResponse(&v1.RemoveRoomPersonResponse{}), nil
	}
	if who == u.ID {
		s.notice(r.ID, u.Email+" left.")
	} else {
		s.notice(r.ID, fmt.Sprintf("%s was taken out by %s.", access.Email(s.db, who), u.Email))
	}
	s.publish(r.ID, change{room: true})
	out := &v1.RemoveRoomPersonResponse{}
	if who != u.ID {
		out.Room = s.proto(ctx, r)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) PostRoomMessage(ctx context.Context, req *connect.Request[v1.PostRoomMessageRequest]) (*connect.Response[v1.RoomMessage], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(textx.ValidUTF8(req.Msg.GetText()))
	if text == "" {
		return nil, invalid("text required")
	}
	text = textx.CapRunes(text, textMax)
	u := access.User(ctx)
	m := db.RoomMessage{
		ID: ids.New(), RoomID: r.ID, Kind: "message", UserID: u.ID, Author: u.Email, Body: text,
		Mentions: strings.Join(mentionsIn(text, s.members(r.ID)), ","), CreatedAt: time.Now(),
	}
	if err := s.db.Create(&m).Error; err != nil {
		return nil, err
	}
	s.stamp(&m)
	s.read(r.ID, u.ID, m.Seq)
	s.touch(r.ID)
	s.publish(r.ID, change{msg: &m})
	go s.heard(&m)
	return connect.NewResponse(messageProto(&m, u.ID)), nil
}

func (s *Service) StopRoom(ctx context.Context, req *connect.Request[v1.StopRoomRequest]) (*connect.Response[v1.StopRoomResponse], error) {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return nil, err
	}
	s.stop(r.ID)
	return connect.NewResponse(&v1.StopRoomResponse{}), nil
}

// StreamRoom opens with the room and its newest messages (or those after
// after_seq), then sends each message as it is said, each Bot's state as it
// changes, and the room again when people or Bots come or go. It ends when
// the caller may no longer be in the room.
func (s *Service) StreamRoom(ctx context.Context, req *connect.Request[v1.StreamRoomRequest], stream *connect.ServerStream[v1.RoomEvent]) error {
	r, err := access.Room(ctx, s.db, req.Msg.GetRoomId())
	if err != nil {
		return err
	}
	u := access.User(ctx)
	ch, stop := s.subscribe(r.ID)
	defer stop()
	room := s.proto(ctx, r)
	shown := map[string]string{}
	for _, b := range room.GetBots() {
		shown[b.GetBotId()] = stateKey(b)
	}
	if err := stream.Send(&v1.RoomEvent{Room: room}); err != nil {
		return err
	}
	var msgs []db.RoomMessage
	if after := req.Msg.GetAfterSeq(); after > 0 {
		s.db.Where("room_id = ? AND seq > ?", r.ID, after).Order("seq").Find(&msgs)
	} else {
		s.db.Where("room_id = ?", r.ID).Order("seq desc").Limit(replayMax + 1).Find(&msgs)
		if len(msgs) > replayMax {
			msgs = msgs[:replayMax]
			if err := stream.Send(&v1.RoomEvent{Earlier: true}); err != nil {
				return err
			}
		}
		slices.Reverse(msgs)
	}
	var top int64
	for i := range msgs {
		if err := stream.Send(&v1.RoomEvent{Message: messageProto(&msgs[i], u.ID)}); err != nil {
			return err
		}
		top = max(top, msgs[i].Seq)
	}
	s.read(r.ID, u.ID, top)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for n := 0; ; n++ {
		select {
		case <-ctx.Done():
			return nil
		case c, ok := <-ch:
			if !ok {
				return connect.NewError(connect.CodeAborted, errors.New("stream overflow"))
			}
			if c.room {
				if r, err = access.Room(ctx, s.db, r.ID); err != nil {
					return err
				}
				room := s.proto(ctx, r)
				for _, b := range room.GetBots() {
					shown[b.GetBotId()] = stateKey(b)
				}
				if err := stream.Send(&v1.RoomEvent{Room: room}); err != nil {
					return err
				}
				continue
			}
			if err := stream.Send(&v1.RoomEvent{Message: messageProto(c.msg, u.ID)}); err != nil {
				return err
			}
			s.read(r.ID, u.ID, c.msg.Seq)
		case <-tick.C:
			// Who is at work, and on what, is read each second rather than
			// told: a run's tool changes in the middle of its loop.
			if n%5 == 0 {
				if _, err := access.Room(ctx, s.db, r.ID); err != nil {
					return err
				}
			}
			for _, b := range s.botStates(r, u.ID) {
				if k := stateKey(b); shown[b.GetBotId()] != k {
					shown[b.GetBotId()] = k
					if err := stream.Send(&v1.RoomEvent{Bot: b}); err != nil {
						return err
					}
				}
			}
		}
	}
}

func stateKey(b *v1.RoomBot) string {
	return fmt.Sprintf("%t|%s|%t|%s", b.GetWorking(), b.GetStep(), b.GetNeedsYou(), b.GetName())
}

// read moves what the person has been shown of the room.
func (s *Service) read(roomID, userID string, seq int64) {
	if seq > 0 {
		s.db.Model(&db.RoomPerson{}).Where("room_id = ? AND user_id = ? AND read_seq < ?", roomID, userID, seq).UpdateColumn("read_seq", seq)
	}
}

// stamp reads back the place the database gave a message just written.
func (s *Service) stamp(m *db.RoomMessage) {
	if m.Seq == 0 {
		s.db.Model(&db.RoomMessage{}).Select("seq").Where("id = ?", m.ID).Scan(&m.Seq)
	}
}

func (s *Service) topSeq(roomID string) int64 {
	var seq int64
	s.db.Model(&db.RoomMessage{}).Select("COALESCE(MAX(seq), 0)").Where("room_id = ?", roomID).Scan(&seq)
	return seq
}

// --- the wire ---

// proto is the room as the caller sees it.
func (s *Service) proto(ctx context.Context, r *db.Room) *v1.Room {
	u := access.User(ctx)
	out := &v1.Room{
		Id: r.ID, SpaceId: r.SpaceID, Title: r.Title, ReplyMode: r.ReplyMode,
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339), CanDelete: s.canDelete(r, u.ID),
		Bots: s.botStates(r, u.ID),
	}
	var people []struct {
		UserID string
		Email  string
	}
	s.db.Table("room_people AS p").Select("p.user_id, u.email").Joins("JOIN users u ON u.id = p.user_id").
		Where("p.room_id = ?", r.ID).Order("p.added_at").Scan(&people)
	var read int64
	for _, p := range people {
		out.People = append(out.People, &v1.RoomPerson{UserId: p.UserID, Email: p.Email, Me: p.UserID == u.ID})
	}
	s.db.Model(&db.RoomPerson{}).Select("read_seq").Where("room_id = ? AND user_id = ?", r.ID, u.ID).Scan(&read)
	s.db.Model(&db.RoomMessage{}).Where("room_id = ? AND kind = ? AND seq > ? AND user_id <> ?", r.ID, "message", read, u.ID).Count(&out.Unread)
	var last db.RoomMessage
	if s.db.Where("room_id = ? AND kind = ?", r.ID, "message").Order("seq desc").Limit(1).Find(&last); last.ID != "" {
		out.LastMessage, out.LastAuthor, out.LastAt = oneLine(last.Body, 140), last.Author, last.CreatedAt.Format(time.RFC3339)
	}
	return out
}

// botStates are the room's Bots as the person sees them: what each is doing,
// and its seat only for one who manages it.
func (s *Service) botStates(r *db.Room, userID string) []*v1.RoomBot {
	var rows []struct {
		BotID  string
		ChatID string
		Name   string
		Crest  int32
	}
	s.db.Table("room_bots AS rb").Select("rb.bot_id, rb.chat_id, b.name, b.crest").Joins("JOIN bots b ON b.id = rb.bot_id").
		Where("rb.room_id = ?", r.ID).Order("rb.added_at").Scan(&rows)
	out := make([]*v1.RoomBot, 0, len(rows))
	for _, row := range rows {
		b := &v1.RoomBot{BotId: row.BotID, Name: row.Name, Crest: row.Crest}
		b.Working, b.Step = s.host.RunStep(row.BotID, row.ChatID)
		if b.Working {
			b.NeedsYou = s.host.NeedsYou(row.BotID, row.ChatID, userID)
		}
		if access.BotRole(s.db, row.BotID, userID) == access.RoleManage {
			b.ChatId = row.ChatID
		}
		out = append(out, b)
	}
	return out
}

func messageProto(m *db.RoomMessage, viewer string) *v1.RoomMessage {
	return &v1.RoomMessage{
		Id: m.ID, RoomId: m.RoomID, Seq: m.Seq, Kind: m.Kind, UserId: m.UserID, BotId: m.BotID, Author: m.Author,
		Body: m.Body, Mentions: splitIDs(m.Mentions), CreatedAt: m.CreatedAt.Format(time.RFC3339), Mine: m.UserID != "" && m.UserID == viewer,
	}
}
