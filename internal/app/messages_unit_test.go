package app

import (
	"testing"

	"silo.agent/internal/db"
)

func TestEventsAfterMissingAnchorReplaysAll(t *testing.T) {
	rows := []db.RunEvent{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := eventsAfter(rows, "b"); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("anchor b -> %+v", got)
	}
	if got := eventsAfter(rows, ""); len(got) != 3 {
		t.Fatalf("empty anchor -> %+v", got)
	}
	// A truncated anchor must not swallow the replay.
	if got := eventsAfter(rows, "gone"); len(got) != 3 {
		t.Fatalf("missing anchor -> %+v", got)
	}
}
