package egress

import (
	"context"
	"net"
	"net/netip"
	"time"
)

// CPLink is the plain TCP forward that carries a worker's or a bridge's
// connection to the control plane. The Box has no route to the CP, so it dials
// the gateway (its DNS already answers with the gateway) and the gateway dials
// the real CP. Nothing is inspected here: this is the control plane's own
// channel, not the Bot's traffic.
type CPLink struct {
	// Target is the current "host:port" of the control plane, from the last
	// apply. Empty means the gateway has not been told where the CP is.
	Target func() string
	// Dial reaches the control plane; the real one is a plain net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Client, when set, finds who a connection belongs to; one from nobody
	// (the gateway's own ordinary network, a stray route) is closed. The CP
	// authenticates what comes through, but its link is its clients' only.
	Client func(remote, local netip.Addr) *Client
}

func (l *CPLink) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if l.Dial != nil {
		return l.Dial(ctx, network, addr)
	}
	return cpDialer().DialContext(ctx, network, addr)
}

// engineResolver looks a name up as the container engine set this container
// up to (its resolv.conf and hosts file), whatever net.DefaultResolver has
// become.
var engineResolver = &net.Resolver{PreferGo: true}

// cpDialer dials the control plane, and is the one dialer that does not
// resolve through the gateway's own Resolver. That one sends every lookup to
// the outside resolvers the control plane named, which is right for what a
// client asks for and wrong for the control plane itself: when it is a
// container on the gateway's network (cp_network) its name is one only the
// engine's DNS knows, and with it unresolved no worker gets through the link
// and the gateway never finds the control plane again after its first
// session.
func cpDialer() *net.Dialer { return &net.Dialer{Resolver: engineResolver} }

// Serve accepts CP-link connections until ctx ends.
func (l *CPLink) Serve(ctx context.Context, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go l.forward(ctx, conn)
	}
}

func (l *CPLink) forward(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	if l.Client != nil && l.Client(addrOf(conn.RemoteAddr()), addrOf(conn.LocalAddr())) == nil {
		return
	}
	target := ""
	if l.Target != nil {
		target = l.Target()
	}
	if target == "" {
		return
	}
	up, err := l.dial(ctx, "tcp", target)
	if err != nil {
		return
	}
	defer up.Close()
	// The CP's h2c stream is bidirectional and long-lived; keep it up until
	// either side ends.
	_ = conn.SetDeadline(time.Time{})
	splice(conn, conn, up)
}
