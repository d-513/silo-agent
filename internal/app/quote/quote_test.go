package quote

import (
	"strings"
	"testing"
	"time"

	"silo.agent/internal/db"
)

var posted = time.Date(2026, 10, 11, 12, 30, 0, 0, time.UTC)

// A quoted post is told with when it was posted, in the zone the quote was
// made in; a quote from before Bots had a zone keeps the text it always had.
func TestFeedQuoteNamesTheZone(t *testing.T) {
	got := Text(&db.RunEvent{Kind: Kind, Body: "Coffee went up", Tool: "automation “Prices”", Zone: "Asia/Tokyo", CreatedAt: posted, Meta: `{"from":"feed"}`})
	if !strings.HasPrefix(got, "I'm quoting a post you made to my Feed on Sun, 2026-10-11 21:30:00 +09:00 Asia/Tokyo from automation “Prices”:\n\n> Coffee went up") {
		t.Fatalf("quote %q", got)
	}
	old := Text(&db.RunEvent{Kind: Kind, Body: "Coffee went up", CreatedAt: posted})
	if want := "I'm quoting a post you made to my Feed on " + posted.Local().Format("Mon 2006-01-02 15:04 MST") + ":\n"; !strings.HasPrefix(old, want) {
		t.Fatalf("an old quote reads %q, want it to start %q", old, want)
	}
}

// A widget or an ask is never passed off as a Feed post.
func TestDashboardQuotesSayWhatTheyAre(t *testing.T) {
	w := Text(&db.RunEvent{Kind: Kind, Body: "```json\n{\"value\":3}\n```", Zone: "UTC", CreatedAt: posted,
		Meta: `{"from":"widget","title":"Open PRs","widget":{"key":"prs","kind":"stat"}}`})
	if !strings.HasPrefix(w, "I'm quoting the “Open PRs” widget from my Dashboard (`prs`, a stat), as it stood on Sun, 2026-10-11 12:30:00 +00:00 UTC:\n\n> ```json") {
		t.Fatalf("widget quote %q", w)
	}
	a := Text(&db.RunEvent{Kind: Kind, Body: "Renew the domain?", Tool: "chat “Ops”", Zone: "UTC", CreatedAt: posted, Meta: `{"from":"ask","title":"Domain"}`})
	if !strings.HasPrefix(a, "I'm quoting the card “Domain” you put on my Dashboard on Sun, 2026-10-11 12:30:00 +00:00 UTC from chat “Ops”:\n\n> Renew the domain?") {
		t.Fatalf("ask quote %q", a)
	}
	for _, s := range []string{w, a} {
		if strings.Contains(s, "Feed") {
			t.Fatalf("a Dashboard quote mentions the Feed: %q", s)
		}
	}
}

// The thread is told what was quoted; a widget comes back drawable, and an
// event from before quotes carried this gets nothing.
func TestProto(t *testing.T) {
	q := Proto(&db.RunEvent{Kind: Kind, CreatedAt: posted, Meta: `{"from":"widget","title":"Disk","widget":{"key":"disk","kind":"progress","data":"{\"value\":40}","source":"/workspace"}}`})
	if q == nil || q.From != FromWidget || q.Title != "Disk" || q.Widget.GetKind() != "progress" || q.Widget.GetDataJson() != `{"value":40}` || q.Widget.GetSource() != "/workspace" || q.Widget.GetDataAt() == "" {
		t.Fatalf("proto %v", q)
	}
	if Proto(&db.RunEvent{Kind: Kind}) != nil || Proto(&db.RunEvent{Kind: "user", Meta: `{"from":"widget"}`}) != nil {
		t.Fatal("a bare or foreign event is no quote")
	}
}
