package app_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

// askTerminal makes the terminal wait for a human, and scripts a run that
// reaches for it.
func askTerminal(t *testing.T, h *apptest.H, botID, script string) {
	t.Helper()
	dummy.Script(script,
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo hi"}`}}},
		dummy.Turn{Text: "finished"},
	)
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: botID, Connector: "terminal", Action: "run", Decision: "ask"})); err != nil {
		t.Fatal(err)
	}
}

func viewOf(t *testing.T, h *apptest.H, c silov1connect.UIClient, botID string) *v1.Bot {
	t.Helper()
	res, err := c.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func sendNewChat(t *testing.T, h *apptest.H, botID, text string) *v1.SendResponse {
	t.Helper()
	res, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: botID, NewChat: true, Text: text}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

// Another run starting must not take back that a run waits for the human.
func TestNeedsYouOutlivesANewRun(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_ns2", dummy.Turn{Text: "busy", Hold: 2 * time.Second})
	h := apptest.New(t)
	bot := h.CreateBot("Status")
	h.StartWorker(bot.GetId())
	askTerminal(t, h, bot.GetId(), "Test_ns1")

	_, chat := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_ns1_Input")
	h.WaitBotStatus(bot.GetId(), "needs_you")
	other := sendNewChat(t, h, bot.GetId(), "Test_ns2_Input")

	got := viewOf(t, h, h.Client, bot.GetId())
	if got.GetStatus() != "needs_you" {
		t.Fatalf("status %q with an approval waiting and another run live", got.GetStatus())
	}
	states := map[string]string{}
	for _, a := range got.GetActivity() {
		states[a.GetChatId()] = a.GetState()
	}
	if states[chat] != "needs_you" || states[other.GetChatId()] != "working" {
		t.Fatalf("activity %v: want each conversation's own state", got.GetActivity())
	}
	if got.GetMachine() != "online" {
		t.Fatalf("machine %q", got.GetMachine())
	}
}

// The status follows the run, with nothing stored to go stale.
func TestStatusComesFromTheRun(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_ns3", dummy.Turn{Text: "busy", Hold: 2 * time.Second})
	h := apptest.New(t)
	bot := h.CreateBot("Status")
	h.StartWorker(bot.GetId())
	h.WaitBotStatus(bot.GetId(), "online")

	runID, _ := h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_ns3_Input")
	h.WaitBotStatus(bot.GetId(), "working")
	h.WaitRun(runID)
	h.WaitBotStatus(bot.GetId(), "online")
}

// Needs you is for who can answer; everyone else sees it wait on them, and
// nothing of a chat they cannot read.
func TestNeedsYouIsPerPerson(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot, bob, _ := team(t, h)
	h.StartWorker(bot.Id)
	askTerminal(t, h, bot.Id, "Test_ns4")

	h.Send(bot.Id, h.FirstChat(bot.Id), "Test_ns4_Input")
	h.WaitBotStatus(bot.Id, "needs_you")

	view := viewOf(t, h, bob, bot.Id)
	if view.GetStatus() != "waiting" {
		t.Fatalf("bob sees %q; he may not answer it", view.GetStatus())
	}
	if len(view.GetActivity()) != 1 {
		t.Fatalf("activity %v", view.GetActivity())
	}
	row := view.GetActivity()[0]
	if row.GetKind() != "hidden" || row.GetState() != "waiting" || !strings.Contains(row.GetWaitingOn(), "@") {
		t.Fatalf("bob's row %+v", row)
	}
	if row.GetChatId() != "" || row.GetTitle() != "" || row.GetStep() != "" || row.GetTask() != "" {
		t.Fatalf("a chat bob cannot read leaked into his row: %+v", row)
	}
}

func TestActivityNamesTheTask(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_ns5",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "task_add", Arguments: `{"tasks":["Compare flights","Book the hotel"]}`}}},
		dummy.Turn{Text: "on it", Hold: 3 * time.Second},
	)
	h := apptest.New(t, apptest.WithChatTools())
	bot := h.CreateBot("Tasks")
	h.StartWorker(bot.GetId())
	h.Send(bot.GetId(), h.FirstChat(bot.GetId()), "Test_ns5_Input")

	waitFor(t, func() bool {
		a := viewOf(t, h, h.Client, bot.GetId()).GetActivity()
		return len(a) == 1 && a[0].GetTask() == "Compare flights" && a[0].GetTaskDone() == 0 && a[0].GetTaskTotal() == 2
	}, "the run's task on its row")
}

func TestActivityLeavesAnOlderBoardOut(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_ns6", dummy.Turn{Text: "busy", Hold: 2 * time.Second})
	h := apptest.New(t)
	bot := h.CreateBot("Tasks")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())
	h.DB.Create(&db.TaskItem{ID: ids.New(), ChatID: chat, N: 1, Text: "Last week's job", CreatedAt: time.Now().Add(-time.Hour)})

	h.Send(bot.GetId(), chat, "Test_ns6_Input")
	got := h.WaitBotStatus(bot.GetId(), "working")
	if a := got.GetActivity(); len(a) != 1 || a[0].GetTask() != "" {
		t.Fatalf("activity %v: a board this run did not touch is not what it is on", a)
	}
}

// A call waits with the human: on a slip it says so, and on the answer it
// goes back to work. Everyone on the Bot can see it is on a call.
func TestVoiceCallNeedsYou(t *testing.T) {
	dummy.Reset()
	h := voiceHarness(t)
	id := h.CreateBot("Talker").GetId()
	h.StartWorker(id)
	askTerminal(t, h, id, "Test_ns7")
	c := dialCall(t, h, id, "")
	if got := viewOf(t, h, h.Client, id); got.GetStatus() != "on_call" {
		t.Fatalf("status %q during a call", got.GetStatus())
	}

	c.speak("Test_ns7 run it")
	for c.expect("state")["state"] != "needs_you" {
	}
	ap := h.WaitApproval(id)
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	for c.expect("state")["state"] == "needs_you" {
	}
}
