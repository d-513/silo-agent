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
