package speech

import "strings"

// Stream turns markdown as a model streams it into chunks a voice can say:
// whole lines are made speakable as they end, and a line still being
// written is spoken up to its last finished sentence, so a long paragraph
// starts playing before the model is through with it. Code blocks and tables
// are one pointer each, however long they run.
type Stream struct {
	p       Pointers
	pending []rune
	inFence bool
	inTable bool
	ch      Chunker
}

// NewStream reads markdown for a listener whose pointers are p.
func NewStream(p Pointers) *Stream { return &Stream{p: p} }

// Push adds what the model wrote and returns the chunks now ready.
func (s *Stream) Push(md string) []string {
	s.pending = append(s.pending, []rune(md)...)
	var text strings.Builder
	for {
		nl := -1
		for i, r := range s.pending {
			if r == '\n' {
				nl = i
				break
			}
		}
		if nl < 0 {
			break
		}
		text.WriteString(s.line(string(s.pending[:nl]), true))
		s.pending = s.pending[nl+1:]
	}
	// The line still being written: its finished sentences, unless it may
	// open a block that must be seen whole.
	if tail := string(s.pending); !s.inFence && !opensBlock(tail) {
		if cut := lastSentenceEnd(s.pending); cut > 0 {
			text.WriteString(s.line(string(s.pending[:cut]), false))
			s.pending = s.pending[cut:]
		}
	}
	return s.ch.Push(text.String())
}

// Flush returns everything left.
func (s *Stream) Flush() []string {
	text := s.line(string(s.pending), true)
	s.pending = nil
	return append(s.ch.Push(text), s.ch.Flush()...)
}

// line makes one piece of a line speakable; whole says the line ended there.
func (s *Stream) line(l string, whole bool) string {
	trim := strings.TrimSpace(l)
	if strings.HasPrefix(trim, "```") {
		s.inFence = !s.inFence
		if s.inFence {
			return s.p.Code + "\n"
		}
		return ""
	}
	if s.inFence {
		return ""
	}
	if reTableRow.MatchString(l) {
		if s.inTable {
			return ""
		}
		s.inTable = true
		return s.p.Table + "\n"
	}
	if whole {
		s.inTable = false
	}
	out := ForSpeech(l, s.p)
	if out == "" {
		if whole {
			return "\n"
		}
		return ""
	}
	if whole {
		return out + "\n"
	}
	return out + " "
}

// opensBlock reports whether a line being written may yet turn out to be a
// fence or a table row.
func opensBlock(l string) bool {
	t := strings.TrimSpace(l)
	return t == "" || strings.HasPrefix(t, "`") || strings.HasPrefix(t, "|")
}

// lastSentenceEnd is where the last finished sentence of r ends (after its
// punctuation and the space that follows), or 0.
func lastSentenceEnd(r []rune) int {
	for i := len(r) - 2; i > 0; i-- {
		if r[i+1] != ' ' {
			continue
		}
		switch r[i] {
		case '.', '!', '?', '…':
			return i + 2
		}
	}
	return 0
}
