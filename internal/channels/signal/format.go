package signal

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Signal shows no Markdown: a message is plain text plus style ranges
// (signal-cli's textStyle, start:length:STYLE), counted in UTF-16 code units.
// format writes the Bot's Markdown that way: the marks go, the styles they
// meant stay, and what Signal has no style for (links, headings, bullets) is
// written out in words.

var (
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
)

// styled builds the text and its ranges together.
type styled struct {
	sb     strings.Builder
	units  int // UTF-16 length of what is written
	styles []string
}

func (b *styled) write(s string) {
	b.sb.WriteString(s)
	for _, r := range s {
		b.units++
		if r > 0xFFFF {
			b.units++
		}
	}
}

// span styles whatever fn writes.
func (b *styled) span(style string, fn func()) {
	start := b.units
	fn()
	if b.units > start {
		b.styles = append(b.styles, fmt.Sprintf("%d:%d:%s", start, b.units-start, style))
	}
}

// format turns Markdown into Signal's text and style ranges.
func format(md string) (string, []string) {
	b := &styled{}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if i > 0 {
			b.write("\n")
		}
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			// A fenced block, to its closing fence or the end.
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				end++
			}
			b.span("MONOSPACE", func() { b.write(strings.Join(lines[i+1:end], "\n")) })
			i = end
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			b.span("BOLD", func() { b.inline(m[1]) })
			continue
		}
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			b.write(m[1] + "• ")
			line = m[2]
		}
		b.inline(line)
	}
	// Ranges count from the start, so only the end may be trimmed.
	return strings.TrimRight(b.sb.String(), " \n"), b.styles
}

// pairs are the marks that come in twos, and the style each means.
var pairs = []struct{ mark, style string }{
	{"**", "BOLD"}, {"__", "BOLD"}, {"~~", "STRIKETHROUGH"}, {"||", "SPOILER"},
}

// inline writes one line's text, reading the marks inside it.
func (b *styled) inline(s string) {
next:
	for len(s) > 0 {
		switch {
		case s[0] == '\\' && len(s) > 1 && isMark(s[1]):
			b.write(s[1:2])
			s = s[2:]
			continue
		case s[0] == '`':
			if end := strings.IndexByte(s[1:], '`'); end > 0 {
				code := s[1 : 1+end]
				b.span("MONOSPACE", func() { b.write(code) })
				s = s[end+2:]
				continue
			}
		case s[0] == '!' && strings.HasPrefix(s, "!["), s[0] == '[':
			if label, url, rest, ok := link(strings.TrimPrefix(s, "!")); ok {
				b.inline(label)
				if url != "" && url != label {
					if label != "" {
						b.write(" (" + url + ")")
					} else {
						b.write(url)
					}
				}
				s = rest
				continue
			}
		}
		for _, p := range pairs {
			if !strings.HasPrefix(s, p.mark) {
				continue
			}
			if end := strings.Index(s[2:], p.mark); end > 0 {
				inner := s[2 : 2+end]
				b.span(p.style, func() { b.inline(inner) })
				s = s[end+4:]
				continue next
			}
		}
		if s[0] == '*' || s[0] == '_' {
			if end := emphasisEnd(s, b.lastRune()); end > 0 {
				inner := s[1:end]
				b.span("ITALIC", func() { b.inline(inner) })
				s = s[end+1:]
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s)
		b.write(s[:size])
		s = s[size:]
	}
}

func isMark(c byte) bool { return strings.IndexByte("\\`*_~|[]()#!-+.>", c) >= 0 }

// lastRune is the last thing written, 0 at the start.
func (b *styled) lastRune() rune {
	r, _ := utf8.DecodeLastRuneInString(b.sb.String())
	if r == utf8.RuneError {
		return 0
	}
	return r
}

// emphasisEnd finds where the emphasis opened by s[0] closes, 0 when s[0] is
// only a character: a lone asterisk, or an underscore inside snake_case.
func emphasisEnd(s string, before rune) int {
	mark := s[0]
	if len(s) < 3 || s[1] == ' ' || s[1] == mark {
		return 0
	}
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	if mark == '_' && word(before) {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if s[i] != mark || s[i-1] == ' ' {
			continue
		}
		after, _ := utf8.DecodeRuneInString(s[i+1:])
		if mark == '_' && i+1 < len(s) && word(after) {
			continue
		}
		return i
	}
	return 0
}

// link reads [label](url) at the start of s.
func link(s string) (label, url, rest string, ok bool) {
	mid := strings.Index(s, "](")
	if !strings.HasPrefix(s, "[") || mid < 0 {
		return "", "", "", false
	}
	end := strings.IndexByte(s[mid+2:], ')')
	if end < 0 || strings.ContainsAny(s[1:mid], "[]") {
		return "", "", "", false
	}
	url, _, _ = strings.Cut(strings.TrimSpace(s[mid+2:mid+2+end]), " ")
	return s[1:mid], url, s[mid+2+end+1:], true
}
