package app_test

import (
	"fmt"
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
