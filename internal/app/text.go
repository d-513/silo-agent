package app

import (
	"unicode/utf8"

	"silo.agent/internal/db"
)

// validUTF8 replaces invalid UTF-8 bytes and drops NULs so protobuf string
// fields stay marshalable and Postgres text accepts them. Command output (a
// docx dump, terminal bytes, a masking splice) can otherwise poison a run event
// and make the whole chat unreplayable. It matches what the DB layer stores, so
// a live event and its replay are the same string.
func validUTF8(s string) string {
	return db.CleanText(s)
}

// truncateUTF8 cuts s to at most n bytes without splitting a rune. Slicing a
// Go string at an arbitrary byte offset can leave a partial multibyte rune,
// which is invalid UTF-8 and unparseable over protobuf.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// capText cuts s to at most n bytes (never mid-rune) and marks the cut, so the
// reader and the model can both see that text is missing.
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return truncateUTF8(s, n) + "\n…truncated"
}
