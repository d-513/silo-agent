package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func TestDeriveStatusRules(t *testing.T) {
	cases := []struct {
		conn, run bool
		db, want  string
	}{
		{true, true, "working", "working"},
		{true, true, "needs_you", "needs_you"},
		{true, true, "starting", "online"},
		{true, false, "stopped", "online"},
		{false, true, "idle", "starting"},
		{false, false, "working", "stopped"},
	}
	for _, c := range cases {
		if got := app.DeriveStatus(c.conn, c.run, c.db); got != c.want {
			t.Fatalf("conn=%v running=%v db=%s got %s want %s", c.conn, c.run, c.db, got, c.want)
		}
	}
}

func TestLiveInspectErrorPreservesContainer(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Transient")
	before := h.Fake.Drops.Load()
	h.Fake.SetInspectError(errors.New("podman busy"))

	got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetStatus() != "starting" {
		t.Fatalf("status %q", got.Msg.GetStatus())
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", bot.GetId())
	if row.ContainerID == "" {
		t.Fatal("container id cleared on a transient inspect error")
	}
	if h.Fake.Drops.Load() != before {
		t.Fatalf("dropped on a transient error: %d", h.Fake.Drops.Load()-before)
	}
}

func TestLiveNotFoundClearsContainer(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Gone")
	h.Fake.Forget(bot.GetId())

	got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetStatus() != "stopped" {
		t.Fatalf("status %q", got.Msg.GetStatus())
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", bot.GetId())
	if row.ContainerID != "" {
		t.Fatalf("stale container id kept: %q", row.ContainerID)
	}
}

func TestLiveStaleTokenDrops(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Stale")
	before := h.Fake.Drops.Load()
	h.Fake.SetEnvHash(bot.GetId(), "box-hash")

	if _, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()})); err != nil {
		t.Fatal(err)
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", bot.GetId())
	if row.ContainerID != "" {
		t.Fatalf("stale box kept: %q", row.ContainerID)
	}
	if h.Fake.Drops.Load() != before+1 {
		t.Fatalf("drops %d", h.Fake.Drops.Load()-before)
	}
}

func TestStoppedKeepsContainer(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Stopped")
	before := h.Fake.Drops.Load()
	h.Fake.SetRunning(bot.GetId(), false)

	got, err := h.Client.GetBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetStatus() != "stopped" {
		t.Fatalf("status %q", got.Msg.GetStatus())
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", bot.GetId())
	if row.ContainerID == "" {
		t.Fatal("stopped box lost its container id")
	}
	if h.Fake.Drops.Load() != before {
		t.Fatal("stopped box was dropped")
	}
}

func TestRecoverOrphans(t *testing.T) {
	h := apptest.New(t, apptest.WithBeforeApp(func(gdb *gorm.DB) {
		gdb.Create(&db.Run{ID: "r1", BotID: "b", Status: "running"})
		gdb.Create(&db.Approval{ID: "a1", BotID: "b", Status: "pending"})
	}))
	var r db.Run
	h.DB.First(&r, "id = ?", "r1")
	if r.Status != "interrupted" {
		t.Fatalf("run %s", r.Status)
	}
	var ap db.Approval
	h.DB.First(&ap, "id = ?", "a1")
	if ap.Status != "interrupted" {
		t.Fatalf("approval %s", ap.Status)
	}
	var n int64
	h.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", "r1", "done").Count(&n)
	if n == 0 {
		t.Fatal("interrupted run has no synthetic done event")
	}
}

func TestBackfillCopiesSharedAttachment(t *testing.T) {
	h := apptest.New(t, apptest.WithBeforeApp(func(gdb *gorm.DB) {
		gdb.Create(&db.Connector{ID: "lib1", Kind: "library", Name: "Old", Type: "mcp", Transport: "http", HTTPURL: "http://x", Auth: "none"})
		gdb.Create(&db.BotConnector{ID: "bc1", BotID: "b1", ConnectorID: "lib1"})
	}))
	var bc db.BotConnector
	h.DB.First(&bc, "id = ?", "bc1")
	if bc.ConnectorID == "lib1" {
		t.Fatal("attachment still points at the library row")
	}
	var inst db.Connector
	h.DB.First(&inst, "id = ?", bc.ConnectorID)
	if inst.Kind != "custom" || inst.SourceID != "lib1" || inst.HTTPURL != "http://x" {
		t.Fatalf("backfilled copy %+v", inst)
	}
}

func TestStopRun(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_50",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"sleep 5"}`}}},
		dummy.Turn{Text: "should not be reached"},
	)
	h := apptest.New(t)
	bot := h.CreateBot("StopRun")
	h.StartWorker(bot.GetId())
	chat := h.FirstChat(bot.GetId())

	runID, _ := h.Send(bot.GetId(), chat, "Test_50_Input")
	// Wait until the terminal command is actually in flight, then stop it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int64
		h.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", runID, "tool").Count(&n)
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run never reached the tool call")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := h.Client.StopRun(h.Ctx(), connect.NewRequest(&v1.StopRunRequest{
		BotId: bot.GetId(), ChatId: chat,
	})); err != nil {
		t.Fatal(err)
	}
	h.WaitRun(runID)

	var run db.Run
	h.DB.First(&run, "id = ?", runID)
	if run.Status != "stopped" {
		t.Fatalf("run status %q", run.Status)
	}
	if !strings.Contains(h.RunBody(runID), "Stopped.") {
		t.Fatalf("body %q", h.RunBody(runID))
	}
}
