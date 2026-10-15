package egress

import (
	"strings"
	"testing"
)

func classOf(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return 'd'
	case c >= 'A' && c <= 'Z':
		return 'U'
	case c >= 'a' && c <= 'z':
		return 'L'
	default:
		return c
	}
}

func TestSurrogateKeepsShape(t *testing.T) {
	cases := []string{
		"ghp_0123456789abcdefABCDEFxyz",
		"sk-proj-abcdefghijklmnopqrstuvwxyz",
		"AKIAIOSFODNN7EXAMPLE",
		"xoxb-1234567890-abcdefghij",
		"1234-5678-9012-3456",
		"aB3dE5fG7hI9jK1lM3nO5pQ7",
	}
	for _, real := range cases {
		got := NewSurrogate(real)
		if got == "" {
			t.Fatalf("NewSurrogate(%q) = empty", real)
		}
		if len(got) != len(real) {
			t.Errorf("%q: length %d, want %d", real, len(got), len(real))
		}
		if got == real {
			t.Errorf("%q: the stand-in is the secret itself", real)
		}
		p := surrogatePrefix(real)
		if got[:p] != real[:p] {
			t.Errorf("%q: prefix %q changed to %q", real, real[:p], got[:p])
		}
		for i := p; i < len(real); i++ {
			if classOf(got[i]) != classOf(real[i]) {
				t.Errorf("%q: position %d class %c, want %c (got %q)", real, i, classOf(got[i]), classOf(real[i]), got)
				break
			}
		}
	}
}

func TestSurrogatePrefixEndsAtSeparator(t *testing.T) {
	if p := surrogatePrefix("ghp_ABCDEFGH"); p != 4 {
		t.Errorf("ghp_ prefix = %d, want 4", p)
	}
	if p := surrogatePrefix("sk-proj-xyz"); p != 3 {
		t.Errorf("sk- prefix = %d, want 3", p)
	}
	if p := surrogatePrefix("AKIAIOSFODNN7EXAMPLE"); p != 0 {
		t.Errorf("no separator: prefix = %d, want 0", p)
	}
	// A separator past the first eight bytes is not a prefix.
	if p := surrogatePrefix("abcdefghij_klm"); p != 0 {
		t.Errorf("late separator: prefix = %d, want 0", p)
	}
}

func TestSurrogateTooShort(t *testing.T) {
	for _, short := range []string{"", "a", "short", "1234567"} {
		if got := NewSurrogate(short); got != "" {
			t.Errorf("NewSurrogate(%q) = %q, want empty", short, got)
		}
	}
	if got := NewSurrogate("12345678"); got == "" {
		t.Error("an eight-byte value has no stand-in")
	}
}

func TestSurrogateIsRandom(t *testing.T) {
	real := "ghp_0123456789abcdefABCDEFxyz"
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		seen[NewSurrogate(real)] = true
	}
	if len(seen) < 2 {
		t.Errorf("20 stand-ins gave %d distinct values", len(seen))
	}
	for got := range seen {
		if strings.EqualFold(got, real) {
			t.Errorf("a stand-in equals the secret: %q", got)
		}
	}
}
