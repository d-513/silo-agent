package speech

import (
	"strings"
	"testing"
)

func TestForSpeech(t *testing.T) {
	en := PointersFor("en")
	for name, tc := range map[string]struct{ in, want string }{
		"emphasis and code":  {"This is **really** _fine_ and `go test` works.", "This is really fine and go test works."},
		"link and url":       {"See [the docs](https://x.dev/a) or https://www.example.com/path?q=1.", "See the docs or example.com."},
		"fence":              {"Here:\n```go\nfmt.Println(1)\n```\nDone.", "Here: " + en.Code + " Done."},
		"list":               {"Steps:\n- install it\n- run it\n\nThen relax.", "Steps: install it. run it.\nThen relax."},
		"heading":            {"## Summary\nAll good", "Summary. All good"},
		"table":              {"| a | b |\n|---|---|\n| 1 | 2 |\nAfter.", en.Table + " After."},
		"emoji and sentinel": {"Great 🎉 news<section_send />", "Great news"},
		"snake_case stays":   {"Use my_var_name here.", "Use my_var_name here."},
		"polish pointer":     {"```\nx\n```", PointersFor("pl").Code},
	} {
		p := en
		if name == "polish pointer" {
			p = PointersFor("pl")
		}
		if got := ForSpeech(tc.in, p); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
	}
}

func TestSplit(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want []string
	}{
		"sentences":       {"Hello there. How are you? Fine!", []string{"Hello there.", "How are you? Fine!"}},
		"abbreviations":   {"Take e.g. apples, np. jabłka, z.B. Äpfel. Then stop.", []string{"Take e.g. apples, np. jabłka, z.B. Äpfel.", "Then stop."}},
		"decimals":        {"It costs 3.50 euro. Cheap.", []string{"It costs 3.50 euro.", "Cheap."}},
		"initial":         {"Ask J. Smith about it. He knows.", []string{"Ask J. Smith about it.", "He knows."}},
		"newline":         {"First line\nSecond line", []string{"First line", "Second line"}},
		"no end":          {"no punctuation at all", []string{"no punctuation at all"}},
		"first at clause": {strings.Repeat("word ", 14) + "and then, more words follow here until the end.", []string{strings.Repeat("word ", 14) + "and then,", "more words follow here until the end."}},
	} {
		got := Split(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
	}
	// A wall of text with no punctuation is still cut, at a space.
	long := strings.Repeat("blah ", 200)
	for _, c := range Split(long) {
		if n := len([]rune(c)); n > maxChunk {
			t.Fatalf("chunk of %d runes", n)
		}
	}
}

// Pushing piece by piece gives the same chunks as the whole text, and a
// chunk is handed out only once its sentence is over.
func TestChunkerStreams(t *testing.T) {
	text := "Sure. Let me check the calendar for tomorrow. You have two meetings: one at nine, one at three."
	var c Chunker
	var got []string
	for _, r := range text {
		got = append(got, c.Push(string(r))...)
	}
	got = append(got, c.Flush()...)
	if strings.Join(got, "|") != strings.Join(Split(text), "|") {
		t.Fatalf("streamed %q, whole %q", got, Split(text))
	}
	var d Chunker
	if out := d.Push("Sure"); len(out) != 0 {
		t.Fatalf("a sentence still going was handed out: %q", out)
	}
	if out := d.Push(". Next"); len(out) != 1 || out[0] != "Sure." {
		t.Fatalf("got %q", out)
	}
}

func TestHeard(t *testing.T) {
	text := "The meeting is at six. Bring the slides."
	if h, u := Heard(text, 1000, 1000); h != text || u != "" {
		t.Fatalf("all played: %q / %q", h, u)
	}
	if h, u := Heard(text, 0, 1000); h != "" || u != text {
		t.Fatalf("nothing played: %q / %q", h, u)
	}
	h, u := Heard(text, 560, 1000)
	if h != "The meeting is at six." || u != "Bring the slides." {
		t.Fatalf("half: %q / %q", h, u)
	}
	// Mid-word cuts back to the last whole word.
	if h, _ := Heard("abcdefgh ijkl", 300, 1000); h != "" {
		t.Fatalf("a half-heard word is not heard: %q", h)
	}
}

// A streamed reply speaks finished sentences early, a code block once, and
// the same words as the whole text would.
func TestStream(t *testing.T) {
	p := PointersFor("en")
	md := "Sure, here it is. The function:\n```go\nfunc main() {}\n```\nThat's **all**. Anything else?"
	s := NewStream(p)
	var got []string
	for _, r := range md {
		got = append(got, s.Push(string(r))...)
	}
	got = append(got, s.Flush()...)
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "func main") || strings.Count(joined, p.Code) != 1 {
		t.Fatalf("code leaked or pointer missing: %q", got)
	}
	if !strings.Contains(joined, "That's all.") || strings.Contains(joined, "**") {
		t.Fatalf("markup leaked: %q", got)
	}
	// The first sentence is ready before the line ends.
	s2 := NewStream(p)
	if out := s2.Push("Sure, here it is. The func"); len(out) != 1 || out[0] != "Sure, here it is." {
		t.Fatalf("early sentence: %q", out)
	}
}
