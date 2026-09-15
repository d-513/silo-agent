package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// testStartBridge registers an in-process MCP server as if a sidecar had dialed
// the CP, exercising the real bridge registry and transport without Docker.
func (a *App) testStartBridge(id string) {
	b := newBridgeTunnel(id)
	a.registerBridge(id, b)
	go func() {
		_, _ = fakeStdioMCP().Connect(context.Background(), &serverBridgeTransport{b: b}, nil)
	}()
}

type serverBridgeTransport struct{ b *bridgeTunnel }

func (t *serverBridgeTransport) Connect(context.Context) (mcp.Connection, error) {
	return &serverBridgeLink{b: t.b}, nil
}

// serverBridgeLink is the child side: it reads CP->sidecar frames and writes
// sidecar->CP frames, the mirror of bridgeLink.
type serverBridgeLink struct{ b *bridgeTunnel }

func (l *serverBridgeLink) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.b.done:
		return nil, l.b.err()
	case data := <-l.b.sends:
		return jsonrpc.DecodeMessage(data)
	}
}

func (l *serverBridgeLink) Write(ctx context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.b.done:
		return l.b.err()
	case l.b.frames <- data:
		return nil
	}
}

func (l *serverBridgeLink) Close() error      { return nil }
func (l *serverBridgeLink) SessionID() string { return "" }
