package room

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMentionsIn(t *testing.T) {
	bots := []member{{ID: "r", Name: "Research"}, {ID: "rb", Name: "Research Bot"}, {ID: "w", Name: "Writer"}}
	for _, c := range []struct {
		text string
		want []string
	}{
		{"@writer can you", []string{"w"}},
		// The longer name wins: @Research Bot is not also @Research.
		{"ask @Research Bot", []string{"rb"}},
		{"@Research, then @Writer.", []string{"r", "w"}},
		// A name must be whole, and an @ inside a word (an address) names nobody.
		{"@Writers and ann@writer.com", nil},
		{"@all please", []string{"r", "rb", "w"}},
		{"nobody here", nil},
	} {
		if got := mentionsIn(c.text, bots); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}

func TestPassed(t *testing.T) {
	for in, want := range map[string]string{
		"<pass/>":             "",
		"  <pass/>\n":         "",
		"<pass/> Actually: 4": "Actually: 4",
		"It is 4.":            "It is 4.",
	} {
		if got := passed(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestHanded(t *testing.T) {
	at := time.Date(2026, 11, 21, 9, 5, 0, 0, time.UTC)
	loc := time.FixedZone("CET", 3600)
	lines := []line{{Author: "ann@x.com", Body: "what is 2+2?", At: at}, {Author: "Writer", Bot: true, Body: " It is 4. ", At: at.Add(time.Minute)}}
	got := handed(lines, 2, []string{"Editor"}, loc)
	want := "(2 earlier messages are not shown.)\n\nann@x.com · 10:05\nwhat is 2+2?\n\nWriter (a Bot) · 10:06\nIt is 4.\n\n(At work on a reply now: Editor.)"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if one := handed(lines[:1], 1, nil, loc); !strings.HasPrefix(one, "(1 earlier message is not shown.)") {
		t.Fatalf("one left out: %q", one)
	}
}

func TestFitKeepsTheNewest(t *testing.T) {
	var lines []line
	for i := range 10 {
		lines = append(lines, line{Author: "a", Body: strings.Repeat("x", 100) + string(rune('0'+i))})
	}
	kept, left := fit(lines, 4, 100000)
	if len(kept) != 4 || left != 6 || !strings.HasSuffix(kept[3].Body, "9") {
		t.Fatalf("by count: kept %d, left %d", len(kept), left)
	}
	kept, left = fit(lines, 40, 250)
	if len(kept) != 2 || left != 8 {
		t.Fatalf("by size: kept %d, left %d", len(kept), left)
	}
	// The newest is kept however long it is.
	kept, _ = fit([]line{{Body: strings.Repeat("y", 500)}}, 40, 10)
	if len(kept) != 1 {
		t.Fatal("the newest message was dropped")
	}
}
