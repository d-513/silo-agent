package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/memory"
	"silo.agent/internal/apptest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func command(t *testing.T, h *apptest.H, botID, chatID, text string, confirmed bool) *v1.SendResponse {
	t.Helper()
	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: botID, ChatId: chatID, Text: text, Confirmed: confirmed}))
	if err != nil {
		t.Fatalf("Send %q: %v", text, err)
	}
	return res.Msg
}

func userBodies(evs []db.RunEvent) []string {
	var out []string
	for _, e := range evs {
		if e.Kind == "user" {
			out = append(out, e.Body)
		}
	}
	return out
}

// /clear starts the model's context over without deleting anything: the
// thread keeps the old turns, the next request does not carry them.
func TestSlashClearStartsContextOver(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	id := h.CreateBot("Clearer").GetId()
	chat := h.FirstChat(id)

	run, _ := h.Send(id, chat, "remember the word pelican")
	h.WaitRun(run)
	if !strings.Contains(lastChatRequest(t, h, id), "pelican") {
		t.Fatal("first request lacks the message")
	}

	res := command(t, h, id, chat, "/clear", false)
	if res.GetCommand() != "clear" || res.GetRunId() != "" || res.GetNotice() == "" {
		t.Fatalf("/clear answered %+v", res)
	}
	if _, ok := find(h.ChatEvents(chat), "context_cleared"); !ok {
		t.Fatal("no context_cleared event")
	}

	run, _ = h.Send(id, chat, "what now")
	h.WaitRun(run)
	req := lastChatRequest(t, h, id)
	if strings.Contains(req, "pelican") || !strings.Contains(req, "what now") {
		t.Fatalf("after /clear the request still carries the old turn:\n%s", req)
	}
	if got := userBodies(h.ChatEvents(chat)); len(got) != 2 || got[0] != "remember the word pelican" {
		t.Fatalf("/clear deleted history: %q", got)
	}

	// Cleared twice in a row: nothing to clear.
	command(t, h, id, chat, "/clear", false)
	if res := command(t, h, id, chat, "/clear", false); !strings.Contains(res.GetNotice(), "already empty") {
		t.Fatalf("second /clear: %+v", res)
	}
}

// /wipe asks first, then deletes for good: the last exchange, or everything.
func TestSlashWipeDeletesForGood(t *testing.T) {
	dummy.Reset()
	h := compactHarness(t)
	id := h.CreateBot("Wiper").GetId()
	chat := h.FirstChat(id)

	run, _ := h.Send(id, chat, "harmless note")
	h.WaitRun(run)
	run, _ = h.Send(id, chat, "my password is hunter2")
	h.WaitRun(run)
	mem, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: id, ChatID: chat, Content: "The owner keeps a harmless note"})
	if err != nil {
		t.Fatal(err)
	}

	ask := command(t, h, id, chat, "/wipe last", false)
	if ask.GetConfirm() == "" || ask.GetCommand() != "wipe" {
		t.Fatalf("/wipe did not ask first: %+v", ask)
	}
	if got := userBodies(h.ChatEvents(chat)); len(got) != 2 {
		t.Fatalf("an unconfirmed /wipe deleted: %q", got)
	}

	done := command(t, h, id, chat, "/wipe last", true)
	if done.GetNotice() == "" {
		t.Fatalf("/wipe confirmed: %+v", done)
	}
	evs := h.ChatEvents(chat)
	for _, e := range evs {
		if strings.Contains(e.Body, "hunter2") {
			t.Fatalf("wiped text survives in a %s event", e.Kind)
		}
	}
	if got := userBodies(evs); len(got) != 1 || got[0] != "harmless note" {
		t.Fatalf("after /wipe last: %q", got)
	}
	if _, ok := find(evs, "wiped"); !ok {
		t.Fatal("no wiped marker")
	}

	command(t, h, id, chat, "/wipe all", true)
	if got := userBodies(h.ChatEvents(chat)); len(got) != 0 {
		t.Fatalf("after /wipe all: %q", got)
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("id = ?", mem.ID).Count(&n)
	if n != 1 {
		t.Fatal("/wipe touched memories")
	}

	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: chat, Text: "/wipe everything"})); err == nil {
		t.Fatal("/wipe with an unknown scope ran")
	}
}

// A slash that is not a command, or one escaped with //, is a message.
func TestSlashTextThatIsAMessage(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Literal").GetId()
	chat := h.FirstChat(id)

	res := command(t, h, id, chat, "//clear", false)
	if res.GetCommand() != "" || res.GetRunId() == "" {
		t.Fatalf("//clear: %+v", res)
	}
	h.WaitRun(res.GetRunId())
	res = command(t, h, id, chat, "/usr/bin/env says hi", false)
	if res.GetCommand() != "" || res.GetRunId() == "" {
		t.Fatalf("a path: %+v", res)
	}
	h.WaitRun(res.GetRunId())
	if got := userBodies(h.ChatEvents(chat)); len(got) != 2 || got[0] != "/clear" || got[1] != "/usr/bin/env says hi" {
		t.Fatalf("messages %q", got)
	}
}

func TestSlashModelThinkingRename(t *testing.T) {
	h := apptest.New(t)
	id := h.CreateBot("Switcher").GetId()
	main := h.FirstChat(id)
	chat := newChat(t, h, id)
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: main, Text: "/rename Other"})); err == nil {
		t.Fatal("the main chat was renamed")
	}

	if res := command(t, h, id, chat, "/model echo", false); !strings.Contains(res.GetNotice(), "dummy/echo") {
		t.Fatalf("/model echo: %+v", res)
	}
	var c db.Chat
	h.DB.First(&c, "id = ?", chat)
	if c.Model != "dummy/echo" {
		t.Fatalf("chat model %q", c.Model)
	}
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: chat, Text: "/model nope"})); err == nil {
		t.Fatal("/model with no match succeeded")
	}
	command(t, h, id, chat, "/thinking high", false)
	command(t, h, id, chat, "/rename  Trip   plans", false)
	h.DB.First(&c, "id = ?", chat)
	if c.Thinking != "high" || c.Title != "Trip plans" {
		t.Fatalf("chat %+v", c)
	}
	if res := command(t, h, id, "", "/new", false); !res.GetNewChat() {
		t.Fatalf("/new: %+v", res)
	}
	list, err := h.Client.ListCommands(h.Ctx(), connect.NewRequest(&v1.ListCommandsRequest{BotId: id}))
	if err != nil || len(list.Msg.GetCommands()) < 5 {
		t.Fatalf("ListCommands: %v %+v", err, list)
	}
}

// A channel conversation takes the harmless commands and answers over the
// channel; the owner's settings stay the owner's.
func TestSlashFromChannel(t *testing.T) {
	slug := apptest.UniqueSlug("cmdchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	id := h.CreateBot("Channeled").GetId()
	ch := newInboundChannel(t, h, id, adapter, slug, "chat-7")
	host := adapter.WaitHost(t)

	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-7", Text: "/help@SiloBot"}); err != nil {
		t.Fatal(err)
	}
	waitSentPrefix(t, adapter, "Commands:")
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-7", Text: "/wipe all"}); err != nil {
		t.Fatal(err)
	}
	waitSentPrefix(t, adapter, "/wipe works only in Silo")
	var runs int64
	h.DB.Model(&db.Run{}).Where("bot_id = ?", id).Count(&runs)
	if runs != 0 {
		t.Fatalf("a command started %d runs", runs)
	}
}

func waitSentPrefix(t *testing.T, adapter *apptest.FakeAdapter, prefix string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range adapter.Messages() {
			if strings.HasPrefix(m.Text, prefix) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("adapter never sent %q…; got %+v", prefix, adapter.Messages())
}

// A long thread is replayed a page at a time.
func TestThreadPaging(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Pager").GetId()
	chat := h.FirstChat(id)
	for i := range 23 {
		run, _ := h.Send(id, chat, "turn "+string(rune('a'+i)))
		h.WaitRun(run)
	}

	ctx, cancel := context.WithTimeout(h.Ctx(), 5*time.Second)
	defer cancel()
	stream, err := h.Client.StreamRun(ctx, connect.NewRequest(&v1.StreamRunRequest{BotId: id, ChatId: chat}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() || stream.Msg().GetKind() != "earlier" {
		t.Fatalf("stream did not open with earlier: %+v", stream.Msg())
	}
	var first string
	users := 0
	for users < 20 && stream.Receive() {
		ev := stream.Msg()
		if first == "" {
			first = ev.GetId()
		}
		if ev.GetKind() == "user" {
			users++
			if users == 1 && ev.GetBody() != "turn d" {
				t.Fatalf("page opens at %q, want turn d", ev.GetBody())
			}
		}
	}
	cancel()

	page, err := h.Client.ListChatEvents(h.Ctx(), connect.NewRequest(&v1.ListChatEventsRequest{BotId: id, ChatId: chat, BeforeEventId: first}))
	if err != nil {
		t.Fatal(err)
	}
	var older []string
	for _, ev := range page.Msg.GetEvents() {
		if ev.GetKind() == "user" {
			older = append(older, ev.GetBody())
		}
	}
	if page.Msg.GetMore() || strings.Join(older, ",") != "turn a,turn b,turn c" {
		t.Fatalf("earlier page more=%v %q", page.Msg.GetMore(), older)
	}
}
