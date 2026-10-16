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
	"sync"
	"testing"
	"time"
)

// eventLog captures what a proxy reports.
type eventLog struct {
	mu  sync.Mutex
	got []string
}

func (l *eventLog) report(_ *Client, host string, v Verdict, _, _ int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, v.Reason+" "+host)
}

func (l *eventLog) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, g := range l.got {
		if g == s {
			return true
		}
	}
	return false
}

// passProxy is a proxy whose state has no CA, so every TLS connection is
// spliced on SNI: what a gateway runs when the CA did not load.
func passProxy(t *testing.T, client netip.Addr, origin net.Listener, mode string) (string, *eventLog) {
	t.Helper()
	a := testApply(t)
	a.CaCert, a.CaKey = nil, nil
	s, err := NewState(a)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Client(client)
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	log := &eventLog{}
	p := &Proxy{
		Client: func(_, _ netip.Addr) *Client { return c },
		Guard:  func() *Guard { return g },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return net.Dial("tcp", origin.Addr().String())
		},
		report: log.report,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go p.Serve(ctx, ln, mode)
	return ln.Addr().String(), log
}

func connectVia(t *testing.T, addr, target string) (net.Conn, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT %s: %v", target, err)
	}
	return conn, resp
}

// A spliced connection is never decrypted, but it is still judged: neither a
// Pass-through entry nor a CA that failed to load turns the filter off.
func TestPassThroughConnectIsJudged(t *testing.T) {
	origin := originEcho(t)
	defer origin.Close()
	b2 := netip.MustParseAddr("10.213.3.1") // a Bot with no Other protocols
	addr, log := passProxy(t, b2, origin, ModeProxy)

	_, resp := connectVia(t, addr, "x.evil.example:443")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("CONNECT to an operator-blocked host: %d, want 403", resp.StatusCode)
	}
	if !log.has("blocked_host x.evil.example") {
		t.Errorf("the block was not reported: %v", log.got)
	}

	_, resp = connectVia(t, addr, "10.0.0.5:443")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("CONNECT to an internal address: %d, want 403", resp.StatusCode)
	}

	conn, resp := connectVia(t, addr, "origin.test:443")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT to an ordinary host: %d, want 200", resp.StatusCode)
	}
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Errorf("spliced echo = %q, %v", buf, err)
	}
}

// A spliced CONNECT to a port other than 443 is a raw TCP tunnel: only the
// Other-protocols switch opens one.
func TestPassThroughOtherPortsNeedTheSwitch(t *testing.T) {
	origin := originEcho(t)
	defer origin.Close()

	off, log := passProxy(t, netip.MustParseAddr("10.213.3.1"), origin, ModeProxy)
	_, resp := connectVia(t, off, "origin.test:22")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "Other protocols") {
		t.Errorf("port 22 with the switch off: %d %q", resp.StatusCode, body)
	}
	if !log.has("other_protocols origin.test") {
		t.Errorf("the refusal was not reported: %v", log.got)
	}

	on, _ := passProxy(t, netip.MustParseAddr("10.213.1.1"), origin, ModeProxy) // b1 has the switch on
	if _, resp := connectVia(t, on, "origin.test:22"); resp.StatusCode != http.StatusOK {
		t.Errorf("port 22 with the switch on: %d, want 200", resp.StatusCode)
	}
}

func TestPassThroughTransparentIsJudged(t *testing.T) {
	a := testApply(t)
	ca, err := LoadCA(a.CaCert, a.CaKey)
	if err != nil {
		t.Fatal(err)
	}
	origin := originTLS(t, ca)
	defer origin.Close()
	addr, log := passProxy(t, netip.MustParseAddr("10.213.3.1"), origin, ModeTLS)

	hello := func(name string) error {
		raw, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
		return tls.Client(raw, &tls.Config{ServerName: name, RootCAs: ca.Pool(), InsecureSkipVerify: name != "origin.test"}).Handshake()
	}
	if err := hello("x.evil.example"); err == nil {
		t.Error("a hello naming an operator-blocked host was spliced")
	}
	if !log.has("blocked_host x.evil.example") {
		t.Errorf("the block was not reported: %v", log.got)
	}
	if err := hello("origin.test"); err != nil {
		t.Errorf("an ordinary host was not spliced: %v", err)
	}
}

// The filter is nobody's open proxy: a connection from outside every client
// network (the gateway's own ordinary network) is closed unanswered.
func TestProxyRefusesUnknownClient(t *testing.T) {
	origin := originPlain(t)
	defer origin.Close()
	p, _, _ := testProxy(t, origin)
	p.Client = func(_, _ netip.Addr) *Client { return nil }
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
	_, _ = fmt.Fprintf(conn, "GET http://origin.test/ HTTP/1.1\r\nHost: origin.test\r\n\r\n")
	if resp, err := http.ReadResponse(bufio.NewReader(conn), nil); err == nil {
		t.Fatalf("an unknown client got an answer: %d", resp.StatusCode)
	}
}

func TestCheckRefusesNoHost(t *testing.T) {
	s, _ := NewState(testApply(t))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	if v := g.Check(context.Background(), Request{Client: s.Client(netip.MustParseAddr("10.213.1.1")), Host: " "}); !v.Block {
		t.Errorf("an empty host passed: %+v", v)
	}
}

func TestSidecarGetsNoSurrogates(t *testing.T) {
	s, _ := NewState(testApply(t))
	sidecar := s.Client(netip.MustParseAddr("10.213.2.1"))
	b := s.bot(sidecar)
	if len(b.surrogates) != 0 || b.plainSecret("hunter2hunter2") {
		t.Error("a STDIO sidecar was given the Bot's surrogates or plain-secret watch")
	}
	if leaked, _ := b.leak("Bearer ghp_SURROGATE0000000000", "pastebin.com", true); leaked != "" {
		t.Error("a sidecar's request was read for the Bot's surrogates")
	}
	if !b.otherProtocols {
		t.Error("the sidecar lost the Bot's Other-protocols switch")
	}
	if _, ok := s.blocked(sidecar, "ads.example"); !ok {
		t.Error("the sidecar lost the Bot's block list")
	}
}
