package app

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"silo.agent/internal/db"
)

func TestTruncateUTF8(t *testing.T) {
	s := strings.Repeat("a", 11999) + "€" + strings.Repeat("b", 10)
	got := truncateUTF8(s, 12000)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid truncation: %q", got)
	}
	if len(got) > 12000 {
		t.Fatalf("len %d", len(got))
	}
	if got != strings.Repeat("a", 11999) {
		t.Fatalf("expected rune-boundary backoff, got %q", got)
	}
}

func TestEmitSanitizesUTF8(t *testing.T) {
	a := testApp(t, nil)
	if err := a.DB.Create(&db.Run{ID: "r1", BotID: "b1", ChatID: "c1", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	a.emit("b1", "c1", "r1", "tool_result", "abc\xff\xfedef", "exec_python")
	var ev db.RunEvent
	if err := a.DB.Where("run_id = ? AND kind = ?", "r1", "tool_result").First(&ev).Error; err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(ev.Body) {
		t.Fatalf("stored invalid utf8: %q", ev.Body)
	}
}

func TestEnsureTerminalEventOnce(t *testing.T) {
	a := testApp(t, nil)
	run := db.Run{ID: "r1", BotID: "b1", ChatID: "c1", CreatedAt: time.Now()}
	if err := a.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	a.ensureTerminalEvent(run, "interrupted")
	a.ensureTerminalEvent(run, "interrupted")
	var n int64
	a.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", "r1", "done").Count(&n)
	if n != 1 {
		t.Fatalf("done events %d", n)
	}
}

func TestRecoverOrphansClosesRun(t *testing.T) {
	a := testApp(t, nil)
	if err := a.DB.Create(&db.Run{ID: "r1", BotID: "b1", ChatID: "c1", Status: "running", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	a.recoverOrphans()
	var run db.Run
	if err := a.DB.First(&run, "id = ?", "r1").Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "interrupted" {
		t.Fatalf("status %q", run.Status)
	}
	var n int64
	a.DB.Model(&db.RunEvent{}).Where("run_id = ? AND kind = ?", "r1", "done").Count(&n)
	if n != 1 {
		t.Fatalf("done events %d", n)
	}
}
