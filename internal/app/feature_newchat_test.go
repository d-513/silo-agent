package app_test

import (
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func listedChats(t *testing.T, h *apptest.H, botID string) []*v1.Chat {
	t.Helper()
	res, err := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetChats()
}

// The console's New chat is an empty page: the chat is made by its first
// message, with the model and thinking level picked there, so a chat nobody
// wrote in never reaches the list.
func TestSendMakesTheNewChat(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(modelYAML(t.TempDir())))
	id := h.CreateBot("Fresh").GetId()
	first := h.FirstChat(id)

	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{
		BotId: id, Text: "Test_50_Input", NewChat: true, Model: "dummy/extra", Thinking: "High",
	}))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	chatID := res.Msg.GetChatId()
	if chatID == "" || chatID == first {
		t.Fatalf("chat %q, want a new one (first is %q)", chatID, first)
	}
	h.WaitRun(res.Msg.GetRunId())

	var row db.Chat
	if err := h.DB.First(&row, "id = ?", chatID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Model != "dummy/extra" || row.Thinking != "high" {
		t.Fatalf("chat model %q thinking %q", row.Model, row.Thinking)
	}
	var call db.LLMLog
	h.DB.Where("bot_id = ? AND label = ?", id, "chat").Order("at desc").First(&call)
	if call.Model != "extra" {
		t.Fatalf("the run used %q, want extra", call.Model)
	}
	if got := listedChats(t, h, id); len(got) != 2 || got[0].GetId() != chatID {
		t.Fatalf("chats %v, want the new one first of two", got)
	}
}

// A first message the server refuses leaves no chat behind.
func TestRefusedNewChatLeavesNoRow(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(modelYAML(t.TempDir())))
	id := h.CreateBot("Fresh").GetId()
	h.FirstChat(id)

	for name, req := range map[string]*v1.SendRequest{
		"model":    {BotId: id, Text: "hi", NewChat: true, Model: "dummy/nope"},
		"thinking": {BotId: id, Text: "hi", NewChat: true, Thinking: "loud"},
		"empty":    {BotId: id, NewChat: true},
	} {
		if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(req)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("%s: %v, want InvalidArgument", name, err)
		}
	}
	if got := listedChats(t, h, id); len(got) != 1 {
		t.Fatalf("%d chats, want only the first", len(got))
	}
}

// Without new_chat an empty chat id still means the newest chat, and the
// model fields do not touch a chat that exists.
func TestSendToAnExistingChatKeepsItsModel(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t, apptest.WithYAML(modelYAML(t.TempDir())))
	id := h.CreateBot("Fresh").GetId()
	first := h.FirstChat(id)

	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{
		BotId: id, ChatId: first, Text: "Test_50_Input", NewChat: true, Model: "dummy/extra",
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(res.Msg.GetRunId())
	if res.Msg.GetChatId() != first {
		t.Fatalf("chat %q, want %q", res.Msg.GetChatId(), first)
	}
	var row db.Chat
	h.DB.First(&row, "id = ?", first)
	if row.Model != "" {
		t.Fatalf("model %q written onto an existing chat", row.Model)
	}
}
