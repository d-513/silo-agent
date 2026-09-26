package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func listAutomations(t *testing.T, h *apptest.H, botID string) []*v1.Automation {
	t.Helper()
	res, err := h.Client.ListAutomations(h.Ctx(), connect.NewRequest(&v1.ListAutomationsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetAutomations()
}

func createAutomation(t *testing.T, h *apptest.H, botID, name, prompt, schedule string) *v1.Automation {
	t.Helper()
	res, err := h.Client.CreateAutomation(h.Ctx(), connect.NewRequest(&v1.CreateAutomationRequest{
		BotId: botID, Name: name, Prompt: prompt, Schedule: schedule, Enabled: true,
	}))
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	return res.Msg
}

func TestAutomationHeartbeatIsPinnedAndIdle(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Pulse")
	list := listAutomations(t, h, bot.GetId())
	if len(list) != 1 || list[0].GetKind() != "heartbeat" {
		t.Fatalf("new bot should have exactly the heartbeat, got %v", list)
	}
	hb := list[0]
	if hb.GetSchedule() != "" || hb.GetNextRunAt() != "" || hb.GetPrompt() == "" {
		t.Fatalf("heartbeat should start with a prompt and no timer: %v", hb)
	}
	if _, err := h.Client.DeleteAutomation(h.Ctx(), connect.NewRequest(&v1.DeleteAutomationRequest{BotId: bot.GetId(), Id: hb.GetId()})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("heartbeat delete should be refused, got %v", err)
	}
	// Its log chat is not a web chat.
	chats, _ := h.Client.ListChats(h.Ctx(), connect.NewRequest(&v1.ListChatsRequest{BotId: bot.GetId()}))
	for _, c := range chats.Msg.GetChats() {
		if c.GetId() == hb.GetChatId() {
			t.Fatal("automation log leaked into the chat list")
		}
	}
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.GetId(), ChatId: hb.GetChatId(), Text: "hi"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("sending into an automation log should be refused, got %v", err)
	}
	// A second list does not duplicate it.
	if n := len(listAutomations(t, h, bot.GetId())); n != 1 {
		t.Fatalf("heartbeat duplicated: %d", n)
	}
}

func TestAutomationValidationAndSchedule(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Clock")
	for _, bad := range []string{"not cron", "* * * * *", "*/2 * * * *"} {
		_, err := h.Client.CreateAutomation(h.Ctx(), connect.NewRequest(&v1.CreateAutomationRequest{
			BotId: bot.GetId(), Name: "x " + bad, Prompt: "p", Schedule: bad, Enabled: true,
		}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("schedule %q should be rejected, got %v", bad, err)
		}
	}
	au := createAutomation(t, h, bot.GetId(), "Morning digest", "summarize", "0 9 * * 1-5")
	next, err := time.Parse(time.RFC3339, au.GetNextRunAt())
	if err != nil || !next.After(time.Now()) || next.Local().Hour() != 9 {
		t.Fatalf("next run %q", au.GetNextRunAt())
	}
	if _, err := h.Client.CreateAutomation(h.Ctx(), connect.NewRequest(&v1.CreateAutomationRequest{
		BotId: bot.GetId(), Name: "morning DIGEST", Prompt: "p", Enabled: true,
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("duplicate name should be rejected, got %v", err)
	}
	paused, err := h.Client.UpdateAutomation(h.Ctx(), connect.NewRequest(&v1.UpdateAutomationRequest{
		BotId: bot.GetId(), Id: au.GetId(), Name: au.GetName(), Prompt: au.GetPrompt(), Schedule: au.GetSchedule(), Enabled: false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if paused.Msg.GetNextRunAt() != "" {
		t.Fatal("a paused automation must not have a next run")
	}
	if _, err := h.Client.DeleteAutomation(h.Ctx(), connect.NewRequest(&v1.DeleteAutomationRequest{BotId: bot.GetId(), Id: au.GetId()})); err != nil {
		t.Fatal(err)
	}
	var n int64
	h.DB.Model(&db.Chat{}).Where("id = ?", au.GetChatId()).Count(&n)
	if n != 0 {
		t.Fatal("deleting an automation should drop its log chat")
	}
}

func TestAutomationRunsWithFreshContextInItsLog(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Fresh")
	au := createAutomation(t, h, bot.GetId(), "Echo", "Test_80_Input", "")
	for i := 0; i < 2; i++ {
		res, err := h.Client.RunAutomation(h.Ctx(), connect.NewRequest(&v1.RunAutomationRequest{BotId: bot.GetId(), Id: au.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		if res.Msg.GetChatId() != au.GetChatId() {
			t.Fatal("run should land in the automation's log chat")
		}
		h.WaitRun(res.Msg.GetRunId())
		// A replayed earlier run would make the dummy answer its *_End turn.
		if body := h.RunBody(res.Msg.GetRunId()); !strings.Contains(body, "Test_80_Output") {
			t.Fatalf("run %d should see only its own prompt, got %q", i, body)
		}
		var run db.Run
		h.DB.First(&run, "id = ?", res.Msg.GetRunId())
		if run.Origin != "automation" {
			t.Fatalf("origin %q", run.Origin)
		}
	}
	got := listAutomations(t, h, bot.GetId())
	for _, a := range got {
		if a.GetId() == au.GetId() && (a.GetLastStatus() != "done" || a.GetLastRunAt() == "") {
			t.Fatalf("last run not recorded: %v", a)
		}
	}
}

func TestAutomationSchedulerFiresDueOnce(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Tick")
	au := createAutomation(t, h, bot.GetId(), "Hourly", "Test_81_Input", "0 * * * *")
	past := time.Now().Add(-time.Minute)
	h.DB.Model(&db.Automation{}).Where("id = ?", au.GetId()).Update("next_run_at", past)

	if n := h.App.FireDueAutomations(time.Now()); n != 1 {
		t.Fatalf("fired %d, want 1", n)
	}
	if n := h.App.FireDueAutomations(time.Now()); n != 0 {
		t.Fatalf("fired again: %d", n)
	}
	var row db.Automation
	h.DB.First(&row, "id = ?", au.GetId())
	if row.NextRunAt == nil || !row.NextRunAt.After(time.Now()) {
		t.Fatalf("next run not advanced: %v", row.NextRunAt)
	}
	h.WaitRun(row.LastRunID)
	if body := h.RunBody(row.LastRunID); !strings.Contains(body, "Test_81_Output") {
		t.Fatalf("scheduled run body %q", body)
	}
}

func TestAutomationToolsCreateListUpdateDelete(t *testing.T) {
	dummy.Reset()
	call := func(name, args string) dummy.Turn {
		return dummy.Turn{ToolCalls: []llm.ToolCall{{Name: name, Arguments: args}}}
	}
	dummy.Script("Test_82",
		call("create_automation", `{"name":"Inbox sweep","prompt":"check the inbox","schedule":"*/30 * * * *"}`),
		call("update_automation", `{"automation":"heartbeat","schedule":"0 */6 * * *"}`),
		call("list_automations", `{}`),
		call("delete_automation", `{"automation":"Inbox sweep"}`),
		call("delete_automation", `{"automation":"Heartbeat"}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("Scheduler")
	run, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_82_Input")
	// create and update ask by default: they keep running unattended.
	for i := 0; i < 2; i++ {
		ap := h.WaitApproval(bot.GetId())
		if ap.GetConnector() != "automations" {
			t.Fatalf("approval %v", ap)
		}
		if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var n int64
			h.DB.Model(&db.Approval{}).Where("id = ? AND status = 'pending'", ap.GetId()).Count(&n)
			if n == 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	res := memResults(h.WaitRun(run))
	if len(res) != 5 {
		t.Fatalf("tool results %q", res)
	}
	if !strings.HasPrefix(res[0], "created") || !strings.HasPrefix(res[1], "updated") {
		t.Fatalf("create/update: %q", res[:2])
	}
	if !strings.Contains(res[2], "Inbox sweep") || !strings.Contains(res[2], "0 */6 * * *") {
		t.Fatalf("list should show both:\n%s", res[2])
	}
	if !strings.HasPrefix(res[3], "deleted") || !strings.Contains(res[4], "pinned") {
		t.Fatalf("delete results %q", res[3:])
	}
	list := listAutomations(t, h, bot.GetId())
	if len(list) != 1 || list[0].GetSchedule() != "0 */6 * * *" || list[0].GetNextRunAt() == "" {
		t.Fatalf("after tools: %v", list)
	}
}
