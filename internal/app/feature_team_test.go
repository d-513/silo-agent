package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// team is a Bot the harness user manages and bob uses.
func team(t *testing.T, h *apptest.H) (*v1.Bot, silov1connect.UIClient, *db.User) {
	t.Helper()
	sp := homeSpace(t, h)
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	bot := h.CreateBot("Scout")
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	return bot, bob, bu
}

func chatsOf(t *testing.T, c silov1connect.UIClient, botID string) []*v1.Chat {
	t.Helper()
	res, err := c.ListChats(context.Background(), connect.NewRequest(&v1.ListChatsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetChats()
}

func send(t *testing.T, h *apptest.H, c silov1connect.UIClient, botID, chatID, text string) *v1.SendResponse {
	t.Helper()
	res, err := c.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: botID, ChatId: chatID, Text: text}))
	if err != nil {
		t.Fatalf("send %q: %v", text, err)
	}
	h.WaitRun(res.Msg.GetRunId())
	return res.Msg
}

func TestEveryPersonHasAMainChat(t *testing.T) {
	h := apptest.New(t)
	bot, bob, _ := team(t, h)
	mine, his := chatsOf(t, h.Client, bot.Id), chatsOf(t, bob, bot.Id)
	if len(his) != 1 || !his[0].GetMain() || !his[0].GetMine() {
		t.Fatalf("bob's chats = %+v; want his own main chat", his)
	}
	var ownMain string
	for _, c := range mine {
		if c.GetMain() && c.GetMine() {
			ownMain = c.GetId()
		}
	}
	if ownMain == "" || ownMain == his[0].GetId() {
		t.Fatalf("owner's main = %q, bob's = %q: want one each", ownMain, his[0].GetId())
	}
	// A Send with no chat goes to the sender's own main chat.
	res, err := bob.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.Id, Text: "hello"}))
	if err != nil || res.Msg.GetChatId() != his[0].GetId() {
		t.Fatalf("bob's send went to %q (%v), want his main chat", res.Msg.GetChatId(), err)
	}
	h.WaitRun(res.Msg.GetRunId())
}

func TestOthersChatsAreReadOnly(t *testing.T) {
	h := apptest.New(t)
	bot, bob, _ := team(t, h)
	his := chatsOf(t, bob, bot.Id)[0].GetId()
	send(t, h, bob, bot.Id, his, "a note of bob's")

	// The manager reads it, in the Team, and cannot write in it.
	var seen *v1.Chat
	for _, c := range chatsOf(t, h.Client, bot.Id) {
		if c.GetId() == his {
			seen = c
		}
	}
	if seen == nil || seen.GetMine() || seen.GetAuthor() != "bob@test.local" {
		t.Fatalf("the manager's view of bob's chat = %+v", seen)
	}
	if _, err := h.Client.RenameChat(h.Ctx(), connect.NewRequest(&v1.RenameChatRequest{BotId: bot.Id, Id: his, Title: "x"})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("the manager renamed bob's chat: %v", err)
	}
	if _, err := h.Client.CollectMemories(h.Ctx(), connect.NewRequest(&v1.CollectMemoriesRequest{BotId: bot.Id, ChatId: his})); code(err) == connect.CodeNotFound {
		t.Fatalf("the manager could not read bob's chat: %v", err)
	}
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.Id, ChatId: his, Text: "me too"})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("the manager wrote in bob's chat: %v", err)
	}
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: bot.Id, ChatId: his})); err != nil {
		t.Fatalf("the manager could not stop bob's chat: %v", err)
	}

	// Bob neither lists nor reads the owner's.
	var ownerMain string
	for _, c := range chatsOf(t, h.Client, bot.Id) {
		if c.GetMain() && c.GetMine() {
			ownerMain = c.GetId()
		}
	}
	for _, c := range chatsOf(t, bob, bot.Id) {
		if c.GetId() == ownerMain {
			t.Fatal("bob lists the owner's chat")
		}
	}
	if _, err := bob.ListChatEvents(h.Ctx(), connect.NewRequest(&v1.ListChatEventsRequest{BotId: bot.Id, ChatId: ownerMain})); code(err) != connect.CodeNotFound || !strings.Contains(err.Error(), access.NoChat) {
		t.Fatalf("bob read the owner's chat: %v", err)
	}
	if _, err := bob.DeleteChat(h.Ctx(), connect.NewRequest(&v1.DeleteChatRequest{BotId: bot.Id, Id: ownerMain})); code(err) != connect.CodeNotFound {
		t.Fatalf("bob deleted the owner's chat: %v", err)
	}
}

func TestAuthorsAreFrozen(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bot := h.CreateBot("Scout")
	main := chatsOf(t, h.Client, bot.Id)[0].GetId()
	solo := send(t, h, h.Client, bot.Id, main, "alone")
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: bot.Id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	shared := send(t, h, h.Client, bot.Id, main, "together")
	author := func(runID string) string {
		var ev db.RunEvent
		h.DB.Where("run_id = ? AND kind = ?", runID, "user").Limit(1).Find(&ev)
		return ev.Author
	}
	if a := author(solo.GetRunId()); a != "" {
		t.Fatalf("a solo Bot's message has author %q", a)
	}
	want := h.AdminUser().Email
	if a := author(shared.GetRunId()); a != want {
		t.Fatalf("a shared Bot's message has author %q, want %q", a, want)
	}
	// Bob leaves: what was written stays as it was.
	if _, err := h.Client.RemoveBotPerson(h.Ctx(), connect.NewRequest(&v1.RemoveBotPersonRequest{BotId: bot.Id, UserId: bu.ID})); err != nil {
		t.Fatal(err)
	}
	if a := author(shared.GetRunId()); a != want {
		t.Fatalf("the author moved to %q", a)
	}
}

func TestApprovalVoters(t *testing.T) {
	h := apptest.New(t)
	bot, bob, bu := team(t, h)
	his := chatsOf(t, bob, bot.Id)[0].GetId()
	bobRun := db.Run{ID: ids.New(), BotID: bot.Id, ChatID: his, Origin: "chat", Status: "running", UserID: bu.ID, CreatedAt: time.Now()}
	bgRun := db.Run{ID: ids.New(), BotID: bot.Id, Origin: "automation", Status: "running", CreatedAt: time.Now()}
	h.DB.Create(&bobRun)
	h.DB.Create(&bgRun)
	approval := func(runID, connector, action string) db.Approval {
		a := db.Approval{ID: ids.New(), BotID: bot.Id, RunID: runID, Connector: connector, Action: action, Status: "pending", CreatedAt: time.Now()}
		h.DB.Create(&a)
		return a
	}
	own := approval(bobRun.ID, "python", "run")
	manager := approval(bobRun.ID, "bot", "soul")
	background := approval(bgRun.ID, "python", "run")

	list := func(c silov1connect.UIClient) map[string]*v1.Approval {
		res, err := c.ListApprovals(h.Ctx(), connect.NewRequest(&v1.ListApprovalsRequest{BotId: bot.Id}))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*v1.Approval{}
		for _, a := range res.Msg.GetApprovals() {
			out[a.GetId()] = a
		}
		return out
	}
	if l := list(bob); len(l) != 1 || l[own.ID] == nil || !l[own.ID].GetOnceOnly() {
		t.Fatalf("bob's approvals = %v; want only his own chat's, once only", l)
	}
	if l := list(h.Client); len(l) != 3 || l[own.ID].GetPerson() != "bob@test.local" {
		t.Fatalf("the manager's approvals = %v", l)
	}
	decide := func(c silov1connect.UIClient, id, vote string) (*v1.Approval, error) {
		res, err := c.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: id, Decision: vote}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	if _, err := decide(bob, manager.ID, "allow_once"); code(err) != connect.CodePermissionDenied {
		t.Fatalf("bob answered a manager's approval: %v", err)
	}
	if _, err := decide(bob, background.ID, "allow_once"); code(err) != connect.CodePermissionDenied {
		t.Fatalf("bob answered a background approval: %v", err)
	}
	got, err := decide(bob, own.ID, "always")
	if err != nil || got.GetStatus() != "allow_once" {
		t.Fatalf("bob's always = %+v, %v; want allow_once", got, err)
	}
	var rules int64
	h.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ? AND action = ?", bot.Id, "python", "run").Count(&rules)
	if rules != 0 {
		t.Fatal("a user's vote changed the rule")
	}
	if _, err := decide(h.Client, background.ID, "deny"); err != nil {
		t.Fatalf("the manager could not answer a background approval: %v", err)
	}
}

func TestReadStateIsPerPerson(t *testing.T) {
	h := apptest.New(t)
	bot, bob, _ := team(t, h)
	h.DB.Create(&db.FeedPost{ID: ids.New(), BotID: bot.Id, Body: "news", SourceKind: "chat", CreatedAt: time.Now()})
	unread := func(c silov1connect.UIClient) int32 {
		res, err := c.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.GetFeedUnread()
	}
	if unread(h.Client) != 1 || unread(bob) != 1 {
		t.Fatal("a new post is not unread for both")
	}
	if _, err := h.Client.MarkFeedRead(h.Ctx(), connect.NewRequest(&v1.MarkFeedReadRequest{BotId: bot.Id})); err != nil {
		t.Fatal(err)
	}
	if unread(h.Client) != 0 || unread(bob) != 1 {
		t.Fatalf("after the owner read it: owner %d, bob %d; want 0 and 1", unread(h.Client), unread(bob))
	}
}

func TestZoneFollowsOnlyWhenSolo(t *testing.T) {
	h := apptest.New(t)
	bot, bob, _ := team(t, h)
	his := chatsOf(t, bob, bot.Id)[0].GetId()
	before := botZone(t, h, bot.Id)
	res, err := bob.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.Id, ChatId: his, Text: "hi", TimeZone: "Asia/Tokyo"}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	if z := botZone(t, h, bot.Id); z != before {
		t.Fatalf("a shared Bot followed bob's device to %s", z)
	}
}

func botZone(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	var b db.Bot
	h.DB.Select("time_zone").Where("id = ?", botID).Find(&b)
	return b.TimeZone
}

func TestPeopleLineInSessionPrompt(t *testing.T) {
	h := apptest.New(t)
	bot, _, _ := team(t, h)
	prompt := h.App.SystemPromptText(bot.Id)
	if !containsAll(prompt, "People who use you", "bob@test.local", "(manages you)") {
		t.Fatalf("the session tier has no people line:\n%s", prompt)
	}
	solo := h.CreateBot("Alone")
	if p := h.App.SystemPromptText(solo.Id); containsAll(p, "People who use you") {
		t.Fatal("a solo Bot's prompt names its people")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
