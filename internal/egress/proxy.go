package egress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const (
	// maxBody is how much of one request or response body the gateway holds in
	// memory. A request past it is refused; a response past it is streamed on.
	maxBody = 64 << 20
	// payloadCap bounds what the shape checks read of one request.
	payloadCap = 256 << 10
	// headPeek is how much of a response is read to spot an executable.
	headPeek = 512
)

// Proxy is the HTTP door: an explicit HTTP proxy (absolute-URI and CONNECT), a
// transparent listener on 80, and a transparent MITM on 443. Every request is
// judged, surrogates are swapped, and the origin is re-originated with a hello
// that mirrors the client's.
type Proxy struct {
	Client func(netip.Addr) *Client
	Guard  func() *Guard
	// Dial opens a plain TCP connection to the origin.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// DialTLS wraps a raw connection in TLS for a decrypted origin. Tests
	// replace it with a stdlib client; the default mirrors the client's hello.
	DialTLS func(raw net.Conn, host string, spec *utls.ClientHelloSpec) (net.Conn, error)
	// report records a block or a flag; Traffic sums every request (pass
	// included) into the per-minute rows.
	report  func(c *Client, host string, v Verdict, up, down int64)
	Traffic func(c *Client, host string, up, down int64, verdict string)
}

// verdictName is the traffic-row label for a verdict: block, flag, or "" for a
// pass.
func verdictName(v Verdict) string {
	switch {
	case v.Block:
		return "block"
	case v.Flag:
		return "flag"
	}
	return ""
}

func (p *Proxy) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, network, addr)
	}
	return originDial(ctx, network, addr)
}

func (p *Proxy) dialTLS(raw net.Conn, host string, spec *utls.ClientHelloSpec) (net.Conn, error) {
	if p.DialTLS != nil {
		return p.DialTLS(raw, host, spec)
	}
	return dialTLS(raw, host, spec)
}

// Serve accepts connections until ctx ends, running each through one of the
// three doors.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener, mode string) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go func() {
			defer conn.Close()
			_ = p.conn(ctx, conn, mode)
		}()
	}
}

// Modes a listener runs in.
const (
	// ModeProxy is an explicit HTTP proxy (proxy env vars, Chromium's flag).
	ModeProxy = "proxy"
	// ModeHTTP is a transparent listener on 80: origin-form requests.
	ModeHTTP = "http"
	// ModeTLS is a transparent listener on 443: a raw TLS hello.
	ModeTLS = "tls"
)

func (p *Proxy) conn(ctx context.Context, conn net.Conn, mode string) error {
	c := p.Client(addrOf(conn.RemoteAddr()))
	if c == nil {
		// Not from a client network: the gateway's own ordinary network (any
		// container on it) or a stray route. DNS and SOCKS refuse these too;
		// the filter is nobody's open proxy.
		return nil
	}
	switch mode {
	case ModeTLS:
		return p.transparentTLS(ctx, conn, c)
	default:
		br := bufio.NewReader(conn)
		transparent := mode == ModeHTTP
		// A transparent listener only ever sees origin-form requests, but the
		// same loop handles a proxy's absolute-URI ones.
		return p.httpLoop(ctx, conn, conn, br, c, transparent, nil)
	}
}

// httpLoop serves requests off br, writing them to the same conn. origin, when
// set, is a fixed upstream (used inside a MITM session where the TLS conn to
// the client is separate from the socket).
func (p *Proxy) httpLoop(ctx context.Context, client net.Conn, out io.Writer, br *bufio.Reader, c *Client, transparent bool, _ net.Conn) error {
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return err
		}
		done, err := p.handleRequest(ctx, client, out, br, c, req)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// handleRequest judges and forwards one request. It reports whether the
// connection must end.
func (p *Proxy) handleRequest(ctx context.Context, client net.Conn, out io.Writer, br *bufio.Reader, c *Client, req *http.Request) (bool, error) {
	if req.Method == http.MethodConnect {
		return p.handleConnect(ctx, client, br, c, req)
	}
	scheme := "http"
	host, port := hostPort(req.URL.Host, 80)
	if req.URL.Host == "" { // transparent: origin-form
		host, port = hostPort(req.Host, 80)
	} else if req.URL.Scheme != "" {
		scheme = req.URL.Scheme
	}
	if host == "" {
		writeBlock(out, "no host in the request")
		return true, nil
	}
	if scheme == "https" {
		// An absolute https URI (rare in a plain proxy): fetch it over TLS.
		return p.httpsRequest(ctx, out, c, host, port, nil, req)
	}
	// Read the body (capped) so surrogates and the shape checks see it whole.
	if req.Header.Get("Expect") == "100-continue" {
		_, _ = io.WriteString(client, "HTTP/1.1 100 Continue\r\n\r\n")
		req.Header.Del("Expect")
	}
	body, err := readCapped(req.Body, maxBody)
	if err != nil {
		writeStatusCode(out, http.StatusRequestEntityTooLarge, "this request is larger than the filter allows")
		return true, nil
	}
	g := p.Guard()
	path := req.URL.RequestURI()
	verdict := g.Check(ctx, Request{Client: c, Host: host, Port: port, Proto: "http", Method: req.Method, Path: path, Payload: payloadOf(req, body)})
	if verdict.Block {
		p.reportVerdict(c, host, verdict, 0, 0)
		p.traffic(c, host, 0, 0, "block")
		writeBlock(out, verdict.Detail)
		return true, nil
	}
	if verdict.Flag {
		p.reportVerdict(c, host, verdict, int64(len(body)), 0)
	}
	if g.State != nil && c != nil && g.State.bot(c).surrogateHost(host) {
		body = swapRequest(g.State.bot(c), req, body, host)
	}
	// Forward.
	origin, err := p.dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		writeStatusCode(out, http.StatusBadGateway, "the filter could not reach "+host)
		return true, nil
	}
	defer origin.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.RequestURI = ""
	req.URL.Scheme = ""
	req.URL.Host = ""
	req.Host = net.JoinHostPort(host, strconv.Itoa(port))
	if req.Host == host+":80" {
		req.Host = host
	}
	if err := req.Write(origin); err != nil {
		writeStatusCode(out, http.StatusBadGateway, "the filter lost the origin")
		return true, nil
	}
	done, down, err := p.relayResponse(ctx, out, origin, req, c, host)
	p.traffic(c, host, int64(len(body)), down, verdictName(verdict))
	return done, err
}

// relayResponse streams the origin's response back, checking its head and
// swapping a surrogate's real value back to the stand-in.
func (p *Proxy) relayResponse(ctx context.Context, out io.Writer, origin net.Conn, req *http.Request, c *Client, host string) (bool, int64, error) {
	resp, err := http.ReadResponse(bufio.NewReader(origin), req)
	if err != nil {
		writeStatusCode(out, http.StatusBadGateway, "the filter got no answer from "+host)
		return true, 0, nil
	}
	defer resp.Body.Close()
	g := p.Guard()
	body := resp.Body
	if c != nil && g.State != nil && g.State.bot(c).surrogateHost(host) {
		raw, err := readCapped(body, maxBody)
		if err != nil {
			raw = []byte{}
		}
		swapped := swapBackBody(g.State.bot(c), string(raw))
		body = io.NopCloser(strings.NewReader(swapped))
		resp.ContentLength = int64(len(swapped))
		resp.Header.Set("Content-Length", strconv.Itoa(len(swapped)))
		resp.TransferEncoding = nil
	}
	// Peek the head for an executable, then put it back.
	head, pre, err := peek(body, headPeek)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return true, 0, nil
	}
	if v := g.CheckResponse(c, host, head, resp.Header.Get("Content-Type")); v.Flag {
		p.reportVerdict(c, host, v, 0, 0)
	}
	resp.Body = io.NopCloser(pre)
	resp.Close = resp.Close || req.Close
	cw := &countWriter{w: out}
	if err := resp.Write(cw); err != nil {
		return true, cw.n, err
	}
	return resp.Close || req.Close, cw.n, nil
}

// countWriter counts what went back to the client, for the traffic rows.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// handleConnect answers a proxy CONNECT: a plain TLS splice for a pass-through
// host, a MITM otherwise.
func (p *Proxy) handleConnect(ctx context.Context, client net.Conn, br *bufio.Reader, c *Client, req *http.Request) (bool, error) {
	host, port := hostPort(req.Host, 443)
	if host == "" {
		writeBlock(client, "no host in the CONNECT")
		return true, nil
	}
	g := p.Guard()
	if g.State != nil && g.State.passThrough(c, host) {
		if v := p.passVerdict(ctx, g, c, host, port); v.Block {
			writeBlock(client, v.Detail)
			return true, nil
		}
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		up, err := p.dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return true, nil
		}
		defer up.Close()
		splice(io.MultiReader(br, client), client, up)
		return true, nil
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return true, err
	}
	// The client's hello follows; read it, then decrypt both sides.
	return true, p.mitmStream(ctx, client, c, host, port, br, nil)
}

// transparentTLS handles a raw TLS connection on 443: it reads the hello to
// learn the host, then splices or MITMs.
func (p *Proxy) transparentTLS(ctx context.Context, conn net.Conn, c *Client) error {
	raw, sni, err := readClientHello(conn)
	if err != nil {
		return err
	}
	host := NormHost(sni)
	if host == "" {
		return errors.New("no SNI in the hello")
	}
	g := p.Guard()
	if g.State != nil && g.State.passThrough(c, host) {
		if v := p.passVerdict(ctx, g, c, host, 443); v.Block {
			// No TLS to answer in: the box sees the connection end.
			return nil
		}
		up, err := p.dial(ctx, "tcp", net.JoinHostPort(host, "443"))
		if err != nil {
			return err
		}
		defer up.Close()
		splice(io.MultiReader(bytes.NewReader(raw), conn), conn, up)
		return nil
	}
	return p.mitmStream(ctx, conn, c, host, 443, nil, raw)
}

// mitmStream decrypts the client (replaying raw first when the hello was
// already read), judges each request, and re-originates over a mirrored TLS
// connection. br, when set, is the client reader (a proxy CONNECT); raw is the
// already-read hello.
func (p *Proxy) mitmStream(ctx context.Context, client net.Conn, c *Client, host string, port int, br *bufio.Reader, raw []byte) error {
	g := p.Guard()
	if g.State == nil || g.State.ca == nil {
		// No CA: pass through on SNI only.
		up, err := p.dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		defer up.Close()
		src := clientReader(client, br, raw)
		splice(src, client, up)
		return nil
	}
	src := clientReader(client, br, raw)
	// If the hello was not read yet, read it now to fingerprint.
	if raw == nil {
		r, sni, err := readClientHello(src)
		if err != nil {
			return err
		}
		raw = r
		if hello := NormHost(sni); hello != "" {
			host = hello
		}
		src = io.MultiReader(bytes.NewReader(raw), src)
	}
	tlsConn := tls.Server(&prefixConn{Conn: client, r: src}, serverTLSConfig(g.State.ca))
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return err
	}
	spec := mirrorSpec(raw, host)
	return p.httpsLoop(ctx, tlsConn, c, host, port, spec)
}

// httpsLoop reads requests off the decrypted client connection and forwards
// them, minting a fresh mirrored origin connection per request.
func (p *Proxy) httpsLoop(ctx context.Context, client net.Conn, c *Client, host string, port int, spec *utls.ClientHelloSpec) error {
	br := bufio.NewReader(client)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return err
		}
		req.URL.Scheme = "https"
		if req.URL.Host == "" {
			req.URL.Host = net.JoinHostPort(host, strconv.Itoa(port))
		}
		done, err := p.httpsRequest(ctx, client, c, host, port, spec, req)
		if err != nil || done {
			return err
		}
	}
}

func (p *Proxy) httpsRequest(ctx context.Context, out io.Writer, c *Client, host string, port int, spec *utls.ClientHelloSpec, req *http.Request) (bool, error) {
	if req.Header.Get("Expect") == "100-continue" {
		_, _ = io.WriteString(out, "HTTP/1.1 100 Continue\r\n\r\n")
		req.Header.Del("Expect")
	}
	body, err := readCapped(req.Body, maxBody)
	if err != nil {
		writeStatusCode(out, http.StatusRequestEntityTooLarge, "this request is larger than the filter allows")
		return true, nil
	}
	g := p.Guard()
	path := req.URL.RequestURI()
	v := g.Check(ctx, Request{Client: c, Host: host, Port: port, Proto: "https", Method: req.Method, Path: path, Payload: payloadOf(req, body)})
	if v.Block {
		p.reportVerdict(c, host, v, 0, 0)
		p.traffic(c, host, 0, 0, "block")
		writeBlock(out, v.Detail)
		return true, nil
	}
	if v.Flag {
		p.reportVerdict(c, host, v, int64(len(body)), 0)
	}
	if c != nil && g.State != nil && g.State.bot(c).surrogateHost(host) {
		body = swapRequest(g.State.bot(c), req, body, host)
	}
	raw, err := p.dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		writeStatusCode(out, http.StatusBadGateway, "the filter could not reach "+host)
		return true, nil
	}
	origin, err := p.dialTLS(raw, host, spec)
	if err != nil {
		raw.Close()
		writeStatusCode(out, http.StatusBadGateway, "the filter could not secure the origin")
		return true, nil
	}
	defer origin.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.RequestURI = ""
	req.URL.Scheme = ""
	req.URL.Host = ""
	req.Host = host
	if err := req.Write(origin); err != nil {
		writeStatusCode(out, http.StatusBadGateway, "the filter lost the origin")
		return true, nil
	}
	done, down, err := p.relayResponse(ctx, out, origin, req, c, host)
	p.traffic(c, host, int64(len(body)), down, verdictName(v))
	return done, err
}

// payloadOf is the text the shape checks read: the request line, its headers
// and a capped prefix of the body.
func payloadOf(req *http.Request, body []byte) string {
	var b strings.Builder
	b.WriteString(req.Method)
	b.WriteByte(' ')
	b.WriteString(req.URL.RequestURI())
	b.WriteByte('\n')
	_ = req.Header.Write(&b)
	if len(body) > 0 {
		b.WriteByte('\n')
		if len(body) > payloadCap {
			b.Write(body[:payloadCap])
		} else {
			b.Write(body)
		}
	}
	return b.String()
}

// swapRequest writes the real value over the in-host surrogates of a request:
// always in its headers, and in its body only for a secret whose owner
// allowed that. The verdict has already blocked a surrogate headed anywhere
// else.
func swapRequest(b *botState, req *http.Request, body []byte, host string) []byte {
	b.swapHeaders(req.Header, host)
	if len(body) == 0 {
		return body
	}
	return []byte(b.swapBody(string(body), host))
}

// swapBackBody writes each surrogate over its real value.
func swapBackBody(b *botState, s string) string { return b.swapBack(s) }

// passVerdict judges a connection that will be spliced rather than decrypted:
// a host on the Pass-through list, or every host while no CA is loaded. Nothing
// inside it can be read, but the lists, the threat feeds and the
// internal-address check hold for it as for any request, so neither an owner's
// Pass entry nor a CA that failed to load turns the filter off. A spliced
// stream to a port other than 443 is a raw TCP tunnel, which only the
// Other-protocols switch opens.
func (p *Proxy) passVerdict(ctx context.Context, g *Guard, c *Client, host string, port int) Verdict {
	v := g.Check(ctx, Request{Client: c, Host: host, Port: port, Proto: "https"})
	if !v.Block && port != 443 && !g.State.OtherProtocols(c) {
		v = Verdict{Block: true, Reason: "other_protocols",
			Detail: fmt.Sprintf("port %d is not HTTPS here; the Bot's owner can turn on Other protocols", port)}
	}
	if v.Block || v.Flag {
		p.reportVerdict(c, host, v, 0, 0)
	}
	if v.Block {
		p.traffic(c, host, 0, 0, "block")
	}
	return v
}

func (p *Proxy) reportVerdict(c *Client, host string, v Verdict, up, down int64) {
	if p.report != nil {
		p.report(c, host, v, up, down)
	}
}

func (p *Proxy) traffic(c *Client, host string, up, down int64, verdict string) {
	if p.Traffic != nil {
		p.Traffic(c, host, up, down, verdict)
	}
}

// clientReader replays raw (an already-read hello) then reads from br or conn.
func clientReader(conn net.Conn, br *bufio.Reader, raw []byte) io.Reader {
	base := io.Reader(conn)
	if br != nil {
		base = br
	}
	if raw == nil {
		return base
	}
	return io.MultiReader(bytes.NewReader(raw), base)
}

// prefixConn reads through r but writes and reports the address of the real
// connection (so tls.Server still sees a net.Conn).
type prefixConn struct {
	net.Conn
	r io.Reader
}

func (c *prefixConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// hostPort splits "host:port" (or a bare host) and applies a default port.
func hostPort(hostport string, def int) (string, int) {
	if hostport == "" {
		return "", def
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return NormHost(hostport), def
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return NormHost(host), def
	}
	return NormHost(host), n
}

func readCapped(r io.Reader, max int64) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return b, err
	}
	if int64(len(b)) > max {
		return b, errors.New("too large")
	}
	return b, nil
}

// peek reads up to n bytes and returns them with a reader that replays them
// before the rest.
func peek(r io.Reader, n int) ([]byte, io.Reader, error) {
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	head := buf[:got]
	return head, io.MultiReader(bytes.NewReader(head), r), err
}

// writeBlock answers a request the filter refused, with words the Box can act
// on. It never disguises itself as the origin.
func writeBlock(w io.Writer, detail string) {
	msg := "Blocked by the Silo network filter."
	if detail != "" {
		msg += " " + detail
	}
	writeStatusCode(w, http.StatusForbidden, msg)
}

func writeStatusCode(w io.Writer, code int, msg string) {
	body := msg + "\n"
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		code, http.StatusText(code), len(body), body)
}

// splice copies both ways until either side ends, half-closing as it goes.
func splice(src io.Reader, client net.Conn, up net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, src); closeWrite(up); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, up); closeWrite(client); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.SetReadDeadline(time.Now())
}
