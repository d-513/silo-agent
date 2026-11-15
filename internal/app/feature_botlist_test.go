package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/llm/dummy"
)

func listedBot(t *testing.T, c silov1connect.UIClient, h *apptest.H, id string) *v1.Bot {
	t.Helper()
	res, err := c.ListBots(h.Ctx(), connect.NewRequest(&v1.ListBotsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range res.Msg.GetBots() {
		if b.GetId() == id {
			return b
		}
	}
	t.Fatalf("Bot %s is not listed", id)
	return nil
}

// A listed Bot carries the newest thing said in the viewer's conversations
// with it, as one plain line: what the list shows under its name and sorts by.
func TestListedBotCarriesItsLastMessage(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_lm1", dummy.Turn{Text: "## Done\n\nThe **report** is in `bot/report.md`."})
	h := apptest.New(t)
	id := h.CreateBot("Chatty").GetId()

	if b := listedBot(t, h.Client, h, id); b.GetLastMessage() != "" || b.GetLastMessageAt() != "" {
		t.Fatalf("a Bot nobody wrote to has a last message: %q at %q", b.GetLastMessage(), b.GetLastMessageAt())
	}

	run, _ := h.Send(id, h.FirstChat(id), "Test_lm1_Input")
	h.WaitRun(run)
	b := listedBot(t, h.Client, h, id)
	if got, want := b.GetLastMessage(), "Done The report is in bot/report.md."; got != want {
		t.Fatalf("last message %q, want %q", got, want)
	}
	if b.GetLastMessageMine() || b.GetLastMessageAt() == "" {
		t.Fatalf("the Bot's reply: mine %v at %q", b.GetLastMessageMine(), b.GetLastMessageAt())
	}
	// GetBot is the same row.
	got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
	if err != nil || got.Msg.GetLastMessage() != b.GetLastMessage() {
		t.Fatalf("GetBot says %q (%v)", got.Msg.GetLastMessage(), err)
	}
}

// What someone else said to the Bot in their own chat is theirs: a person who
// only uses the Bot sees their own conversations in the list, never a
// colleague's.
func TestLastMessageIsTheViewersOwn(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_lm2", dummy.Turn{Text: "the payroll numbers are up"})
	h := apptest.New(t)
	sp := homeSpace(t, h)
	id := h.CreateBot("Shared").GetId()
	bob, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	if _, err := h.Client.AddBotPerson(h.Ctx(), connect.NewRequest(&v1.AddBotPersonRequest{BotId: id, UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	run, _ := h.Send(id, h.FirstChat(id), "Test_lm2_Input")
	h.WaitRun(run)

	if b := listedBot(t, h.Client, h, id); !strings.Contains(b.GetLastMessage(), "payroll") {
		t.Fatalf("its author's list: %q", b.GetLastMessage())
	}
	if b := listedBot(t, bob, h, id); b.GetLastMessage() != "" {
		t.Fatalf("a colleague's list shows %q", b.GetLastMessage())
	}
}

// A chat put away is still there and still opens; it is marked hidden until
// it is brought back, and its place in the order (its last activity) does not
// move. The main chat cannot be put away.
func TestHidingAChat(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Tidy").GetId()
	main := h.FirstChat(id)
	c, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: id}))
	if err != nil {
		t.Fatal(err)
	}
	side := c.Msg.GetId()
	find := func() *v1.Chat {
		for _, x := range webChats(t, h, id) {
			if x.GetId() == side {
				return x
			}
		}
		t.Fatal("the chat is gone from the list")
		return nil
	}
	before := find()
	if before.GetHidden() {
		t.Fatal("a new chat starts hidden")
	}

	got, err := h.Client.SetChatHidden(h.Ctx(), connect.NewRequest(&v1.SetChatHiddenRequest{BotId: id, ChatId: side, Hidden: true}))
	if err != nil || !got.Msg.GetHidden() {
		t.Fatalf("hide: %+v %v", got, err)
	}
	if after := find(); !after.GetHidden() || after.GetUpdatedAt() != before.GetUpdatedAt() {
		t.Fatalf("hidden chat: hidden %v, moved from %q to %q", after.GetHidden(), before.GetUpdatedAt(), after.GetUpdatedAt())
	}
	if _, err := h.Client.SetChatHidden(h.Ctx(), connect.NewRequest(&v1.SetChatHiddenRequest{BotId: id, ChatId: side})); err != nil || find().GetHidden() {
		t.Fatalf("bring back: %v", err)
	}
	if _, err := h.Client.SetChatHidden(h.Ctx(), connect.NewRequest(&v1.SetChatHiddenRequest{BotId: id, ChatId: main, Hidden: true})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("hiding the main chat: %v", err)
	}
}
