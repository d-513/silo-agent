package automation

import (
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
)

func TestPromptNamesTheRunAndItsSchedule(t *testing.T) {
	got := Prompt(&db.Automation{Name: "Digest", Schedule: "0 9 * * *"})
	if !strings.Contains(got, "“Digest”") || !strings.Contains(got, "0 9 * * *") || !strings.Contains(got, "fresh context") {
		t.Fatalf("prompt %q", got)
	}
	if got := Prompt(&db.Automation{Name: "Manual"}); strings.Contains(got, "schedule `") {
		t.Fatalf("an automation without a schedule must not name one: %q", got)
	}
}

func TestParseScheduleEnforcesGap(t *testing.T) {
	for _, ok := range []string{"", "*/5 * * * *", "0 9 * * 1-5", "@daily", "@every 1h"} {
		if _, err := parseSchedule(ok); err != nil {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"* * * * *", "@every 1m", "61 * * * *", "0 9 * *"} {
		if _, err := parseSchedule(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

// A schedule's fields are wall-clock time in the Bot's zone, not the server's:
// the same cron names a different moment for a Bot somewhere else.
func TestNextRunIsInTheBotsZone(t *testing.T) {
	au := &db.Automation{Enabled: true, Schedule: "0 9 * * *"}
	from := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for name, want := range map[string]time.Time{
		"Asia/Tokyo":       time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),  // 09:00 +09:00, already past today there
		"Europe/Warsaw":    time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC),  // 09:00 +02:00
		"America/New_York": time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC), // 09:00 -04:00, still to come today
		"UTC":              time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
	} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		got := nextRun(au, from, loc)
		if got == nil || !got.Equal(want) {
			t.Errorf("09:00 in %s after %s is %v, want %s", name, from, got, want)
		}
	}
}

// 09:00 stays 09:00 on the wall when the clocks change, so the moment moves.
func TestNextRunKeepsWallClockAcrossDST(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	au := &db.Automation{Enabled: true, Schedule: "0 9 * * *"}
	// The clocks go back on 2026-10-25: +02:00 the day before, +01:00 after.
	before := nextRun(au, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), warsaw)
	after := nextRun(au, time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), warsaw)
	if before == nil || !before.Equal(time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("the firing before the change is %v", before)
	}
	if after == nil || !after.Equal(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("the firing after the change is %v", after)
	}
	for _, at := range []*time.Time{before, after} {
		if w := at.In(warsaw); w.Hour() != 9 || w.Minute() != 0 {
			t.Fatalf("fires at %s on the wall", w)
		}
	}
}

func TestNextRunPausedOrUnscheduled(t *testing.T) {
	now := time.Now()
	if got := nextRun(&db.Automation{Enabled: false, Schedule: "0 9 * * *"}, now, time.UTC); got != nil {
		t.Fatalf("a paused automation fires at %v", got)
	}
	if got := nextRun(&db.Automation{Enabled: true}, now, time.UTC); got != nil {
		t.Fatalf("an automation with no schedule fires at %v", got)
	}
}
