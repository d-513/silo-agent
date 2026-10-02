package rag

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitEmpty(t *testing.T) {
	for _, in := range []string{"", "   \n\n\t", "\f\f"} {
		if got := Split(in); len(got) != 0 {
			t.Fatalf("Split(%q) = %v, want none", in, got)
		}
	}
}

func TestSplitShortPlainText(t *testing.T) {
	got := Split("alpha\nbeta\n")
	if len(got) != 1 {
		t.Fatalf("chunks = %d, want 1: %+v", len(got), got)
	}
	if got[0].Locator != "L1" {
		t.Fatalf("locator = %q, want L1", got[0].Locator)
	}
	if !strings.Contains(got[0].Text, "alpha") || !strings.Contains(got[0].Text, "beta") {
		t.Fatalf("text = %q", got[0].Text)
	}
}

func TestSplitMarkdownHeadingPath(t *testing.T) {
	intro := strings.Repeat("Intro words here. ", 40)
	setup := strings.Repeat("Install the thing carefully. ", 40)
	usage := strings.Repeat("Run it like so. ", 40)
	doc := "# Guide\n\n" + intro + "\n\n## Setup\n\n" + setup + "\n\n### Usage\n\n" + usage + "\n"
	got := Split(doc)
	if len(got) < 3 {
		t.Fatalf("chunks = %d, want sections split: %+v", len(got), headings(got))
	}
	var sawSetup, sawUsage bool
	for _, c := range got {
		if strings.Contains(c.Text, "Install the thing") && strings.HasPrefix(c.Heading, "Guide › Setup") {
			sawSetup = true
		}
		if strings.Contains(c.Text, "Run it like so") && c.Heading == "Guide › Setup › Usage" {
			sawUsage = true
		}
	}
	if !sawSetup || !sawUsage {
		t.Fatalf("heading paths wrong: %v", headings(got))
	}
}

func headings(cs []Chunk) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Locator+" | "+c.Heading)
	}
	return out
}

func TestSplitPagesFromFormFeed(t *testing.T) {
	doc := "first page text\fsecond page text\f\fforth page text"
	got := Split(doc)
	var locs []string
	for _, c := range got {
		locs = append(locs, c.Locator)
	}
	want := []string{"p. 1", "p. 2", "p. 4"}
	if strings.Join(locs, ",") != strings.Join(want, ",") {
		t.Fatalf("locators = %v, want %v", locs, want)
	}
}

func TestSplitLongTextRespectsMaxAndOverlaps(t *testing.T) {
	var b strings.Builder
	for i := 0; b.Len() < MaxChars*5; i++ {
		b.WriteString("Sentence number ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(" ends here. ")
	}
	got := Split(b.String())
	if len(got) < 5 {
		t.Fatalf("chunks = %d, want at least 5", len(got))
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c.Text); n > MaxChars {
			t.Fatalf("chunk %d has %d runes, max %d", i, n, MaxChars)
		}
		if strings.TrimSpace(c.Text) == "" {
			t.Fatalf("chunk %d is blank", i)
		}
	}
	// Consecutive chunks share a tail so a sentence at a boundary is findable.
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1].Text, got[i].Text
		tail := prev[len(prev)-40:]
		if !strings.Contains(cur[:min(len(cur), OverlapChars+200)], strings.Fields(tail)[len(strings.Fields(tail))-1]) {
			t.Fatalf("chunk %d does not overlap chunk %d", i, i-1)
		}
	}
}

func TestSplitNeverCutsRunes(t *testing.T) {
	doc := strings.Repeat("żółć 日本語のテキスト ", MaxChars) // long, no blank lines
	for _, c := range Split(doc) {
		if !utf8.ValidString(c.Text) {
			t.Fatalf("invalid UTF-8 in chunk")
		}
		if utf8.RuneCountInString(c.Text) > MaxChars {
			t.Fatalf("chunk over cap")
		}
	}
}

func TestSplitLineLocator(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString("line of plain text number that keeps going and going\n")
	}
	got := Split(b.String())
	if len(got) < 2 {
		t.Fatalf("want several chunks, got %d", len(got))
	}
	if got[0].Locator != "L1" {
		t.Fatalf("first locator = %q", got[0].Locator)
	}
	if got[1].Locator == "L1" || !strings.HasPrefix(got[1].Locator, "L") {
		t.Fatalf("second locator = %q, want a later line", got[1].Locator)
	}
}

func TestEmbedTextCarriesNameAndHeading(t *testing.T) {
	c := Chunk{Heading: "Guide › Setup", Text: "body"}
	if got := c.EmbedText("docs/guide.md"); got != "docs/guide.md › Guide › Setup\nbody" {
		t.Fatalf("EmbedText = %q", got)
	}
	if got := (Chunk{Text: "body"}).EmbedText("a.txt"); got != "a.txt\nbody" {
		t.Fatalf("EmbedText = %q", got)
	}
}

func TestSplitSmallSectionsMerge(t *testing.T) {
	doc := "# A\n\nshort.\n\n# B\n\nshort too.\n"
	got := Split(doc)
	if len(got) != 1 {
		t.Fatalf("tiny sections should share one chunk, got %d: %+v", len(got), got)
	}
}
