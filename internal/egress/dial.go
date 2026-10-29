package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"

	"silo.agent/internal/netguard"
)

// originTimeout bounds one dial to what a client asked for.
const originTimeout = 30 * time.Second

// originDial reaches what a client asked for, and is where the
// internal-address rule holds. The verdict's own check (checkAddrs) resolves
// the name once to give the box a readable 403, but the dial resolves it again:
// a name that answers outside first and inside second (DNS rebinding), or that
// did not resolve for the verdict at all, would reach the operator's network
// through that gap. Control sees the literal address being connected to, after
// DNS, so every way in is the same refusal. The control-plane link is the one
// dial that must reach inside, and it has its own.
func originDial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: originTimeout, Control: refuseInternal}
	return d.DialContext(ctx, network, addr)
}

// refuseInternal is a net.Dialer Control that refuses loopback, private,
// link-local, metadata and the other ranges netguard counts as inside,
// including an IPv4 address carried in IPv6 (mapped, NAT64, 6to4).
func refuseInternal(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("egress: unreadable address %q", address)
	}
	if netguard.Internal(ap.Addr()) {
		return &refusedAddr{addr: ap.Addr()}
	}
	return nil
}

// refusedAddr is a dial the filter itself turned away at the address actually
// connected to: an internal one, or one on a threat list (list names it). It
// is a block, worded as one, never a site that failed to answer.
type refusedAddr struct {
	addr netip.Addr
	list string
}

func (e *refusedAddr) Error() string {
	if e.list != "" {
		return fmt.Sprintf("egress: %s is on threat list %s", e.addr, e.list)
	}
	return fmt.Sprintf("egress: %s is an internal address", e.addr)
}

// Dial is the origin dial with the threat lists' addresses refused as well as
// the internal ones: the verdict judged the name's addresses when it looked,
// and this judges the one actually connected to.
func (g *Guard) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var feeds *Feeds
	if g != nil {
		feeds = g.Feeds
	}
	d := net.Dialer{Timeout: originTimeout, Control: func(network, address string, c syscall.RawConn) error {
		if err := refuseInternal(network, address, c); err != nil {
			return err
		}
		if feeds == nil {
			return nil
		}
		ap, _ := netip.ParseAddrPort(address)
		if id, ok := feeds.Addr(ap.Addr()); ok {
			return &refusedAddr{addr: ap.Addr(), list: id}
		}
		return nil
	}}
	return d.DialContext(ctx, network, addr)
}
