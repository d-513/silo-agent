package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// upstreamRest is how long an upstream resolver that let a query time out is
// passed over while another one is left.
const upstreamRest = 30 * time.Second

// Resolver is how the gateway looks up the names its clients ask for.
//
// The control plane names the upstream resolvers in every apply (the
// operator's egress.dns, else its own system resolvers), and every lookup goes
// to them whatever the gateway's /etc/resolv.conf says. That file is the
// engine's to write and it cannot be trusted here: the gateway sits on every
// client network, a client network is internal, and its DNS (podman's
// aardvark) answers NXDOMAIN for every outside name. Podman lists those
// servers first, and on a restart it writes only those, dropping the host's
// own, so a gateway that read the file worked until its first restart.
//
// With no upstream named, the file is all there is, and a nameserver on a
// client network is refused so Go moves on to the next one listed.
func (gw *Gateway) Resolver() *net.Resolver {
	var d net.Dialer
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if up := gw.upstreams.pick(); up != nil {
				c, err := d.DialContext(ctx, network, up.addr)
				if err != nil {
					up.fail()
					return nil, err
				}
				uc := &upstreamConn{Conn: c, up: up}
				if pc, ok := c.(net.PacketConn); ok {
					// Go's DNS client frames a query by whether its conn
					// is a PacketConn: hiding that would send UDP the TCP
					// length prefix.
					return &upstreamPacketConn{upstreamConn: uc, pc: pc}, nil
				}
				return uc, nil
			}
			if ap, err := netip.ParseAddrPort(address); err == nil && gw.Client(ap.Addr()) != nil {
				return nil, fmt.Errorf("egress: %s is a client network's DNS", ap.Addr())
			}
			return d.DialContext(ctx, network, address)
		},
	}
}

// upstreams is the ordered list of resolvers the control plane named. The
// first one that has not just failed is asked, the way resolv.conf is read,
// so a dead primary costs one timeout every upstreamRest, not every query.
type upstreams struct {
	mu   sync.Mutex
	list []*upstream
}

type upstream struct {
	addr string
	mu   sync.Mutex
	// failedAt is when a query to it last went unanswered.
	failedAt time.Time
}

func (u *upstream) fail() {
	u.mu.Lock()
	u.failedAt = time.Now()
	u.mu.Unlock()
}

func (u *upstream) ok() {
	u.mu.Lock()
	u.failedAt = time.Time{}
	u.mu.Unlock()
}

func (u *upstream) resting(now time.Time) (bool, time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return !u.failedAt.IsZero() && now.Sub(u.failedAt) < upstreamRest, u.failedAt
}

// set replaces the list, keeping what is known about a server still on it.
func (us *upstreams) set(addrs []string) {
	us.mu.Lock()
	defer us.mu.Unlock()
	old := map[string]*upstream{}
	for _, u := range us.list {
		old[u.addr] = u
	}
	var list []*upstream
	for _, a := range addrs {
		ap, err := netip.ParseAddrPort(a)
		if err != nil {
			continue
		}
		a = ap.String()
		if u, ok := old[a]; ok {
			list = append(list, u)
			continue
		}
		list = append(list, &upstream{addr: a})
	}
	us.list = list
}

// pick is the first upstream not resting, else the one that rested longest;
// nil when none is named.
func (us *upstreams) pick() *upstream {
	us.mu.Lock()
	defer us.mu.Unlock()
	now := time.Now()
	var oldest *upstream
	var oldestAt time.Time
	for _, u := range us.list {
		resting, at := u.resting(now)
		if !resting {
			return u
		}
		if oldest == nil || at.Before(oldestAt) {
			oldest, oldestAt = u, at
		}
	}
	return oldest
}

// upstreamConn marks its upstream failed when a read times out, and well
// again when an answer arrives.
type upstreamConn struct {
	net.Conn
	up *upstream
}

func (c *upstreamConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	switch {
	case n > 0:
		c.up.ok()
	case errors.Is(err, os.ErrDeadlineExceeded):
		c.up.fail()
	}
	return n, err
}

// upstreamPacketConn is an upstreamConn over UDP.
type upstreamPacketConn struct {
	*upstreamConn
	pc net.PacketConn
}

func (c *upstreamPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.pc.ReadFrom(b)
	switch {
	case n > 0:
		c.up.ok()
	case errors.Is(err, os.ErrDeadlineExceeded):
		c.up.fail()
	}
	return n, addr, err
}

func (c *upstreamPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return c.pc.WriteTo(b, addr)
}

// ResolvConfServers reads the nameservers out of a resolv.conf, as ip:53, in
// the order listed. Loopback ones are left out: a stub such as
// systemd-resolved's 127.0.0.53 is the host's own, and the gateway cannot
// reach it from its container.
func ResolvConfServers(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		// An IPv6 link-local server carries its zone (fe80::1%eth0), which
		// means nothing inside another network namespace.
		a, err := netip.ParseAddr(f[1])
		if err != nil || a.IsLoopback() || a.IsUnspecified() || a.Zone() != "" {
			continue
		}
		out = append(out, netip.AddrPortFrom(a.Unmap(), 53).String())
	}
	return out
}
