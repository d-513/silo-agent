package slack

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Slack's message markup (mrkdwn) is not Markdown: *bold*, _italic_, ~strike~,
// <url|label> links, no headings or tables, and &, < and > are control
// characters that must be escaped wherever they are meant literally. The model
// writes Markdown, so outbound text is translated, and escaping everything
// outside the links Silo makes is also what keeps a model's "<!channel>" from
// paging a workspace.
var (
	fenceLine  = regexp.MustCompile("^\\s*```")
	heading    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	quote      = regexp.MustCompile(`^\s{0,3}(?:>\s?)+`)
	bullet     = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	task       = regexp.MustCompile(`^• \[([ xX])\]\s+`)
	rule       = regexp.MustCompile(`^\s{0,3}(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	imageOrLnk = regexp.MustCompile(`!?\[([^\]\n]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	autolink   = regexp.MustCompile(`<((?:https?|mailto):[^>\s]+)>`)
	boldStars  = regexp.MustCompile(`\*\*(\S(?:[^\n]*?\S)??)\*\*`)
	boldUnders = regexp.MustCompile(`__(\S(?:[^\n]*?\S)??)__`)
	italStars  = regexp.MustCompile(`(^|[^\w*])\*([^\s*](?:[^*\n]*[^\s*])?)\*($|[^\w*])`)
	strike     = regexp.MustCompile(`~~(\S(?:[^\n]*?\S)??)~~`)
	inlineCode = regexp.MustCompile("`[^`\n]+`")
	tableSep   = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)*\|?\s*$`)
	parked     = regexp.MustCompile("(\\d+)")
)

// Private-use markers: bold is held as a marker while italics are rewritten,
// so Markdown's *italic* and Slack's *bold* are never mistaken for each other,
// and finished pieces (code, links) are parked out of the way of the rest.
const (
	boldMark = ""
	parkMark = ""
)

// escape makes text literal in Slack markup.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// toMrkdwn rewrites Markdown into Slack's markup. Fenced code keeps its text
// (escaped) and loses its language tag, which Slack would show as a line; a
// table becomes a code block, the only way Slack lines columns up.
func toMrkdwn(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if fenceLine.MatchString(line) {
			inFence = !inFence
			out = append(out, "```")
			continue
		}
		if inFence {
			out = append(out, escape(line))
			continue
		}
		if rows, n := table(lines[i:]); n > 0 {
			out = append(out, rows...)
			i += n - 1
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
	var parts []string
	park := func(s string) string {
		parts = append(parts, s)
		return parkMark + strconv.Itoa(len(parts)-1) + parkMark
	}
	// Inline code is opaque: escaped, then parked while the rest is rewritten.
	line = inlineCode.ReplaceAllStringFunc(line, func(s string) string { return park(escape(s)) })
	line = imageOrLnk.ReplaceAllStringFunc(line, func(s string) string {
		m := imageOrLnk.FindStringSubmatch(s)
		return park(link(m[2], m[1]))
	})
	line = autolink.ReplaceAllStringFunc(line, func(s string) string {
		return park(link(autolink.FindStringSubmatch(s)[1], ""))
	})

	// A blockquote's > is Slack's own syntax and must stay unescaped.
	lead := ""
	if m := quote.FindString(line); m != "" {
		lead, line = "> ", line[len(m):]
	}
	line = escape(line)
	if m := heading.FindStringSubmatch(line); m != nil {
		line = boldMark + strings.Trim(m[1], "*_ ") + boldMark
	}
	line = bullet.ReplaceAllString(line, "$1• ")
	if m := task.FindStringSubmatch(strings.TrimLeft(line, " ")); m != nil {
		box := "☐ "
		if m[1] != " " {
			box = "☑ "
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		line = indent + "• " + box + strings.TrimLeft(line, " ")[len(m[0]):]
	}
	line = boldStars.ReplaceAllString(line, boldMark+"$1"+boldMark)
	line = boldUnders.ReplaceAllString(line, boldMark+"$1"+boldMark)
	// Overlapping neighbours ("*a* *b*") share a boundary character, so run twice.
	for range 2 {
		line = italStars.ReplaceAllString(line, "${1}_${2}_${3}")
	}
	line = strike.ReplaceAllString(line, "~$1~")
	line = strings.ReplaceAll(line, boldMark, "*")
	line = parked.ReplaceAllStringFunc(line, func(s string) string {
		i, _ := strconv.Atoi(parked.FindStringSubmatch(s)[1])
		return parts[i]
	})
	return lead + line
}

// link is a Slack link to url showing label. A label that only repeats the
// address is dropped; Slack shows the address itself.
func link(url, label string) string {
	url = strings.TrimSpace(url)
	label = strings.TrimSpace(strings.NewReplacer("**", "", "__", "", "`", "").Replace(label))
	url = strings.ReplaceAll(escape(url), "|", "%7C")
	if label == "" || label == strings.TrimPrefix(url, "mailto:") || escape(label) == url {
		return "<" + url + ">"
	}
	return "<" + url + "|" + escape(label) + ">"
}

// table reads a Markdown table at the head of lines (a header row, then its
// separator, then rows) into a code block with the columns padded, and says how
// many lines it took; 0 when lines does not start with one.
func table(lines []string) ([]string, int) {
	if len(lines) < 2 || !strings.Contains(lines[0], "|") || !tableSep.MatchString(lines[1]) {
		return nil, 0
	}
	rows := [][]string{cells(lines[0])}
	n := 2
	for ; n < len(lines); n++ {
		l := strings.TrimSpace(lines[n])
		if l == "" || !strings.Contains(l, "|") {
			break
		}
		rows = append(rows, cells(l))
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	width := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			width[i] = max(width[i], utf8.RuneCountInString(c))
		}
	}
	line := func(r []string) string {
		var b strings.Builder
		for i := range cols {
			c := ""
			if i < len(r) {
				c = r[i]
			}
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(c)
			if i < cols-1 {
				b.WriteString(strings.Repeat(" ", width[i]-utf8.RuneCountInString(c)))
			}
		}
		return escape(strings.TrimRight(b.String(), " "))
	}
	out := []string{"```", line(rows[0])}
	var sep []string
	for _, w := range width {
		sep = append(sep, strings.Repeat("-", max(w, 1)))
	}
	out = append(out, strings.Join(sep, "-+-"))
	for _, r := range rows[1:] {
		out = append(out, line(r))
	}
	return append(out, "```"), n
}

// cells splits a table row, with its inline Markdown read out: a code block
// shows every character as it is.
func cells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	row = strings.ReplaceAll(row, `\|`, "")
	var out []string
	for c := range strings.SplitSeq(row, "|") {
		c = strings.ReplaceAll(strings.TrimSpace(c), "", "|")
		c = imageOrLnk.ReplaceAllStringFunc(c, func(s string) string {
			m := imageOrLnk.FindStringSubmatch(s)
			if m[1] == "" || m[1] == m[2] {
				return m[2]
			}
			return m[1] + " (" + m[2] + ")"
		})
		c = strings.NewReplacer("**", "", "__", "", "`", "", "~~", "").Replace(c)
		out = append(out, c)
	}
	return out
}

// slackRef is one piece of Slack markup in a message: <@U…>, <#C…|name>,
// <!here>, <https://…|label>.
var slackRef = regexp.MustCompile(`<([^<>\n]+)>`)

// fromSlack reads a message's markup out for the Bot: people and channels by
// name, links as their words and address, and the escapes undone. user and
// channel name an id the markup does not label.
func fromSlack(text string, user, channel func(id string) string) string {
	out := slackRef.ReplaceAllStringFunc(text, func(m string) string {
		target, label, _ := strings.Cut(m[1:len(m)-1], "|")
		switch {
		case strings.HasPrefix(target, "@"):
			if label != "" {
				return "@" + label
			}
			if name := user(target[1:]); name != "" {
				return "@" + name
			}
			return target
		case strings.HasPrefix(target, "#"):
			if label != "" {
				return "#" + label
			}
			if name := channel(target[1:]); name != "" {
				return "#" + name
			}
			return target
		case strings.HasPrefix(target, "!"):
			// <!subteam^S…|@team> and <!date^…|fallback> carry their words.
			if label != "" {
				return label
			}
			base, _, _ := strings.Cut(target[1:], "^")
			return "@" + base
		}
		address := target
		if mail, ok := strings.CutPrefix(address, "mailto:"); ok {
			address = mail
		}
		if label == "" || label == target || label == address {
			return address
		}
		return label + " (" + address + ")"
	})
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(out)
}
