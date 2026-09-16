package channels

import (
	"regexp"
	"strings"
)

// SectionSentinel is the marker the model emits to end a user-visible section.
// The prompt engine tells it to write it on its own line; the splitter accepts
// whitespace and a missing slash so a slightly different spelling still works.
const SectionSentinel = "<section_send />"

var sectionRe = regexp.MustCompile(`(?i)<\s*section_send\s*/?\s*>`)

// Splitter turns a stream of model deltas into completed sections. A partial
// sentinel at the end of the buffer is held until the next write, so the
// marker can span chunks.
type Splitter struct {
	buf strings.Builder
}

// Write appends a delta and returns any sections completed by it.
func (s *Splitter) Write(delta string) []string {
	if delta != "" {
		s.buf.WriteString(delta)
	}
	text := s.buf.String()
	var out []string
	for {
		loc := sectionRe.FindStringIndex(text)
		if loc == nil {
			break
		}
		if section := strings.TrimSpace(text[:loc[0]]); section != "" {
			out = append(out, section)
		}
		text = text[loc[1]:]
	}
	s.buf.Reset()
	s.buf.WriteString(text)
	return out
}

// Flush returns the trailing section (text after the last sentinel) and resets.
func (s *Splitter) Flush() string {
	tail := strings.TrimSpace(s.buf.String())
	s.buf.Reset()
	return tail
}

// Split cuts a completed message into sections. found reports whether the
// model actually used a sentinel; when false the caller treats the single
// part as a normal assistant reply.
func Split(text string) (parts []string, found bool) {
	if !sectionRe.MatchString(text) {
		if t := strings.TrimSpace(text); t != "" {
			return []string{t}, false
		}
		return nil, false
	}
	for {
		loc := sectionRe.FindStringIndex(text)
		if loc == nil {
			break
		}
		if s := strings.TrimSpace(text[:loc[0]]); s != "" {
			parts = append(parts, s)
		}
		text = text[loc[1]:]
	}
	if s := strings.TrimSpace(text); s != "" {
		parts = append(parts, s)
	}
	return parts, true
}
