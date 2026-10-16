package app

import "testing"

// A surrogate's real value goes only to hosts somebody named: never a public
// suffix, and never an address range.
func TestCheckSecretHost(t *testing.T) {
	for _, h := range []string{"api.github.com", "*.example.com", "45.9.148.9"} {
		if err := checkSecretHost(h); err != nil {
			t.Errorf("%s: %v", h, err)
		}
	}
	for _, h := range []string{"com", "*.co.uk", "github.io", "*.githubusercontent.com", "0.0.0.0/0", "45.9.148.0/24", "not a host"} {
		if err := checkSecretHost(h); err == nil {
			t.Errorf("%s was allowed", h)
		}
	}
}
