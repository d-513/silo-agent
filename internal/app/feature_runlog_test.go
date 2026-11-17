package app_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func listRuns(t *testing.T, h *apptest.H, botID string, kinds ...string) []*v1.RunRow {
	t.Helper()
	res, err := h.Client.ListRuns(h.Ctx(), connect.NewRequest(&v1.ListRunsRequest{BotId: botID, Kinds: kinds}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetRuns()
}

func runRow(t *testing.T, h *apptest.H, botID, runID string) *v1.RunRow {
	t.Helper()
	for _, r := range listRuns(t, h, botID) {
		if r.GetId() == runID {
			return r
		}
	}
	t.Fatalf("run %s is not listed", runID)
	return nil
}

func countOf(cs []*v1.RunCount, name string) int32 {
	for _, c := range cs {
		if c.GetName() == name {
			return c.GetCount()
		}
	}
	return 0
}

// runCalls waits for a run's side call of a label: a title is written beside
// the run and can land after it ends.
func runCalls(t *testing.T, h *apptest.H, botID, runID, label string) *v1.ModelCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := h.Client.GetRunCalls(h.Ctx(), connect.NewRequest(&v1.GetRunCallsRequest{BotId: botID, RunId: runID}))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Msg.GetCalls() {
			if c.GetLabel() == label {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s has no %q call: %v", runID, label, res.Msg.GetCalls())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A run is listed with what it was, what started it and what it came to, and
// the model calls made for it beside its turns are kept in its log.
func TestRunsListWhatABotDid(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_Posts", memCall("feed", `{"text":"The summary."}`), dummy.Turn{Text: "done"})
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+apptest.ChatTools))
	bot := h.CreateBot("Review")
	setAuto(t, h, bot.GetId(), "Feed posts are fine. Test_Approve", "bot", "feed")
	// A side chat, since only a chat with no title yet is named.
	runID, chatID := h.Send(bot.GetId(), newChat(t, h, bot.GetId()), "Test_Posts_Input post the summary\nand a second line")
	h.WaitRun(runID)

	r := runRow(t, h, bot.GetId(), runID)
	if r.GetKind() != "chat" || r.GetTrigger() != "message" || r.GetStatus() != "done" {
		t.Fatalf("row %+v", r)
	}
	if r.GetTitle() != "Test_Posts_Input post the summary" || r.GetModel() == "" || r.GetChatId() != chatID || r.GetSourceKind() != "chat" {
		t.Fatalf("row %+v", r)
	}
	sum := r.GetSummary()
	if sum == nil || sum.GetTurns() != 2 || countOf(sum.GetTools(), "feed") != 1 {
		t.Fatalf("summary %+v", sum)
	}
	// The judge that approved the post is the run's, with its verdict.
	judge := runCalls(t, h, bot.GetId(), runID, "approval")
	if judge.GetVerdict() == "" || judge.GetModel() == "" {
		t.Fatalf("judge %+v", judge)
	}
	runCalls(t, h, bot.GetId(), runID, "title")
	if got := runRow(t, h, bot.GetId(), runID).GetSummary().GetSideCalls(); got < 1 {
		t.Fatalf("side calls %d", got)
	}
	next, _ := h.Send(bot.GetId(), chatID, "and again")
	h.WaitRun(next)
	// Nothing of it reaches a thread: it is the managers' to review here.
	page, err := h.Client.ListChatEvents(h.Ctx(), connect.NewRequest(&v1.ListChatEventsRequest{
		BotId: bot.GetId(), ChatId: chatID, BeforeEventId: h.LastUserEvent(chatID).ID,
	}))
	if err != nil || len(page.Msg.GetEvents()) == 0 {
		t.Fatalf("the thread's first page: %v %v", page, err)
	}
	for _, ev := range page.Msg.GetEvents() {
		if ev.GetKind() == "side" {
			t.Fatalf("a side call is in the thread: %+v", ev)
		}
	}
	if rows := listRuns(t, h, bot.GetId()); len(rows) != 2 || rows[0].GetId() != next {
		t.Fatalf("newest first: %+v", rows)
	}
}

// A model call made for no run changes nothing a history has to explain: it
// is a line in memory and no row anywhere.
func TestCallsOutsideARunStayInMemory(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Auto")
	setAutoPolicy(t, h, bot.GetId(), "Auto-approve clicks. Test_Approve")
	wc := h.WorkerClient(bot.GetId())
	for range 2 {
		if _, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`})); err != nil {
			t.Fatal(err)
		}
	}
	res, err := h.Client.ListBackground(h.Ctx(), connect.NewRequest(&v1.ListBackgroundRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	// Two like calls in a burst are one line.
	calls := res.Msg.GetCalls()
	if len(calls) != 1 || calls[0].GetLabel() != "approval" || calls[0].GetCount() != 2 || calls[0].GetVerdict() == "" || res.Msg.GetSince() == "" {
		t.Fatalf("background %+v", calls)
	}
	var n int64
	h.DB.Model(&db.RunEvent{}).Where("kind = ?", "side").Count(&n)
	if n != 0 {
		t.Fatalf("%d side events were stored for calls outside a run", n)
	}
	// Another Bot's manager sees none of it.
	other := h.CreateBot("Other")
	res, err = h.Client.ListBackground(h.Ctx(), connect.NewRequest(&v1.ListBackgroundRequest{BotId: other.GetId()}))
	if err != nil || len(res.Msg.GetCalls()) != 0 {
		t.Fatalf("other Bot's background: %v %v", res.Msg.GetCalls(), err)
	}
}

// Runs are told apart by kind, and a list can be narrowed to some.
func TestRunsAreListedByKind(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Kinds")
	chat := h.FirstChat(bot.GetId())
	runID, _ := h.Send(bot.GetId(), chat, "hello")
	h.WaitRun(runID)
	if _, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: bot.GetId(), ChatId: chat, Text: "/clear"})); err != nil {
		t.Fatal(err)
	}
	au := createAutomation(t, h, bot.GetId(), "Nightly check", "sweep the inbox", "")
	fired, err := h.Client.RunAutomation(h.Ctx(), connect.NewRequest(&v1.RunAutomationRequest{BotId: bot.GetId(), Id: au.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitRun(fired.Msg.GetRunId())

	got := map[string]int{}
	for _, r := range listRuns(t, h, bot.GetId()) {
		got[r.GetKind()]++
	}
	if got["chat"] != 1 || got["command"] != 1 || got["automation"] != 1 {
		t.Fatalf("kinds %v", got)
	}
	only := listRuns(t, h, bot.GetId(), "automation", "command")
	if len(only) != 2 {
		t.Fatalf("narrowed to two kinds: %+v", only)
	}
	for _, r := range only {
		if r.GetKind() == "automation" && (r.GetTitle() != "Nightly check" || r.GetAutomationId() != au.GetId() || r.GetSourceKind() != "automation" || r.GetTrigger() != "automation") {
			t.Fatalf("automation row %+v", r)
		}
	}
}

// Only a Bot's managers review its runs: they hold what other people's chats
// asked for.
func TestRunsAreTheManagers(t *testing.T) {
	h := apptest.New(t)
	bot, user, _ := team(t, h)
	if _, err := user.ListRuns(h.Ctx(), connect.NewRequest(&v1.ListRunsRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a user listed the runs: %v", err)
	}
	if _, err := user.ListBackground(h.Ctx(), connect.NewRequest(&v1.ListBackgroundRequest{BotId: bot.GetId()})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a user listed the background calls: %v", err)
	}
}
