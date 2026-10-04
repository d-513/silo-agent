package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// newInboundChannel registers a fake adapter and creates an enabled, inbound
// channel bound to one external chat.
func newInboundChannel(t *testing.T, h *apptest.H, botID string, adapter *apptest.FakeAdapter, slug, external string) db.Channel {
	t.Helper()
	created, err := h.Client.CreateChannel(h.Ctx(), connect.NewRequest(&v1.CreateChannelRequest{
		BotId: botID, Adapter: slug, Name: "Inbox", Enabled: true, Inbound: true,
		ExternalId: external, TargetTitle: "Target",
		Secrets: map[string]string{"token": "s3cret"},
	}))
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	var ch db.Channel
	if err := h.DB.First(&ch, "id = ?", created.Msg.GetId()).Error; err != nil {
		t.Fatal(err)
	}
	return ch
}

func waitSent(t *testing.T, adapter *apptest.FakeAdapter, want string) channels.Outbound {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range adapter.Messages() {
			if m.Text == want {
				return m
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("adapter never delivered %q; got %+v", want, adapter.Messages())
	return channels.Outbound{}
}

// freshChat opens a new web chat: the dummy provider routes on the first
// Test_ token of a conversation, so each scripted exchange needs its own.
func freshChat(t *testing.T, h *apptest.H, botID string) string {
	t.Helper()
	c, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return c.Msg.GetId()
}

func channelChats(h *apptest.H, channelID string) []db.Chat {
	var chats []db.Chat
	h.DB.Where("channel_id = ?", channelID).Find(&chats)
	return chats
}

func TestChannelInboundStartsRunAndDeliversSections(t *testing.T) {
	slug := apptest.UniqueSlug("inchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	bot := h.CreateBot("Inbound")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-9")
	host := adapter.WaitHost(t)

	dummy.Reset()
	dummy.Script("Test_41",
		dummy.Turn{Text: "pong<section_send />"},
		dummy.Turn{Text: "again<section_send />"},
	)
	err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-9", Title: "Nine", Text: "Test_41_Input"})
	if err != nil {
		t.Fatal(err)
	}
	out := waitSent(t, adapter, "pong")
	if out.ExternalID != "chat-9" {
		t.Fatalf("section went to %q, want the conversation it came from", out.ExternalID)
	}

	chats := channelChats(h, ch.ID)
	if len(chats) != 1 || chats[0].ExternalID != "chat-9" || chats[0].Title != "Nine" || chats[0].BotID != bot.GetId() {
		t.Fatalf("channel conversation %+v", chats)
	}

	// A second message from the same conversation reuses its chat.
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-9", Text: "Test_41_Input, once more"}); err != nil {
		t.Fatal(err)
	}
	waitSent(t, adapter, "again")
	if n := len(channelChats(h, ch.ID)); n != 1 {
		t.Fatalf("%d chats for one conversation", n)
	}

	// The channel's chats stay out of the web chat list.
	list, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Msg.GetChats() {
		if c.GetId() == chats[0].ID {
			t.Fatal("a channel conversation is listed as a web chat")
		}
	}
}

func TestChannelInboundIgnoredWhenSendOnlyOrUnbound(t *testing.T) {
	slug := apptest.UniqueSlug("quietchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	bot := h.CreateBot("Quiet")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-1")
	host := adapter.WaitHost(t)

	// Send-only: tools write to it, nothing it hears starts a run.
	h.DB.Model(&db.Channel{}).Where("id = ?", ch.ID).Update("inbound", false)
	ch.Inbound = false
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-1", Text: "ignored"}); err != nil {
		t.Fatal(err)
	}
	// No target chosen yet: nothing to read from.
	ch.Inbound, ch.ExternalID = true, ""
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-1", Text: "ignored"}); err != nil {
		t.Fatal(err)
	}
	if n := len(channelChats(h, ch.ID)); n != 0 {
		t.Fatalf("%d conversations were created", n)
	}
}

func TestChatsToolReadsChannelHistoryFromAdapterThenLocalLog(t *testing.T) {
	slug := apptest.UniqueSlug("histchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	bot := h.CreateBot("History")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-7")
	host := adapter.WaitHost(t)

	dummy.Reset()
	dummy.Script("Test_43", dummy.Turn{Text: "noted<section_send />"})
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-7", Title: "Seven", Text: "Test_43_Input"}); err != nil {
		t.Fatal(err)
	}
	waitSent(t, adapter, "noted")
	conv := channelChats(h, ch.ID)[0]

	read := func(marker string) string {
		dummy.Script(marker,
			dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "chats", Arguments: `{"chat":"` + conv.ID + `"}`}}},
			dummy.Turn{Text: "read it"},
		)
		runID, _ := h.Send(bot.GetId(), freshChat(t, h, bot.GetId()), marker+"_Input")
		h.WaitRun(runID)
		return toolResults(h, runID)["chats"]
	}

	// The platform's own history wins when the adapter can give it.
	adapter.HistoryMsgs = []channels.Message{{Author: "Alice", Text: "from the platform"}, {Out: true, Text: "bot reply"}}
	got := read("Test_44")
	if !strings.Contains(got, "Alice: from the platform") || !strings.Contains(got, "Bot: bot reply") || !strings.Contains(got, "via ") {
		t.Fatalf("adapter history: %q", got)
	}

	// A bot account cannot read it (BOT_METHOD_INVALID): fall back to the log.
	adapter.HistoryErr = context.DeadlineExceeded
	got = read("Test_45")
	if !strings.Contains(got, "User: Test_43_Input") || !strings.Contains(got, "Bot: noted") || strings.Contains(got, "from the platform") {
		t.Fatalf("local history: %q", got)
	}

	// With no argument it lists this Bot's chats with their kind.
	dummy.Script("Test_46",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "chats", Arguments: `{}`}}},
		dummy.Turn{Text: "listed"},
	)
	runID, _ := h.Send(bot.GetId(), freshChat(t, h, bot.GetId()), "Test_46_Input")
	h.WaitRun(runID)
	if got := toolResults(h, runID)["chats"]; !strings.Contains(got, conv.ID) || !strings.Contains(got, "(channel)") {
		t.Fatalf("chat list: %q", got)
	}
}

func TestDeleteChannelClearsItsConversationsSecretsAndRules(t *testing.T) {
	slug := apptest.UniqueSlug("delchan")
	adapter := apptest.NewFakeAdapter(slug)
	channels.Register(adapter)
	h := apptest.New(t)
	bot := h.CreateBot("Cleanup")
	ch := newInboundChannel(t, h, bot.GetId(), adapter, slug, "chat-3")
	host := adapter.WaitHost(t)

	dummy.Reset()
	dummy.Script("Test_47", dummy.Turn{Text: "hi<section_send />"})
	if err := host.DeliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "chat-3", Text: "Test_47_Input"}); err != nil {
		t.Fatal(err)
	}
	waitSent(t, adapter, "hi")
	h.DB.Create(&db.Rule{ID: ids.New(), BotID: bot.GetId(), Connector: "channels", Action: ch.ID, Decision: "allow"})

	count := func(model any, where string, args ...any) int64 {
		var n int64
		h.DB.Model(model).Where(where, args...).Count(&n)
		return n
	}
	if count(&db.Secret{}, "bot_id = ? AND name LIKE ?", bot.GetId(), "channel."+ch.ID+".%") == 0 || len(channelChats(h, ch.ID)) == 0 {
		t.Fatal("setup: the channel should have a secret and a conversation")
	}

	if _, err := h.Client.DeleteChannel(h.Ctx(), connect.NewRequest(&v1.DeleteChannelRequest{BotId: bot.GetId(), Id: ch.ID})); err != nil {
		t.Fatal(err)
	}
	if n := len(channelChats(h, ch.ID)); n != 0 {
		t.Fatalf("%d conversations survive the channel", n)
	}
	if n := count(&db.Secret{}, "bot_id = ? AND name LIKE ?", bot.GetId(), "channel."+ch.ID+".%"); n != 0 {
		t.Fatalf("%d channel secrets survive", n)
	}
	if n := count(&db.Rule{}, "bot_id = ? AND connector = ? AND action = ?", bot.GetId(), "channels", ch.ID); n != 0 {
		t.Fatalf("%d channel rules survive", n)
	}
	if n := count(&db.Channel{}, "id = ?", ch.ID); n != 0 {
		t.Fatal("the channel row survives")
	}
}
