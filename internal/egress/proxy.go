package egress

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	utls "github.com/refraction-networking/utls"
)

const (
	// payloadCap bounds what the shape checks read of one request. The rest
	// of a body streams on, read only for surrogates.
	payloadCap = 256 << 10
	// headPeek is how much of a response is read to spot an executable.
	headPeek = 512
	// idleTimeout is how long a connection may sit between requests, and
	// helloTimeout how long a TLS hello and handshake may take: a box must
	// not hold the gateway's connections open by saying nothing.
	idleTimeout  = 2 * time.Minute
	helloTimeout = 30 * time.Second
)

// Proxy is the HTTP door: an explicit HTTP proxy (absolute-URI and CONNECT), a
// transparent listener on 80, and a transparent MITM on 443. Every request is
// judged, surrogates are swapped, and the origin is re-originated with a hello
// that mirrors the client's.
type Proxy struct {
	// Client finds who a connection belongs to from its two ends.
	Client func(remote, local netip.Addr) *Client
	Guard  func() *Guard
	// Admit takes one of the client's connection slots; nil admits all. A
	// box past its share is refused, so one Bot cannot use up the gateway
	// every other Bot goes through.
	Admit func(c *Client) (release func(), ok bool)
	// Dial opens a plain TCP connection to the origin; nil is the guarded
	// dial.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// DialTLS wraps a raw connection in TLS for a decrypted origin. Tests
	// replace it with a stdlib client; the default mirrors the client's hello
	// and verifies the origin against Roots.
	DialTLS func(raw net.Conn, host string, spec *utls.ClientHelloSpec) (net.Conn, error)
	// Roots verify the origins; nil is the system's.
	Roots *x509.CertPool
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

func (p *Proxy) dial(ctx context.Context, g *Guard, network, addr string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, network, addr)
	}
	return g.Dial(ctx, network, addr)
}

// dialTLS makes the TLS connection to the origin, mirroring hello (the box's
// own ClientHello; nil for a request that came over plain HTTP).
//
// The spec is built anew for every handshake and never kept: applying one
// writes the handshake's public keys into its key_share extension, and utls
// generates no private key for a share that already has data. A spec reused
// for the next request on a kept-alive connection therefore failed every
// handshake after the first with "local error: tls: internal error": a page's
// HTML loaded and its styles, scripts and API calls did not.
func (p *Proxy) dialTLS(raw net.Conn, host string, hello []byte) (net.Conn, error) {
	var spec *utls.ClientHelloSpec
	if hello != nil {
		spec = mirrorSpec(hello, host)
	}
	_ = raw.SetDeadline(time.Now().Add(helloTimeout))
	defer func() { _ = raw.SetDeadline(time.Time{}) }()
	if p.DialTLS != nil {
		return p.DialTLS(raw, host, spec)
	}
	return dialTLS(raw, host, spec, p.Roots)
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
	// ModeProxy is an explicit HTTP proxy (absolute-URI and CONNECT).
	ModeProxy = "proxy"
	// ModeHTTP is a transparent listener on 80: origin-form requests.
	ModeHTTP = "http"
	// ModeTLS is a transparent listener on 443: a raw TLS hello.
	ModeTLS = "tls"
)

func (p *Proxy) conn(ctx context.Context, conn net.Conn, mode string) error {
	c := p.Client(addrOf(conn.RemoteAddr()), addrOf(conn.LocalAddr()))
	if c == nil {
		// Not from a client network: the gateway's own ordinary network (any
		// container on it) or a stray route. DNS and SOCKS refuse these too;
		// the filter is nobody's open proxy.
		return nil
	}
	if p.Admit != nil {
		release, ok := p.Admit(c)
		if !ok {
			if mode != ModeTLS {
				writeStatusCode(conn, http.StatusServiceUnavailable, "too many connections through the Silo network filter at once")
			}
			return nil
		}
		defer release()
	}
	if mode == ModeTLS {
		return p.transparentTLS(ctx, conn, c)
	}
	// A transparent listener only ever sees origin-form requests, but the
	// same loop handles a proxy's absolute-URI ones.
	return p.serveHTTP(ctx, conn, bufio.NewReader(conn), c, "", 0, nil)
}

// serveHTTP serves requests off br, answering on client. tlsHost, when set, is
// the decrypted origin every request goes to (a MITM session); otherwise each
// request names its own.
func (p *Proxy) serveHTTP(ctx context.Context, client net.Conn, br *bufio.Reader, c *Client, tlsHost string, port int, hello []byte) error {
	for {
		_ = client.SetReadDeadline(time.Now().Add(idleTimeout))
		req, err := http.ReadRequest(br)
		if err != nil {
			return err
		}
		// A body may take as long as it takes; the slot cap bounds how many.
		_ = client.SetReadDeadline(time.Time{})
		done, err := p.handleRequest(ctx, client, br, c, req, tlsHost, port, hello)
		if err != nil || done {
			return err
		}
	}
}

// handleRequest judges and forwards one request. It reports whether the
// connection must end.
func (p *Proxy) handleRequest(ctx context.Context, client net.Conn, br *bufio.Reader, c *Client, req *http.Request, tlsHost string, tlsPort int, hello []byte) (bool, error) {
	if req.Method == http.MethodConnect {
		if tlsHost != "" {
			writeBlock(client, "CONNECT inside a decrypted connection is not served")
			return true, nil
		}
		return p.handleConnect(ctx, client, br, c, req)
	}
	host, port, secure := tlsHost, tlsPort, tlsHost != ""
	if !secure {
		switch {
		case req.URL.Host == "": // transparent, or plain inside a CONNECT: origin-form
			def := 80
			if tlsPort != 0 {
				def = tlsPort
			}
			host, port = hostPort(req.Host, def)
		case req.URL.Scheme == "https":
			// An absolute https URI (rare in a plain proxy): fetch it over TLS.
			host, port = hostPort(req.URL.Host, 443)
			secure = true
		default:
			host, port = hostPort(req.URL.Host, 80)
		}
	}
	if host == "" {
		writeBlock(client, "no host in the request")
		return true, nil
	}
	return p.forward(ctx, client, br, c, req, host, port, secure, hello)
}

// forward sends one request on to host and its answer back. The body is never
// held whole: its first payloadCap bytes are read for the verdict, and the
// rest streams through the surrogate swaps, which end the upload if a stand-in
// turns up where it must not go.
func (p *Proxy) forward(ctx context.Context, client net.Conn, br *bufio.Reader, c *Client, req *http.Request, host string, port int, secure bool, hello []byte) (bool, error) {
	if req.Header.Get("Expect") == "100-continue" {
		_, _ = io.WriteString(client, "HTTP/1.1 100 Continue\r\n\r\n")
		req.Header.Del("Expect")
	}
	head, err := io.ReadAll(io.LimitReader(req.Body, payloadCap))
	if err != nil {
		writeStatusCode(client, http.StatusBadRequest, "the filter could not read the request body")
		return true, nil
	}
	g := p.Guard()
	proto := "http"
	if secure {
		proto = "https"
	}
	size := req.ContentLength
	if size < 0 {
		size = int64(len(head))
	}
	v := g.Check(ctx, Request{Client: c, Host: host, Port: port, Proto: proto, Method: req.Method,
		Path: req.URL.RequestURI(), Payload: payloadOf(req, head), Size: size, Secure: secure})
	if v.Block {
		p.reportVerdict(c, host, v, 0, 0)
		p.traffic(c, host, 0, 0, "block")
		writeBlock(client, v.Detail)
		return true, nil
	}
	if v.Flag {
		p.reportVerdict(c, host, v, size, 0)
	}
	b := g.State.bot(c)
	swapHost := secure && b.surrogateHost(host)
	if swapHost {
		b.swapHeaders(req.Header, host)
		// The answer is read for the real value, which a compressed body
		// would hide; ask for it plain.
		req.Header.Del("Accept-Encoding")
	}
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization", "Keep-Alive"} {
		req.Header.Del(h)
	}

	origin, err := p.dial(ctx, g, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		log.Printf("egress: dial %s:%d: %s", host, port, dialProblem(err))
		p.traffic(c, host, 0, 0, "")
		writeStatusCode(client, http.StatusBadGateway, "the filter could not reach "+host)
		return true, nil
	}
	if secure {
		tc, err := p.dialTLS(origin, host, hello)
		if err != nil {
			origin.Close()
			log.Printf("egress: tls to %s:%d: %v", host, port, err)
			p.traffic(c, host, 0, 0, "")
			writeStatusCode(client, http.StatusBadGateway, "the filter could not make a verified TLS connection to "+host+": "+err.Error())
			return true, nil
		}
		origin = tc
	}
	defer origin.Close()

	body := newReplacer(io.MultiReader(bytes.NewReader(head), req.Body), b.bodySwaps(host, secure))
	sent := &countReader{r: body}
	if req.ContentLength == 0 && len(req.TransferEncoding) == 0 {
		req.Body = http.NoBody
	} else {
		req.Body = io.NopCloser(sent)
	}
	req.RequestURI = ""
	req.URL.Scheme = ""
	req.URL.Host = ""
	req.Host = host
	if (secure && port != 443) || (!secure && port != 80) {
		req.Host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if err := req.Write(origin); err != nil {
		if name := leakedFrom(body); name != "" {
			lv := Verdict{Block: true, Reason: "surrogate_leak", Detail: "secret " + name + " tried to leave to " + host}
			if !secure && b.allowedTo(name, host) {
				lv = Verdict{Block: true, Reason: "surrogate_insecure", Detail: "secret " + name + " is only sent over HTTPS"}
			}
			p.reportVerdict(c, host, lv, sent.n, 0)
			p.traffic(c, host, sent.n, 0, "block")
			writeBlock(client, lv.Detail)
			return true, nil
		}
		log.Printf("egress: %s %s%s: sending the request: %v", req.Method, host, req.URL.Path, err)
		writeStatusCode(client, http.StatusBadGateway, "the filter lost the origin")
		return true, nil
	}
	if req.ContentLength < 0 {
		if fv := g.Uploaded(c, host, sent.n-int64(len(head))); fv.Flag {
			p.reportVerdict(c, host, fv, sent.n, 0)
		}
	}
	done, down, err := p.relayResponse(client, br, origin, req, c, host, swapHost)
	p.traffic(c, host, sent.n, down, verdictName(v))
	return done, err
}

// dialProblem words a failed origin dial for the log. A lookup error names
// the nameserver from resolv.conf, which is not the one asked when the
// control plane named upstreams (see Resolver), so it is reworded without it.
func dialProblem(err error) string {
	var de *net.DNSError
	if errors.As(err, &de) {
		switch {
		case de.IsNotFound:
			return de.Name + " does not resolve (the upstream DNS has no such name)"
		case de.IsTimeout:
			return "the upstream DNS did not answer for " + de.Name
		}
		return "looking up " + de.Name + ": " + de.Err
	}
	return err.Error()
}

// relayResponse streams the origin's answer back. For a host a surrogate goes
// to, the real value is written back to the stand-in in its headers and body
// (decoded first if the origin compressed it anyway). An upgrade (WebSocket)
// becomes a splice once the 101 is through.
func (p *Proxy) relayResponse(client net.Conn, br *bufio.Reader, origin net.Conn, req *http.Request, c *Client, host string, swapHost bool) (bool, int64, error) {
	obr := bufio.NewReader(origin)
	cw := &countWriter{w: client}
	resp, err := http.ReadResponse(obr, req)
	// An interim answer (103 Early Hints, a 100 the origin sent anyway) is
	// passed on and the real one read after it: taking it for the answer
	// would close the origin with the response still to come, and leave the
	// box waiting for it on a connection the gateway thinks is done.
	for err == nil && resp.StatusCode >= 100 && resp.StatusCode < 200 && resp.StatusCode != http.StatusSwitchingProtocols {
		if err := writeInterim(cw, resp); err != nil {
			return true, cw.n, err
		}
		resp, err = http.ReadResponse(obr, req)
	}
	if err != nil {
		log.Printf("egress: %s %s%s: no answer: %v", req.Method, host, req.URL.Path, err)
		writeStatusCode(client, http.StatusBadGateway, "the filter got no answer from "+host)
		return true, cw.n, nil
	}
	defer resp.Body.Close()
	// Alt-Svc offers HTTP/3 over UDP, which the gateway does not carry: a
	// browser that took it up would race QUIC against every request and mark
	// the origin broken.
	resp.Header.Del("Alt-Svc")
	g := p.Guard()
	b := g.State.bot(c)
	if swapHost {
		b.swapBackHeader(resp.Header)
	}

	if resp.StatusCode == http.StatusSwitchingProtocols {
		if err := resp.Write(cw); err != nil {
			return true, cw.n, err
		}
		var down io.Reader = obr
		if swapHost {
			down = newReplacer(obr, b.backSwaps())
		}
		spliceRW(br, client, down, origin)
		return true, cw.n, nil
	}

	if swapHost && hasBody(req, resp) {
		if enc := strings.TrimSpace(resp.Header.Get("Content-Encoding")); enc != "" && !strings.EqualFold(enc, "identity") {
			dec, err := decoder(enc, resp.Body)
			if err != nil {
				writeStatusCode(client, http.StatusBadGateway, "the filter cannot read this answer's encoding ("+enc+")")
				return true, 0, nil
			}
			resp.Body = io.NopCloser(dec)
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
			resp.ContentLength = -1
			if resp.ProtoAtLeast(1, 1) {
				resp.TransferEncoding = []string{"chunked"}
			} else {
				resp.Close = true
			}
		}
		resp.Body = io.NopCloser(newReplacer(resp.Body, b.backSwaps()))
	}

	head, pre, err := peekHead(resp.Body)
	if err != nil && err != io.EOF {
		writeStatusCode(client, http.StatusBadGateway, "the filter lost the answer from "+host)
		return true, 0, nil
	}
	if v := g.CheckResponse(c, host, head, resp.Header.Get("Content-Type")); v.Flag {
		p.reportVerdict(c, host, v, 0, 0)
	}
	resp.Body = io.NopCloser(pre)
	resp.Close = resp.Close || req.Close
	if err := resp.Write(cw); err != nil {
		return true, cw.n, err
	}
	return resp.Close, cw.n, nil
}

// writeInterim passes a 1xx answer on as it came: a status line and headers,
// never a body or a length (http.Response.Write would add one).
func writeInterim(w io.Writer, resp *http.Response) error {
	if _, err := fmt.Fprintf(w, "HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode)); err != nil {
		return err
	}
	if err := resp.Header.Write(w); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\r\n")
	return err
}

// hasBody reports whether an answer to req carries a body at all.
func hasBody(req *http.Request, resp *http.Response) bool {
	if req.Method == http.MethodHead {
		return false
	}
	s := resp.StatusCode
	return !(s >= 100 && s < 200 || s == http.StatusNoContent || s == http.StatusNotModified)
}

// decoder reads a body in the encoding the origin chose.
func decoder(enc string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(enc) {
	case "gzip", "x-gzip":
		return gzip.NewReader(r)
	case "deflate":
		return zlib.NewReader(r)
	case "br":
		return brotli.NewReader(r), nil
	case "zstd":
		d, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	}
	return nil, fmt.Errorf("unknown encoding %q", enc)
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

// countReader counts what went to the origin.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
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
	if g.State.passThrough(c, host) {
		if v := p.passVerdict(ctx, g, c, host, port); v.Block {
			writeBlock(client, v.Detail)
			return true, nil
		}
		up, err := p.dial(ctx, g, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			writeStatusCode(client, http.StatusBadGateway, "the filter could not reach "+host)
			return true, nil
		}
		defer up.Close()
		if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return true, err
		}
		splice(br, client, up)
		return true, nil
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return true, err
	}
	// A TLS hello follows for https and wss: decrypt both sides. A browser
	// also tunnels plain ws:// (and http:// on some setups) through CONNECT,
	// and that is served as the plain HTTP door does, the tunnel's port the
	// default for a Host header that names none.
	_ = client.SetReadDeadline(time.Now().Add(helloTimeout))
	first, err := br.Peek(1)
	_ = client.SetReadDeadline(time.Time{})
	if err != nil {
		return true, nil
	}
	if first[0] != recordHandshake {
		return true, p.serveHTTP(ctx, client, br, c, "", port, nil)
	}
	return true, p.mitm(ctx, client, br, nil, c, host, port)
}

// transparentTLS handles a raw TLS connection on 443: it reads the hello to
// learn the host, then splices or MITMs.
func (p *Proxy) transparentTLS(ctx context.Context, conn net.Conn, c *Client) error {
	_ = conn.SetReadDeadline(time.Now().Add(helloTimeout))
	raw, sni, err := readClientHello(conn)
	if err != nil {
		return err
	}
	host := NormHost(sni)
	if host == "" {
		return errors.New("no SNI in the hello")
	}
	g := p.Guard()
	if g.State.passThrough(c, host) {
		_ = conn.SetReadDeadline(time.Time{})
		if v := p.passVerdict(ctx, g, c, host, 443); v.Block {
			// No TLS to answer in: the box sees the connection end.
			return nil
		}
		up, err := p.dial(ctx, g, "tcp", net.JoinHostPort(host, "443"))
		if err != nil {
			return err
		}
		defer up.Close()
		splice(io.MultiReader(bytes.NewReader(raw), conn), conn, up)
		return nil
	}
	return p.mitm(ctx, conn, conn, raw, c, host, 443)
}

// mitm decrypts the client and serves its requests to host. src reads the
// client (a CONNECT's buffered reader, or the conn); raw is the hello when it
// was already read off it.
func (p *Proxy) mitm(ctx context.Context, client net.Conn, src io.Reader, raw []byte, c *Client, host string, port int) error {
	g := p.Guard()
	if g.State == nil || g.State.ca == nil {
		// The CA went away under this connection: splice, judged as one.
		if v := p.passVerdict(ctx, g, c, host, port); v.Block {
			return nil
		}
		up, err := p.dial(ctx, g, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		defer up.Close()
		splice(clientReader(src, raw), client, up)
		return nil
	}
	_ = client.SetReadDeadline(time.Now().Add(helloTimeout))
	if raw == nil {
		r, sni, err := readClientHello(src)
		if err != nil {
			return err
		}
		raw = r
		if hello := NormHost(sni); hello != "" {
			host = hello
		}
	}
	tlsConn := tls.Server(&prefixConn{Conn: client, r: clientReader(src, raw)}, serverTLSConfig(g.State.ca))
	hctx, cancel := context.WithTimeout(ctx, helloTimeout)
	err := tlsConn.HandshakeContext(hctx)
	cancel()
	if err != nil {
		// The box refused the gateway's certificate (a stale CA after a
		// rotation, a client with its own trust store): say so, because the
		// box only sees a broken connection. A connection closed before the
		// handshake ends is a browser dropping a socket it opened ahead of
		// need, which is nothing to report.
		if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
			log.Printf("egress: tls from a client for %s: %v", host, err)
		}
		return err
	}
	_ = client.SetReadDeadline(time.Time{})
	return p.serveHTTP(ctx, tlsConn, bufio.NewReader(tlsConn), c, host, port, raw)
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

// clientReader replays raw (an already-read hello) then reads from src.
func clientReader(src io.Reader, raw []byte) io.Reader {
	if raw == nil {
		return src
	}
	return io.MultiReader(bytes.NewReader(raw), src)
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

// peekHead reads the start of a body, enough to know a program by its magic,
// and returns it with a reader that replays it before the rest. It never waits
// for more than the first few bytes: a stream of small events goes on as each
// arrives.
func peekHead(r io.Reader) ([]byte, io.Reader, error) {
	buf := make([]byte, headPeek)
	n := 0
	var err error
	for n < 4 && err == nil {
		var m int
		m, err = r.Read(buf[n:])
		n += m
	}
	head := buf[:n]
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
func splice(src io.Reader, client net.Conn, up net.Conn) { spliceRW(src, client, up, up) }

// spliceRW is splice with each side's reader given: what a buffered reader
// already holds must not be lost.
func spliceRW(clientR io.Reader, client net.Conn, upR io.Reader, up net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, clientR); closeWrite(up); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upR); closeWrite(client); done <- struct{}{} }()
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
