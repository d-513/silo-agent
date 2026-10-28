package speech

import (
	"strings"
	"unicode"
)

// Heard splits what a voice was saying into what the listener heard and what
// they did not, from how much of its audio played. With no word timings the
// share of audio is taken as the share of the text, and the cut moves back to
// the last whole word, so nothing half heard is claimed as said.
func Heard(text string, playedMs, totalMs int) (heard, unheard string) {
	if totalMs <= 0 || playedMs >= totalMs {
		return text, ""
	}
	if playedMs <= 0 {
		return "", text
	}
	rs := []rune(text)
	at := len(rs) * playedMs / totalMs
	// Back to the end of the last whole word.
	for at > 0 && at < len(rs) && !unicode.IsSpace(rs[at]) {
		at--
	}
	return strings.TrimSpace(string(rs[:at])), strings.TrimSpace(string(rs[at:]))
}
