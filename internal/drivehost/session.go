package drivehost

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
)

// Serve keeps a session with the control plane open for the life of ctx.
// Mounts live in sup and survive reconnects; each new session re-sends the
// snapshot and receives a fresh DriveApply.
func Serve(ctx context.Context, client silov1connect.DriveHostClient, sup *Supervisor, out <-chan *v1.DriveUp) error {
	wait := reconnectMin
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		err := session(ctx, client, sup, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) > time.Minute {
			wait = reconnectMin
		}
		_ = err
		if !sleep(ctx, wait) {
			return ctx.Err()
		}
		wait = min(wait*2, reconnectMax)
	}
}

func session(ctx context.Context, client silov1connect.DriveHostClient, sup *Supervisor, out <-chan *v1.DriveUp) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn := client.Session(ctx)
	defer func() {
		_ = conn.CloseRequest()
		_ = conn.CloseResponse()
	}()
	var mu sync.Mutex
	send := func(m *v1.DriveUp) error {
		mu.Lock()
		defer mu.Unlock()
		return conn.Send(m)
	}
	// ConnectRPC sends no headers until the first message: say hello now.
	if err := send(&v1.DriveUp{Body: &v1.DriveUp_Hello{Hello: &v1.DriveHello{Version: Version}}}); err != nil {
		return err
	}
	for _, m := range sup.Snapshot() {
		if err := send(m); err != nil {
			return err
		}
	}
	errc := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-out:
				if err := send(m); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			down, err := conn.Receive()
			if err != nil {
				errc <- err
				return
			}
			switch b := down.GetBody().(type) {
			case *v1.DriveDown_Apply:
				sup.Apply(b.Apply)
			case *v1.DriveDown_List:
				go func(req *v1.DriveList) {
					res := sup.List(ctx, req)
					_ = send(&v1.DriveUp{Body: &v1.DriveUp_List{List: res}})
				}(b.List)
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

// Outbox is a buffered, never-blocking sink for supervisor frames. While no
// session is up, frames that overflow are dropped: statuses and tokens are
// both re-sent in the next session's snapshot.
func Outbox() (chan *v1.DriveUp, func(*v1.DriveUp)) {
	ch := make(chan *v1.DriveUp, 256)
	return ch, func(m *v1.DriveUp) {
		select {
		case ch <- m:
		default:
		}
	}
}

// NewClient dials the CP over h2c with the drive token.
func NewClient(cpURL, token string) (silov1connect.DriveHostClient, error) {
	if strings.TrimSpace(cpURL) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("cp url and drive token required")
	}
	return silov1connect.NewDriveHostClient(rpcx.H2CClient(dialTimeout), cpURL, connect.WithInterceptors(rpcx.Bearer(token))), nil
}
