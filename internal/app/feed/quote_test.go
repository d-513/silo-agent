package feed

import (
	"strings"
	"testing"
	"time"
)

// A quoted post is told with when it was posted, in the zone the quote was
// made in; a quote from before Bots had a zone keeps the text it always had.
func TestQuoteTextNamesTheZone(t *testing.T) {
	posted := time.Date(2026, 10, 11, 12, 30, 0, 0, time.UTC)
	got := QuoteText("automation “Prices”", "Coffee went up", posted, "Asia/Tokyo")
	if !strings.HasPrefix(got, "I'm quoting a post you made to my Feed on Sun, 2026-10-11 21:30:00 +09:00 Asia/Tokyo from automation “Prices”:\n\n> Coffee went up") {
		t.Fatalf("quote %q", got)
	}
	old := QuoteText("", "Coffee went up", posted, "")
	if want := " on " + posted.Local().Format("Mon 2006-01-02 15:04 MST") + ":\n"; !strings.Contains(old, want) {
		t.Fatalf("an old quote reads %q, want it to hold %q", old, want)
	}
}
