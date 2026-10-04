package app

import (
	"testing"

	"silo.agent/internal/security"
)

func TestParseVerdict(t *testing.T) {
	cases := map[string]string{
		"approve":         security.Allow,
		"Approve.\n":      security.Allow,
		"allow":           security.Allow,
		"deny":            security.Deny,
		"Denied":          security.Deny,
		"ask":             security.Ask,
		"human review":    security.Ask,
		"":                security.Ask,
		"whatever":        security.Ask,
		"yes, it is safe": security.Allow,
	}
	for in, want := range cases {
		if got := parseVerdict(in); got != want {
			t.Fatalf("parseVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}
