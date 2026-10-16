package egress

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"
)

// SOCKS5 reply codes (RFC 1928).
const (
	socksOK          = 0x00
	socksFailure     = 0x01
	socksNotAllowed  = 0x02
	socksHostUnreach = 0x04
	socksCmdBad      = 0x07
	socksAtypBad     = 0x08
)

// SOCKS is the SOCKS5 door for protocols that are not HTTP: an ssh
// ProxyCommand, proxychains4, a database client. It is off unless the Bot's
// Other-protocols switch is on. The hostname arrives plain, so the filter reads
// it and the gateway dials it; nothing is decrypted because nothing here is TLS
// to us.
type SOCKS struct {
	Client func(remote, local netip.Addr) *Client
	Guard  func() *Guard
	// Admit takes one of the client's connection slots; nil admits all.
	Admit func(c *Client) (release func(), ok bool)
	// Dial reaches the destination; nil is the guarded origin dial.
	Dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	report func(c *Client, host string, v Verdict)
}

// dial reaches the destination. With no Dial set it is the guarded origin
// dial, which refuses an internal address however the name resolved.
func (s *SOCKS) dial(ctx context.Context, g *Guard, network, addr string) (net.Conn, error) {
	if s.Dial != nil {
		return s.Dial(ctx, network, addr)
	}
	return g.Dial(ctx, network, addr)
}

// Serve accepts connections until ctx ends. It answers one CONNECT per
// connection.
func (s *SOCKS) Serve(ctx context.Context, ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.conn(ctx, conn)
	}
}

func (s *SOCKS) conn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c := s.Client(addrOf(conn.RemoteAddr()), addrOf(conn.LocalAddr()))
	if c == nil {
		return
	}
	if s.Admit != nil {
		release, ok := s.Admit(c)
		if !ok {
			return
		}
		defer release()
	}
	g := s.Guard()
	if g == nil || !g.State.OtherProtocols(c) {
		// The switch is off: this Bot has no non-HTTP door.
		_, _ = conn.Write([]byte{5, socksNotAllowed})
		return
	}
	if err := socksGreet(conn); err != nil {
		return
	}
	host, port, err := socksRequest(conn)
	if err != nil {
		return
	}
	g = s.Guard()
	v := g.Check(ctx, Request{Client: c, Host: host, Port: port, Proto: "tcp"})
	if v.Flag && s.report != nil {
		s.report(c, host, v)
	}
	if v.Block {
		if s.report != nil {
			s.report(c, host, v)
		}
		socksReply(conn, socksNotAllowed)
		return
	}
	target := net.JoinHostPort(host, fmt.Sprint(port))
	up, err := s.dial(ctx, g, "tcp", target)
	if err != nil {
		socksReply(conn, socksHostUnreach)
		return
	}
	defer up.Close()
	_ = conn.SetDeadline(time.Time{})
	if err := socksReply(conn, socksOK); err != nil {
		return
	}
	splice(conn, conn, up)
}

// socksGreet reads the method negotiation and picks "no authentication".
func socksGreet(conn net.Conn) error {
	var head [2]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return err
	}
	if head[0] != 5 {
		return fmt.Errorf("socks: version %d", head[0])
	}
	if head[1] == 0 {
		return fmt.Errorf("socks: no methods")
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	for _, m := range methods {
		if m == 0 {
			_, err := conn.Write([]byte{5, 0})
			return err
		}
	}
	_, _ = conn.Write([]byte{5, 0xFF})
	return fmt.Errorf("socks: no acceptable method")
}

// socksRequest reads a CONNECT request and returns the host and port.
func socksRequest(conn net.Conn) (string, int, error) {
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return "", 0, err
	}
	if head[0] != 5 || head[1] != 1 { // only CONNECT
		socksReply(conn, socksCmdBad)
		return "", 0, fmt.Errorf("socks: command %d", head[1])
	}
	var host string
	switch head[3] {
	case 1:
		var b [4]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return "", 0, err
		}
		host = netip.AddrFrom4(b).String()
	case 4:
		var b [16]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return "", 0, err
		}
		host = netip.AddrFrom16(b).Unmap().String()
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return "", 0, err
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return "", 0, err
		}
		host = string(name)
	default:
		socksReply(conn, socksAtypBad)
		return "", 0, fmt.Errorf("socks: address type %d", head[3])
	}
	var pb [2]byte
	if _, err := io.ReadFull(conn, pb[:]); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(pb[:])), nil
}

// socksReply writes a reply with a zero bound address: a client rarely needs
// it, and the meaningful part is the code.
func socksReply(conn net.Conn, code byte) error {
	_, err := conn.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}
