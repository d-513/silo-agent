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

// runStatus is a run's stored status.
func runStatus(t *testing.T, h *apptest.H, runID string) string {
	t.Helper()
	var r db.Run
	h.DB.Select("status").Where("id = ?", runID).Limit(1).Find(&r)
	return r.Status
}

func doneBody(evs []db.RunEvent) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == "done" {
			return evs[i].Body
		}
	}
	return ""
}

// A message the inbox took while the turn was failing is answered: the
// inbox took it, so no other run would.
func TestFailedTurnAnswersMessageSentMeanwhile(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_rs_fail",
		dummy.Turn{Delay: 400 * time.Millisecond, Fail: "provider fell over", Text: "answered the second"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("FailInject")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_rs_fail please")
	// Sent while the turn is being written, before it fails.
	time.Sleep(150 * time.Millisecond)
	second, _ := h.Send(bot.GetId(), chat, "and then this")
	if second != runID {
		t.Fatalf("the second message started run %s beside %s", second, runID)
	}
	evs := h.WaitRun(runID)
	var kinds []string
	for _, ev := range evs {
		kinds = append(kinds, ev.Kind)
	}
	if !strings.Contains(h.RunBody(runID), "answered the second") {
		t.Fatalf("the message sent while the turn failed was not answered: %v", kinds)
	}
	if got := doneBody(evs); got != "done" {
		t.Fatalf("done = %q", got)
	}
	// The thread reads in the order the model did: the error, then the
	// message, then its answer.
	at := map[string]int{}
	for i, ev := range evs {
		switch {
		case ev.Kind == "error":
			at["error"] = i
		case ev.Kind == "user" && strings.Contains(ev.Body, "and then this"):
			at["second"] = i
		}
	}
	if at["second"] < at["error"] {
		t.Fatalf("the second message sits before the error: %v", kinds)
	}
}

// Stop Bot writes interrupted; the run unwinding afterwards does not turn it
// into stopped, and its done says what the row says.
func TestStopBotKeepsInterrupted(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_rs_halt", dummy.Turn{Delay: 5 * time.Second, Text: "too late"})
	h := apptest.New(t)
	bot := h.CreateBot("Halt")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_rs_halt")
	time.Sleep(200 * time.Millisecond)
	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	evs := h.WaitRun(runID)
	if got := runStatus(t, h, runID); got != "interrupted" {
		t.Fatalf("run status %q, want interrupted", got)
	}
	if got := doneBody(evs); got != "interrupted" {
		t.Fatalf("done = %q, want interrupted", got)
	}
}

func askAutomation(token string) {
	dummy.Script(token,
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "create_automation", Arguments: `{"name":"Sweep","prompt":"x","schedule":"*/30 * * * *"}`}}},
		dummy.Turn{Text: "ok"},
	)
}

func decide(h *apptest.H, id, vote string) error {
	_, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: id, Decision: vote}))
	return err
}

func ruleCount(h *apptest.H, botID string) int64 {
	var n int64
	h.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ? AND action = ?", botID, "automations", "create").Count(&n)
	return n
}

// An approval is answered once: a second answer changes nothing.
func TestApprovalIsSettledOnce(t *testing.T) {
	dummy.Reset()
	askAutomation("Test_rs_once")
	h := apptest.New(t)
	bot := h.CreateBot("Once")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_rs_once")
	ap := h.WaitApproval(bot.GetId())
	if err := decide(h, ap.GetId(), "deny"); err != nil {
		t.Fatal(err)
	}
	if err := decide(h, ap.GetId(), "always"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a second answer: %v", err)
	}
	h.WaitRun(runID)
	var row db.Approval
	h.DB.First(&row, "id = ?", ap.GetId())
	if row.Status != "deny" {
		t.Fatalf("approval status %q, want deny", row.Status)
	}
	if n := ruleCount(h, bot.GetId()); n != 0 {
		t.Fatalf("the late always made %d rules", n)
	}
	var decisions int64
	h.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", runID, "decision").Count(&decisions)
	if decisions != 1 {
		t.Fatalf("%d decision receipts", decisions)
	}
}

// A stopped run's approval is canceled and cannot be answered after.
func TestStoppedApprovalCannotBeAnswered(t *testing.T) {
	dummy.Reset()
	askAutomation("Test_rs_stopap")
	h := apptest.New(t)
	bot := h.CreateBot("StopAp")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "Test_rs_stopap")
	ap := h.WaitApproval(bot.GetId())
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: bot.GetId(), ChatId: chat})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(runID)
	var row db.Approval
	h.DB.First(&row, "id = ?", ap.GetId())
	if row.Status != "canceled" {
		t.Fatalf("approval status %q, want canceled", row.Status)
	}
	if err := decide(h, ap.GetId(), "always"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("answering a canceled approval: %v", err)
	}
	if n := ruleCount(h, bot.GetId()); n != 0 {
		t.Fatalf("answering a canceled approval made %d rules", n)
	}
	if got := runStatus(t, h, runID); got != "stopped" {
		t.Fatalf("run status %q", got)
	}
}
