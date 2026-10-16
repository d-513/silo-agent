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
// internal-address rule holds. The verdict's own check (privateDest) resolves
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
		return fmt.Errorf("egress: %s is an internal address", ap.Addr())
	}
	return nil
}
