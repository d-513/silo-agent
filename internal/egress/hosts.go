// Package egress is the network filter every Bot box and STDIO sidecar reaches
// the internet through (the silo-egress gateway). It is permissive: what is
// clearly bad is blocked, what looks like data leaving is flagged, and the
// rest goes through, logged. There are no modes and nobody is asked.
package egress

import (
	"net"
	"net/netip"
	"strings"
)

// NormHost is a host as the lists compare it: lower case, no port, no
// brackets, no trailing dot.
func NormHost(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

// ValidPattern reports whether p is a host pattern the lists take:
// "example.com" (that host and everything below it), "*.example.com" (only
// below it), an IP address, or a CIDR (every address in it).
func ValidPattern(p string) bool {
	if _, err := netip.ParsePrefix(strings.TrimSpace(p)); err == nil {
		return true
	}
	p = strings.TrimPrefix(NormHost(p), "*.")
	if p == "" || len(p) > 253 {
		return false
	}
	if _, err := netip.ParseAddr(p); err == nil {
		return true
	}
	if !strings.Contains(p, ".") {
		return false
	}
	for _, label := range strings.Split(p, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// hostSet matches hosts against patterns.
type hostSet struct {
	// self holds "example.com" patterns: the host and every name below it.
	self map[string]struct{}
	// below holds "*.example.com" patterns, stored without the "*.".
	below map[string]struct{}
	// nets holds CIDR patterns; they cover addresses only.
	nets *[]netip.Prefix
}

func newHostSet(patterns ...[]string) hostSet {
	s := hostSet{self: map[string]struct{}{}, below: map[string]struct{}{}, nets: &[]netip.Prefix{}}
	for _, list := range patterns {
		for _, p := range list {
			s.add(p)
		}
	}
	return s
}

func (s hostSet) add(p string) {
	if pre, err := netip.ParsePrefix(strings.TrimSpace(p)); err == nil {
		*s.nets = append(*s.nets, pre.Masked())
		return
	}
	p = NormHost(p)
	if rest, ok := strings.CutPrefix(p, "*."); ok {
		if rest != "" {
			s.below[rest] = struct{}{}
		}
		return
	}
	if p != "" {
		s.self[p] = struct{}{}
	}
}

func (s hostSet) empty() bool {
	return len(s.self) == 0 && len(s.below) == 0 && (s.nets == nil || len(*s.nets) == 0)
}

// match reports whether host is covered, and by which pattern.
func (s hostSet) match(host string) (string, bool) {
	host = NormHost(host)
	if host == "" {
		return "", false
	}
	// An address is matched whole: "1.1" must not cover 1.1.1.1.
	if a, err := netip.ParseAddr(host); err == nil {
		if _, ok := s.self[host]; ok {
			return host, true
		}
		if s.nets != nil {
			for _, p := range *s.nets {
				if p.Contains(a.Unmap()) {
					return p.String(), true
				}
			}
		}
		return host, false
	}
	for h := host; ; {
		if _, ok := s.self[h]; ok {
			return h, true
		}
		if h != host {
			if _, ok := s.below[h]; ok {
				return "*." + h, true
			}
		}
		i := strings.IndexByte(h, '.')
		if i < 0 {
			return "", false
		}
		h = h[i+1:]
	}
}
