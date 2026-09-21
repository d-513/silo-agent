package ids

import (
	"encoding/hex"
	"testing"
)

func TestNewIsUniqueHex(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		v := New()
		if len(v) != 32 {
			t.Fatalf("New length %d", len(v))
		}
		if _, err := hex.DecodeString(v); err != nil {
			t.Fatalf("New not hex: %q", v)
		}
		if seen[v] {
			t.Fatalf("duplicate id %q", v)
		}
		seen[v] = true
	}
}

func TestTokenIsLongerThanID(t *testing.T) {
	tok := Token()
	if len(tok) != 64 {
		t.Fatalf("Token length %d", len(tok))
	}
	if _, err := hex.DecodeString(tok); err != nil {
		t.Fatalf("Token not hex: %q", tok)
	}
}

func TestHashDeterministic(t *testing.T) {
	a := Hash("same")
	b := Hash("same")
	if a != b {
		t.Fatal("hash not deterministic")
	}
	if a == Hash("different") {
		t.Fatal("hash collision on trivial input")
	}
	if len(a) != 64 {
		t.Fatalf("hash length %d", len(a))
	}
}

func TestCrestInRange(t *testing.T) {
	for _, name := range []string{"", "Alpha", "Beta"} {
		for _, id := range []string{"", "abc123"} {
			got := Crest(name, id)
			if got < 0 || got >= CrestCount {
				t.Fatalf("Crest(%q,%q)=%d out of range", name, id, got)
			}
		}
	}
	if Crest("Alpha", "1") != Crest("Alpha", "1") {
		t.Fatal("Crest not deterministic")
	}
}

func TestPackCrestWraps(t *testing.T) {
	if got := PackCrest(CrestShapes, CrestColors); got != 0 {
		t.Fatalf("wrap: %d", got)
	}
	if got := PackCrest(0, 1); got != CrestShapes {
		t.Fatalf("color offset: %d", got)
	}
}
