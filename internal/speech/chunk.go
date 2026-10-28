package speech

import (
	"strings"
	"unicode"
)

// Chunk sizes, in runes. The first chunk may end at a clause so the voice
// starts sooner; later ones are whole sentences, short ones merged so the
// provider is not asked for a word at a time.
const (
	firstClause = 60
	shortChunk  = 24
	maxChunk    = 360
)

// abbreviations end in a dot that does not end a sentence, in the languages
// Silo speaks.
var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "vs": true, "mr": true, "mrs": true, "ms": true, "dr": true, "st": true, "no": true, "approx": true,
	"np": true, "itd": true, "itp": true, "tzn": true, "tj": true, "nr": true, "ok": true, "godz": true, "ul": true, "wg": true, "prof": true, "dot": true,
	"z.b": true, "bzw": true, "usw": true, "ca": true, "evtl": true, "ggf": true, "inkl": true, "str": true,
}

// Chunker cuts a stream of text into pieces a speech model can say one at a
// time: Push what arrives, take the chunks that are ready, Flush at the end.
type Chunker struct {
	buf   []rune
	given int
	// held is a short sentence waiting to go out with the next one; never
	// the first, which the voice should start on at once.
	held string
}

// Push adds text and returns the chunks it completed.
func (c *Chunker) Push(text string) []string {
	c.buf = append(c.buf, []rune(text)...)
	var out []string
	for {
		cut := c.cut(false)
		if cut <= 0 {
			break
		}
		if chunk := c.take(cut); chunk != "" {
			out = append(out, chunk)
		}
	}
	return out
}

// Flush returns what is left.
func (c *Chunker) Flush() []string {
	var out []string
	for len(c.buf) > 0 {
		cut := c.cut(true)
		if cut <= 0 {
			cut = len(c.buf)
		}
		if chunk := c.take(cut); chunk != "" {
			out = append(out, chunk)
		}
	}
	if c.held != "" {
		out = append(out, c.held)
		c.held = ""
		c.given++
	}
	return out
}

// take cuts the next chunk off the buffer: the held sentence goes with it,
// and a short one is held instead of handed out.
func (c *Chunker) take(cut int) string {
	chunk := strings.TrimSpace(string(c.buf[:cut]))
	c.buf = c.buf[cut:]
	if chunk == "" {
		return ""
	}
	if c.held != "" {
		chunk, c.held = c.held+" "+chunk, ""
	}
	if c.given > 0 && len([]rune(chunk)) < shortChunk {
		c.held = chunk
		return ""
	}
	c.given++
	return chunk
}

// cut is where the next chunk ends in the buffer, or 0 when none is ready.
// At the end of the text (final) a sentence may end without a space after it.
func (c *Chunker) cut(final bool) int {
	b := c.buf
	clause := 0
	for i, r := range b {
		if r == '\n' {
			return i + 1
		}
		atEnd := i+1 == len(b)
		if atEnd && !final {
			break
		}
		next := ' '
		if !atEnd {
			next = b[i+1]
		}
		// Punctuation ends something only before a space ("3.5" and
		// "e.g.x" do not).
		if !unicode.IsSpace(next) && !atEnd {
			continue
		}
		switch r {
		case '!', '?', '…':
			return i + 1
		case '.':
			if !c.abbreviation(i) {
				return i + 1
			}
		case ',', ';', ':', '—':
			clause = i + 1
		}
		if c.given == 0 && clause >= firstClause {
			return clause
		}
		if i+1 >= maxChunk {
			if clause > 0 {
				return clause
			}
			return lastSpace(b[:i+1])
		}
	}
	if len(b) >= maxChunk {
		if clause > 0 {
			return clause
		}
		return lastSpace(b[:maxChunk])
	}
	return 0
}

// abbreviation reports whether the dot at i ends a known abbreviation, an
// initial or a number, not a sentence.
func (c *Chunker) abbreviation(i int) bool {
	start := i
	for start > 0 && !unicode.IsSpace(c.buf[start-1]) && c.buf[start-1] != '(' {
		start--
	}
	word := strings.ToLower(string(c.buf[start:i]))
	if abbreviations[word] {
		return true
	}
	rs := []rune(word)
	// A single letter is an initial ("J. Smith"); an ordinal ("3. Mai").
	if len(rs) == 1 && unicode.IsLetter(rs[0]) && unicode.IsUpper(c.buf[start]) {
		return true
	}
	return false
}

func lastSpace(b []rune) int {
	for i := len(b) - 1; i > 0; i-- {
		if unicode.IsSpace(b[i]) {
			return i + 1
		}
	}
	return len(b)
}

// Split cuts a whole text into chunks.
func Split(text string) []string {
	var c Chunker
	return append(c.Push(text), c.Flush()...)
}
