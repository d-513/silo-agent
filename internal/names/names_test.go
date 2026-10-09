package names

import (
	"regexp"
	"testing"
)

// A name is a subdomain, so it must be a DNS label.
var label = regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+$`)

func TestNewIsADNSLabel(t *testing.T) {
	seen := map[string]bool{}
	for range 2000 {
		n := New()
		if !label.MatchString(n) || len(n) > 63 {
			t.Fatalf("bad name %q", n)
		}
		seen[n] = true
	}
	if len(seen) < 1900 {
		t.Fatalf("only %d distinct names in 2000", len(seen))
	}
}

func TestWordListsAreCleanAndUnique(t *testing.T) {
	for name, words := range map[string][]string{"adjectives": adjectives, "colors": colors, "animals": animals} {
		seen := map[string]bool{}
		for _, w := range words {
			if !regexp.MustCompile(`^[a-z]+$`).MatchString(w) {
				t.Errorf("%s: %q is not a plain lowercase word", name, w)
			}
			if seen[w] {
				t.Errorf("%s: %q twice", name, w)
			}
			seen[w] = true
		}
	}
	if n := len(adjectives) * len(colors) * len(animals); n < 200000 {
		t.Fatalf("only %d combinations", n)
	}
}
