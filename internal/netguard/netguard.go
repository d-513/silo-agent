// Package netguard keeps the connections the control plane makes for a Bot's
// owner off the networks only the control plane can see.
//
// A connector's URL, a mail server, a calendar, a skill's download address are
// all typed in by a user, and it is the control plane, not their browser, that
// connects. Left alone, a user of a hosted Silo can point one at 127.0.0.1,
// at the database beside it, at a cloud metadata service or at anything else
// on the operator's network, and read the answer through the connector.
//
// The check is made in the dialer's Control hook, on the address the socket is
// about to connect to. That is after DNS, so a name that resolves inside is
// refused like the address itself, a redirect that leads inside is refused at
// its own dial, and a name that answers differently the second time (DNS
// rebinding) gains nothing: there is no earlier lookup to disagree with.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is what a refused connection's error wraps.
var ErrBlocked = errors.New("private network address")

// Exception is an address range the operator opened: every port of it, or one.
type Exception struct {
	Prefix netip.Prefix
	// Port is the only port opened; 0 opens them all.
	Port uint16
}

// Policy is what outbound connections made for users may reach. The zero
// Policy refuses nothing.
type Policy struct {
	// Block refuses internal addresses (see Internal).
	Block bool
	// Allow lists the internal addresses that stay reachable.
	Allow []Exception
}

// extra are the ranges that are not public and that net/netip has no method
// for.
var extra = prefixes(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // carrier-grade NAT; Tailscale lives here
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // documentation
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
	"240.0.0.0/4",     // reserved, and broadcast
	"2001:db8::/32",   // documentation
)

var (
	nat64  = netip.MustParsePrefix("64:ff9b::/96")
	sixTo4 = netip.MustParsePrefix("2002::/16")
)

func prefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// Internal reports whether a is anything but a public unicast address:
// loopback, a private or link-local range (the cloud metadata address is one),
// carrier-grade NAT, multicast, unspecified or reserved. An IPv6 address that
// carries an IPv4 one (mapped, NAT64, 6to4) is judged by the address inside,
// since that is where the packets end up.
func Internal(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range extra {
		if p.Contains(a) {
			return true
		}
	}
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64.Contains(a):
			return Internal(netip.AddrFrom4([4]byte(b[12:16])))
		case sixTo4.Contains(a):
			return Internal(netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	return false
}

// ParseAllow reads the operator's exceptions: a comma- or space-separated list
// of an IP or a CIDR, each optionally with one port (127.0.0.1:9223,
// 10.0.0.0/8, [::1]:8080). Names are not taken: the check sees addresses only.
func ParseAllow(s string) ([]Exception, error) {
	var out []Exception
	for _, item := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		e, err := parseException(item)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func parseException(item string) (Exception, error) {
	bad := fmt.Errorf("%q is not an IP address or CIDR, with an optional :port", item)
	if p, err := netip.ParsePrefix(item); err == nil {
		return Exception{Prefix: p.Masked()}, nil
	}
	if a, err := netip.ParseAddr(strings.Trim(item, "[]")); err == nil {
		a = a.Unmap()
		return Exception{Prefix: netip.PrefixFrom(a, a.BitLen())}, nil
	}
	host, port, err := net.SplitHostPort(item)
	if err != nil {
		return Exception{}, bad
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Exception{}, bad
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return Exception{}, bad
	}
	a = a.Unmap()
	return Exception{Prefix: netip.PrefixFrom(a, a.BitLen()), Port: uint16(n)}, nil
}

// Check is nil when the policy lets a connection to ap be made.
func (p Policy) Check(ap netip.AddrPort) error {
	a := ap.Addr().Unmap()
	if !p.Block || !Internal(a) {
		return nil
	}
	for _, e := range p.Allow {
		if e.Prefix.Contains(a) && (e.Port == 0 || e.Port == ap.Port()) {
			return nil
		}
	}
	return fmt.Errorf("%s is a %w, which this Silo does not let connectors, mail, calendars or downloads reach", a, ErrBlocked)
}

// control is the dialer hook: it sees the literal address of every connection
// attempt, each one a name resolved to.
func (p Policy) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		if !p.Block {
			return nil
		}
		return fmt.Errorf("%q: %w", address, ErrBlocked)
	}
	return p.Check(ap)
}

// Dialer is a net.Dialer that makes only the connections the policy allows.
func (p Policy) Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second, Control: p.control}
}

// DialContext dials under the policy, with a 30 second connect timeout.
func (p Policy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return p.Dialer(30*time.Second).DialContext(ctx, network, addr)
}

// Transport is http.DefaultTransport's settings over the policy's dialer.
func (p Policy) Transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = p.DialContext
	return t
}

// Client is an HTTP client whose every connection, redirects included, goes
// through the policy. A zero timeout means none.
func (p Policy) Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: p.Transport(), Timeout: timeout}
}
