package app_test

import (
	"reflect"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// thinkingYAML allows dummy/think (the provider reports off..high) and
// dummy/echo (no levels from the provider; the operator lists two).
func thinkingYAML(dataDir string) string {
	yaml := apptest.DefaultYAML(dataDir)
	yaml = strings.Replace(yaml, "  - dummy/echo\n", "  - dummy/echo\n  - dummy/think\n  - dummy/plain\n", 1)
	return yaml + `thinking:
  levels:
    - model: dummy/echo
      levels: [high, low, bogus]
`
}

func TestThinkingLevelsListed(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(thinkingYAML(t.TempDir())))
	bot := h.CreateBot("Thinker")
	lm, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, m := range lm.Msg.GetModels() {
		got[m.GetId()] = m.GetThinkingLevels()
	}
	if !reflect.DeepEqual(got["dummy/think"], []string{"off", "low", "medium", "high"}) {
		t.Fatalf("provider levels %v", got["dummy/think"])
	}
	// The operator override wins, sorted, unknown names dropped.
	if !reflect.DeepEqual(got["dummy/echo"], []string{"low", "high"}) {
		t.Fatalf("override levels %v", got["dummy/echo"])
	}
	if len(got["dummy/plain"]) != 0 {
		t.Fatalf("a model without thinking lists %v", got["dummy/plain"])
	}
}

func TestSetChatThinking(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(thinkingYAML(t.TempDir())))
	bot := h.CreateBot("Thinker")
	chat := h.FirstChat(bot.GetId())

	c, err := h.Client.SetChatThinking(h.Ctx(), connect.NewRequest(&v1.SetChatThinkingRequest{
		BotId: bot.GetId(), ChatId: chat, Thinking: "XHigh",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Msg.GetThinking() != "xhigh" {
		t.Fatalf("thinking %q", c.Msg.GetThinking())
	}
	var row db.Chat
	h.DB.First(&row, "id = ?", chat)
	if row.Thinking != "xhigh" {
		t.Fatalf("stored thinking %q", row.Thinking)
	}
	if _, err := h.Client.SetChatThinking(h.Ctx(), connect.NewRequest(&v1.SetChatThinkingRequest{
		BotId: bot.GetId(), ChatId: chat, Thinking: "ultra",
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown level: %v", err)
	}
	chats, _ := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: bot.GetId()}))
	if chats.Msg.GetChats()[0].GetThinking() != "xhigh" {
		t.Fatal("ListChats does not carry the level")
	}
	// "" goes back to the model default.
	c, err = h.Client.SetChatThinking(h.Ctx(), connect.NewRequest(&v1.SetChatThinkingRequest{BotId: bot.GetId(), ChatId: chat}))
	if err != nil || c.Msg.GetThinking() != "" {
		t.Fatalf("clear: %v %q", err, c.Msg.GetThinking())
	}
}

// TestThinkingReachesProvider sends the chat's level, fitted to the model, on
// every turn, and hands signed reasoning back to the same model mid tool loop.
func TestThinkingReachesProvider(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_94",
		dummy.Turn{Reasoning: "check the models", ToolCalls: []llm.ToolCall{{Name: "list_models", Arguments: `{}`}}},
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t, apptest.WithYAML(thinkingYAML(t.TempDir())))
	bot := h.CreateBot("Thinker")
	chat := h.FirstChat(bot.GetId())
	if _, err := h.Client.SetChatModel(h.Ctx(), connect.NewRequest(&v1.SetChatModelRequest{
		BotId: bot.GetId(), ChatId: chat, Model: "dummy/think",
	})); err != nil {
		t.Fatal(err)
	}
	// xhigh is not offered by dummy/think; the nearest (high) is sent.
	if _, err := h.Client.SetChatThinking(h.Ctx(), connect.NewRequest(&v1.SetChatThinkingRequest{
		BotId: bot.GetId(), ChatId: chat, Thinking: "xhigh",
	})); err != nil {
		t.Fatal(err)
	}
	runID, _ := h.Send(bot.GetId(), chat, "Test_94_Input")
	h.WaitRun(runID)

	var turns []llm.Request
	for _, r := range dummy.Streamed() {
		if r.Model == "think" {
			turns = append(turns, r)
		}
	}
	if len(turns) != 2 {
		t.Fatalf("%d turns on dummy/think", len(turns))
	}
	for i, r := range turns {
		if r.Thinking != "high" {
			t.Fatalf("turn %d thinking %q, want high", i, r.Thinking)
		}
	}
	var replayed *llm.Message
	for i, m := range turns[1].Messages {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			replayed = &turns[1].Messages[i]
		}
	}
	if replayed == nil || len(replayed.Thinking) != 1 || replayed.Thinking[0].Signature != dummy.ThinkingSignature || replayed.ThinkingModel != "think" {
		t.Fatalf("signed reasoning not handed back: %+v", replayed)
	}

	// A model with no levels gets nothing, whatever the chat chose.
	dummy.Reset()
	h.Client.SetChatModel(h.Ctx(), connect.NewRequest(&v1.SetChatModelRequest{BotId: bot.GetId(), ChatId: chat, Model: "dummy/plain"}))
	runID, _ = h.Send(bot.GetId(), chat, "hello")
	h.WaitRun(runID)
	for _, r := range dummy.Streamed() {
		if r.Model == "plain" && r.Thinking != "" {
			t.Fatalf("model without levels got thinking %q", r.Thinking)
		}
	}
}
