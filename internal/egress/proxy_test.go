package egress

import (
	"bufio"
	"bytes"
	"compress/gzip"
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
		Client: func(_, _ netip.Addr) *Client { return b1 },
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
		Client: func(_, _ netip.Addr) *Client { return b1 },
		Guard:  func() *Guard { return g },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", origin.Addr().String())
		},
		Roots: ca.Pool(),
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

// originServe is a TLS origin, certified by ca for every name, that answers
// each request with answer and hands back what it read.
func originServe(t *testing.T, ca *CA, answer func(req *http.Request, body []byte, w io.Writer)) (net.Listener, <-chan *http.Request) {
	t.Helper()
	cfg := &tls.Config{GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		return ca.Cert(h.ServerName, time.Now())
	}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan *http.Request, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tc := tls.Server(c, cfg)
				defer tc.Close()
				req, err := http.ReadRequest(bufio.NewReader(tc))
				if err != nil {
					return
				}
				body, _ := io.ReadAll(req.Body)
				req.Body = io.NopCloser(strings.NewReader(string(body)))
				got <- req
				answer(req, body, tc)
			}()
		}
	}()
	return ln, got
}

// mitmProxy is a decrypting proxy in front of origin, served on ModeTLS, with
// its reports logged.
func mitmProxy(t *testing.T, a *v1.EgressApply, origin net.Listener) (string, *CA, *eventLog) {
	t.Helper()
	s, err := NewState(a)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := LoadCA(a.CaCert, a.CaKey)
	if err != nil {
		t.Fatal(err)
	}
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	log := &eventLog{}
	p := &Proxy{
		Client: func(_, _ netip.Addr) *Client { return b1 },
		Guard:  func() *Guard { return g },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", origin.Addr().String())
		},
		Roots:  ca.Pool(),
		report: log.report,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go p.Serve(ctx, ln, ModeTLS)
	return ln.Addr().String(), ca, log
}

// tlsVia opens a decrypted connection to host through the proxy at addr.
func tlsVia(t *testing.T, addr, host string, ca *CA) *tls.Conn {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	tc := tls.Client(raw, &tls.Config{ServerName: host, RootCAs: ca.Pool()})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	return tc
}

func answerOK(_ *http.Request, _ []byte, w io.Writer) {
	_, _ = io.WriteString(w, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
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
		{"body allowed", true, "ghp_REALVALUE1111111111"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApply(t)
			a.Bots[0].Surrogates[0].Body = tc.body
			ca, _ := LoadCA(a.CaCert, a.CaKey)
			origin, got := originServe(t, ca, answerOK)
			defer origin.Close()
			addr, ca, _ := mitmProxy(t, a, origin)

			conn := tlsVia(t, addr, "api.github.com", ca)
			// Past the verdict's payload cap, so the swap is the stream's.
			body := `{"pad":"` + strings.Repeat("x", payloadCap) + `","note":"ghp_SURROGATE0000000000"}`
			_, _ = fmt.Fprintf(conn, "POST /x HTTP/1.1\r\nHost: api.github.com\r\n"+
				"Authorization: Bearer ghp_SURROGATE0000000000\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			seen := <-got
			if h := seen.Header.Get("Authorization"); h != "Bearer ghp_REALVALUE1111111111" {
				t.Errorf("Authorization = %q, want the real value", h)
			}
			sent, _ := io.ReadAll(seen.Body)
			if !strings.HasSuffix(string(sent), tc.wantBody+`"}`) || len(sent) != len(body) {
				t.Errorf("body ends %q (len %d of %d), want %s", sent[len(sent)-40:], len(sent), len(body), tc.wantBody)
			}
		})
	}
}

// The real value never crosses the internet unencrypted: a surrogate to its
// own host over plain HTTP is refused, not swapped.
func TestProxyRefusesASurrogateOverPlainHTTP(t *testing.T) {
	origin, got := originCapture(t)
	defer origin.Close()
	p, _, _ := testProxy(t, origin)
	addr := serveProxy(t, p, ModeProxy)
	conn := dialProxy(t, addr)
	_, _ = fmt.Fprintf(conn, "GET http://api.github.com/x HTTP/1.1\r\nHost: api.github.com\r\n"+
		"Authorization: Bearer ghp_SURROGATE0000000000\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "only sent over HTTPS") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	select {
	case seen := <-got:
		t.Fatalf("the origin was reached: %q", seen)
	case <-time.After(100 * time.Millisecond):
	}
}

// A stand-in deep in a body (past what the verdict reads) headed to a host it
// is not for ends the upload before it reaches the origin.
func TestProxyStopsALeakPastThePayloadCap(t *testing.T) {
	a := testApply(t)
	ca, _ := LoadCA(a.CaCert, a.CaKey)
	origin, got := originServe(t, ca, answerOK)
	defer origin.Close()
	addr, ca, log := mitmProxy(t, a, origin)
	conn := tlsVia(t, addr, "upload.example", ca)
	body := strings.Repeat("x", payloadCap+100) + "ghp_SURROGATE0000000000"
	_, _ = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: upload.example\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden || !log.has("surrogate_leak upload.example") {
		t.Fatalf("status %d events %v", resp.StatusCode, log.got)
	}
	select {
	case seen := <-got:
		if b, _ := io.ReadAll(seen.Body); strings.Contains(string(b), "SURROGATE") {
			t.Fatal("the stand-in reached the origin")
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// An answer that echoes the real value is swapped back even when the origin
// compressed it, and its headers are read too.
func TestProxySwapsBackACompressedAnswer(t *testing.T) {
	a := testApply(t)
	ca, _ := LoadCA(a.CaCert, a.CaKey)
	origin, got := originServe(t, ca, func(_ *http.Request, _ []byte, w io.Writer) {
		var zb bytes.Buffer
		zw := gzip.NewWriter(&zb)
		_, _ = zw.Write([]byte(`{"echo":"ghp_REALVALUE1111111111"}`))
		_ = zw.Close()
		fmt.Fprintf(w, "HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nX-Echo: ghp_REALVALUE1111111111\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", zb.Len())
		_, _ = w.Write(zb.Bytes())
	})
	defer origin.Close()
	addr, ca, _ := mitmProxy(t, a, origin)
	conn := tlsVia(t, addr, "api.github.com", ca)
	_, _ = fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: api.github.com\r\nAccept-Encoding: gzip\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "REALVALUE") || resp.Header.Get("X-Echo") != "ghp_SURROGATE0000000000" {
		t.Fatalf("the real value reached the box: %q %q", body, resp.Header.Get("X-Echo"))
	}
	if string(body) != `{"echo":"ghp_SURROGATE0000000000"}` {
		t.Fatalf("body %q", body)
	}
	if seen := <-got; seen.Header.Get("Accept-Encoding") != "" {
		t.Errorf("the origin was asked for %q", seen.Header.Get("Accept-Encoding"))
	}
}

// An origin whose certificate does not verify is never sent the request: the
// box only sees the gateway's certificate, so this is the one check there is.
func TestProxyRefusesAnUnverifiedOrigin(t *testing.T) {
	a := testApply(t)
	oc, ok, _ := GenerateCA("impostor", time.Now())
	impostor, err := LoadCA(oc, ok)
	if err != nil {
		t.Fatal(err)
	}
	origin, got := originServe(t, impostor, answerOK)
	defer origin.Close()
	addr, ca, _ := mitmProxy(t, a, origin)
	conn := tlsVia(t, addr, "api.github.com", ca)
	_, _ = fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: api.github.com\r\nAuthorization: Bearer ghp_SURROGATE0000000000\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "verified") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	select {
	case <-got:
		t.Fatal("the request reached an origin that did not verify")
	case <-time.After(100 * time.Millisecond):
	}
}

// A WebSocket upgrade is spliced once the 101 is through, rather than read as
// the next request.
func TestProxySplicesAnUpgrade(t *testing.T) {
	a := testApply(t)
	ca, _ := LoadCA(a.CaCert, a.CaKey)
	origin, _ := originServe(t, ca, func(_ *http.Request, _ []byte, w io.Writer) {
		_, _ = io.WriteString(w, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nhello-frames")
	})
	defer origin.Close()
	addr, ca, _ := mitmProxy(t, a, origin)
	conn := tlsVia(t, addr, "ws.example", ca)
	_, _ = fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: ws.example\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	rest, _ := io.ReadAll(br)
	if string(rest) != "hello-frames" {
		t.Fatalf("after the upgrade: %q", rest)
	}
}

// A web request to a port browsers refuse (here SMTP) is another protocol
// through the HTTP door, which only the Other-protocols switch opens.
func TestProxyRefusesBadPorts(t *testing.T) {
	origin := originPlain(t)
	defer origin.Close()
	a := testApply(t)
	a.Bots[0].OtherProtocols = false
	p, _, _ := testProxyWith(t, origin, a)
	conn := dialProxy(t, serveProxy(t, p, ModeProxy))
	_, _ = fmt.Fprintf(conn, "POST http://mail.example:25/ HTTP/1.1\r\nHost: mail.example:25\r\nContent-Length: 4\r\nConnection: close\r\n\r\nHELO")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func serveProxy(t *testing.T, p *Proxy, mode string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go p.Serve(ctx, ln, mode)
	return ln.Addr().String()
}

func dialProxy(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn
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
