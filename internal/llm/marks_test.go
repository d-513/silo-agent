package llm

import (
	"os"
	"regexp"
	"testing"
)

// Every provider has a mark in the console (web/src/admin/providerIcons.ts),
// so none stands in the model map and the provider grid as two initials
// beside the others' logos. `local` is any self-hosted server and takes a
// server glyph there; the dummy is only in tests.
func TestProvidersHaveMarks(t *testing.T) {
	src, err := os.ReadFile("../../web/src/admin/providerIcons.ts")
	if err != nil {
		t.Fatal(err)
	}
	table := regexp.MustCompile(`(?s)export const PROVIDER_ICONS[^{]*\{(.*?)\n\};`).FindSubmatch(src)
	if table == nil {
		t.Fatal("providerIcons.ts has no PROVIDER_ICONS table")
	}
	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  ([a-z0-9_]+): \{`).FindAllSubmatch(table[1], -1) {
		have[string(m[1])] = true
	}
	for _, d := range Descriptors() {
		if d.ID == "local" || d.ID == "dummy" {
			continue
		}
		if !have[d.ID] {
			t.Errorf("provider %q has no mark in web/src/admin/providerIcons.ts", d.ID)
		}
	}
}
