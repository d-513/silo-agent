package rpcx

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestBearerUnary(t *testing.T) {
	var got string
	next := func(_ context.Context, r connect.AnyRequest) (connect.AnyResponse, error) {
		got = r.Header().Get("Authorization")
		return nil, nil
	}
	if _, err := Bearer("tok").WrapUnary(next)(context.Background(), connect.NewRequest(&emptypb.Empty{})); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
}

type fakeStream struct {
	connect.StreamingClientConn
	h http.Header
}

func (f fakeStream) RequestHeader() http.Header { return f.h }

func TestBearerStream(t *testing.T) {
	conn := fakeStream{h: http.Header{}}
	next := func(context.Context, connect.Spec) connect.StreamingClientConn { return conn }
	Bearer("tok").WrapStreamingClient(next)(context.Background(), connect.Spec{})
	if got := conn.h.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestHandlerPassesThroughWhenEmpty(t *testing.T) {
	called := false
	unary := func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) { called = true; return nil, nil }
	var h Handler
	if _, err := h.WrapUnary(unary)(context.Background(), connect.NewRequest(&emptypb.Empty{})); err != nil || !called {
		t.Fatalf("unary passthrough: called=%v err=%v", called, err)
	}
	stream := func(context.Context, connect.StreamingHandlerConn) error { called = false; return nil }
	if err := h.WrapStreamingHandler(stream)(context.Background(), nil); err != nil || called {
		t.Fatalf("stream passthrough: called=%v err=%v", called, err)
	}
}

func TestHandlerWrapsEachKind(t *testing.T) {
	var order []string
	h := Handler{
		Unary: func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, r connect.AnyRequest) (connect.AnyResponse, error) {
				order = append(order, "unary")
				return next(ctx, r)
			}
		},
		Stream: func(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
			return func(ctx context.Context, c connect.StreamingHandlerConn) error {
				order = append(order, "stream")
				return next(ctx, c)
			}
		},
	}
	_, _ = h.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) { return nil, nil })(
		context.Background(), connect.NewRequest(&emptypb.Empty{}))
	_ = h.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error { return nil })(context.Background(), nil)
	if len(order) != 2 || order[0] != "unary" || order[1] != "stream" {
		t.Fatalf("order = %v", order)
	}
}

// The CP serves cleartext HTTP/2; the client must reach it with prior
// knowledge, not fall back to HTTP/1.1.
func TestH2CClientSpeaksHTTP2(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.Proto) })}
	EnableH2C(srv)
	go srv.Serve(ln)
	defer srv.Close()

	addr := "http://" + ln.Addr().String()
	for client, want := range map[*http.Client]string{
		H2CClient(time.Second): "HTTP/2.0",
		http.DefaultClient:     "HTTP/1.1", // browsers and WebSockets keep working
	} {
		res, err := client.Get(addr)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) != want {
			t.Fatalf("proto = %q, want %q", body, want)
		}
	}
}
