package egress

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func originEcho(t *testing.T) net.Listener {
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
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	return ln
}

func TestSOCKSConnect(t *testing.T) {
	s, _ := NewState(testApply(t))
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	origin := originEcho(t)
	defer origin.Close()

	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	socks := &SOCKS{
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
	go socks.Serve(ctx, ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var greet [2]byte
	if _, err := io.ReadFull(conn, greet[:]); err != nil {
		t.Fatal(err)
	}
	if greet != [2]byte{5, 0} {
		t.Fatalf("method reply = %v", greet)
	}
	// CONNECT origin.test:9999 (domain form).
	req := append([]byte{5, 1, 0, 3, byte(len("origin.test"))}, []byte("origin.test")...)
	req = append(req, 0x27, 0x0f)
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	var reply [10]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		t.Fatal(err)
	}
	if reply[1] != socksOK {
		t.Fatalf("connect reply = %v", reply)
	}
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("echo = %q", got)
	}
}

func TestSOCKSBlockedWhenSwitchOff(t *testing.T) {
	s, _ := NewState(testApply(t))
	b2 := s.Client(netip.MustParseAddr("10.213.3.1"))
	g := NewGuard(s, nil, nil)
	socks := &SOCKS{
		Client: func(netip.Addr) *Client { return b2 },
		Guard:  func() *Guard { return g },
		Dial:   func(context.Context, string, string) (net.Conn, error) { return nil, io.EOF },
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go socks.Serve(ctx, ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		t.Fatal(err)
	}
	if reply != [2]byte{5, socksNotAllowed} {
		t.Fatalf("a Bot with Other protocols off got %v", reply)
	}
}
