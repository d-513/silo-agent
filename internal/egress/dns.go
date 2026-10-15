package egress

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsTTL is short on purpose: the answer is the gateway's own address, which
// does not move, but a short TTL keeps a stale client cache from outliving a
// network change.
const dnsTTL = 30

// DNS answers the resolution of every client with the gateway's address on
// that client's own network. The box therefore connects to the gateway and the
// real hostname rides in the Host header or the TLS SNI, which is where the
// filter reads it. A name that will not resolve is not a filter's business
// here: the client's DNS simply keeps working.
type DNS struct {
	// Addr returns the gateway's address on the client's network.
	Addr func(c *Client) (netip.Addr, bool)
	// Client finds who a source address belongs to.
	Client func(netip.Addr) *Client
	// Guard, when set, gets each queried name so a DNS-tunnelling shape or a
	// machine-made name is flagged (silently; the answer is the same).
	Guard func() *Guard
	// report records a flag.
	report func(c *Client, host string, v Verdict)
}

// dnsName reads a question name as a lower-case host.
func dnsName(n dnsmessage.Name) string { return strings.ToLower(strings.TrimSuffix(n.String(), ".")) }

// answer builds a reply for one query: an A (and AAAA when the gateway has
// one) pointing at the gateway, or an empty NOERROR for any other type.
func (d *DNS) answer(src netip.Addr, q dnsmessage.Question, m *dnsmessage.Message) {
	c := d.Client(src)
	if c == nil {
		// Not a client network: refuse rather than hand out the gateway.
		m.RCode = dnsmessage.RCodeRefused
		return
	}
	if d.Guard != nil {
		if g := d.Guard(); g != nil && g.Rep != nil && d.report != nil {
			if dnsShape(dnsName(q.Name)) {
				d.report(c, dnsName(q.Name), Verdict{Flag: true, Reason: "dns_shape", Detail: "a name shaped like data packed into labels"})
			}
		}
	}
	gw, ok := d.Addr(c)
	if !ok {
		m.RCode = dnsmessage.RCodeServerFailure
		return
	}
	gw = gw.Unmap()
	hdr := func(t dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: q.Name, Type: t, Class: dnsmessage.ClassINET, TTL: dnsTTL}
	}
	switch q.Type {
	case dnsmessage.TypeA:
		if v4 := gw.As4(); gw.Is4() {
			m.Answers = append(m.Answers, dnsmessage.Resource{Header: hdr(dnsmessage.TypeA), Body: &dnsmessage.AResource{A: v4}})
			return
		}
		// An IPv4 client on a v6-only network: nothing to hand it.
		m.RCode = dnsmessage.RCodeSuccess
	case dnsmessage.TypeAAAA:
		if gw.Is6() && !gw.Is4In6() {
			m.Answers = append(m.Answers, dnsmessage.Resource{Header: hdr(dnsmessage.TypeAAAA), Body: &dnsmessage.AAAAResource{AAAA: gw.As16()}})
			return
		}
		// No AAAA here: an empty success makes the client fall back to A.
		m.RCode = dnsmessage.RCodeSuccess
	default:
		// HTTPS/SVCB and the rest: no records, but no failure either.
		m.RCode = dnsmessage.RCodeSuccess
	}
}

// handle parses a wire query and returns the wire reply. It reads the source
// address to find the client network.
func (d *DNS) handle(src netip.Addr, query []byte) ([]byte, bool) {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return nil, false
	}
	q, err := p.Question()
	if err != nil {
		// A malformed or answer-less query: reflect it as a refusal.
		m := &dnsmessage.Message{Header: dnsmessage.Header{ID: hdr.ID, Response: true, RCode: dnsmessage.RCodeFormatError}}
		b, err := m.Pack()
		return b, err == nil
	}
	m := &dnsmessage.Message{Header: dnsmessage.Header{
		ID: hdr.ID, Response: true, RecursionDesired: hdr.RecursionDesired, RecursionAvailable: true,
	}}
	m.Questions = []dnsmessage.Question{q}
	d.answer(src, q, m)
	b, err := m.Pack()
	return b, err == nil
}

// ServeUDP answers datagrams until ctx ends.
func (d *DNS) ServeUDP(ctx context.Context, pc net.PacketConn) error {
	buf := make([]byte, 4096)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		src := addrOf(from)
		reply, ok := d.handle(src, buf[:n])
		if ok {
			_, _ = pc.WriteTo(reply, from)
		}
	}
}

// ServeTCP answers length-prefixed queries until ctx ends. A truncated UDP
// answer keeps TCP sane; we answer in full, so it is rarely used, but clients
// still try it.
func (d *DNS) ServeTCP(ctx context.Context, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go d.tcpConn(conn)
	}
}

func (d *DNS) tcpConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var mu sync.Mutex
	for {
		var l [2]byte
		if _, err := readFull(conn, l[:]); err != nil {
			return
		}
		n := int(l[0])<<8 | int(l[1])
		if n == 0 || n > 4096 {
			return
		}
		q := make([]byte, n)
		if _, err := readFull(conn, q); err != nil {
			return
		}
		reply, ok := d.handle(addrOf(conn.RemoteAddr()), q)
		if !ok {
			return
		}
		mu.Lock()
		err := writeFull(conn, append([]byte{byte(len(reply) >> 8), byte(len(reply))}, reply...))
		mu.Unlock()
		if err != nil {
			return
		}
	}
}

func addrOf(a net.Addr) netip.Addr {
	if ua, ok := a.(*net.UDPAddr); ok {
		if ap, ok := netip.AddrFromSlice(ua.IP); ok {
			return ap.Unmap()
		}
	}
	if ta, ok := a.(*net.TCPAddr); ok {
		if ap, ok := netip.AddrFromSlice(ta.IP); ok {
			return ap.Unmap()
		}
	}
	if ap, err := netip.ParseAddrPort(a.String()); err == nil {
		return ap.Addr().Unmap()
	}
	return netip.Addr{}
}

func readFull(c net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := c.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func writeFull(c net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := c.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}
