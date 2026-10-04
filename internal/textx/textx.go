// Package textx holds the small string helpers the control plane shares: making
// text safe to store and send, and cutting it to a size without breaking a rune.
package textx

import (
	"strings"
	"unicode/utf8"

	"silo.agent/internal/db"
)

// ValidUTF8 replaces invalid UTF-8 bytes and drops NULs so protobuf string
// fields stay marshalable and Postgres text accepts them. Command output (a
// docx dump, terminal bytes, a masking splice) can otherwise poison a run event
// and make the whole chat unreplayable. It matches what the DB layer stores, so
// a live event and its replay are the same string.
func ValidUTF8(s string) string {
	return db.CleanText(s)
}

// TruncateUTF8 cuts s to at most n bytes without splitting a rune. Slicing a
// Go string at an arbitrary byte offset can leave a partial multibyte rune,
// which is invalid UTF-8 and unparseable over protobuf.
func TruncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Cap cuts s to at most n bytes (never mid-rune) and marks the cut, so the
// reader and the model can both see that text is missing.
func Cap(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return TruncateUTF8(s, n) + "\n…truncated"
}

// ClipRunes trims s and cuts it to at most n runes.
func ClipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = strings.TrimSpace(string([]rune(s)[:n]))
	}
	return s
}

// FirstLine is the first non-blank line of s, trimmed; "" when there is none.
func FirstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// Plural is "s" unless n is exactly 1, for "%d file%s".
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
