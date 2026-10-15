package egress

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/rpcx"
)

const (
	reconnectMin = 200 * time.Millisecond
	reconnectMax = 15 * time.Second
	dialTimeout  = 10 * time.Second
	// Version is reported in the hello so the CP can spot an old image.
	Version = "1"
	// flushEvery is how often gathered events and traffic go up.
	flushEvery = 5 * time.Second
	// healthEvery is how often the feeds' health is reported.
	healthEvery = 30 * time.Second
)

// Serve keeps a session with the control plane open for the life of ctx. The
// gateway's state survives a reconnect; each new session says hello and waits
// for a fresh apply.
func Serve(ctx context.Context, client silov1connect.EgressHostClient, gw *Gateway) error {
	wait := reconnectMin
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		_ = session(ctx, client, gw)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) > time.Minute {
			wait = reconnectMin
		}
		if !sleepCtx(ctx, wait) {
			return ctx.Err()
		}
		wait = min(wait*2, reconnectMax)
	}
}

func session(ctx context.Context, client silov1connect.EgressHostClient, gw *Gateway) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn := client.Session(ctx)
	defer func() {
		_ = conn.CloseRequest()
		_ = conn.CloseResponse()
	}()
	var mu sync.Mutex
	send := func(m *v1.EgressUp) error {
		mu.Lock()
		defer mu.Unlock()
		return conn.Send(m)
	}
	// ConnectRPC sends no headers until the first message: say hello now.
	if err := send(&v1.EgressUp{Body: &v1.EgressUp_Hello{Hello: &v1.EgressHello{Version: Version}}}); err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() {
		for {
			down, err := conn.Receive()
			if err != nil {
				errc <- err
				return
			}
			if b, ok := down.GetBody().(*v1.EgressDown_Apply); ok {
				gw.Apply(b.Apply)
			}
		}
	}()
	go func() {
		flush := time.NewTicker(flushEvery)
		defer flush.Stop()
		health := time.NewTicker(healthEvery)
		defer health.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-flush.C:
				events, rows := gw.Drain()
				if len(events) > 0 {
					m := &v1.EgressUp{Body: &v1.EgressUp_Events{Events: &v1.EgressEvents{Events: events}}}
					if err := send(m); err != nil {
						gw.Requeue(events, rows)
						errc <- err
						return
					}
				}
				if len(rows) > 0 {
					m := &v1.EgressUp{Body: &v1.EgressUp_Traffic{Traffic: &v1.EgressTraffic{Rows: rows}}}
					if err := send(m); err != nil {
						gw.Requeue(nil, rows)
						errc <- err
						return
					}
				}
			case <-health.C:
				m := &v1.EgressUp{Body: &v1.EgressUp_Health{Health: gw.Health()}}
				_ = send(m)
			}
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// NewClient dials the CP over h2c with the gateway token.
func NewClient(cpURL, token string) (silov1connect.EgressHostClient, error) {
	if strings.TrimSpace(cpURL) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("cp url and gateway token required")
	}
	return silov1connect.NewEgressHostClient(rpcx.H2CClient(dialTimeout), cpURL, connect.WithInterceptors(rpcx.Bearer(token))), nil
}
