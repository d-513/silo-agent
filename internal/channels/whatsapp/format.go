package whatsapp

import (
	"regexp"
	"strings"
)

// WhatsApp has its own light markup: *bold*, _italic_, ~strike~, `code` and
// ``` fences. The model writes Markdown, whose **bold** and [links](url) would
// show up as literal punctuation, so outbound text is translated first.
var (
	fenceLine  = regexp.MustCompile("^\\s*```")
	heading    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	bullet     = regexp.MustCompile(`^(\s*)[*+]\s+`)
	rule       = regexp.MustCompile(`^\s{0,3}(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	imageOrLnk = regexp.MustCompile(`!?\[([^\]\n]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	boldStars  = regexp.MustCompile(`\*\*(\S(?:[^\n]*?\S)??)\*\*`)
	boldUnders = regexp.MustCompile(`__(\S(?:[^\n]*?\S)??)__`)
	italStars  = regexp.MustCompile(`(^|[^\w*])\*([^\s*](?:[^*\n]*[^\s*])?)\*($|[^\w*])`)
	strike     = regexp.MustCompile(`~~(\S(?:[^\n]*?\S)??)~~`)
	inlineCode = regexp.MustCompile("`[^`\n]+`")
)

// Private-use markers: bold is held as a marker while italics are rewritten, so
// the *…* that markdown uses for italic and the *…* WhatsApp uses for bold
// never get mistaken for each other.
const (
	boldMark = ""
	codeMark = ""
)

// toWhatsApp rewrites Markdown into WhatsApp's markup. Fenced code is left
// exactly as written.
func toWhatsApp(md string) string {
	var out []string
	inFence := false
	for line := range strings.SplitSeq(md, "\n") {
		if fenceLine.MatchString(line) {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		out = append(out, convertLine(line))
	}
	return strings.Join(out, "\n")
}

func convertLine(line string) string {
	if rule.MatchString(line) {
		return ""
	}
	// Inline code is opaque: park it while the rest is rewritten.
	var codes []string
	line = inlineCode.ReplaceAllStringFunc(line, func(s string) string {
		codes = append(codes, s)
		return codeMark + string(rune('0'+len(codes)-1)) + codeMark
	})

	if m := heading.FindStringSubmatch(line); m != nil {
		line = boldMark + strings.Trim(m[1], "*_ ") + boldMark
	}
	line = bullet.ReplaceAllString(line, "$1- ")
	line = imageOrLnk.ReplaceAllStringFunc(line, func(s string) string {
		m := imageOrLnk.FindStringSubmatch(s)
		if m[1] == "" || m[1] == m[2] {
			return m[2]
		}
		return m[1] + " (" + m[2] + ")"
	})
	line = boldStars.ReplaceAllString(line, boldMark+"$1"+boldMark)
	line = boldUnders.ReplaceAllString(line, boldMark+"$1"+boldMark)
	// Overlapping neighbours ("*a* *b*") share a boundary character, so run twice.
	for range 2 {
		line = italStars.ReplaceAllString(line, "${1}_${2}_${3}")
	}
	line = strike.ReplaceAllString(line, "~$1~")
	line = strings.ReplaceAll(line, boldMark, "*")

	for i, c := range codes {
		line = strings.Replace(line, codeMark+string(rune('0'+i))+codeMark, c, 1)
	}
	return line
}
