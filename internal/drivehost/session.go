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
func Serve(ctx context.Context, client silov1connect.DriveHostClient, sup *Supervisor, out *Box) error {
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

func session(ctx context.Context, client silov1connect.DriveHostClient, sup *Supervisor, out *Box) error {
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
	// The snapshot is every mount as it is now. A status or token still
	// waiting in the box is the newest of its kind, so sending it after the
	// snapshot never takes the CP back to an older state.
	for _, m := range sup.Snapshot() {
		if err := send(m); err != nil {
			return err
		}
	}
	errc := make(chan error, 2)
	go func() {
		for {
			for _, m := range out.Take() {
				if err := send(m); err != nil {
					errc <- err
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-out.Ready():
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

// Box holds the supervisor's frames until a session sends them, and never
// blocks the supervisor. Statuses and tokens are state: only the newest of
// each per drive is kept, so a frame queued while the CP was away can never
// arrive after a newer one. The drive journal is a log: its frames keep their
// order, up to boxJournal of them, past which the newest are dropped.
type Box struct {
	mu      sync.Mutex
	ready   chan struct{}
	order   []string
	state   map[string]*v1.DriveUp
	journal []*v1.DriveUp
}

// boxJournal bounds the journal frames held while no session is up.
const boxJournal = 256

// NewBox makes an empty box.
func NewBox() *Box {
	return &Box{ready: make(chan struct{}, 1), state: map[string]*v1.DriveUp{}}
}

// Outbox is a box and the supervisor's emit that fills it.
func Outbox() (*Box, func(*v1.DriveUp)) {
	b := NewBox()
	return b, b.Put
}

// Put queues one frame.
func (b *Box) Put(m *v1.DriveUp) {
	b.mu.Lock()
	key := ""
	switch x := m.GetBody().(type) {
	case *v1.DriveUp_Status:
		key = "status:" + x.Status.GetId()
	case *v1.DriveUp_Token:
		key = "token:" + x.Token.GetId()
	}
	switch {
	case key != "":
		if _, ok := b.state[key]; !ok {
			b.order = append(b.order, key)
		}
		b.state[key] = m
	case len(b.journal) < boxJournal:
		b.journal = append(b.journal, m)
	}
	b.mu.Unlock()
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// Take empties the box: the journal in order, then each drive's newest
// status and token.
func (b *Box) Take() []*v1.DriveUp {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.journal
	b.journal = nil
	for _, k := range b.order {
		out = append(out, b.state[k])
	}
	b.order, b.state = nil, map[string]*v1.DriveUp{}
	return out
}

// Ready signals that something was put since the last Take.
func (b *Box) Ready() <-chan struct{} { return b.ready }

// NewClient dials the CP over h2c with the drive token.
func NewClient(cpURL, token string) (silov1connect.DriveHostClient, error) {
	if strings.TrimSpace(cpURL) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("cp url and drive token required")
	}
	return silov1connect.NewDriveHostClient(rpcx.H2CClient(dialTimeout), cpURL, connect.WithInterceptors(rpcx.Bearer(token))), nil
}
