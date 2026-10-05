package channels

import (
	"strings"
	"unicode/utf8"
)

// fenceClose is appended to a piece that ends inside a code block, and the
// fence's opening line is repeated at the top of the next piece, so each
// message renders as complete code instead of leaking backticks into prose.
const fenceClose = "\n```"

// Chunk cuts text into messages of at most max runes, preferring paragraph,
// line and word boundaries, and keeping a code fence balanced across the cut.
// Platforms count characters (Discord 2000) or UTF-16 units (Telegram 4096), so
// a rune budget with some headroom is safe for both. Empty input gives nil.
func Chunk(text string, max int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if max < 32 {
		max = 32 // room for a fence reopen + close and at least some text
	}
	var out []string
	reopen := ""
	for utf8.RuneCountInString(reopen+text) > max {
		budget := max - utf8.RuneCountInString(reopen) - utf8.RuneCountInString(fenceClose)
		head := text[:byteIndex(text, budget)]
		cut := lastBoundary(head)
		piece := reopen + strings.TrimRight(text[:cut], " \n")
		if line, open := openFence(piece); open {
			piece += fenceClose
			reopen = line + "\n"
		} else {
			reopen = ""
		}
		out = append(out, piece)
		text = strings.TrimLeft(text[cut:], " \n")
	}
	if text != "" {
		out = append(out, reopen+text)
	}
	return out
}

// byteIndex is the byte offset after the first n runes of s (len(s) if shorter).
func byteIndex(s string, n int) int {
	i := 0
	for range n {
		if i >= len(s) {
			return len(s)
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}

// lastBoundary picks where to cut head: the last blank line, else the last
// newline, else the last space — each only if it keeps at least half of head —
// else the end of head. It never returns 0, so Chunk always makes progress.
func lastBoundary(head string) int {
	half := len(head) / 2
	for _, sep := range []string{"\n\n", "\n", " "} {
		if i := strings.LastIndex(head, sep); i > 0 && i >= half {
			return i
		}
	}
	if len(head) == 0 {
		return 1
	}
	return len(head)
}

// openFence reports whether s ends inside a ``` code block, and the line that
// opened it (with its language tag).
func openFence(s string) (line string, open bool) {
	for l := range strings.SplitSeq(s, "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "```") {
			continue
		}
		if open {
			open, line = false, ""
		} else {
			open, line = true, t
		}
	}
	return line, open
}
