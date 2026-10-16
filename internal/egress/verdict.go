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
	if addr, ok := g.privateDest(ctx, host); ok {
		return Verdict{Block: true, Reason: "private", Detail: "resolves to internal address " + addr.String()}
	}

	// A surrogate headed anywhere but its hosts is a leak: the real value
	// would have gone out, so the request is blocked and the owner told.
	if c != nil {
		if _, leaked := g.State.bot(c).swapOut(req.Payload, host); leaked != "" {
			return Verdict{Block: true, Reason: "surrogate_leak", Detail: "secret " + leaked + " tried to leave to " + host}
		}
	}

	// Everything below is a flag, and trust silences it. A block already won.
	if g.State.trusted(c, host) {
		return Allow
	}

	if p, ok := g.sinks.match(host, req.Path); ok {
		return Verdict{Flag: true, Reason: "exfil_sink", Detail: "a place anything can post to: " + p}
	}
	if name, ok := findSecretShape(req.Payload); ok {
		return Verdict{Flag: true, Reason: "secret_shape", Detail: "a " + name + " in the request"}
	}
	if c != nil && g.State.bot(c).plainSecret(req.Payload) {
		return Verdict{Flag: true, Reason: "plain_secret", Detail: "one of the Bot's own secrets in the request"}
	}
	if randomDomain(host) {
		return Verdict{Flag: true, Reason: "random_domain", Detail: "a machine-made-looking name: " + host}
	}
	if q := queryOf(req.Path); encodedQuery(q, g.State.queryMin) {
		return Verdict{Flag: true, Reason: "query_entropy", Detail: fmt.Sprintf("a long, random-looking query (%d bytes)", len(q))}
	}
	if g.Rep != nil && g.Rep.Upload(c, host, int64(len(req.Payload)), g.State.budget) {
		return Verdict{Flag: true, Reason: "upload_budget", Detail: "past this hour's upload budget to " + host}
	}
	if g.Rep != nil && g.Rep.Beacon(c, host) {
		return Verdict{Flag: true, Reason: "beaconing", Detail: "calling " + host + " on a steady clock"}
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

// privateDest reports the offending address when host (or what it resolves to)
// is inside, loopback or metadata: an address the box must never reach.
func (g *Guard) privateDest(ctx context.Context, host string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(host); err == nil {
		a = a.Unmap()
		return a, internalAddr(a)
	}
	if g.Resolve == nil {
		return netip.Addr{}, false
	}
	// The lookup is bounded: a name that will not resolve is not a block, the
	// dial will say so.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := g.Resolve(ctx, host)
	if err != nil {
		return netip.Addr{}, false
	}
	for _, a := range addrs {
		a = a.Unmap()
		if internalAddr(a) {
			return a, true
		}
	}
	return netip.Addr{}, false
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
