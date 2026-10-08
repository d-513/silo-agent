package catalog

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"silo.agent/internal/db"
)

// Identifier normalises text into a library connector's identifier: lowercase
// ASCII letters and digits, with every run of anything else as one underscore
// ("fal.ai" → "fal_ai"). It is the name an operator writes in
// autoenable_connectors, so it is compared in this form and never by case.
func Identifier(s string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(s) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteByte('_')
		}
		gap = false
		b.WriteRune(r)
	}
	return b.String()
}

// Identifiers normalises a list of identifiers, dropping blanks and repeats
// and keeping the order.
func Identifiers(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		id := Identifier(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// IdentifierTaken reports whether a library preset other than exceptID
// already holds ident.
func IdentifierTaken(gdb *gorm.DB, ident, exceptID string) bool {
	var n int64
	gdb.Model(&db.Connector{}).Where("kind = ? AND identifier = ? AND id <> ?", KindLibrary, ident, exceptID).Count(&n)
	return n > 0
}

// FreeIdentifier is want, or want_2, want_3… when another preset holds it.
func FreeIdentifier(gdb *gorm.DB, want, exceptID string) string {
	if !IdentifierTaken(gdb, want, exceptID) {
		return want
	}
	for n := 2; ; n++ {
		if cand := fmt.Sprintf("%s_%d", want, n); !IdentifierTaken(gdb, cand, exceptID) {
			return cand
		}
	}
}

// backfillIdentifiers names the library presets made before identifiers
// existed: a seeded one takes its catalog identifier while that is still free,
// anything else keeps its own id as the identifier.
func backfillIdentifiers(gdb *gorm.DB, entries []entry) error {
	var rows []db.Connector
	if err := gdb.Select("id, seed_key").Where("kind = ? AND COALESCE(identifier, '') = ''", KindLibrary).
		Order("created_at").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	byKey := make(map[string]string, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e.Identifier
	}
	for _, r := range rows {
		ident := byKey[r.SeedKey]
		if ident == "" || IdentifierTaken(gdb, ident, r.ID) {
			ident = r.ID
		}
		if err := gdb.Model(&db.Connector{}).Where("id = ?", r.ID).Update("identifier", ident).Error; err != nil {
			return err
		}
	}
	return nil
}
