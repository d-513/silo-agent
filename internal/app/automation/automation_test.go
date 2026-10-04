package automation

import (
	"strings"
	"testing"

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
