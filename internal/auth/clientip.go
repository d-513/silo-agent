package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP is the address a request came from. That is the peer of the
// connection, unless the peer is one of the operator's trusted proxies: then it
// is the last address in X-Forwarded-For that is not itself a trusted proxy.
// The header is read from the right because a client can put anything it likes
// in front; only what a trusted proxy appended is believed.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	peer, ok := parseAddr(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}
	if !inAny(peer, trusted) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		a, ok := parseAddr(hop)
		if !ok {
			break
		}
		client = a
		if !inAny(a, trusted) {
			break
		}
	}
	return client.String()
}

func parseAddr(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return a.Unmap(), true
	}
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func inAny(a netip.Addr, nets []netip.Prefix) bool {
	for _, n := range nets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}
