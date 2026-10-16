package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

func originPlain(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, req.Body)
				_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
			}()
		}
	}()
	return ln
}

func originTLS(t *testing.T, ca *CA) net.Listener {
	t.Helper()
	crt, err := ca.Cert("origin.test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{*crt}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tc := tls.Server(c, cfg)
				if err := tc.Handshake(); err != nil {
					c.Close()
					return
				}
				defer tc.Close()
				req, err := http.ReadRequest(bufio.NewReader(tc))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, req.Body)
				_, _ = fmt.Fprintf(tc, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
			}()
		}
	}()
	return ln
}

func testProxy(t *testing.T, origin net.Listener) (*Proxy, *State, *Client) {
	t.Helper()
	return testProxyWith(t, origin, testApply(t))
}

func testProxyWith(t *testing.T, origin net.Listener, a *v1.EgressApply) (*Proxy, *State, *Client) {
	t.Helper()
	s, err := NewState(a)
	if err != nil {
		t.Fatal(err)
	}
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	p := &Proxy{
		Client: func(netip.Addr) *Client { return b1 },
		Guard:  func() *Guard { return g },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", origin.Addr().String())
		},
	}
	return p, s, b1
}

func TestProxyHTTPMode(t *testing.T) {
	origin := originPlain(t)
	defer origin.Close()
	p, _, _ := testProxy(t, origin)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Serve(ctx, ln, ModeProxy)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET http://origin.test/ HTTP/1.1\r\nHost: origin.test\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "hello" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestProxyBlocksHost(t *testing.T) {
	origin := originPlain(t)
	defer origin.Close()
	p, _, _ := testProxy(t, origin)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Serve(ctx, ln, ModeProxy)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET http://ads.example/ HTTP/1.1\r\nHost: ads.example\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "network filter") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestProxyTLSMITM(t *testing.T) {
	a := testApply(t)
	s, err := NewState(a)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := LoadCA(a.CaCert, a.CaKey)
	if err != nil {
		t.Fatal(err)
	}
	origin := originTLS(t, ca)
	defer origin.Close()

	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	p := &Proxy{
		Client: func(netip.Addr) *Client { return b1 },
		Guard:  func() *Guard { return g },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", origin.Addr().String())
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Serve(ctx, ln, ModeTLS)

	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	tc := tls.Client(raw, &tls.Config{ServerName: "origin.test", RootCAs: ca.Pool()})
	if err := tc.Handshake(); err != nil {
		t.Fatalf("client handshake against the minted certificate: %v", err)
	}
	_, _ = fmt.Fprintf(tc, "GET / HTTP/1.1\r\nHost: origin.test\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(tc), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "hello" {
		t.Fatalf("decrypted status %d body %q", resp.StatusCode, body)
	}
}

// originCapture is a plain origin that hands back each request it read, with
// its body.
func originCapture(t *testing.T) (net.Listener, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				body, _ := io.ReadAll(req.Body)
				got <- req.Header.Get("Authorization") + "\n" + string(body)
				_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
			}()
		}
	}()
	return ln, got
}

// A surrogate becomes the real value in the request headers. In the body it
// stays the stand-in unless the owner allowed bodies for that secret: a body
// to an allowed host can still publish what it carries.
func TestProxySwapsHeadersAndBodyOnlyWhenAllowed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     bool
		wantBody string
	}{
		{"headers only", false, "ghp_SURROGATE0000000000"},
		{"body allowed", true, "ghp_REALVALUE11111111111"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin, got := originCapture(t)
			defer origin.Close()
			a := testApply(t)
			a.Bots[0].Surrogates[0].Body = tc.body
			p, _, _ := testProxyWith(t, origin, a)

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go p.Serve(ctx, ln, ModeProxy)

			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			body := `{"note":"ghp_SURROGATE0000000000"}`
			_, _ = fmt.Fprintf(conn, "POST http://api.github.com/x HTTP/1.1\r\nHost: api.github.com\r\n"+
				"Authorization: Bearer ghp_SURROGATE0000000000\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			seen := <-got
			head, sent, _ := strings.Cut(seen, "\n")
			if head != "Bearer ghp_REALVALUE11111111111" {
				t.Errorf("Authorization = %q, want the real value", head)
			}
			if !strings.Contains(sent, tc.wantBody) {
				t.Errorf("body = %q, want %s", sent, tc.wantBody)
			}
		})
	}
}
