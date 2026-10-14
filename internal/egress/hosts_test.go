package egress

import "testing"

func TestNormHost(t *testing.T) {
	for in, want := range map[string]string{
		"Example.COM":         "example.com",
		"example.com.":        "example.com",
		"example.com:443":     "example.com",
		" example.com ":       "example.com",
		"[::1]:8080":          "::1",
		"[2001:db8::1]":       "2001:db8::1",
		"10.0.0.1:53":         "10.0.0.1",
		"":                    "",
		"Sub.Example.Com.:80": "sub.example.com",
	} {
		if got := NormHost(in); got != want {
			t.Errorf("NormHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidPattern(t *testing.T) {
	good := []string{"example.com", "*.example.com", "a-b.example.co.uk", "_dmarc.example.com", "10.0.0.1", "2001:db8::1", "EXAMPLE.com."}
	bad := []string{"", "*.", "com", "*.com", "exa mple.com", "example..com", "ex!ample.com", "*.*.example.com", "http://example.com"}
	for _, p := range good {
		if !ValidPattern(p) {
			t.Errorf("ValidPattern(%q) = false, want true", p)
		}
	}
	for _, p := range bad {
		if ValidPattern(p) {
			t.Errorf("ValidPattern(%q) = true, want false", p)
		}
	}
}

func TestHostSetMatch(t *testing.T) {
	s := newHostSet([]string{"example.com", "*.wild.org", "10.1.1.1", "1.1"})
	cases := []struct {
		host, pattern string
		ok            bool
	}{
		{"example.com", "example.com", true},
		{"api.example.com", "example.com", true},
		{"a.b.example.com:443", "example.com", true},
		{"EXAMPLE.COM.", "example.com", true},
		{"notexample.com", "", false},
		{"example.com.evil.net", "", false},
		{"wild.org", "", false},
		{"x.wild.org", "*.wild.org", true},
		{"y.x.wild.org", "*.wild.org", true},
		{"10.1.1.1", "10.1.1.1", true},
		{"10.1.1.2", "", false},
		// An address is matched whole, never walked like a name.
		{"1.1.1.1", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		p, ok := s.match(c.host)
		if ok != c.ok || (ok && p != c.pattern) {
			t.Errorf("match(%q) = %q, %v; want %q, %v", c.host, p, ok, c.pattern, c.ok)
		}
	}
	if !newHostSet().empty() || s.empty() {
		t.Error("empty() is wrong")
	}
}
