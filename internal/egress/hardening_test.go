package egress

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// A stand-in on its way to its own host is made to look like the token it
// stands for; it is expected there, not a credential leaving.
func TestVerdictDoesNotFlagASurrogateAtHome(t *testing.T) {
	a := testApply(t)
	a.Bots[0].Surrogates[0].Surrogate = "ghp_" + strings.Repeat("S", 36)
	a.Bots[0].Surrogates[0].Real = "ghp_" + strings.Repeat("R", 36)
	s, _ := NewState(a)
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	v := g.Check(context.Background(), Request{Client: b1, Host: "api.github.com", Proto: "https", Port: 443, Secure: true,
		Payload: "GET / \nAuthorization: Bearer ghp_" + strings.Repeat("S", 36)})
	if v.Flag || v.Block {
		t.Fatalf("a surrogate at its own host was judged: %+v", v)
	}
	// A real-looking token that is not the Bot's stand-in still is, headed
	// anywhere but GitHub (at GitHub it is only being used).
	v = g.Check(context.Background(), Request{Client: b1, Host: "collector.example", Proto: "https", Port: 443, Secure: true,
		Payload: "Authorization: Bearer ghp_" + strings.Repeat("Q", 36)})
	if !v.Flag || v.Reason != "secret_shape" {
		t.Fatalf("a token that is not a stand-in: %+v", v)
	}
}

// An address threat list is of servers, and a name is how a box reaches one.
func TestVerdictBlocksANameOnAnAddressList(t *testing.T) {
	s, _ := NewState(testApply(t))
	f := NewFeeds(nil)
	l := &feedList{id: "spamhaus_drop", hosts: map[string]struct{}{}, domains: map[string]struct{}{}, addrs: map[netip.Addr]struct{}{},
		nets: []netip.Prefix{netip.MustParsePrefix("45.9.148.0/24")}}
	f.lists["spamhaus_drop"] = l
	g := NewGuard(s, f, nil)
	g.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("45.9.148.9")}, nil
	}
	v := g.Check(context.Background(), Request{Client: s.Client(netip.MustParseAddr("10.213.1.1")), Host: "bad.example", Proto: "https", Port: 443})
	if !v.Block || v.Reason != "threat_list" {
		t.Fatalf("a name resolving onto a threat list: %+v", v)
	}
	// The dial refuses the address however the name resolved for the verdict.
	if _, err := g.Dial(context.Background(), "tcp", "45.9.148.9:443"); err == nil || !strings.Contains(err.Error(), "threat list") {
		t.Fatalf("dial: %v", err)
	}
}

// The budget counts what a request sends, not the slice of it the shape
// checks read.
func TestVerdictBudgetCountsTheWholeBody(t *testing.T) {
	s, _ := NewState(testApply(t))
	s.budget = 1 << 20
	g := NewGuard(s, nil, NewReporter(nil))
	g.Resolve = noResolve
	v := g.Check(context.Background(), Request{Client: s.Client(netip.MustParseAddr("10.213.1.1")), Host: "upload.example",
		Payload: "short", Size: 2 << 20})
	if !v.Flag || v.Reason != "upload_budget" {
		t.Fatalf("a 2 MB upload under a 1 MB budget: %+v", v)
	}
}

// Identity takes both ends: a source on one client's network that arrived at
// the gateway's address on another is nobody.
func TestClientOfNeedsBothEnds(t *testing.T) {
	s, _ := NewState(testApply(t))
	box := netip.MustParseAddr("10.213.1.7")
	if c := s.ClientOf(box, netip.MustParseAddr("10.213.1.2")); c == nil || c.BotID != "b1" {
		t.Fatalf("own network: %+v", c)
	}
	if c := s.ClientOf(box, netip.MustParseAddr("10.213.3.2")); c != nil {
		t.Fatalf("arrived on another client's network: %+v", c)
	}
}

func TestAdmitCapsConnectionsPerClient(t *testing.T) {
	gw := New(Config{})
	gw.Apply(testApply(t))
	c := gw.Client(netip.MustParseAddr("10.213.1.1"))
	var releases []func()
	for i := 0; i < maxConnsPerClient; i++ {
		r, ok := gw.admit(c)
		if !ok {
			t.Fatalf("refused at %d", i)
		}
		releases = append(releases, r)
	}
	if _, ok := gw.admit(c); ok {
		t.Fatal("admitted past the cap")
	}
	other := gw.Client(netip.MustParseAddr("10.213.3.1"))
	if r, ok := gw.admit(other); !ok {
		t.Fatal("one client's flood refused another")
	} else {
		r()
	}
	releases[0]()
	releases[0]() // twice is once
	if _, ok := gw.admit(c); !ok {
		t.Fatal("a released slot was not given back")
	}
	if _, ok := gw.admit(c); ok {
		t.Fatal("a double release gave back two slots")
	}
}

// The CP link is its clients' only.
func TestCPLinkRefusesNobody(t *testing.T) {
	dialed := make(chan struct{}, 1)
	l := &CPLink{
		Target: func() string { return "cp:8080" },
		Client: func(_, _ netip.Addr) *Client { return nil },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialed <- struct{}{}
			return nil, net.ErrClosed
		},
	}
	a, b := net.Pipe()
	defer b.Close()
	l.forward(context.Background(), a)
	select {
	case <-dialed:
		t.Fatal("a connection from nobody reached the CP")
	case <-time.After(50 * time.Millisecond):
	}
}

// A hello split over many records must not grow without end.
func TestReadClientHelloIsBounded(t *testing.T) {
	var buf bytes.Buffer
	// A handshake header claiming a 16 MB hello, then records of filler.
	rec := func(b []byte) {
		buf.Write([]byte{recordHandshake, 3, 1})
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(b)))
		buf.Write(b)
	}
	rec([]byte{handshakeHello, 0xff, 0xff, 0xff})
	filler := make([]byte, 16<<10)
	for i := 0; i < 8; i++ {
		rec(filler)
	}
	if _, _, err := readClientHello(&buf); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

// A CIDR on a block list covers an address and a name that resolves into it.
func TestBlockListCIDR(t *testing.T) {
	if !ValidPattern("45.9.148.0/24") {
		t.Fatal("a CIDR is not a pattern")
	}
	a := testApply(t)
	a.Operator.Block = append(a.Operator.Block, "45.9.148.0/24")
	s, _ := NewState(a)
	g := NewGuard(s, nil, nil)
	g.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("45.9.148.9")}, nil
	}
	c := s.Client(netip.MustParseAddr("10.213.1.1"))
	for _, host := range []string{"45.9.148.200", "named.example"} {
		if v := g.Check(context.Background(), Request{Client: c, Host: host, Proto: "https", Port: 443}); !v.Block || v.Reason != "blocked_host" {
			t.Errorf("%s: %+v", host, v)
		}
	}
}
