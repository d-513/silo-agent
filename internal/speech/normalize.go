// Package speech prepares text for a voice and keeps track of what was said:
// markdown made speakable, text cut into sentences a speech model can start
// on early, and what a listener heard when they cut the voice off. It does no
// I/O.
package speech

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Pointers are what is said in place of things that do not read aloud. They
// are fixed text in the listener's language, never a model's.
type Pointers struct {
	Code  string
	Table string
}

var pointers = map[string]Pointers{
	"en": {Code: "I've put the code in the chat.", Table: "There's a table in the chat."},
	"pl": {Code: "Kod jest w czacie.", Table: "Tabela jest w czacie."},
	"de": {Code: "Der Code steht im Chat.", Table: "Die Tabelle steht im Chat."},
}

// PointersFor are the pointers in a language (an ISO-639-1 code), English when
// there are none in it.
func PointersFor(lang string) Pointers {
	if p, ok := pointers[strings.ToLower(lang)]; ok {
		return p
	}
	return pointers["en"]
}

var (
	reFence    = regexp.MustCompile("(?s)```.*?(```|$)")
	reImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)
	reAuto     = regexp.MustCompile(`<(https?://[^>\s]+)>`)
	reURL      = regexp.MustCompile(`https?://[^\s)\]>]+`)
	reTag      = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	reInline   = regexp.MustCompile("`([^`]*)`")
	reHeading  = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	reBullet   = regexp.MustCompile(`^\s*(?:[-*+•]|\d{1,3}[.)])\s+`)
	reQuote    = regexp.MustCompile(`^\s*>\s?`)
	reRule     = regexp.MustCompile(`^\s*(?:[-*_]\s*){3,}$`)
	reTableRow = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	reEmphasis = regexp.MustCompile(`(\*\*|__|\*|~~)([^\s*_~](?:.*?[^\s*_~])?)(\*\*|__|\*|~~)`)
	reUnder    = regexp.MustCompile(`(^|[\s(])_([^_\s](?:[^_]*[^_\s])?)_([\s.,;:!?)]|$)`)
	reSentinel = regexp.MustCompile(`<section_send\s*/>`)
	reSpaces   = regexp.MustCompile(`[ \t]+`)
)

// ForSpeech turns a model's markdown into text a voice can read: code and
// tables become a pointer to the chat, links their words, a bare URL its
// site, and the markup goes. Each line of a list or a heading ends as a
// sentence, so the voice pauses where the eye would.
func ForSpeech(md string, p Pointers) string {
	md = reSentinel.ReplaceAllString(md, "")
	md = reFence.ReplaceAllLiteralString(md, p.Code)
	md = reImage.ReplaceAllString(md, "")
	md = reLink.ReplaceAllString(md, "$1")
	md = reAuto.ReplaceAllString(md, "$1")
	md = reURL.ReplaceAllStringFunc(md, siteOf)
	md = reTag.ReplaceAllString(md, "")
	md = reInline.ReplaceAllString(md, "$1")

	var out []string
	inTable := false
	for line := range strings.SplitSeq(md, "\n") {
		if reTableRow.MatchString(line) {
			if !inTable {
				out = append(out, p.Table)
			}
			inTable = true
			continue
		}
		inTable = false
		if reRule.MatchString(line) {
			continue
		}
		heading := reHeading.MatchString(line)
		line = reHeading.ReplaceAllString(line, "")
		bullet := reBullet.MatchString(line)
		line = reBullet.ReplaceAllString(line, "")
		line = reQuote.ReplaceAllString(line, "")
		line = stripEmphasis(line)
		line = strings.TrimSpace(dropPictographs(line))
		if line == "" {
			out = append(out, "")
			continue
		}
		if (heading || bullet) && !endsSentence(line) {
			line += "."
		}
		out = append(out, line)
	}
	// Lines of one paragraph run on; a blank line is a pause.
	var b strings.Builder
	for i, line := range out {
		if line == "" {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			}
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			if !endsSentence(out[i-1]) && out[i-1] != "" {
				b.WriteString(",")
			}
			b.WriteString(" ")
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(reSpaces.ReplaceAllString(b.String(), " "))
}

func stripEmphasis(s string) string {
	for range 3 {
		next := reEmphasis.ReplaceAllString(s, "$2")
		next = reUnder.ReplaceAllString(next, "$1$2$3")
		if next == s {
			break
		}
		s = next
	}
	return s
}

// siteOf reads a URL as the site it is on: nobody wants a query string read
// out.
func siteOf(raw string) string {
	u, err := url.Parse(strings.TrimRight(raw, ".,;:!?"))
	if err != nil || u.Host == "" {
		return raw
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if strings.HasSuffix(raw, ".") {
		return host + "."
	}
	return host
}

// dropPictographs removes emoji, which voices read as their names.
func dropPictographs(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF, r == 0xFE0F, r == 0x200D:
			return -1
		case unicode.Is(unicode.Co, r):
			return -1
		}
		return r
	}, s)
}

func endsSentence(s string) bool {
	s = strings.TrimRight(s, `"'”’)»]`)
	if s == "" {
		return false
	}
	switch []rune(s)[len([]rune(s))-1] {
	case '.', '!', '?', '…', ':', ';':
		return true
	}
	return false
}
