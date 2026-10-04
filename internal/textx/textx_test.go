package textx

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8NeverSplitsARune(t *testing.T) {
	s := "aé世" // 1 + 2 + 3 bytes
	for n := 0; n <= len(s)+1; n++ {
		got := TruncateUTF8(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d: %q is not valid UTF-8", n, got)
		}
		if len(got) > n && n < len(s) {
			t.Fatalf("n=%d: %q is longer than the cap", n, got)
		}
	}
	if got := TruncateUTF8(s, 2); got != "a" {
		t.Fatalf("cut inside é: %q", got)
	}
}

func TestCapMarksTheCut(t *testing.T) {
	if got := Cap("short", 10); got != "short" {
		t.Fatalf("uncut text changed: %q", got)
	}
	if got := Cap("abcdef", 3); got != "abc\n…truncated" {
		t.Fatalf("cut text: %q", got)
	}
}

func TestClipRunesTrimsAndCounts(t *testing.T) {
	if got := ClipRunes("  héllo wörld  ", 5); got != "héllo" {
		t.Fatalf("got %q", got)
	}
	if got := ClipRunes("  hi  ", 5); got != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestFirstLineSkipsBlanks(t *testing.T) {
	if got := FirstLine("\n  \n  second  \nthird"); got != "second" {
		t.Fatalf("got %q", got)
	}
	if got := FirstLine(" \n\t\n"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestValidUTF8ReplacesBadBytesAndDropsNUL(t *testing.T) {
	got := ValidUTF8("a\x00b\xffc")
	if !utf8.ValidString(got) || strings.ContainsRune(got, 0) {
		t.Fatalf("not clean: %q", got)
	}
	if !strings.HasPrefix(got, "ab") {
		t.Fatalf("lost the good bytes: %q", got)
	}
}
