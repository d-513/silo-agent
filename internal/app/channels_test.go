package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/channels"
	_ "silo.agent/internal/channels/telegram"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

func TestChannelConfigResolvesSecrets(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ch := db.Channel{ID: "ch1", BotID: "b1", Adapter: "telegram", Name: "TG"}
	a.DB.Create(&ch)
	ad, ok := channels.Lookup("telegram")
	if !ok {
		t.Fatal("telegram adapter not registered")
	}
	if err := a.saveChannelConfig(&ch, ad, map[string]string{"api_id": "123", "reply_mode": "mentions"}, map[string]string{"api_hash": "hash", "bot_token": "tok"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg := a.channelConfig(&ch)
	if cfg.Get("api_id") != "123" || cfg.Get("api_hash") != "hash" || cfg.Get("bot_token") != "tok" {
		t.Fatalf("resolved config: %#v", cfg.Values)
	}
	if nsc := nonSecretConfig(&ch); nsc["api_hash"] != "" || nsc["bot_token"] != "" {
		t.Fatalf("secret leaked into non-secret config: %#v", nsc)
	}
	set := a.channelSecretsSet(&ch)
	if len(set) != 2 {
		t.Fatalf("secrets set: %v", set)
	}
	// Missing required secret must fail.
	ch2 := db.Channel{ID: "ch2", BotID: "b1", Adapter: "telegram", Name: "TG2"}
	a.DB.Create(&ch2)
	if err := a.saveChannelConfig(&ch2, ad, map[string]string{"api_id": "1"}, nil); err == nil {
		t.Fatal("expected required-secret error")
	}
}

func TestChannelSecretNameHidden(t *testing.T) {
	if !channels.IsSecretName(channels.SecretName("ch1", "bot_token")) {
		t.Fatal("channel secret should be hidden")
	}
	if channels.IsSecretName("vendor_password") {
		t.Fatal("plain secret misclassified")
	}
}

func TestChannelPromptIncludesSections(t *testing.T) {
	a := testApp(t, nil)
	ch := db.Channel{ID: "ch1", BotID: "b1", Adapter: "telegram", Name: "Support", Prompt: "Be brief."}
	joined := ""
	for _, sec := range a.channelSections(promptContext{channel: &ch, channels: []db.Channel{ch}}) {
		joined += sec.render()
	}
	for _, want := range []string{"section_send", "Be brief.", "Support"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("channel prompt missing %q:\n%s", want, joined)
		}
	}
}

func TestChannelStateMergePreservesValues(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Channel{ID: "ch1", BotID: "b1", Adapter: "telegram", Name: "TG"})
	a.publishChannelState("ch1", channels.State{
		Kind:   channels.StateSelect,
		Values: map[string]string{"peer:user:1": `{"kind":"user","id":1}`},
	})
	a.publishChannelState("ch1", channels.State{Kind: channels.StateInfo, Message: "Connected"})
	if got := a.channelState("ch1").Values["peer:user:1"]; got == "" {
		t.Fatalf("peer value lost on status update: %#v", a.channelState("ch1").Values)
	}
	// And it must survive a reload (what a reboot does).
	var row db.Channel
	if err := a.DB.First(&row, "id = ?", "ch1").Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(row.StateJSON, "peer:user:1") {
		t.Fatalf("peer not persisted: %q", row.StateJSON)
	}
}

func TestChannelsRuleDefaultAllow(t *testing.T) {
	if d, ok := security.Default(security.Channels, "ch-anything"); !ok || d != security.Allow {
		t.Fatalf("channels default = %q ok=%v", d, ok)
	}
	if d, ok := security.Default(security.Chats, "read"); !ok || d != security.Allow {
		t.Fatalf("chats default = %q ok=%v", d, ok)
	}
}

func TestInjectLiveRun(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1", Title: "t"})
	inbox := make(chan inboxMsg, 4)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.trackRun("b1", "c1", "r1", cancel, inbox)

	if !a.inject("b1", "c1", "r1", "steer", nil) {
		t.Fatal("inject reported no live run")
	}
	select {
	case m := <-inbox:
		if m.text != "steer" {
			t.Fatalf("inbox text %q", m.text)
		}
	case <-time.After(time.Second):
		t.Fatal("message not delivered to inbox")
	}
	var n int64
	a.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", "r1", "user").Count(&n)
	if n != 1 {
		t.Fatalf("expected 1 user event, got %d", n)
	}
	if a.liveRunID("b1", "c1") != "r1" {
		t.Fatal("live run not found")
	}
}

func TestDeliverInboundDropsWhenSendOnly(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ch := db.Channel{ID: "ch1", BotID: "b1", Adapter: "telegram", Name: "TG", Enabled: true, Inbound: false}
	a.DB.Create(&ch)
	if err := a.deliverInbound(context.Background(), &ch, channels.Inbound{ExternalID: "user:1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	var n int64
	a.DB.Model(&db.Chat{}).Where("channel_id = ?", "ch1").Count(&n)
	if n != 0 {
		t.Fatalf("send-only channel created %d chats", n)
	}
}

func TestChatsReadTool(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Chat{ID: "c1", BotID: "b1", Title: "Alpha"})
	a.DB.Create(&db.Run{ID: "r1", BotID: "b1", ChatID: "c1"})
	a.DB.Create(&db.RunEvent{ID: "e1", RunID: "r1", Kind: "user", Body: "hello"})
	a.DB.Create(&db.RunEvent{ID: "e2", RunID: "r1", Kind: "section", Body: "hi there"})

	list, err := a.chatsReadTool(context.Background(), "b1", "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "Alpha") {
		t.Fatalf("list missing chat: %q", list)
	}
	read, err := a.chatsReadTool(context.Background(), "b1", "", map[string]any{"chat": "Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read, "User: hello") || !strings.Contains(read, "Bot: hi there") {
		t.Fatalf("read missing messages: %q", read)
	}
}
