package app

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
)

// These are pure helpers behind OAuth and connector editing. Their behavior is
// expensive to reproduce end-to-end (it needs a live authorization server), so
// they are tested directly here.

func TestCallTitle(t *testing.T) {
	if got := callTitle("Twilio Docs", "twilio__retrieve"); got != "Twilio Docs · retrieve" {
		t.Fatalf("callTitle %q", got)
	}
}

// TestStampUserTextUsesLocalTime guards the date/time marker that rides on user
// messages: it must render in the machine's local zone and leave the original
// text intact.
func TestStampUserTextUsesLocalTime(t *testing.T) {
	when := time.Date(2026, 9, 21, 14, 30, 5, 0, time.UTC)
	got := stampUserText("hello", when)
	if !strings.HasSuffix(got, " hello") || !strings.HasPrefix(got, "[") {
		t.Fatalf("stampUserText %q", got)
	}
	want := "[" + when.Local().Format("Mon, 2006-01-02 15:04:05 MST") + "] hello"
	if got != want {
		t.Fatalf("stampUserText = %q, want %q", got, want)
	}
	if !regexp.MustCompile(`^\[\w{3}, \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \S+\] hello$`).MatchString(got) {
		t.Fatalf("marker shape %q", got)
	}
}

func TestAutomationSectionIsTrailing(t *testing.T) {
	a := &App{}
	if got := a.automationSections(promptContext{}); len(got) != 0 {
		t.Fatalf("no automation, no section: %v", got)
	}
	secs := a.automationSections(promptContext{automation: &db.Automation{Name: "Digest"}})
	if len(secs) != 1 || !secs[0].trailing || !strings.Contains(secs[0].body, "Digest") {
		t.Fatalf("want one trailing section naming the run, got %v", secs)
	}
}
