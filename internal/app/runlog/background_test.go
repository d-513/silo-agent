package runlog

import (
	"testing"
	"time"

	"silo.agent/internal/app/models"
)

func TestBackgroundJoinsBurstsAndForgets(t *testing.T) {
	b := newBackground()
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	embed := models.Call{BotID: "bot", Label: "embed", Model: "m", In: 10, Duration: time.Second}

	// Like calls within a minute are one line with their costs added up.
	b.Note(embed)
	now = now.Add(20 * time.Second)
	b.Note(embed)
	// A different outcome, or a pause, starts a new line.
	failed := embed
	failed.Err = "boom"
	b.Note(failed)
	now = now.Add(2 * time.Minute)
	b.Note(embed)
	b.Note(models.Call{BotID: "other", Label: "speech"})
	b.Note(models.Call{Label: "nobody's"})

	got := b.List("bot")
	if len(got) != 3 || got[2].Count != 2 || got[2].In != 20 || got[2].Duration != 2*time.Second || got[1].Err != "boom" || got[0].Count != 1 {
		t.Fatalf("lines, newest first: %+v", got)
	}
	if other := b.List("other"); len(other) != 1 || other[0].Label != "speech" {
		t.Fatalf("another Bot's lines: %+v", other)
	}

	// A line is forgotten after a day, and a Bot's lines are capped.
	now = now.Add(backgroundAge + time.Minute)
	if got := b.List("bot"); len(got) != 0 {
		t.Fatalf("lines a day old: %+v", got)
	}
	for i := range backgroundMax + 20 {
		now = now.Add(2 * time.Minute)
		// Two minutes apart: lines of their own, all inside the day.
		b.Note(models.Call{BotID: "bot", Label: "embed", In: i})
	}
	b.mu.Lock()
	kept := len(b.bots["bot"])
	b.mu.Unlock()
	if kept != backgroundMax {
		t.Fatalf("kept %d lines, the cap is %d", kept, backgroundMax)
	}
	b.Drop("bot")
	if got := b.List("bot"); len(got) != 0 {
		t.Fatalf("a dropped Bot's lines: %+v", got)
	}
}
