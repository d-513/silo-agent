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

// TestStampUserTextUsesTheMessagesZone guards the date/time marker that rides
// on user messages: it is the wall-clock time in the zone the message was
// written in, with the offset and the zone's name, and the text is left intact.
func TestStampUserTextUsesTheMessagesZone(t *testing.T) {
	when := time.Date(2026, 9, 21, 14, 30, 5, 0, time.UTC)
	if got, want := stampUserText("hello", when, "Asia/Tokyo"), "[Mon, 2026-09-21 23:30:05 +09:00 Asia/Tokyo] hello"; got != want {
		t.Fatalf("stampUserText = %q, want %q", got, want)
	}
	if got, want := stampUserText("hello", when, "America/New_York"), "[Mon, 2026-09-21 10:30:05 -04:00 America/New_York] hello"; got != want {
		t.Fatalf("stampUserText = %q, want %q", got, want)
	}
}

// A message from before Bots had a zone carries none, and must replay as the
// bytes it was first sent as: the server's zone in the old layout. Changing
// them would cost every existing conversation its prompt cache.
func TestStampUserTextKeepsOldMessagesAsTheyWere(t *testing.T) {
	when := time.Date(2026, 9, 21, 14, 30, 5, 0, time.UTC)
	got := stampUserText("hello", when, "")
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
