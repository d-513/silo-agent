package egress

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// A nameserver on a client network is never asked: that network is internal
// and its DNS answers NXDOMAIN for every outside name.
func TestResolverSkipsClientNetworkDNS(t *testing.T) {
	gw := New(Config{})
	gw.Apply(testApply(t))
	dial := gw.Resolver().Dial
	if c, err := dial(context.Background(), "udp", "10.213.1.1:53"); err == nil {
		c.Close()
		t.Fatal("the resolver dialled a client network's DNS")
	}
	// Anything else is dialled as usual (UDP needs no listener).
	c, err := dial(context.Background(), "udp", "192.0.2.1:53")
	if err != nil {
		t.Fatalf("upstream dial: %v", err)
	}
	c.Close()
}

// fakeUpstream answers every A query with addr, over UDP on loopback.
func fakeUpstream(t *testing.T, addr netip.Addr) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			hdr, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			m := dnsmessage.Message{Header: dnsmessage.Header{ID: hdr.ID, Response: true, RecursionAvailable: true}, Questions: []dnsmessage.Question{q}}
			if q.Type == dnsmessage.TypeA {
				m.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.AResource{A: addr.As4()},
				}}
			}
			b, _ := m.Pack()
			_, _ = pc.WriteTo(b, from)
		}
	}()
	return pc.LocalAddr().String()
}

// The upstreams the control plane names are asked whatever the gateway's
// resolv.conf says: the engine rewrites that file on a restart with only the
// client networks' DNS, and a gateway that trusted it resolved nothing.
func TestResolverUsesTheNamedUpstreams(t *testing.T) {
	want := netip.MustParseAddr("192.0.2.7")
	gw := New(Config{})
	a := testApply(t)
	a.DnsServers = []string{fakeUpstream(t, want)}
	gw.Apply(a)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := gw.Resolver().LookupNetIP(ctx, "ip4", "outside.example.")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("lookup = %v, want %v", got, want)
	}
}

// A resolver that let a query time out is passed over for the next one, and
// asked again once it has rested; with all of them resting, the one that
// rested longest is tried.
func TestUpstreamFailover(t *testing.T) {
	var us upstreams
	us.set([]string{"192.0.2.1:53", "192.0.2.2:53", "bad entry"})
	if len(us.list) != 2 {
		t.Fatalf("list = %d entries, want the 2 readable ones", len(us.list))
	}
	if u := us.pick(); u.addr != "192.0.2.1:53" {
		t.Fatalf("pick = %s, want the first", u.addr)
	}
	us.list[0].fail()
	if u := us.pick(); u.addr != "192.0.2.2:53" {
		t.Fatalf("pick after a failure = %s, want the second", u.addr)
	}
	us.list[1].fail()
	if u := us.pick(); u.addr != "192.0.2.1:53" {
		t.Fatalf("pick with both resting = %s, want the one that failed first", u.addr)
	}
	us.list[0].mu.Lock()
	us.list[0].failedAt = time.Now().Add(-upstreamRest - time.Second)
	us.list[0].mu.Unlock()
	if u := us.pick(); u.addr != "192.0.2.1:53" {
		t.Fatalf("pick after a rest = %s, want the first again", u.addr)
	}
	// A new list keeps what is known of a server still on it.
	failed := us.list[1].failedAt
	us.set([]string{"192.0.2.2:53"})
	if us.list[0].failedAt != failed {
		t.Fatal("set forgot a server's failure")
	}
	us.set(nil)
	if us.pick() != nil {
		t.Fatal("pick with no upstreams must be nil")
	}
}

func TestResolvConfServers(t *testing.T) {
	got := ResolvConfServers(`# Generated
search home
nameserver 127.0.0.53
nameserver 192.168.10.1
nameserver ::1
nameserver fe80::1%eth0
nameserver 2001:db8::53
nameserver 0.0.0.0
nameservers 9.9.9.9
options edns0`)
	want := []string{"192.168.10.1:53", "[2001:db8::53]:53"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ResolvConfServers = %v, want %v", got, want)
	}
}
