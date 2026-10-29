package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"silo.agent/internal/netguard"
)

// Request is one flow the filter is asked about. Payload is the text it reads
// for the shape checks: the request line, headers and (a capped prefix of) the
// body, already decoded where the proxy could. It is never the whole stream.
type Request struct {
	Client  *Client
	Host    string
	Port    int
	Proto   string // http | https | tcp
	Method  string
	Path    string // path and query, for the sink check
	Payload string
	// Size is how many bytes the request sends, for the upload budget; 0
	// counts the payload.
	Size int64
	// Secure is set when what is sent travels encrypted to a verified origin:
	// only then may a surrogate become its real value.
	Secure bool
}

// Verdict is the filter's answer. Block carries human words the Box sees;
// Flag is silent (the owner hears it in the audit).
type Verdict struct {
	Block  bool
	Flag   bool
	Reason string
	Detail string
}

// Allow is the zero verdict: everything that is not flagged or blocked.
var Allow = Verdict{}

// Guard decides what happens to one flow. It is rebuilt whole on every apply,
// so a connection always reads one consistent set of lists.
type Guard struct {
	State *State
	Feeds *Feeds
	Rep   *Reporter
	// Resolve turns a host into the addresses the gateway would dial; it is
	// where the private-address check bites.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	// Now is a seam for tests.
	Now   func() time.Time
	sinks sinkSet
}

// NewGuard wires a guard over an applied state. A nil resolver uses the
// system's.
func NewGuard(s *State, f *Feeds, r *Reporter) *Guard {
	return &Guard{
		State:   s,
		Feeds:   f,
		Rep:     r,
		Resolve: systemResolve,
		Now:     time.Now,
		sinks:   defaultSinks,
	}
}

func systemResolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{a.Unmap()}, nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Unmap())
	}
	return out, nil
}

// Check judges one request: what is clearly bad is blocked, what looks like
// data leaving is flagged, the rest passes. It never asks anybody and always
// answers.
func (g *Guard) Check(ctx context.Context, req Request) Verdict {
	if g == nil || g.State == nil {
		return Allow
	}
	host := NormHost(req.Host)
	if host == "" {
		// Nothing to judge is nothing to let through: an empty host dials
		// the gateway's own loopback.
		return Verdict{Block: true, Reason: "blocked_host", Detail: "no host to reach"}
	}
	c := req.Client

	// Blocked outright: the lists first, then the threat feeds, then an
	// address that would reach inside the operator's network.
	if why, ok := g.State.blocked(c, host); ok {
		return Verdict{Block: true, Reason: "blocked_host", Detail: why}
	}
	if g.Feeds != nil {
		if id, ok := g.Feeds.Host(host); ok {
			return Verdict{Block: true, Reason: "threat_list", Detail: "on threat list " + id}
		}
	}
	if v, ok := g.checkAddrs(ctx, c, host); ok {
		return v
	}
	// A web request to a port browsers refuse (mail, ssh, databases) is a way
	// to speak another protocol through the HTTP door; that is what the
	// Other-protocols switch is for.
	if (req.Proto == "http" || req.Proto == "https") && badPort(req.Port) && !g.State.OtherProtocols(c) {
		return Verdict{Block: true, Reason: "other_protocols",
			Detail: fmt.Sprintf("port %d is not for web traffic; the Bot's owner can turn on Other protocols", req.Port)}
	}

	// A surrogate headed anywhere but its hosts is a leak: the real value
	// would have gone out, so the request is blocked and the owner told. One
	// headed to its own host over plain HTTP is refused too: the real value
	// would cross the internet readable.
	if c != nil {
		if leaked, insecure := g.State.bot(c).leak(req.Payload, host, req.Secure); leaked != "" {
			if insecure {
				return Verdict{Block: true, Reason: "surrogate_insecure", Detail: "secret " + leaked + " is only sent over HTTPS"}
			}
			return Verdict{Block: true, Reason: "surrogate_leak", Detail: "secret " + leaked + " tried to leave to " + host}
		}
	}

	// Everything below is a flag, and trust silences it. A block already won.
	if g.State.trusted(c, host) {
		return Allow
	}

	payload := req.Payload
	if c != nil {
		payload = g.State.bot(c).withoutSurrogates(payload)
	}
	if p, ok := g.sinks.match(host, req.Path); ok {
		return Verdict{Flag: true, Reason: "exfil_sink", Detail: "a place anything can post to: " + p}
	}
	if name, ok := findSecretShape(payload, host); ok {
		return Verdict{Flag: true, Reason: "secret_shape", Detail: "a " + name + " in the request"}
	}
	if c != nil && g.State.bot(c).plainSecret(payload) {
		return Verdict{Flag: true, Reason: "plain_secret", Detail: "one of the Bot's own secrets in the request"}
	}
	if randomDomain(host) {
		return Verdict{Flag: true, Reason: "random_domain", Detail: "a machine-made-looking name: " + host}
	}
	if q := queryOf(req.Path); encodedQuery(q, g.State.queryMin) {
		return Verdict{Flag: true, Reason: "query_entropy", Detail: fmt.Sprintf("a long, random-looking query (%d bytes)", len(q))}
	}
	size := req.Size
	if size <= 0 {
		size = int64(len(req.Payload))
	}
	if v := g.Uploaded(c, host, size); v.Flag {
		return v
	}
	if g.Rep != nil && g.Rep.Beacon(c, host) {
		return Verdict{Flag: true, Reason: "beaconing", Detail: "calling " + host + " on a steady clock"}
	}
	return Allow
}

// Uploaded counts n more bytes sent to host and flags the send that takes the
// hour past the budget. The proxy calls it for what a streamed body carried
// past the size the verdict was told.
func (g *Guard) Uploaded(c *Client, host string, n int64) Verdict {
	if g == nil || g.State == nil || g.Rep == nil || n <= 0 || g.State.trusted(c, host) {
		return Allow
	}
	if g.Rep.Upload(c, host, n, g.State.budget) {
		return Verdict{Flag: true, Reason: "upload_budget", Detail: "past this hour's upload budget to " + host}
	}
	return Allow
}

// CheckResponse judges a body coming back. Only downloads stand out; a flag
// here is silent like any other.
func (g *Guard) CheckResponse(c *Client, host string, head []byte, contentType string) Verdict {
	if g == nil || g.State == nil {
		return Allow
	}
	host = NormHost(host)
	if g.State.trusted(c, host) {
		return Allow
	}
	if kind, ok := executable(head, contentType); ok {
		return Verdict{Flag: true, Reason: "executable", Detail: "a " + kind + " downloaded from " + host}
	}
	return Allow
}

// checkAddrs blocks a host that is, or resolves to, an address the box must
// never reach (inside, loopback, metadata), one a block list covers, or one a
// threat list names: the
// address lists (Spamhaus DROP, compromised IPs) are of servers, and a name is
// how a box reaches one. The dial checks the address again (Guard.Dial), so a
// name that answers differently the second time gains nothing.
func (g *Guard) checkAddrs(ctx context.Context, c *Client, host string) (Verdict, bool) {
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else if g.Resolve != nil {
		// The lookup is bounded: a name that will not resolve is not a block,
		// the dial will say so.
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		addrs, _ = g.Resolve(ctx, host)
	}
	for _, a := range addrs {
		a = a.Unmap()
		if internalAddr(a) {
			return Verdict{Block: true, Reason: "private", Detail: "resolves to internal address " + a.String()}, true
		}
		// A block list's address or CIDR covers a name that resolves there.
		if why, ok := g.State.blocked(c, a.String()); ok {
			return Verdict{Block: true, Reason: "blocked_host", Detail: host + " resolves into " + why}, true
		}
		if g.Feeds != nil {
			if id, ok := g.Feeds.Addr(a); ok {
				return Verdict{Block: true, Reason: "threat_list", Detail: "address " + a.String() + " is on threat list " + id}, true
			}
		}
	}
	return Verdict{}, false
}

// internalAddr reports whether a is loopback, private, link-local, multicast,
// unspecified, metadata (169.254.169.254, and 100.100.100.200 in carrier-grade
// NAT space) or reserved: never a place a Bot's traffic should reach. It is
// netguard's list, so an IPv4 address carried in IPv6 (mapped, NAT64, 6to4) is
// judged by the one inside, the same as the dial guard judges it.
func internalAddr(a netip.Addr) bool {
	return a.IsValid() && netguard.Internal(a)
}

// queryOf returns the query string of a path, without the "?".
func queryOf(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[i+1:]
	}
	return ""
}

// badPorts are the ports the Fetch standard keeps browsers off: services that
// read a web request's bytes as their own protocol (mail, ssh, IRC, ...).
var badPorts = map[int]bool{
	0: true, 1: true, 7: true, 9: true, 11: true, 13: true, 15: true, 17: true, 19: true, 20: true,
	21: true, 22: true, 23: true, 25: true, 37: true, 42: true, 43: true, 53: true, 69: true, 77: true,
	79: true, 87: true, 95: true, 101: true, 102: true, 103: true, 104: true, 109: true, 110: true,
	111: true, 113: true, 115: true, 117: true, 119: true, 123: true, 135: true, 137: true, 139: true,
	143: true, 161: true, 179: true, 389: true, 427: true, 465: true, 512: true, 513: true, 514: true,
	515: true, 526: true, 530: true, 531: true, 532: true, 540: true, 548: true, 554: true, 556: true,
	563: true, 587: true, 601: true, 636: true, 989: true, 990: true, 993: true, 995: true, 1719: true,
	1720: true, 1723: true, 2049: true, 3659: true, 4045: true, 4190: true, 5060: true, 5061: true,
	6000: true, 6566: true, 6665: true, 6666: true, 6667: true, 6668: true, 6669: true, 6679: true,
	6697: true, 10080: true,
}

func badPort(port int) bool { return badPorts[port] }
