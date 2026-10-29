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

// WordTime is when one word of a piece ends in its audio.
type WordTime struct {
	Text  string
	EndMs int
}

// HeardWords is Heard with the voice's own word timings: the words that had
// finished playing, cut from the text itself so its punctuation stays.
func HeardWords(text string, words []WordTime, playedMs int) (heard, unheard string) {
	if len(words) == 0 {
		return "", text
	}
	n := 0
	for _, w := range words {
		if w.EndMs <= playedMs {
			n++
		}
	}
	if n >= len(words) {
		return strings.TrimSpace(text), ""
	}
	fields := strings.Fields(text)
	if n > len(fields) {
		n = len(fields)
	}
	return strings.Join(fields[:n], " "), strings.Join(fields[n:], " ")
}
