package masker

import "testing"

func TestApply(t *testing.T) {
	m := New()
	m.Add("hunter22secret")
	got := m.Apply("pw=hunter22secret done")
	if got != "pw=*** done" {
		t.Fatalf("got %q", got)
	}
	b64 := m.Apply("c3VwZXJsb25nc2VjcmV0dmFsdWU=") // not added
	_ = b64
	m.Add("superlongsecretvalue")
	if !contains(m.Apply("see superlongsecretvalue here"), "***") {
		t.Fatal("expected mask")
	}
}

func TestShortIgnored(t *testing.T) {
	m := New()
	m.Add("short")
	if m.Apply("short") != "short" {
		t.Fatal("short secrets must not mask")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
