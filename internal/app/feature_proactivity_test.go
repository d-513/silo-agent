package app_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

func heartbeat(t *testing.T, h *apptest.H, botID string) db.Automation {
	t.Helper()
	var au db.Automation
	if err := h.DB.Where("bot_id = ? AND kind = ?", botID, "heartbeat").First(&au).Error; err != nil {
		t.Fatal(err)
	}
	return au
}

// The proactivity level is one choice: the column and the Heartbeat's
// schedule move together, and a hand-set schedule reads as custom.
func TestProactivityLevels(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Dial")
	id := bot.GetId()
	if bot.GetProactivity() != "off" || bot.GetHeartbeatCustom() {
		t.Fatalf("a new Bot: %q custom=%v", bot.GetProactivity(), bot.GetHeartbeatCustom())
	}
	set := func(level string) *v1.Bot {
		t.Helper()
		res, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Name: "Dial", Proactivity: &level}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	for level, sched := range map[string]string{"active": "0 9-21/3 * * *", "quiet": "0 9 * * *", "off": ""} {
		got := set(level)
		if got.GetProactivity() != level || got.GetHeartbeatCustom() {
			t.Fatalf("%s: %+v", level, got)
		}
		if hb := heartbeat(t, h, id); hb.Schedule != sched {
			t.Fatalf("%s: heartbeat %q, want %q", level, hb.Schedule, sched)
		}
	}
	h.DB.Model(&db.Automation{}).Where("bot_id = ? AND kind = ?", id, "heartbeat").Update("schedule", "0 7 * * 1")
	if got, _ := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); !got.Msg.GetHeartbeatCustom() {
		t.Fatal("a hand-set Heartbeat does not read as custom")
	}
	bad := "loud"
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Name: "Dial", Proactivity: &bad})); err == nil {
		t.Fatal("an unknown level was taken")
	}
}

// Leaving the interview on opens the main chat with it; /onboard runs it
// again. Its opening turn is Silo's, not the owner's.
func TestInterview(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	res, err := h.Client.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Newcomer", Interview: true}))
	if err != nil {
		t.Fatal(err)
	}
	id := res.Msg.GetId()
	main := h.FirstChat(id)
	deadline := time.Now().Add(5 * time.Second)
	var opening db.RunEvent
	for opening.ID == "" {
		if ev, ok := h.FindLastUserEvent(main); ok {
			opening = ev
		} else if time.Now().After(deadline) {
			t.Fatal("the interview did not start")
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if opening.Tool != "onboarding" {
		t.Fatalf("the opening turn is marked %q", opening.Tool)
	}
	var run db.Run
	h.DB.Where("id = ?", opening.RunID).First(&run)
	h.WaitRun(run.ID)

	again, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: main, Text: "/onboard"}))
	if err != nil || again.Msg.GetRunId() == "" {
		t.Fatalf("/onboard: %v %+v", err, again)
	}
	h.WaitRun(again.Msg.GetRunId())

	plain := h.CreateBot("Plain").GetId()
	time.Sleep(100 * time.Millisecond)
	if _, ok := h.FindLastUserEvent(h.FirstChat(plain)); ok {
		t.Fatal("a Bot made without the interview started one")
	}
}

// The Bot row says what is running and in which tool, for the sidebar.
func TestBotActivity(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_act1",
		memCall("sleep", `{"seconds":30}`),
		dummy.Turn{Text: "awake"},
	)
	h := apptest.New(t)
	id := h.CreateBot("Busy").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_act1_Input")
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
		if err != nil {
			t.Fatal(err)
		}
		if a := got.Msg.GetActivity(); len(a) == 1 && a[0].GetStep() == "sleep" {
			if a[0].GetKind() != "main" || a[0].GetChatId() != main {
				t.Fatalf("activity %+v", a[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no activity: %+v", got.Msg.GetActivity())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{BotId: id, ChatId: main})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(run)
	got, _ := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
	if len(got.Msg.GetActivity()) != 0 {
		t.Fatalf("activity after the run: %+v", got.Msg.GetActivity())
	}
}

// set_proactivity from a chat: a level that is not one is refused with the
// levels named, and nothing moves; a good one moves the column and the
// Heartbeat together.
func TestSetProactivityTool(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_pro1",
		memCall("set_proactivity", `{"level":"loud"}`),
		memCall("set_proactivity", `{"level":" Quiet "}`),
		dummy.Turn{Text: "set"},
	)
	h := apptest.New(t, apptest.WithChatTools())
	id := h.CreateBot("Dial").GetId()
	run, _ := h.Send(id, h.FirstChat(id), "Test_pro1_Input")
	var results []string
	for _, ev := range h.WaitRun(run) {
		if ev.Kind == "tool_result" && ev.Tool == "set_proactivity" {
			results = append(results, ev.Body)
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "off, quiet, active") {
		t.Fatalf("results: %q", results)
	}
	if !strings.Contains(results[1], `"proactivity":"quiet"`) {
		t.Fatalf("a good level: %q", results[1])
	}
	var b db.Bot
	h.DB.First(&b, "id = ?", id)
	if b.Proactivity != "quiet" || heartbeat(t, h, id).Schedule != "0 9 * * *" {
		t.Fatalf("after set: %q %q", b.Proactivity, heartbeat(t, h, id).Schedule)
	}
}

// A subagent may not change its Bot's proactivity: only the lead, on the
// owner's word.
func TestSetProactivityIsLeadOnly(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Dial").GetId()
	var row db.Bot
	h.DB.First(&row, "id = ?", id)
	d, err := h.App.Tools.ByAction(id, "bot", "set_proactivity")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.App.Tools.Run(h.Ctx(), &tool.Call{Surface: tool.Python, Bot: &row, Def: d, Args: map[string]any{"level": "active"}, Subagent: true}); err == nil || !strings.Contains(err.Error(), "only the lead") {
		t.Fatalf("a subagent set proactivity: %v", err)
	}
	h.DB.First(&row, "id = ?", id)
	if row.Proactivity != "off" {
		t.Fatalf("proactivity %q", row.Proactivity)
	}
}

// An UpdateBot with a level that is not one saves nothing at all, not even
// the other fields it carried.
func TestUpdateBotBadProactivitySavesNothing(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Dial").GetId()
	bad := "loud"
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Name: "Renamed", Soul: "new soul", Proactivity: &bad})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad level: %v", err)
	}
	var b db.Bot
	h.DB.First(&b, "id = ?", id)
	if b.Name != "Dial" || b.Soul == "new soul" || b.Proactivity != "off" {
		t.Fatalf("saved anyway: %q %q %q", b.Name, b.Soul, b.Proactivity)
	}
}

// UpdateBot does not write proactivity through the whole row: a level the
// Bot set meanwhile survives a save that does not name one.
func TestUpdateBotKeepsProactivity(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Dial").GetId()
	if _, _, err := h.App.Proactivity.Set(id, "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.UpdateBot(h.Ctx(), connect.NewRequest(&v1.UpdateBotRequest{Id: id, Name: "Dial"})); err != nil {
		t.Fatal(err)
	}
	var b db.Bot
	h.DB.First(&b, "id = ?", id)
	if b.Proactivity != "active" {
		t.Fatalf("proactivity %q", b.Proactivity)
	}
}

// /onboard is refused while a reply runs in the chat.
func TestOnboardRefusedWhileReplyRuns(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_onb1", dummy.Turn{Text: "slow", Delay: 2 * time.Second})
	h := apptest.New(t)
	id := h.CreateBot("Busy").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_onb1_Input")
	_, err := h.Client.Send(h.Ctx(), connect.NewRequest(&v1.SendRequest{BotId: id, ChatId: main, Text: "/onboard"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "reply is running") {
		t.Fatalf("/onboard during a reply: %v", err)
	}
	h.WaitRun(run)
	for _, ev := range h.Events(run) {
		if ev.Kind == "user" && ev.Tool == "onboarding" {
			t.Fatal("the interview joined the running reply")
		}
	}
}

// The activity step names the tool only while it runs: once its result is
// in and the model writes again, the step is empty.
func TestBotActivityStepClears(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_act2",
		memCall("sleep", `{"seconds":1}`),
		dummy.Turn{Text: "awake", Delay: 3 * time.Second},
	)
	h := apptest.New(t)
	id := h.CreateBot("Busy").GetId()
	main := h.FirstChat(id)
	run, _ := h.Send(id, main, "Test_act2_Input")
	step := func() (string, bool) {
		got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id}))
		if err != nil {
			t.Fatal(err)
		}
		a := got.Msg.GetActivity()
		if len(a) != 1 {
			return "", false
		}
		return a[0].GetStep(), true
	}
	deadline := time.Now().Add(5 * time.Second)
	sawTool := false
	for {
		s, live := step()
		if live && s == "sleep" {
			sawTool = true
		}
		if live && s == "" && sawTool {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("step did not clear (saw tool %v, last %q live %v)", sawTool, s, live)
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.WaitRun(run)
}

// Every pinned kind the App registers is covered by the automations_pinned
// index, both in the list it is made from and in the database.
func TestPinnedKindsAreIndexed(t *testing.T) {
	h := apptest.New(t)
	var def string
	h.DB.Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'automations_pinned'").Scan(&def)
	if def == "" {
		t.Fatal("no automations_pinned index")
	}
	kinds := h.App.Automations.Kinds()
	if len(kinds) < 2 {
		t.Fatalf("pinned kinds: %v", kinds)
	}
	for _, k := range kinds {
		if !slices.Contains(db.PinnedAutomationKinds, k) {
			t.Errorf("pinned kind %q is not in db.PinnedAutomationKinds", k)
		}
		if !strings.Contains(def, "'"+k+"'") {
			t.Errorf("pinned kind %q is not in the index: %s", k, def)
		}
	}
}
