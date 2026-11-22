package siloosd

import (
	"net/netip"
	"regexp"
	"strings"
)

// Everything a caller hands siloosd is checked here before it reaches a
// command's arguments. The commands run without a shell, so the danger is
// not quoting but an argument that a tool would read as an option or as
// several settings: none of these accepts a leading '-', a space, a comma or
// a control character.

var (
	uuidRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	zoneRE  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+\-]*(/[A-Za-z0-9_+\-]+){0,2}$`)
	pathRE  = regexp.MustCompile(`^/org/freedesktop/NetworkManager/Checkpoint/[0-9]+$`)
)

// validHostname is a lower-case DNS name of labels (RFC 1123), 253 at most.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

func validZone(z string) bool {
	return len(z) <= 64 && zoneRE.MatchString(z) && !strings.Contains(z, "..")
}

func validUUID(u string) bool { return uuidRE.MatchString(u) }

// ipv4Prefix reads an address with its prefix length ("10.0.0.5/24"). A bare
// address is a /32.
func ipv4Prefix(s string) (netip.Prefix, bool) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		s += "/32"
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() || p.Bits() == 0 {
		return netip.Prefix{}, false
	}
	a := p.Addr()
	if a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() || a.IsLinkLocalUnicast() {
		return netip.Prefix{}, false
	}
	return p, true
}

func ipv4Addr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() || a.IsLoopback() {
		return netip.Addr{}, false
	}
	return a, true
}

// dnsAddr is a DNS server: IPv4 or IPv6, no zone.
func dnsAddr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || a.Zone() != "" || a.IsUnspecified() || a.IsMulticast() {
		return netip.Addr{}, false
	}
	return a, true
}
