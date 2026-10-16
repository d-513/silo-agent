package egress

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestRefuseInternal(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:80", "10.213.1.1:8888", "192.168.1.1:443", "169.254.169.254:80",
		"100.100.100.200:80", "0.0.0.0:22", "[::1]:80", "[::ffff:127.0.0.1]:80",
		"[64:ff9b::a00:1]:80", // NAT64 of 10.0.0.1
		"[2002:a00:1::]:80",   // 6to4 of 10.0.0.1
		"[fe80::1]:80", "[fd00::1]:80",
	} {
		if err := refuseInternal("tcp", addr, nil); err == nil {
			t.Errorf("refuseInternal(%s) let it through", addr)
		}
	}
	for _, addr := range []string{"1.1.1.1:443", "[2606:4700:4700::1111]:443"} {
		if err := refuseInternal("tcp", addr, nil); err != nil {
			t.Errorf("refuseInternal(%s) = %v", addr, err)
		}
	}
}

func TestOriginDialRefusesLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if c, err := originDial(context.Background(), "tcp", ln.Addr().String()); err == nil {
		c.Close()
		t.Fatal("the origin dial reached loopback")
	} else if !strings.Contains(err.Error(), "internal address") {
		t.Errorf("err = %v", err)
	}
}

// The verdict's lookup and the dial's are two lookups: a name that answers
// outside for the first and inside for the second (DNS rebinding) is still
// refused, because the dial checks the address it connects to.
func TestSOCKSRebindIsRefusedAtTheDial(t *testing.T) {
	s, _ := NewState(testApply(t))
	b1 := s.Client(netip.MustParseAddr("10.213.1.1")) // Other protocols on
	g := NewGuard(s, nil, nil)
	g.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	inside := originEcho(t)
	defer inside.Close()
	_, port, _ := net.SplitHostPort(inside.Addr().String())

	socks := &SOCKS{Client: func(_, _ netip.Addr) *Client { return b1 }, Guard: func() *Guard { return g }}
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
	_, _ = conn.Write([]byte{5, 1, 0})
	greet := make([]byte, 2)
	if _, err := io.ReadFull(conn, greet); err != nil || greet[1] != 0 {
		t.Fatalf("greeting %v: %v", greet, err)
	}
	var p int
	for _, r := range port {
		p = p*10 + int(r-'0')
	}
	name := "localhost"
	req := append([]byte{5, 1, 0, 3, byte(len(name))}, name...)
	req = append(req, byte(p>>8), byte(p))
	_, _ = conn.Write(req)
	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != socksHostUnreach {
		t.Errorf("reply code %d, want %d (refused at the dial)", reply[1], socksHostUnreach)
	}
}
