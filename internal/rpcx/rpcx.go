// Package rpcx is the ConnectRPC plumbing the Control Plane and everything that
// dials it (the Bot worker, the drive guest, the STDIO bridge) share: the
// cleartext HTTP/2 client, the bearer-token client interceptor, and an adapter
// for interceptors that only act on the server side.
package rpcx

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
)

// H2CClient returns an HTTP client that speaks cleartext HTTP/2 with prior
// knowledge, which is what the CP serves (ConnectRPC bidi streams need h2).
// dialTimeout bounds one TCP dial; the stream itself has no deadline.
func H2CClient(dialTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: dialTimeout, FallbackDelay: 100 * time.Millisecond}
	return &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

// EnableH2C makes srv accept cleartext HTTP/2 (prior knowledge) next to
// HTTP/1.1, which is how the worker, drive guest and bridge reach the CP. The
// browser and WebSocket traffic keep using HTTP/1.1 on the same port.
func EnableH2C(srv *http.Server) {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	srv.Protocols = p
}

// Bearer is a client interceptor that sends "Authorization: Bearer <token>" on
// every unary call and every stream.
type Bearer string

func (b Bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+string(b))
		return next(ctx, req)
	}
}

func (b Bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+string(b))
		return conn
	}
}

func (b Bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// Handler is a server-side interceptor built from the wrappers it needs; a nil
// field passes that kind of call through untouched. It never touches clients.
type Handler struct {
	Unary  func(connect.UnaryFunc) connect.UnaryFunc
	Stream func(connect.StreamingHandlerFunc) connect.StreamingHandlerFunc
}

func (h Handler) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	if h.Unary == nil {
		return next
	}
	return h.Unary(next)
}

func (h Handler) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (h Handler) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	if h.Stream == nil {
		return next
	}
	return h.Stream(next)
}
