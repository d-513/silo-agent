package hub

import (
	"context"
	"errors"
	"net"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/ids"
)

// TunnelTimeout is how long a dial waits for the worker to claim its tunnel.
var TunnelTimeout = 10 * time.Second

var ErrTunnelTimeout = errors.New("the Bot did not answer the tunnel request")

type tunnelResult struct {
	conn net.Conn
	err  error
}

// DialTunnel asks the Bot's worker to open a connection to 127.0.0.1:port in
// the box and returns it once the worker's Tunnel stream has been claimed. It
// never starts a box: a Bot with no worker session is ErrNoWorker.
func (h *Hub) DialTunnel(ctx context.Context, botID string, port int) (net.Conn, error) {
	s := h.Get(botID)
	if s == nil {
		return nil, ErrNoWorker
	}
	id := ids.New()
	ch := make(chan tunnelResult, 1)
	s.mu.Lock()
	s.tunnels[id] = ch
	s.mu.Unlock()

	ctx, cancel := context.WithTimeoutCause(ctx, TunnelTimeout, ErrTunnelTimeout)
	defer cancel()
	cmd := &v1.Cmd{Id: id, Body: &v1.Cmd_OpenTunnel{OpenTunnel: &v1.OpenTunnelCmd{ConnId: id, Port: int32(port)}}}
	select {
	case s.Send <- cmd:
	case <-ctx.Done():
		return nil, s.abandonTunnel(id, ch, context.Cause(ctx))
	case <-s.dead:
		return nil, s.abandonTunnel(id, ch, ErrClosed)
	}
	select {
	case r := <-ch:
		return r.conn, r.err
	case <-ctx.Done():
		return nil, s.abandonTunnel(id, ch, context.Cause(ctx))
	case <-s.dead:
		return nil, s.abandonTunnel(id, ch, ErrClosed)
	}
}

// abandonTunnel gives up on a dial. If the worker has not claimed it yet the
// claim is withdrawn; if it already has, whatever it delivers is closed.
func (s *Session) abandonTunnel(id string, ch chan tunnelResult, why error) error {
	s.mu.Lock()
	_, pending := s.tunnels[id]
	delete(s.tunnels, id)
	s.mu.Unlock()
	if !pending {
		go func() {
			if r := <-ch; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
	}
	return why
}

// ClaimTunnel is the worker's side: it takes the pending dial named connID and
// returns the function that completes it with the opened conn, or with the
// error the worker hit dialing the port. A claim works once, and only for a
// dial made on this session. The caller must call deliver exactly once.
func (s *Session) ClaimTunnel(connID string) (deliver func(net.Conn, error), ok bool) {
	s.mu.Lock()
	ch, ok := s.tunnels[connID]
	delete(s.tunnels, connID)
	s.mu.Unlock()
	if !ok {
		return nil, false
	}
	return func(c net.Conn, err error) {
		select {
		case ch <- tunnelResult{conn: c, err: err}:
		default:
			if c != nil {
				_ = c.Close()
			}
		}
	}, true
}
