package app_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func memResults(events []db.RunEvent) []string {
	var out []string
	for _, ev := range events {
		if ev.Kind == "tool_result" {
			out = append(out, ev.Body)
		}
	}
	return out
}

func memCall(name, args string) dummy.Turn {
	return dummy.Turn{ToolCalls: []llm.ToolCall{{Name: name, Arguments: args}}}
}

func TestMemoriesRememberRecallScoped(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_66",
		memCall("remember", `{"content":"The user's cat is named Miso"}`),
		memCall("remember", `{"content":"Deploys happen on Fridays"}`),
		memCall("remember", `{"content":"The user's cat is named Miso"}`),
		memCall("recall", `{"query":"what is the cat called"}`),
		dummy.Turn{Text: "done"},
	)
	dummy.Script("Test_67",
		memCall("recall", `{"query":"what is the cat called"}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Keeper")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_66_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 4 {
		t.Fatalf("tool results %q\n%s", res, h.RunBody(run))
	}
	if !strings.Contains(res[2], "updated") {
		t.Fatalf("a repeated fact should update, got %q", res[2])
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot.GetId()).Count(&n)
	if n != 2 {
		t.Fatalf("memories %d, want 2 (dedupe)", n)
	}
	lines := strings.Split(strings.TrimSpace(res[3]), "\n")
	if len(lines) < 1 || !strings.Contains(lines[0], "Miso") {
		t.Fatalf("closest memory should lead:\n%s", res[3])
	}
	var used db.Memory
	h.DB.Where("bot_id = ? AND content LIKE ?", bot.GetId(), "%Miso%").First(&used)
	if used.LastUsedAt == nil {
		t.Fatal("recall should bump last_used_at")
	}

	other := h.CreateBot("Stranger")
	run2, _ := h.Send(other.GetId(), h.FirstChat(other.GetId()), "Test_67_Input")
	res2 := memResults(h.WaitRun(run2))
	if len(res2) != 1 || strings.Contains(res2[0], "Miso") {
		t.Fatalf("recall leaked across bots: %q", res2)
	}
}

func TestMemoriesForget(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Forgetful")
	dummy.Script("Test_68", memCall("remember", `{"content":"Temporary fact"}`), dummy.Turn{Text: "ok"})
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_68_Input")
	h.WaitRun(run)
	var m db.Memory
	if err := h.DB.First(&m, "bot_id = ?", bot.GetId()).Error; err != nil {
		t.Fatal(err)
	}

	stranger := h.CreateBot("Stranger")
	dummy.Script("Test_69", memCall("forget", fmt.Sprintf(`{"id":%q}`, m.ID)), dummy.Turn{Text: "ok"})
	run2, _ := h.Send(stranger.GetId(), h.FirstChat(stranger.GetId()), "Test_69_Input")
	if res := memResults(h.WaitRun(run2)); len(res) != 1 || !strings.Contains(res[0], "no memory") {
		t.Fatalf("another bot must not forget it: %q", res)
	}

	dummy.Script("Test_70", memCall("forget", fmt.Sprintf(`{"id":%q}`, m.ID)), dummy.Turn{Text: "ok"})
	// A fresh chat: the dummy counts assistant turns in history.
	chat, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	run3, _ := h.Send(bot.GetId(), chat.Msg.GetId(), "Test_70_Input")
	h.WaitRun(run3)
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ?", bot.GetId()).Count(&n)
	if n != 0 {
		t.Fatalf("forget left %d rows", n)
	}
}

func TestMemoriesAutoRecallInPrompt(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_71",
		memCall("remember", `{"content":"The user's cat is named Miso"}`),
		memCall("remember", `{"content":"Deploys happen on Fridays"}`),
		dummy.Turn{Text: "saved"},
	)
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(strings.Replace(apptest.DefaultYAML(dir), "search:", "debug: true\nsearch:", 1)))
	bot := h.CreateBot("Recaller")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_71_Input")
	h.WaitRun(run)

	chat, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	run2, _ := h.Send(bot.GetId(), chat.Msg.GetId(), "what is the cat called Test_72_Input")
	h.WaitRun(run2)

	logs, err := h.Client.ListLLMLogs(h.Ctx(), connect.NewRequest(&v1.ListLLMLogsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs.Msg.GetLogs() {
		if l.GetLabel() != "chat" || !strings.Contains(l.GetRequest(), "Test_72_Input") {
			continue
		}
		req := l.GetRequest()
		if !strings.Contains(req, "Recalled memories") || !strings.Contains(req, "Miso") {
			t.Fatalf("auto-recall missing from the prompt:\n%s", req)
		}
		if strings.Contains(req, "Fridays") {
			t.Fatalf("an unrelated memory was injected:\n%s", req)
		}
		return
	}
	t.Fatal("no chat log for the second run")
}

func TestMemoriesListAndDeleteRPC(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_73", memCall("remember", `{"content":"Prefers metric units"}`), dummy.Turn{Text: "ok"})
	h := apptest.New(t)
	bot := h.CreateBot("Listed")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_73_Input")
	h.WaitRun(run)

	list, err := h.Client.ListMemories(h.Ctx(), connect.NewRequest(&v1.ListMemoriesRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.GetMemories()) != 1 || list.Msg.GetMemories()[0].GetContent() != "Prefers metric units" {
		t.Fatalf("memories %v", list.Msg.GetMemories())
	}
	id := list.Msg.GetMemories()[0].GetId()
	if _, err := h.Client.DeleteMemory(h.Ctx(), connect.NewRequest(&v1.DeleteMemoryRequest{BotId: bot.GetId(), Id: id})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.DeleteMemory(h.Ctx(), connect.NewRequest(&v1.DeleteMemoryRequest{BotId: bot.GetId(), Id: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := h.NewClient().ListMemories(h.Ctx(), connect.NewRequest(&v1.ListMemoriesRequest{BotId: bot.GetId()})); err == nil {
		t.Fatal("unauthenticated ListMemories should fail")
	}
}

func TestCoreMemoryTool(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_74", memCall("core_memory", `{"append":"Name is Ada"}`), dummy.Turn{Text: "ok"})
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(strings.Replace(apptest.DefaultYAML(dir), "search:", "debug: true\nsearch:", 1)))
	bot := h.CreateBot("Core")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_74_Input")
	if res := memResults(h.WaitRun(run)); len(res) != 1 || !strings.HasPrefix(res[0], "ok") {
		t.Fatalf("core_memory result %q\n%s", res, h.RunBody(run))
	}
	var b db.Bot
	h.DB.First(&b, "id = ?", bot.GetId())
	if b.Memory != "Name is Ada" {
		t.Fatalf("bots.memory %q", b.Memory)
	}
	logs, err := h.Client.ListLLMLogs(h.Ctx(), connect.NewRequest(&v1.ListLLMLogsRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs.Msg.GetLogs() {
		if l.GetLabel() != "chat" {
			continue
		}
		req := l.GetRequest()
		if !strings.Contains(req, "## CORE MEMORY") || strings.Contains(req, "## MEMORY") {
			t.Fatalf("prompt lacks CORE MEMORY:\n%s", req)
		}
		return
	}
	t.Fatal("no chat log")
}

func TestSearchMemoriesRPC(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_75",
		memCall("remember", `{"content":"The user's cat is named Miso"}`),
		memCall("remember", `{"content":"Deploys happen on Fridays"}`),
		dummy.Turn{Text: "ok"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Searcher")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_75_Input")
	h.WaitRun(run)

	res, err := h.Client.SearchMemories(h.Ctx(), connect.NewRequest(&v1.SearchMemoriesRequest{BotId: bot.GetId(), Query: "what is the cat called"}))
	if err != nil {
		t.Fatal(err)
	}
	ms := res.Msg.GetMemories()
	if len(ms) != 2 || !strings.Contains(ms[0].GetContent(), "Miso") {
		t.Fatalf("search order %v", ms)
	}
	if ms[0].GetDistance() > ms[1].GetDistance() {
		t.Fatalf("distances not ascending: %v", ms)
	}
	var n int64
	h.DB.Model(&db.Memory{}).Where("bot_id = ? AND last_used_at IS NOT NULL", bot.GetId()).Count(&n)
	if n != 0 {
		t.Fatal("a human search must not bump last_used_at")
	}
	if _, err := h.Client.SearchMemories(h.Ctx(), connect.NewRequest(&v1.SearchMemoriesRequest{BotId: bot.GetId(), Query: "  "})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty query: %v", err)
	}
	if _, err := h.NewClient().SearchMemories(h.Ctx(), connect.NewRequest(&v1.SearchMemoriesRequest{BotId: bot.GetId(), Query: "cat"})); err == nil {
		t.Fatal("unauthenticated SearchMemories should fail")
	}
}

func TestPutSettingsRejectsNonEmbeddingModel(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/silo.yaml"
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	put := func(v string) error {
		_, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"embedding_model": v}}))
		return err
	}
	if err := put("anthropic/claude-sonnet-5"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("anthropic embedding model: %v", err)
	}
	if err := put("nope"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unparseable embedding model: %v", err)
	}
	if err := put("openai/text-embedding-3-small"); err != nil {
		t.Fatalf("openai embedding model: %v", err)
	}
	if got := h.Store.Config().EmbedModel; got != "openai/text-embedding-3-small" {
		t.Fatalf("embedding_model %q", got)
	}
}
