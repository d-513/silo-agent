package egress

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// Default listener addresses inside the gateway container.
const (
	DefaultDNSAddr   = ":53"
	DefaultProxyAddr = ":8888"
	DefaultHTTPAddr  = ":80"
	DefaultTLSAddr   = ":443"
	DefaultSOCKSAddr = ":1080"
	// DefaultCPLinkAddr is where the gateway forwards a worker's or a bridge's
	// connection to the control plane. The CP tells the client to dial the
	// gateway on this port.
	DefaultCPLinkAddr = ":8443"
	// maxConnsPerClient is how many connections one client may hold through
	// the gateway at once, over every door. A browser keeps a few dozen; a
	// box past this is flooding the one gateway every Bot shares.
	maxConnsPerClient = 512
	// addrsTTL is how long the gateway's own interface addresses are kept
	// before they are asked again (each DNS answer needs one).
	addrsTTL = 5 * time.Second
)

// Config is the gateway's listener setup. Empty fields take the defaults.
type Config struct {
	DNSAddr    string
	ProxyAddr  string
	HTTPAddr   string
	TLSAddr    string
	SOCKSAddr  string
	CPLinkAddr string
}

func (c Config) withDefaults() Config {
	if c.DNSAddr == "" {
		c.DNSAddr = DefaultDNSAddr
	}
	if c.ProxyAddr == "" {
		c.ProxyAddr = DefaultProxyAddr
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = DefaultHTTPAddr
	}
	if c.TLSAddr == "" {
		c.TLSAddr = DefaultTLSAddr
	}
	if c.SOCKSAddr == "" {
		c.SOCKSAddr = DefaultSOCKSAddr
	}
	if c.CPLinkAddr == "" {
		c.CPLinkAddr = DefaultCPLinkAddr
	}
	return c
}

// Gateway is the running filter: the listeners, the applied state and the
// threat feeds and reports that outlive one apply.
type Gateway struct {
	cfg   Config
	feeds *Feeds
	rep   *Reporter

	state atomic.Pointer[State]
	guard atomic.Pointer[Guard]

	dns   *DNS
	proxy *Proxy
	socks *SOCKS
	link  *CPLink

	connMu sync.Mutex
	conns  map[string]int

	addrMu  sync.Mutex
	addrs   []netip.Addr
	addrsAt time.Time
}

// New builds a gateway with empty state: everything passes until an apply.
func New(cfg Config) *Gateway {
	gw := &Gateway{cfg: cfg.withDefaults(), feeds: NewFeeds(nil), rep: NewReporter(nil), conns: map[string]int{}}
	gw.guard.Store(NewGuard(nil, gw.feeds, gw.rep))
	gw.install()
	return gw
}

// install wires the listeners' seams to the gateway. It is idempotent.
func (gw *Gateway) install() {
	// DNS over UDP knows only the source (rp_filter holds that to its own
	// network); every TCP door reads both ends of the connection.
	gw.dns = &DNS{Addr: gw.ownAddr, Client: gw.Client, Guard: gw.Guard, report: gw.reportVerdict}
	gw.proxy = &Proxy{Client: gw.ClientOf, Guard: gw.Guard, Admit: gw.admit, report: gw.report, Traffic: gw.traffic}
	gw.socks = &SOCKS{Client: gw.ClientOf, Guard: gw.Guard, Admit: gw.admit, report: gw.reportVerdict}
	gw.link = &CPLink{Target: gw.cpAddr, Client: gw.ClientOf}
}

// Client finds who a source address belongs to.
func (gw *Gateway) Client(remote netip.Addr) *Client { return gw.state.Load().Client(remote) }

// ClientOf finds who a connection belongs to from both its ends.
func (gw *Gateway) ClientOf(remote, local netip.Addr) *Client {
	return gw.state.Load().ClientOf(remote, local)
}

// admit takes one of c's connection slots.
func (gw *Gateway) admit(c *Client) (func(), bool) {
	k := clientKey(c)
	gw.connMu.Lock()
	defer gw.connMu.Unlock()
	if gw.conns[k] >= maxConnsPerClient {
		return nil, false
	}
	gw.conns[k]++
	var once sync.Once
	return func() {
		once.Do(func() {
			gw.connMu.Lock()
			defer gw.connMu.Unlock()
			if gw.conns[k]--; gw.conns[k] <= 0 {
				delete(gw.conns, k)
			}
		})
	}, true
}

// Guard returns the state a connection reads: one whole apply.
func (gw *Gateway) Guard() *Guard { return gw.guard.Load() }

// Apply replaces the whole state. A CA that will not load is not an error the
// caller must act on: the gateway simply never decrypts, and every TLS
// connection passes through on SNI.
func (gw *Gateway) Apply(a *v1.EgressApply) {
	s, _ := NewState(a)
	gw.state.Store(s)
	gw.feeds.SetSpecs(a.GetFeeds())
	gw.guard.Store(NewGuard(s, gw.feeds, gw.rep))
}

// cpAddr is where a client's control-plane link is forwarded.
func (gw *Gateway) cpAddr() string {
	if s := gw.state.Load(); s != nil {
		return s.cpAddr
	}
	return ""
}

// ownAddr is the gateway's address on the client's own network, the answer it
// hands that client's DNS.
func (gw *Gateway) ownAddr(c *Client) (netip.Addr, bool) {
	if c == nil {
		return netip.Addr{}, false
	}
	addrs := gw.interfaceAddrs(false)
	for _, a := range addrs {
		if c.Net.Contains(a) {
			return a, true
		}
	}
	// A network joined a moment ago: look again rather than wait out the TTL.
	for _, a := range gw.interfaceAddrs(true) {
		if c.Net.Contains(a) {
			return a, true
		}
	}
	return netip.Addr{}, false
}

// interfaceAddrs are the gateway's own addresses, kept for addrsTTL (fresh
// asks again now, at most once a second).
func (gw *Gateway) interfaceAddrs(fresh bool) []netip.Addr {
	gw.addrMu.Lock()
	defer gw.addrMu.Unlock()
	age := time.Since(gw.addrsAt)
	if age < addrsTTL && !(fresh && age > time.Second) {
		return gw.addrs
	}
	ifaddrs, err := net.InterfaceAddrs()
	gw.addrsAt = time.Now()
	if err != nil {
		return gw.addrs
	}
	out := make([]netip.Addr, 0, len(ifaddrs))
	for _, a := range ifaddrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ap, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, ap.Unmap())
		}
	}
	gw.addrs = out
	return out
}

// report records a block or a flag as an event, quieted by the reporter.
func (gw *Gateway) report(c *Client, host string, v Verdict, up, down int64) {
	if !v.Block && !v.Flag {
		return
	}
	verdict := "flag"
	if v.Block {
		verdict = "block"
	}
	gw.rep.Event(c, &v1.EgressEvent{
		Verdict:   verdict,
		Reason:    v.Reason,
		Detail:    v.Detail,
		Host:      host,
		BytesUp:   up,
		BytesDown: down,
	})
}

// reportVerdict is the DNS and SOCKS shape: no byte counts.
func (gw *Gateway) reportVerdict(c *Client, host string, v Verdict) { gw.report(c, host, v, 0, 0) }

// traffic sums every request into the per-minute rows.
func (gw *Gateway) traffic(c *Client, host string, up, down int64, verdict string) {
	gw.rep.Traffic(c, host, up, down, verdict)
}

// Drain takes the events and traffic rows gathered since the last call, for
// the session to send.
func (gw *Gateway) Drain() ([]*v1.EgressEvent, []*v1.EgressTrafficRow) { return gw.rep.Drain() }

// Requeue puts back what a failed send took.
func (gw *Gateway) Requeue(events []*v1.EgressEvent, rows []*v1.EgressTrafficRow) {
	gw.rep.Requeue(events, rows)
}

// Health reports the feeds and how many client networks the gateway is on.
func (gw *Gateway) Health() *v1.EgressHealth {
	nets := 0
	if s := gw.state.Load(); s != nil {
		for _, c := range s.clients {
			if _, ok := gw.ownAddr(c); ok {
				nets++
			}
		}
	}
	return &v1.EgressHealth{Feeds: gw.feeds.States(), Networks: int32(nets)}
}

// Run starts every listener and the feed refresher, and returns when ctx ends
// or a listener fails.
func (gw *Gateway) Run(ctx context.Context) error {
	gw.install()
	errCh := make(chan error, 8)
	start := func(fn func() error) { go func() { errCh <- fn() }() }

	udp, err := net.ListenPacket("udp", gw.cfg.DNSAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.dns.ServeUDP(ctx, udp) })

	tcp, err := net.Listen("tcp", gw.cfg.DNSAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.dns.ServeTCP(ctx, tcp) })

	proxyLn, err := net.Listen("tcp", gw.cfg.ProxyAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.proxy.Serve(ctx, proxyLn, ModeProxy) })

	httpLn, err := net.Listen("tcp", gw.cfg.HTTPAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.proxy.Serve(ctx, httpLn, ModeHTTP) })

	tlsLn, err := net.Listen("tcp", gw.cfg.TLSAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.proxy.Serve(ctx, tlsLn, ModeTLS) })

	socksLn, err := net.Listen("tcp", gw.cfg.SOCKSAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.socks.Serve(ctx, socksLn) })

	linkLn, err := net.Listen("tcp", gw.cfg.CPLinkAddr)
	if err != nil {
		return err
	}
	start(func() error { return gw.link.Serve(ctx, linkLn) })

	start(func() error { return gw.refreshFeeds(ctx) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// refreshFeeds downloads the due threat lists on a slow clock.
func (gw *Gateway) refreshFeeds(ctx context.Context) error {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	gw.feeds.Refresh(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			gw.feeds.Refresh(ctx, time.Now())
		}
	}
}
