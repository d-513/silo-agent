// Package rag holds the pure parts of retrieval over the owner's folders:
// splitting extracted text into chunks that embed well and cite a place.
package rag

import (
	"fmt"
	"strings"
)

const (
	// MaxChars caps one chunk (about 600 tokens of prose).
	MaxChars = 2400
	// OverlapChars is the tail of one chunk repeated at the start of the next
	// when a section had to be cut, so a sentence on the boundary is findable.
	OverlapChars = 300
	// MinChars is the size a chunk must reach before a new heading starts a
	// fresh one; smaller sections share a chunk instead of becoming noise.
	MinChars = 400
)

// Chunk is one embeddable piece of a file.
type Chunk struct {
	// Locator tells a reader where it came from: "p. 12", "Guide › Setup · L40"
	// or "L40". Lines are the extracted text's, which for text files are the
	// file's own.
	Locator string
	// Heading is the markdown heading path at the chunk's start, if any.
	Heading string
	Text    string
}

// EmbedText is what gets embedded: the text with its file name and heading
// path in front, so a chunk that never repeats the document's subject still
// matches a question about it.
func (c Chunk) EmbedText(name string) string {
	head := name
	if c.Heading != "" {
		head += " › " + c.Heading
	}
	return head + "\n" + c.Text
}

type block struct {
	text    string
	page    int // 0 when the text has no page breaks
	line    int // 1-based line in the page (or file)
	heading string
	isHead  bool
}

// Split cuts extracted text into chunks. A form feed starts a new page (what
// pdftotext emits); markdown headings track a path; everything else is
// paragraphs packed up to MaxChars.
func Split(text string) []Chunk {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	paged := strings.Contains(text, "\f")
	var blocks []block
	for i, page := range strings.Split(text, "\f") {
		n := 0
		if paged {
			n = i + 1
		}
		blocks = append(blocks, pageBlocks(page, n)...)
	}
	return pack(blocks)
}

// pageBlocks splits one page into paragraph blocks, tracking the markdown
// heading path. A heading line starts its own block so it stays with the text
// that follows.
func pageBlocks(page string, pageNo int) []block {
	var (
		out     []block
		cur     []string
		curLine int
		path    []string
		curHead string
		isHead  bool
	)
	flush := func() {
		if len(cur) == 0 {
			return
		}
		s := strings.TrimSpace(strings.Join(cur, "\n"))
		if s != "" {
			out = append(out, block{text: s, page: pageNo, line: curLine, heading: curHead, isHead: isHead})
		}
		cur, isHead = nil, false
	}
	for i, line := range strings.Split(page, "\n") {
		lineNo := i + 1
		if level, title, ok := heading(line); ok {
			flush()
			for len(path) >= level {
				path = path[:len(path)-1]
			}
			for len(path) < level-1 { // a skipped level still nests
				path = append(path, "")
			}
			path = append(path, title)
			curHead = strings.Join(nonEmpty(path), " › ")
			cur, curLine, isHead = []string{line}, lineNo, true
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if len(cur) == 0 {
			curLine = lineNo
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// heading parses an ATX markdown heading ("## Title").
func heading(line string) (level int, title string, ok bool) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line[level:]), "#"))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func runeLen(s string) int { return len([]rune(s)) }

func pack(blocks []block) []Chunk {
	var (
		out   []Chunk
		cur   strings.Builder
		first block
		have  bool
	)
	locator := func(b block, line int) string {
		switch {
		case b.page > 0:
			return fmt.Sprintf("p. %d", b.page)
		case b.heading != "":
			return fmt.Sprintf("%s · L%d", b.heading, line)
		default:
			return fmt.Sprintf("L%d", line)
		}
	}
	emit := func(b block, line int, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		out = append(out, Chunk{Locator: locator(b, line), Heading: b.heading, Text: text})
	}
	flush := func() {
		if have {
			emit(first, first.line, cur.String())
		}
		cur.Reset()
		have = false
	}
	for _, b := range blocks {
		if have && b.page != first.page {
			flush()
		}
		if have && b.isHead && runeLen(cur.String()) >= MinChars {
			flush()
		}
		if runeLen(b.text) > MaxChars {
			flush()
			for _, p := range splitLong(b.text) {
				emit(b, b.line+strings.Count(string([]rune(b.text)[:p.start]), "\n"), p.text)
			}
			continue
		}
		if have && runeLen(cur.String())+2+runeLen(b.text) > MaxChars {
			tail := overlapTail(cur.String())
			flush()
			if tail != "" && runeLen(tail)+2+runeLen(b.text) <= MaxChars {
				cur.WriteString(tail)
				cur.WriteString("\n\n")
			}
			first, have = b, true
			cur.WriteString(b.text)
			continue
		}
		if !have {
			first, have = b, true
		} else {
			cur.WriteString("\n\n")
		}
		cur.WriteString(b.text)
	}
	flush()
	return out
}

// overlapTail is the last OverlapChars of s, starting on a word.
func overlapTail(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= OverlapChars {
		return ""
	}
	t := r[len(r)-OverlapChars:]
	for i, c := range t {
		if c == ' ' || c == '\n' {
			return strings.TrimSpace(string(t[i:]))
		}
	}
	return string(t)
}

type piece struct {
	start int // rune offset in the source
	text  string
}

// splitLong cuts one oversized block into pieces of at most MaxChars runes,
// preferring a newline, then a sentence end, then a space, and repeating
// OverlapChars between neighbours.
func splitLong(s string) []piece {
	r := []rune(s)
	var out []piece
	for start := 0; start < len(r); {
		end := min(start+MaxChars, len(r))
		if end < len(r) {
			if cut := breakAt(r, start, end); cut > start {
				end = cut
			}
		}
		out = append(out, piece{start: start, text: string(r[start:end])})
		if end >= len(r) {
			break
		}
		next := end - OverlapChars
		for next < len(r) && next > start && r[next] != ' ' && r[next] != '\n' {
			next++
		}
		if next <= start || next >= end {
			next = end
		}
		start = next
		for start < len(r) && (r[start] == ' ' || r[start] == '\n') {
			start++
		}
	}
	return out
}

// breakAt finds a cut point in the last quarter of r[start:end].
func breakAt(r []rune, start, end int) int {
	lo := end - (end-start)/4
	for _, pred := range []func(i int) bool{
		func(i int) bool { return r[i-1] == '\n' },
		func(i int) bool { return r[i-1] == ' ' && i >= 2 && (r[i-2] == '.' || r[i-2] == '!' || r[i-2] == '?') },
		func(i int) bool { return r[i-1] == ' ' },
	} {
		for i := end; i > lo; i-- {
			if pred(i) {
				return i
			}
		}
	}
	return end
}
