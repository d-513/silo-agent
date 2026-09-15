package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// bridgeTunnel is one live reverse connection from a STDIO sidecar. Frames flow
// bridge -> frames (raw JSON-RPC lines) and sends -> bridge.
type bridgeTunnel struct {
	id     string
	frames chan []byte
	sends  chan []byte
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	why    error
}

func newBridgeTunnel(id string) *bridgeTunnel {
	return &bridgeTunnel{
		id:     id,
		frames: make(chan []byte, 64),
		sends:  make(chan []byte, 64),
		done:   make(chan struct{}),
	}
}

func (b *bridgeTunnel) close(err error) {
	b.once.Do(func() {
		b.mu.Lock()
		b.why = err
		b.mu.Unlock()
		close(b.done)
	})
}

func (b *bridgeTunnel) err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.why != nil {
		return b.why
	}
	return errors.New("bridge disconnected")
}

func (a *App) registerBridge(id string, b *bridgeTunnel) {
	a.bridgesMu.Lock()
	a.bridges[id] = b
	a.bridgesMu.Unlock()
}

func (a *App) unregisterBridge(id string, b *bridgeTunnel) {
	a.bridgesMu.Lock()
	if a.bridges[id] == b {
		delete(a.bridges, id)
	}
	a.bridgesMu.Unlock()
}

func (a *App) lookupBridge(id string) *bridgeTunnel {
	a.bridgesMu.Lock()
	defer a.bridgesMu.Unlock()
	return a.bridges[id]
}

// waitBridge blocks until a sidecar for id has dialed in, up to timeout.
func (a *App) waitBridge(ctx context.Context, id string, timeout time.Duration) (*bridgeTunnel, error) {
	deadline := time.Now().Add(timeout)
	for {
		if b := a.lookupBridge(id); b != nil {
			select {
			case <-b.done:
				a.unregisterBridge(id, b)
			default:
				return b, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, errors.New("sidecar did not connect to the control plane")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (a *App) setBridgeStatus(id, status string) {
	if a.DB == nil {
		return
	}
	// Bridge status is progress for a connection that is still coming up. Only
	// touch the row while it is initializing: a sidecar left running across a
	// control-plane restart reconnects and reports "starting", and writing that
	// onto an already-authorized connector left a stale
	// "Authorized · Starting MCP server…" in the UI forever.
	var row db.BotConnector
	if err := a.DB.First(&row, "id = ?", id).Error; err != nil || row.AuthStatus != statusInit {
		return
	}
	detail := map[string]string{
		"starting": "Starting MCP server…",
	}[status]
	if detail == "" {
		detail = status
	}
	a.DB.Model(&db.BotConnector{}).Where("id = ?", id).Update("status_detail", detail)
}

// Tunnel is the reverse MCP host endpoint. Sidecars authenticate with their
// bridge token; the resolved BotConnector is carried on the context.
func (a *App) Tunnel(ctx context.Context, stream *connect.BidiStream[v1.BridgeFrame, v1.BridgeFrame]) error {
	row, _ := ctx.Value(bridgeKey).(*db.BotConnector)
	if row == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	b := newBridgeTunnel(row.ID)
	a.registerBridge(row.ID, b)
	defer func() {
		a.unregisterBridge(row.ID, b)
		b.close(nil)
	}()

	go func() {
		for {
			frame, err := stream.Receive()
			if err != nil {
				b.close(err)
				return
			}
			if status := frame.GetStatus(); status != "" {
				a.setBridgeStatus(row.ID, status)
				continue
			}
			data := frame.GetData()
			if len(data) == 0 {
				continue
			}
			select {
			case b.frames <- data:
			case <-b.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			b.close(ctx.Err())
			return nil
		case <-b.done:
			return nil
		case data := <-b.sends:
			if err := stream.Send(&v1.BridgeFrame{Body: &v1.BridgeFrame_Data{Data: data}}); err != nil {
				b.close(err)
				return nil
			}
		}
	}
}

// bridgeTransport adapts a live reverse tunnel to the MCP SDK's client
// transport interface.
type bridgeTransport struct {
	a   *App
	row *db.BotConnector
}

func (t *bridgeTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	b, err := t.a.waitBridge(ctx, t.row.ID, stdioInitWait)
	if err != nil {
		return nil, err
	}
	return newBridgeLink(b), nil
}

// bridgeLink is the raw JSON-RPC connection over a bridge tunnel.
type bridgeLink struct {
	b      *bridgeTunnel
	closed chan struct{}
	once   sync.Once
}

func newBridgeLink(b *bridgeTunnel) *bridgeLink {
	return &bridgeLink{b: b, closed: make(chan struct{})}
}

func (l *bridgeLink) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, errors.New("bridge link closed")
	case <-l.b.done:
		return nil, l.b.err()
	case data := <-l.b.frames:
		return jsonrpc.DecodeMessage(data)
	}
}

func (l *bridgeLink) Write(ctx context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.closed:
		return errors.New("bridge link closed")
	case <-l.b.done:
		return l.b.err()
	case l.b.sends <- data:
		return nil
	}
}

// Close detaches this MCP client without tearing down the tunnel; the sidecar
// stays connected so a later session can reuse it.
func (l *bridgeLink) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *bridgeLink) SessionID() string { return "" }

type bridgeInterceptor struct{ a *App }

func (i bridgeInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return next
}

func (i bridgeInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i bridgeInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		row, tok, err := i.a.bridgeFromToken(conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		i.a.Mask(row.BotID).Add(tok)
		return next(context.WithValue(ctx, bridgeKey, row), conn)
	}
}

func (a *App) bridgeFromToken(h string) (*db.BotConnector, string, error) {
	tok := strings.TrimPrefix(h, "Bearer ")
	if tok == "" {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	var row db.BotConnector
	if err := a.DB.Where("bridge_token_hash = ?", ids.Hash(tok)).Limit(1).Find(&row).Error; err != nil {
		return nil, "", err
	}
	if row.ID == "" {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	return &row, tok, nil
}
