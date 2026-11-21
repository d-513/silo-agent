package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// roomHarness is a harness whose Bots can each have a model of their own, so
// a test scripts each Bot apart ("Test_x@alpha").
func roomHarness(t *testing.T) *apptest.H {
	dir := t.TempDir()
	yaml := strings.Replace(apptest.DefaultYAML(dir), "models:\n  - dummy/echo", "models:\n  - dummy/echo\n  - dummy/alpha\n  - dummy/beta", 1)
	h := apptest.New(t, apptest.WithDataDir(dir), apptest.WithYAML(yaml))
	h.App.Rooms.Floor = 5 * time.Second
	return h
}

// roomBot makes a Bot that answers with the dummy model of its own name.
func roomBot(t *testing.T, h *apptest.H, name, model string) *v1.Bot {
	t.Helper()
	b := h.CreateBot(name)
	if err := h.DB.Model(&db.Bot{}).Where("id = ?", b.GetId()).Update("model", "dummy/"+model).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func makeRoom(t *testing.T, h *apptest.H, title string, bots ...*v1.Bot) *v1.Room {
	t.Helper()
	var ids []string
	for _, b := range bots {
		ids = append(ids, b.GetId())
	}
	res, err := h.Client.CreateRoom(h.Ctx(), connect.NewRequest(&v1.CreateRoomRequest{SpaceId: bots[0].GetSpaceId(), Title: title, BotIds: ids}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func post(t *testing.T, h *apptest.H, roomID, text string) {
	t.Helper()
	if _, err := h.Client.PostRoomMessage(h.Ctx(), connect.NewRequest(&v1.PostRoomMessageRequest{RoomId: roomID, Text: text})); err != nil {
		t.Fatal(err)
	}
}

func roomMessages(h *apptest.H, roomID string) []db.RoomMessage {
	var out []db.RoomMessage
	h.DB.Where("room_id = ?", roomID).Order("seq").Find(&out)
	return out
}

// waitRoom polls until the room's transcript satisfies ok.
func waitRoom(t *testing.T, h *apptest.H, roomID string, ok func([]db.RoomMessage) bool) []db.RoomMessage {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		msgs := roomMessages(h, roomID)
		if ok(msgs) {
			return msgs
		}
		if time.Now().After(deadline) {
			t.Fatalf("room never reached the wanted state:\n%s", transcript(msgs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// settled waits until no Bot in the room has a run going and the transcript
// has not moved for a moment.
func settled(t *testing.T, h *apptest.H, roomID string) []db.RoomMessage {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	last, quiet := -1, 0
	for time.Now().Before(deadline) {
		var live int64
		h.DB.Model(&db.Run{}).Where("status = ? AND chat_id IN (?)", "running", h.DB.Model(&db.RoomBot{}).Select("chat_id").Where("room_id = ?", roomID)).Count(&live)
		msgs := roomMessages(h, roomID)
		if live == 0 && len(msgs) == last {
			if quiet++; quiet >= 15 {
				return msgs
			}
		} else {
			quiet = 0
		}
		last = len(msgs)
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("room never settled:\n%s", transcript(roomMessages(h, roomID)))
	return nil
}

func transcript(msgs []db.RoomMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Kind + " " + m.Author + ": " + m.Body + "\n")
	}
	return b.String()
}

// seatOf is a Bot's seat chat in the room.
func seatOf(t *testing.T, h *apptest.H, roomID, botID string) string {
	t.Helper()
	var seat db.RoomBot
	h.DB.Where("room_id = ? AND bot_id = ?", roomID, botID).Limit(1).Find(&seat)
	if seat.ChatID == "" {
		t.Fatalf("no seat for %s", botID)
	}
	return seat.ChatID
}

// handedTo is everything the room handed a Bot in its seat, in order.
func handedTo(h *apptest.H, chatID string) []string {
	var evs []db.RunEvent
	h.DB.Where("kind = ? AND tool = ? AND run_id IN (?)", "user", "room", h.DB.Model(&db.Run{}).Select("id").Where("chat_id = ?", chatID)).Order("seq").Find(&evs)
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Body)
	}
	return out
}

// TestRoomFloor: with nobody named, the Bots decide one after another, so the
// second sees the first one's answer and stays quiet instead of repeating it.
func TestRoomFloor(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_RF@alpha", dummy.Turn{Text: "It is 4."})
	dummy.Script("Test_RF@beta", dummy.Turn{Text: "<pass/>"})
	h := roomHarness(t)
	alpha, beta := roomBot(t, h, "Alpha", "alpha"), roomBot(t, h, "Beta", "beta")
	room := makeRoom(t, h, "Maths", alpha, beta)

	post(t, h, room.GetId(), "Test_RF what is 2+2?")
	msgs := settled(t, h, room.GetId())
	if len(msgs) != 2 || msgs[1].BotID != alpha.GetId() || msgs[1].Body != "It is 4." {
		t.Fatalf("want the question and Alpha's one answer; got\n%s", transcript(msgs))
	}

	// Beta was handed the room after Alpha answered: it read the answer.
	got := handedTo(h, seatOf(t, h, room.GetId(), beta.GetId()))
	if len(got) != 1 || !strings.Contains(got[0], "Alpha (a Bot)") || !strings.Contains(got[0], "It is 4.") {
		t.Fatalf("Beta was not handed Alpha's answer: %q", got)
	}
	// What a Bot says in a room is the room's: its own row's last line is
	// not its pass (or its answer) there.
	for _, id := range []string{alpha.GetId(), beta.GetId()} {
		b, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
		if err != nil {
			t.Fatal(err)
		}
		if b.Msg.GetLastMessage() != "" {
			t.Fatalf("%s's last message is its room's: %q", b.Msg.GetName(), b.Msg.GetLastMessage())
		}
	}
	// Its pass is in its own seat, not in the room.
	var passes int64
	h.DB.Model(&db.RunEvent{}).Where("kind = ? AND body = ? AND run_id IN (?)", "assistant", "<pass/>",
		h.DB.Model(&db.Run{}).Select("id").Where("chat_id = ?", seatOf(t, h, room.GetId(), beta.GetId()))).Count(&passes)
	if passes != 1 {
		t.Fatalf("Beta's pass is not in its seat (%d)", passes)
	}
	// Alpha's seat holds only what others said: never its own words again.
	for _, s := range handedTo(h, seatOf(t, h, room.GetId(), alpha.GetId())) {
		if strings.Contains(s, "It is 4.") {
			t.Fatalf("Alpha was handed its own answer: %q", s)
		}
	}

	// Next time, the Bot that spoke last decides first.
	dummy.Script("Test_RF@beta", dummy.Turn{Text: "<pass/>"}, dummy.Turn{Text: "And 3+3 is 6."})
	dummy.Script("Test_RF@alpha", dummy.Turn{Text: "It is 4."}, dummy.Turn{Text: "<pass/>"})
	post(t, h, room.GetId(), "and 3+3?")
	msgs = settled(t, h, room.GetId())
	if len(msgs) != 4 || msgs[3].BotID != beta.GetId() {
		t.Fatalf("want Beta to answer after Alpha passed; got\n%s", transcript(msgs))
	}
	if got := handedTo(h, seatOf(t, h, room.GetId(), beta.GetId())); len(got) != 2 || !strings.Contains(got[1], "and 3+3?") {
		t.Fatalf("Beta's second hand-over: %q", got)
	}
	// Each Bot's seat only grows: its second run is sent its first, byte for
	// byte, then what came since (a provider's cache keeps hitting).
	for _, model := range []string{"alpha", "beta"} {
		var reqs []llm.Request
		for _, r := range dummy.Streamed() {
			if r.Model == model {
				reqs = append(reqs, r)
			}
		}
		if len(reqs) != 2 {
			t.Fatalf("%s: %d model requests, want 2", model, len(reqs))
		}
		assertAppendOnly(t, reqs)
		if !strings.HasPrefix(lastUser(reqs[1]), "[") || !strings.Contains(lastUser(reqs[1]), "[New in the room \u201cMaths\u201d]") {
			t.Fatalf("%s: the hand-over does not open with the room's line: %q", model, lastUser(reqs[1]))
		}
	}
}

func lastUser(req llm.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == llm.RoleUser {
			return req.Messages[i].Text
		}
	}
	return ""
}

// TestRoomMentions: a named Bot answers; a Bot's mention wakes the Bot it
// names, which is handed the room once however many paths lead to it.
func TestRoomMentions(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_RM@alpha", dummy.Turn{Text: "On it. @Beta can you check the numbers?"})
	dummy.Script("Test_RM@beta", dummy.Turn{Text: "Checked: they add up."})
	h := roomHarness(t)
	alpha, beta := roomBot(t, h, "Alpha", "alpha"), roomBot(t, h, "Beta", "beta")
	room := makeRoom(t, h, "Books", alpha, beta)

	post(t, h, room.GetId(), "Test_RM @alpha please do the books")
	msgs := settled(t, h, room.GetId())
	if len(msgs) != 3 || msgs[1].BotID != alpha.GetId() || msgs[2].BotID != beta.GetId() {
		t.Fatalf("want the request, Alpha, then Beta; got\n%s", transcript(msgs))
	}
	if msgs[0].Mentions != alpha.GetId() || msgs[1].Mentions != beta.GetId() {
		t.Fatalf("mentions: %q, %q", msgs[0].Mentions, msgs[1].Mentions)
	}
	got := handedTo(h, seatOf(t, h, room.GetId(), beta.GetId()))
	if len(got) != 1 {
		t.Fatalf("Beta was handed the room %d times: %q", len(got), got)
	}
	// It was told it is named.
	var ev db.RunEvent
	h.DB.Where("kind = ? AND tool = ? AND run_id IN (?)", "user", "room", h.DB.Model(&db.Run{}).Select("id").Where("chat_id = ?", seatOf(t, h, room.GetId(), beta.GetId()))).Limit(1).Find(&ev)
	if !strings.Contains(ev.Meta, `"mentioned":true`) {
		t.Fatalf("Beta's hand-over does not say it is named: %s", ev.Meta)
	}
	// The run is a room run, triggered by the room.
	var r db.Run
	h.DB.Where("chat_id = ?", seatOf(t, h, room.GetId(), beta.GetId())).Limit(1).Find(&r)
	if r.Origin != "room" || r.Trigger != "room" || r.UserID != "" {
		t.Fatalf("seat run: origin %q trigger %q user %q", r.Origin, r.Trigger, r.UserID)
	}
}

// TestRoomBotsStopAmongThemselves: two Bots naming each other stop after a
// few messages and wait for a person, who says so.
func TestRoomBotsStopAmongThemselves(t *testing.T) {
	dummy.Reset()
	var ping, pong []dummy.Turn
	for range 20 {
		ping = append(ping, dummy.Turn{Text: "@Beta ping"})
		pong = append(pong, dummy.Turn{Text: "@Alpha pong"})
	}
	dummy.Script("Test_RS@alpha", ping...)
	dummy.Script("Test_RS@beta", pong...)
	h := roomHarness(t)
	alpha, beta := roomBot(t, h, "Alpha", "alpha"), roomBot(t, h, "Beta", "beta")
	room := makeRoom(t, h, "Loop", alpha, beta)

	post(t, h, room.GetId(), "Test_RS @Alpha start")
	msgs := waitRoom(t, h, room.GetId(), func(ms []db.RoomMessage) bool {
		for _, m := range ms {
			if m.Kind == "notice" {
				return true
			}
		}
		return false
	})
	msgs = settled(t, h, room.GetId())
	bots := 0
	for _, m := range msgs {
		if m.BotID != "" {
			bots++
		}
	}
	if bots < 9 || bots > 11 {
		t.Fatalf("%d Bot messages before they stopped:\n%s", bots, transcript(msgs))
	}
	// A person speaking lets them go on.
	post(t, h, room.GetId(), "@Alpha once more")
	waitRoom(t, h, room.GetId(), func(ms []db.RoomMessage) bool { return len(ms) > len(msgs)+1 })
	h.App.Rooms.Floor = time.Second
	if _, err := h.Client.StopRoom(h.Ctx(), connect.NewRequest(&v1.StopRoomRequest{RoomId: room.GetId()})); err != nil {
		t.Fatal(err)
	}
}

// TestRoomMentionsMode: in a room set to mentions, only named Bots answer.
func TestRoomMentionsMode(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_RO@alpha", dummy.Turn{Text: "alpha speaking"})
	dummy.Script("Test_RO@beta", dummy.Turn{Text: "beta speaking"})
	h := roomHarness(t)
	alpha, beta := roomBot(t, h, "Alpha", "alpha"), roomBot(t, h, "Beta", "beta")
	room := makeRoom(t, h, "Quiet", alpha, beta)
	mode := "mentions"
	if _, err := h.Client.UpdateRoom(h.Ctx(), connect.NewRequest(&v1.UpdateRoomRequest{RoomId: room.GetId(), ReplyMode: &mode})); err != nil {
		t.Fatal(err)
	}
	post(t, h, room.GetId(), "Test_RO anyone?")
	msgs := settled(t, h, room.GetId())
	if len(msgs) != 2 || msgs[1].Kind != "notice" {
		t.Fatalf("nobody named: want only a notice\n%s", transcript(msgs))
	}
	post(t, h, room.GetId(), "@Beta you then")
	msgs = settled(t, h, room.GetId())
	if last := msgs[len(msgs)-1]; last.BotID != beta.GetId() {
		t.Fatalf("want Beta alone to answer\n%s", transcript(msgs))
	}
	if n := len(handedTo(h, seatOf(t, h, room.GetId(), alpha.GetId()))); n != 0 {
		t.Fatalf("Alpha was handed the room %d times", n)
	}
}

// TestRoomAccess: everyone in a room is on every Bot in it, only a Bot's
// managers put it in a room, and a stranger cannot see the room at all.
func TestRoomAccess(t *testing.T) {
	dummy.Reset()
	h := roomHarness(t)
	alpha, beta := roomBot(t, h, "Alpha", "alpha"), roomBot(t, h, "Beta", "beta")
	sp := alpha.GetSpaceId()

	// A member of the Workspace who uses Alpha only.
	inv, err := h.Client.CreateSpaceInvite(h.Ctx(), connect.NewRequest(&v1.CreateSpaceInviteRequest{SpaceId: sp, Role: access.RoleMember}))
	if err != nil {
		t.Fatal(err)
	}
	bob, _, bobUser := h.Person("bob@test.local")
	url := inv.Msg.GetUrl()
	if _, err := bob.JoinSpace(h.Ctx(), connect.NewRequest(&v1.JoinSpaceRequest{Token: url[strings.LastIndex(url, "/")+1:]})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: alpha.GetId(), UserId: bobUser.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}

	// Bob is not on Beta: a room with both cannot have him.
	_, err = h.Client.CreateRoom(h.Ctx(), connect.NewRequest(&v1.CreateRoomRequest{SpaceId: sp, BotIds: []string{alpha.GetId(), beta.GetId()}, UserIds: []string{bobUser.ID}}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a room with someone not on its Bot: %v", err)
	}
	room := makeRoom(t, h, "Shared", alpha)
	// A stranger to the room does not see it.
	if _, err := bob.GetRoom(h.Ctx(), connect.NewRequest(&v1.GetRoomRequest{RoomId: room.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("a stranger saw the room: %v", err)
	}
	if _, err := h.Client.AddRoomPerson(h.Ctx(), connect.NewRequest(&v1.RoomPersonRequest{RoomId: room.GetId(), UserId: bobUser.ID})); err != nil {
		t.Fatal(err)
	}
	// Bob uses Alpha but manages nothing: he cannot bring a Bot in.
	if _, err := bob.AddRoomBot(h.Ctx(), connect.NewRequest(&v1.RoomBotRequest{RoomId: room.GetId(), BotId: beta.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("someone not on Beta brought it in: %v", err)
	}
	// Nor can its manager while Bob, who is not on it, is in the room.
	if _, err := h.Client.AddRoomBot(h.Ctx(), connect.NewRequest(&v1.RoomBotRequest{RoomId: room.GetId(), BotId: beta.GetId()})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("Beta joined a room with someone not on it: %v", err)
	}
	// Taken off Alpha, Bob is taken out of the room too, and the room says so.
	if _, err := h.Client.RemoveBotPerson(h.Ctx(), connect.NewRequest(&v1.RemoveBotPersonRequest{BotId: alpha.GetId(), UserId: bobUser.ID})); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.PostRoomMessage(h.Ctx(), connect.NewRequest(&v1.PostRoomMessageRequest{RoomId: room.GetId(), Text: "hi"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("someone taken off the room's Bot still posted: %v", err)
	}
	if msgs := roomMessages(h, room.GetId()); !strings.Contains(msgs[len(msgs)-1].Body, "no longer on Alpha") {
		t.Fatalf("the room does not say Bob left:\n%s", transcript(msgs))
	}
	// Even a person still listed while off a Bot is refused (the access
	// check does not lean on the clean-up).
	if err := h.DB.Create(&db.RoomPerson{RoomID: room.GetId(), UserID: bobUser.ID, AddedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := bob.PostRoomMessage(h.Ctx(), connect.NewRequest(&v1.PostRoomMessageRequest{RoomId: room.GetId(), Text: "hi"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("someone off the room's Bot posted: %v", err)
	}
	if _, err := bob.RemoveRoomPerson(h.Ctx(), connect.NewRequest(&v1.RoomPersonRequest{RoomId: room.GetId()})); err != nil {
		t.Fatalf("leaving: %v", err)
	}
	// The seat is no chat a person writes in or lists.
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: alpha.GetId(), ChatId: seatOf(t, h, room.GetId(), alpha.GetId()), Text: "hello"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("wrote in a seat: %v", err)
	}
	list, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: alpha.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Msg.GetChats() {
		if c.GetRoomId() != "" {
			t.Fatalf("a seat is in the Bot's chat list: %+v", c)
		}
	}
	// Deleting a Bot takes it out of its rooms; a room's last person leaving
	// deletes it.
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: alpha.GetId()})); err != nil {
		t.Fatal(err)
	}
	if n := h.DB.Where("room_id = ?", room.GetId()).Find(&[]db.RoomBot{}).RowsAffected; n != 0 {
		t.Fatalf("a deleted Bot kept its seat")
	}
	if _, err := h.Client.RemoveRoomPerson(h.Ctx(), connect.NewRequest(&v1.RoomPersonRequest{RoomId: room.GetId()})); err != nil {
		t.Fatal(err)
	}
	if n := h.DB.Where("id = ?", room.GetId()).Find(&[]db.Room{}).RowsAffected; n != 0 {
		t.Fatalf("an empty room was kept")
	}
}
