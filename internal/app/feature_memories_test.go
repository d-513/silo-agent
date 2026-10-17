package app_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/memory"
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
	h := apptest.New(t, apptest.WithChatTools())
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

// systemOf is the system prompt of the newest streamed request.
func systemOf(t *testing.T) string {
	t.Helper()
	reqs := dummy.Streamed()
	if len(reqs) == 0 {
		t.Fatal("no model request")
	}
	var b strings.Builder
	for _, blk := range reqs[len(reqs)-1].System {
		b.WriteString(blk.Text)
	}
	return b.String()
}

// While a Bot has few memories they are all in the prompt: nothing is embedded
// or searched before the model is called.
func TestFewMemoriesAreAllInThePrompt(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_71",
		memCall("remember", `{"content":"The user's cat is named Miso"}`),
		memCall("remember", `{"content":"Deploys happen on Fridays"}`),
		dummy.Turn{Text: "saved"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Recaller")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_71_Input")
	h.WaitRun(run)

	chat, err := h.Client.CreateChat(h.Ctx(), connect.NewRequest(&v1.CreateChatRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	run2, _ := h.Send(bot.GetId(), chat.Msg.GetId(), "an unrelated question Test_72_Input")
	h.WaitRun(run2)
	sys := systemOf(t)
	if !strings.Contains(sys, "## Memories") || !strings.Contains(sys, "Miso") || !strings.Contains(sys, "Fridays") {
		t.Fatalf("the memories are not in the prompt:\n%s", sys[max(0, len(sys)-1500):])
	}
	// The memories sit after the last cache breakpoint: saving one must not
	// re-bill the tiers before it.
	blocks := dummy.Streamed()[len(dummy.Streamed())-1].System
	for _, blk := range blocks {
		if blk.CacheAfter && strings.Contains(blk.Text, "Miso") {
			t.Fatal("memories are inside a cached tier")
		}
	}
}

// Once the memories outgrow the prompt, a summary written in the background
// stands in for them; the exact facts are a recall away.
func TestManyMemoriesBecomeADigest(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Hoarder").GetId()
	ctx := h.Ctx()
	for i := 0; i < 12; i++ {
		word := fmt.Sprintf("topic%dalpha topic%dbeta topic%dgamma ", i, i, i)
		if _, err := h.App.Memory.Save(ctx, memory.Entry{BotID: id, Content: fmt.Sprintf("Fact %d: ", i) + strings.Repeat(word, 20)}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()

	// Before the first digest: the newest memories that fit, and a pointer to recall.
	run, _ := h.Send(id, h.FirstChat(id), "Test_73_Input")
	h.WaitRun(run)
	sys := systemOf(t)
	if !strings.Contains(sys, "Fact 11:") || strings.Contains(sys, "Fact 0:") || !strings.Contains(sys, "recall") {
		t.Fatalf("no bounded memory section before the digest:\n%s", sys[max(0, len(sys)-800):])
	}

	if n := h.App.Memory.SweepDigests(ctx, now); n != 1 {
		t.Fatalf("digests written: %d, want 1", n)
	}
	chat := newChat(t, h, id)
	run, _ = h.Send(id, chat, "Test_74_Input")
	h.WaitRun(run)
	sys = systemOf(t)
	if !strings.Contains(sys, "Dummy Digest") || strings.Contains(sys, "Fact 11:") {
		t.Fatalf("the digest did not replace the list:\n%s", sys[max(0, len(sys)-800):])
	}

	// Unchanged memories are not summarized again; a change is, but not at once.
	if n := h.App.Memory.SweepDigests(ctx, now.Add(2*time.Hour)); n != 0 {
		t.Fatalf("an unchanged store was digested again (%d)", n)
	}
	if _, err := h.App.Memory.Save(ctx, memory.Entry{BotID: id, Content: "A brand new unrelated fact about zebras"}); err != nil {
		t.Fatal(err)
	}
	if n := h.App.Memory.SweepDigests(ctx, now.Add(time.Minute)); n != 0 {
		t.Fatalf("the digest was rewritten within its quiet period (%d)", n)
	}
	if n := h.App.Memory.SweepDigests(ctx, now.Add(2*time.Hour)); n != 1 {
		t.Fatalf("a changed store was not digested (%d)", n)
	}
}

// memory.in_prompt off keeps memories out of the prompt entirely.
func TestMemoriesOutOfThePromptWhenOff(t *testing.T) {
	dummy.Reset()
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(dir)+"memory:\n  in_prompt: false\n"))
	id := h.CreateBot("Quiet").GetId()
	if _, err := h.App.Memory.Save(h.Ctx(), memory.Entry{BotID: id, Content: "The user's cat is named Miso"}); err != nil {
		t.Fatal(err)
	}
	run, _ := h.Send(id, h.FirstChat(id), "Test_75_Input")
	h.WaitRun(run)
	if sys := systemOf(t); strings.Contains(sys, "Miso") {
		t.Fatal("a memory reached the prompt with memory.in_prompt off")
	}
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
