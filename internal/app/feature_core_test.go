package app_test

import (
	"encoding/json"
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func TestAuthBoundary(t *testing.T) {
	h := apptest.New(t)
	anon := h.NewClient()

	if _, err := anon.ListBots(context.Background(), connect.NewRequest(&v1.ListBotsRequest{})); err == nil {
		t.Fatal("unauthenticated ListBots should fail")
	}
	if _, err := anon.SignIn(context.Background(), connect.NewRequest(&v1.SignInRequest{
		Email: h.Email, Password: "wrong",
	})); err == nil {
		t.Fatal("bad password should fail")
	}
	me, err := h.Client.Me(context.Background(), connect.NewRequest(&v1.MeRequest{}))
	if err != nil || !me.Msg.GetUser().GetAdmin() {
		t.Fatalf("Me: %v %+v", err, me)
	}
	if _, err := h.Client.SignOut(context.Background(), connect.NewRequest(&v1.SignOutRequest{})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.Me(context.Background(), connect.NewRequest(&v1.MeRequest{})); err == nil {
		t.Fatal("Me after sign-out should fail")
	}
}

func TestBotCRUDAndLifecycle(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Alpha")
	id := bot.GetId()
	if bot.GetStatus() != "starting" {
		t.Fatalf("new bot should be starting, got %s", bot.GetStatus())
	}
	if h.Fake.Creates.Load() != 1 || h.Fake.Starts.Load() != 1 {
		t.Fatalf("create/start counts %d/%d", h.Fake.Creates.Load(), h.Fake.Starts.Load())
	}

	list, err := h.Client.ListBots(h.Ctx(), connect.NewRequest(&v1.ListBotsRequest{}))
	if err != nil || len(list.Msg.GetBots()) != 1 {
		t.Fatalf("ListBots: %v %+v", err, list)
	}
	if list.Msg.GetBots()[0].GetSoul() != "" {
		t.Fatal("list must omit SOUL")
	}

	upd, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{
		Id: id, Name: "Beta", Description: "desc", Soul: "be kind", Memory: "likes tea",
	}))
	if err != nil || upd.Msg.GetName() != "Beta" || upd.Msg.GetSoul() != "be kind" {
		t.Fatalf("UpdateBot: %v %+v", err, upd)
	}

	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	stopped, _ := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
	if stopped.Msg.GetStatus() != "stopped" {
		t.Fatalf("stop status %s", stopped.Msg.GetStatus())
	}
	if h.Fake.Stops.Load() == 0 {
		t.Fatal("Stop not called on docker host")
	}

	if _, err := h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", id)
	if row.ContainerID == "" {
		t.Fatal("start should keep the container id")
	}

	reset, err := h.Client.ResetContainer(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
	if err != nil || reset.Msg.GetStatus() != "stopped" {
		t.Fatalf("ResetContainer: %v %+v", err, reset)
	}
	h.DB.First(&row, "id = ?", id)
	if row.ContainerID != "" {
		t.Fatalf("reset should clear container id, got %q", row.ContainerID)
	}
	if h.Fake.Drops.Load() == 0 {
		t.Fatal("reset should drop the container")
	}

	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	var n int64
	h.DB.Model(&db.Bot{}).Count(&n)
	if n != 0 {
		t.Fatalf("bot row remains: %d", n)
	}
	h.DB.Model(&db.Chat{}).Count(&n)
	if n != 0 {
		t.Fatalf("chat rows remain: %d", n)
	}
}

func TestCreateBotRequiresName(t *testing.T) {
	h := apptest.New(t)
	if _, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{})); err == nil {
		t.Fatal("empty name should fail")
	}
}

func TestChatSendAndStreamReplay(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Chatter")
	chat := h.FirstChat(bot.GetId())

	runID, chatID := h.Send(bot.GetId(), chat, "Test_10_Input")
	if chatID != chat {
		t.Fatalf("chat id %s want %s", chatID, chat)
	}
	h.WaitRun(runID)

	// Replay persisted history through the streaming RPC.
	ctx, cancel := context.WithCancel(h.Ctx())
	defer cancel()
	stream, err := h.Client.StreamRun(ctx, connect.NewRequest(&v1.StreamRunRequest{
		BotId: bot.GetId(), ChatId: chat,
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var kinds []string
	var body strings.Builder
	for stream.Receive() {
		ev := stream.Msg()
		kinds = append(kinds, ev.GetKind())
		if ev.GetKind() == "chunk" || ev.GetKind() == "assistant" {
			body.WriteString(ev.GetBody())
		}
		if ev.GetKind() == "done" {
			break
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if !strings.Contains(body.String(), "Test_10_Output") {
		t.Fatalf("replayed body %q", body.String())
	}
	var sawUser, sawDone bool
	for _, k := range kinds {
		if k == "user" {
			sawUser = true
		}
		if k == "done" {
			sawDone = true
		}
	}
	if !sawUser || !sawDone {
		t.Fatalf("kinds %v", kinds)
	}
}

func TestAgentTextAndSections(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_11", dummy.Turn{Text: "part one<section_send />part two"})
	h := apptest.New(t)
	bot := h.CreateBot("Sections")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_11_Input")
	events := h.WaitRun(runID)

	var sections []string
	for _, ev := range events {
		if ev.Kind == "section" {
			sections = append(sections, ev.Body)
		}
	}
	if len(sections) != 2 || sections[0] != "part one" || sections[1] != "part two" {
		t.Fatalf("sections %#v", sections)
	}
	for _, s := range sections {
		if strings.Contains(s, "<section_send") {
			t.Fatalf("sentinel leaked into section %q", s)
		}
	}
}

func TestAgentToolErrorSurfaced(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_12",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "no_such_tool", Arguments: "{}"}}},
		dummy.Turn{Text: "recovered"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Errors")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_12_Input")
	events := h.WaitRun(runID)
	var result string
	for _, ev := range events {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "unknown tool") {
		t.Fatalf("tool_result %q", result)
	}
	if !strings.Contains(h.RunBody(runID), "recovered") {
		t.Fatal("loop should continue after a tool error")
	}
}

func TestAgentInjectWhileRunning(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_13",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"sleep 0.6"}`}}},
		dummy.Turn{Text: "Test_13_Output"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Steering")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())

	runID, _ := h.Send(bot.GetId(), chat, "Test_13_Input")
	// The terminal command sleeps, so a second message should inject into the
	// live run rather than start a parallel one.
	runID2, _ := h.Send(bot.GetId(), chat, "steer now")
	if runID2 != runID {
		t.Fatalf("second send started a new run: %s vs %s", runID2, runID)
	}
	h.WaitRun(runID)
	var userEvents int
	for _, ev := range h.Events(runID) {
		if ev.Kind == "user" {
			userEvents++
		}
	}
	if userEvents != 2 {
		t.Fatalf("expected 2 user events (opening + inject), got %d", userEvents)
	}
}

func TestRulesDenyBlocksTool(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_14",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo nope"}`}}},
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Rules")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "deny",
	})); err != nil {
		t.Fatal(err)
	}
	runID, _ := h.Send(bot.GetId(), chat, "Test_14_Input")
	events := h.WaitRun(runID)
	var result string
	for _, ev := range events {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "denied") {
		t.Fatalf("tool_result %q", result)
	}
}

func TestApprovalAllowOnce(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_15",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo approved"}`}}},
		dummy.Turn{Text: "finished"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Approval")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "ask",
	})); err != nil {
		t.Fatal(err)
	}

	runID, _ := h.Send(bot.GetId(), chat, "Test_15_Input")
	h.WaitBotStatus(bot.GetId(), "needs_you")
	ap := h.WaitApproval(bot.GetId())
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{
		Id: ap.GetId(), Decision: "allow_once",
	})); err != nil {
		t.Fatal(err)
	}
	events := h.WaitRun(runID)
	var result string
	for _, ev := range events {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "approved") {
		t.Fatalf("approved tool result %q", result)
	}
	var row db.Approval
	h.DB.First(&row, "id = ?", ap.GetId())
	if row.Status != "allow_once" {
		t.Fatalf("approval status %q", row.Status)
	}
}

func TestApprovalDeny(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_16",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo secret"}`}}},
		dummy.Turn{Text: "gave up"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("DenyApproval")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "ask",
	}))
	runID, _ := h.Send(bot.GetId(), chat, "Test_16_Input")
	ap := h.WaitApproval(bot.GetId())
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{
		Id: ap.GetId(), Decision: "deny",
	})); err != nil {
		t.Fatal(err)
	}
	events := h.WaitRun(runID)
	var result string
	for _, ev := range events {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "denied") {
		t.Fatalf("denied tool result %q", result)
	}
}

// A decision leaves a persisted receipt in the run log so the thread can show
// it after a reload: a "decision" event carrying the vote and the catalog
// title, emitted before the tool resumes.
func TestApprovalDecisionReceipt(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_16r",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo secret"}`}}},
		dummy.Turn{Text: "gave up"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Receipt")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "ask",
	}))
	runID, _ := h.Send(bot.GetId(), chat, "Test_16r_Input")
	ap := h.WaitApproval(bot.GetId())
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{
		Id: ap.GetId(), Decision: "deny",
	})); err != nil {
		t.Fatal(err)
	}
	events := h.WaitRun(runID)
	decision, result := -1, -1
	var body map[string]string
	for i, ev := range events {
		switch ev.Kind {
		case "decision":
			decision = i
			if ev.Tool != "terminal.run" {
				t.Fatalf("decision tool %q", ev.Tool)
			}
			if err := json.Unmarshal([]byte(ev.Body), &body); err != nil {
				t.Fatalf("decision body %q: %v", ev.Body, err)
			}
		case "tool_result":
			result = i
		}
	}
	if decision < 0 {
		t.Fatal("no decision event in the run log")
	}
	if result >= 0 && result < decision {
		t.Fatalf("decision at %d after tool_result at %d", decision, result)
	}
	if body["decision"] != "deny" || body["approval_id"] != ap.GetId() || body["title"] == "" {
		t.Fatalf("decision body %v", body)
	}
}

func TestApprovalAlwaysWritesRule(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_17",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo hi"}`}}},
		dummy.Turn{Text: "ok"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Always")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "ask",
	}))
	runID, _ := h.Send(bot.GetId(), chat, "Test_17_Input")
	ap := h.WaitApproval(bot.GetId())
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{
		Id: ap.GetId(), Decision: "always",
	})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(runID)
	var rule db.Rule
	h.DB.First(&rule, "bot_id = ? AND connector = ? AND action = ?", bot.GetId(), "terminal", "run")
	if rule.Decision != "allow" {
		t.Fatalf("rule after always = %q", rule.Decision)
	}
}

func TestModelListAndSwitch(t *testing.T) {
	yaml := apptest.DefaultYAML(t.TempDir())
	yaml = strings.Replace(yaml, "  - dummy/echo\n", "  - dummy/echo\n  - dummy/alt\n", 1)
	h := apptest.New(t, apptest.WithYAML(yaml))
	bot := h.CreateBot("Models")
	chat := h.FirstChat(bot.GetId())

	models, err := h.Client.ListModels(h.Ctx(), connect.NewRequest(&v1.ListModelsRequest{BotId: bot.GetId()}))
	if err != nil || len(models.Msg.GetModels()) != 2 {
		t.Fatalf("ListModels: %v %+v", err, models)
	}
	if _, err := h.Client.SetChatModel(h.Ctx(), connect.NewRequest(&v1.SetChatModelRequest{
		BotId: bot.GetId(), ChatId: chat, Model: "dummy/alt",
	})); err != nil {
		t.Fatal(err)
	}
	var row db.Chat
	h.DB.First(&row, "id = ?", chat)
	if row.Model != "dummy/alt" {
		t.Fatalf("chat model %q", row.Model)
	}
	if _, err := h.Client.SetChatModel(h.Ctx(), connect.NewRequest(&v1.SetChatModelRequest{
		BotId: bot.GetId(), ChatId: chat, Model: "dummy/nope",
	})); err == nil {
		t.Fatal("disallowed model should fail")
	}
}

func TestSwitchModelTool(t *testing.T) {
	yaml := apptest.DefaultYAML(t.TempDir())
	yaml = strings.Replace(yaml, "  - dummy/echo\n", "  - dummy/echo\n  - dummy/alt\n", 1)
	dummy.Reset()
	dummy.Script("Test_18",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "switch_model", Arguments: `{"model":"dummy/alt"}`}}},
		dummy.Turn{Text: "switched"},
	)
	h := apptest.New(t, apptest.WithYAML(yaml))
	bot := h.CreateBot("Switch")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_18_Input")
	h.WaitRun(runID)
	var row db.Chat
	h.DB.First(&row, "id = ?", chat)
	if row.Model != "dummy/alt" {
		t.Fatalf("chat model after switch_model = %q", row.Model)
	}
}
