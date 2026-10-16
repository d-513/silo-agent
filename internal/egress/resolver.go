package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// Resolver is how the gateway looks up the names its clients ask for. The
// gateway sits on every client network, and the engine lists each network's
// own DNS in the gateway's /etc/resolv.conf, ahead of the upstream one. A
// client network is internal, so its DNS (podman's aardvark) knows no outside
// names and answers NXDOMAIN, which Go takes as final: every lookup would fail
// and every request end in "the filter could not reach". Refusing to dial a
// nameserver on a client network makes Go move on to the next one listed.
func (gw *Gateway) Resolver() *net.Resolver {
	var d net.Dialer
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if ap, err := netip.ParseAddrPort(address); err == nil && gw.Client(ap.Addr()) != nil {
				return nil, fmt.Errorf("egress: %s is a client network's DNS", ap.Addr())
			}
			return d.DialContext(ctx, network, address)
		},
	}
}
