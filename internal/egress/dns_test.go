package egress

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func dnsQuery(t *testing.T, name string, qt dnsmessage.Type) []byte {
	t.Helper()
	n, err := dnsmessage.NewName(name + ".")
	if err != nil {
		t.Fatal(err)
	}
	m := &dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 0x1234, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: n, Type: qt, Class: dnsmessage.ClassINET}},
	}
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parseReply(t *testing.T, b []byte) *dnsmessage.Message {
	t.Helper()
	var p dnsmessage.Parser
	hdr, err := p.Start(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SkipAllQuestions(); err != nil {
		t.Fatal(err)
	}
	answers, err := p.AllAnswers()
	if err != nil {
		t.Fatal(err)
	}
	return &dnsmessage.Message{Header: hdr, Answers: answers}
}

func TestDNSAnswersGatewayAddress(t *testing.T) {
	s, _ := NewState(testApply(t))
	gw := netip.MustParseAddr("10.213.1.1")
	d := &DNS{
		Client: s.Client,
		Addr: func(c *Client) (netip.Addr, bool) {
			if c != nil && c.Net.Contains(gw) {
				return gw, true
			}
			return netip.Addr{}, false
		},
		Guard: func() *Guard { return nil },
	}
	reply, ok := d.handle(netip.MustParseAddr("10.213.1.5"), dnsQuery(t, "example.com", dnsmessage.TypeA))
	if !ok {
		t.Fatal("no reply")
	}
	m := parseReply(t, reply)
	if len(m.Answers) != 1 {
		t.Fatalf("answers = %d, want 1", len(m.Answers))
	}
	a, ok := m.Answers[0].Body.(*dnsmessage.AResource)
	if !ok || netip.AddrFrom4(a.A) != gw {
		t.Fatalf("A record = %+v, want %s", m.Answers[0].Body, gw)
	}
}

func TestDNSRefusesUnknownSource(t *testing.T) {
	s, _ := NewState(testApply(t))
	d := &DNS{Client: s.Client, Addr: func(*Client) (netip.Addr, bool) { return netip.Addr{}, false }}
	reply, ok := d.handle(netip.MustParseAddr("192.0.2.9"), dnsQuery(t, "example.com", dnsmessage.TypeA))
	if !ok {
		t.Fatal("no reply")
	}
	if m := parseReply(t, reply); m.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("RCode = %v, want Refused", m.RCode)
	}
}

func TestDNSAAAAEmptySuccessForV4Gateway(t *testing.T) {
	s, _ := NewState(testApply(t))
	d := &DNS{
		Client: s.Client,
		Addr:   func(*Client) (netip.Addr, bool) { return netip.MustParseAddr("10.213.1.1"), true },
	}
	reply, _ := d.handle(netip.MustParseAddr("10.213.1.5"), dnsQuery(t, "example.com", dnsmessage.TypeAAAA))
	m := parseReply(t, reply)
	if m.RCode != dnsmessage.RCodeSuccess || len(m.Answers) != 0 {
		t.Fatalf("AAAA on a v4-only gateway: %+v", m)
	}
}

func TestDNSServeUDP(t *testing.T) {
	s, _ := NewState(testApply(t))
	d := &DNS{
		Client: s.Client,
		Addr:   func(*Client) (netip.Addr, bool) { return netip.MustParseAddr("10.213.1.1"), true },
	}
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.ServeUDP(ctx, pc)
	conn, err := net.Dial("udp4", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A datagram from an unknown source is refused, not dropped.
	if _, err := conn.Write(dnsQuery(t, "example.com", dnsmessage.TypeA)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if m := parseReply(t, buf[:n]); m.RCode != dnsmessage.RCodeRefused {
		t.Fatalf("RCode = %v", m.RCode)
	}
}
