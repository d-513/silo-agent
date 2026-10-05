package channels

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkShortAndEmpty(t *testing.T) {
	if got := Chunk("  \n ", 100); got != nil {
		t.Fatalf("blank = %v", got)
	}
	if got := Chunk("hello", 100); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("short = %v", got)
	}
}

func TestChunkKeepsEveryWordUnderTheLimit(t *testing.T) {
	long := strings.Repeat("word ", 1200)
	parts := Chunk(long, 500)
	if len(parts) < 2 {
		t.Fatalf("expected a split, got %d part(s)", len(parts))
	}
	words := 0
	for i, p := range parts {
		if n := utf8.RuneCountInString(p); n > 500 {
			t.Fatalf("part %d is %d runes", i, n)
		}
		words += len(strings.Fields(p))
	}
	if words != 1200 {
		t.Fatalf("words = %d, want 1200", words)
	}
}

func TestChunkCountsRunesNotBytes(t *testing.T) {
	// 600 two-byte runes: 1200 bytes, but one message under a 700-rune limit.
	text := strings.Repeat("ł", 600)
	if got := Chunk(text, 700); len(got) != 1 {
		t.Fatalf("got %d parts, want 1 (runes, not bytes)", len(got))
	}
	// A word-less run longer than the limit is still cut, on rune boundaries.
	parts := Chunk(strings.Repeat("ł", 900), 400)
	if len(parts) < 3 {
		t.Fatalf("got %d parts", len(parts))
	}
	for _, p := range parts {
		if !utf8.ValidString(p) || utf8.RuneCountInString(p) > 400 {
			t.Fatalf("bad part: %d runes, valid=%v", utf8.RuneCountInString(p), utf8.ValidString(p))
		}
	}
}

func TestChunkPrefersParagraphBreaks(t *testing.T) {
	a := strings.Repeat("a", 300)
	b := strings.Repeat("b", 300)
	parts := Chunk(a+"\n\n"+b, 400)
	if len(parts) != 2 || parts[0] != a || parts[1] != b {
		t.Fatalf("parts = %q", parts)
	}
}

func TestChunkRebalancesACodeFence(t *testing.T) {
	code := "```go\n" + strings.Repeat("x := 1\n", 120) + "```"
	parts := Chunk("Here:\n\n"+code, 300)
	if len(parts) < 2 {
		t.Fatalf("expected a split, got %d", len(parts))
	}
	for i, p := range parts {
		if _, open := openFence(p); open {
			t.Fatalf("part %d leaves a fence open:\n%s", i, p)
		}
		if i > 0 && !strings.HasPrefix(p, "```go\n") {
			t.Fatalf("part %d does not reopen the fence:\n%s", i, p)
		}
	}
}

func TestSeen(t *testing.T) {
	s := NewSeen(2)
	if s.Add("a") || s.Add("b") {
		t.Fatal("first sight reported as duplicate")
	}
	if !s.Add("a") {
		t.Fatal("repeat not reported")
	}
	if s.Add("c") { // full: forgets, then records c
		t.Fatal("new id reported as duplicate")
	}
	if s.Add("a") {
		t.Fatal("expected the reset to forget a")
	}
}
