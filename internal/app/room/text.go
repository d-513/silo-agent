package room

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Pass is what a Bot answers when it has nothing to add: the whole reply, so
// nothing is posted. prompts/ROOM.md tells it so.
const Pass = "<pass/>"

// passed is what of a Bot's reply goes to the room: the reply without the
// pass marker, "" when there is nothing left (it stayed quiet).
func passed(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, Pass, ""))
}

// member is one of the room's Bots, as a mention names it.
type member struct {
	ID   string
	Name string
}

// everyone are the mentions that name every Bot in the room.
var everyone = []string{"all", "everyone", "bots"}

// mentionsIn are the ids of the room's Bots that text names with @Name (any
// case, the name whole: @Ann is not @Anna), in the order the room lists them.
// @all, @everyone and @bots name them all. A longer name wins where two
// begin alike (@Research Bot is not also @Research).
func mentionsIn(text string, bots []member) []string {
	byLen := append([]member(nil), bots...)
	sort.SliceStable(byLen, func(i, j int) bool { return len(byLen[i].Name) > len(byLen[j].Name) })
	named := map[string]bool{}
	for i := 0; i < len(text); i++ {
		if text[i] != '@' || (i > 0 && wordBefore(text[:i])) {
			continue
		}
		rest := text[i+1:]
		for _, w := range everyone {
			if hasName(rest, w) {
				for _, b := range bots {
					named[b.ID] = true
				}
			}
		}
		for _, b := range byLen {
			if b.Name != "" && hasName(rest, b.Name) {
				named[b.ID] = true
				i += len(b.Name)
				break
			}
		}
	}
	var out []string
	for _, b := range bots {
		if named[b.ID] {
			out = append(out, b.ID)
		}
	}
	return out
}

// hasName reports whether s opens with name, whole (no letter or digit
// right after it).
func hasName(s, name string) bool {
	if len(s) < len(name) || !strings.EqualFold(s[:len(name)], name) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[len(name):])
	return len(s) == len(name) || !isWord(r)
}

// wordBefore reports whether s ends in a letter or digit: an @ inside a word
// (an email address) names nobody.
func wordBefore(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return isWord(r)
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// line is one room message as a Bot is handed it.
type line struct {
	Author string
	Bot    bool
	Body   string
	At     time.Time
}

// handed is what a Bot is told the room said since it last heard it: each
// message under who wrote it and when (in the Bot's zone), the ones left out
// counted, and which other Bots were at work on a reply when it was handed.
// The text is frozen in the Bot's seat once handed, so nothing in it may be
// worked out again later.
func handed(lines []line, omitted int, working []string, loc *time.Location) string {
	var b strings.Builder
	if omitted > 0 {
		fmt.Fprintf(&b, "(%d earlier message%s not shown.)\n\n", omitted, plural(omitted, " is", "s are"))
	}
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n\n")
		}
		who := l.Author
		if l.Bot {
			who += " (a Bot)"
		}
		fmt.Fprintf(&b, "%s · %s\n%s", who, l.At.In(loc).Format("15:04"), strings.TrimSpace(l.Body))
	}
	if len(working) > 0 {
		fmt.Fprintf(&b, "\n\n(At work on a reply now: %s.)", strings.Join(working, ", "))
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// fit keeps the newest lines that fit in max runes (at least the newest one),
// and says how many older ones it left out.
func fit(lines []line, maxLines, maxRunes int) ([]line, int) {
	keep, size := 0, 0
	for i := len(lines) - 1; i >= 0; i-- {
		n := utf8.RuneCountInString(lines[i].Body) + utf8.RuneCountInString(lines[i].Author) + 16
		if keep > 0 && (keep >= maxLines || size+n > maxRunes) {
			break
		}
		keep++
		size += n
	}
	return lines[len(lines)-keep:], len(lines) - keep
}

// oneLine is s on one line, cut to n runes: a room's newest message in a list.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
