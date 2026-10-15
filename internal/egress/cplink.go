package egress

import (
	"context"
	"net"
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
}

func (l *CPLink) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if l.Dial != nil {
		return l.Dial(ctx, network, addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

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
