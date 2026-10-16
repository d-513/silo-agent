package egress

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	v1 "silo.agent/gen/silo/v1"
)

const (
	// DefaultUploadBudget is how much one client may send one host in an hour
	// before it is flagged.
	DefaultUploadBudget = 50 << 20
	// DefaultQueryMin is the length past which a random-looking query string
	// is flagged.
	DefaultQueryMin = 1024
)

// Client is one container on its own network.
type Client struct {
	Net         netip.Prefix
	Kind        string
	BotID       string
	ConnectorID string
	Name        string
}

// surrogate is one secret the box knows only by a stand-in.
type surrogate struct {
	name      string
	surrogate string
	real      string
	hosts     hostSet
	// body: the owner allowed the real value in request bodies too.
	body bool
}

type botState struct {
	block, trust, pass hostSet
	surrogates         []surrogate
	// plain holds the Bot's plain secret values and their usual encodings.
	plain          []string
	otherProtocols bool
}

// State is what the gateway knows, replaced whole on every apply.
type State struct {
	clients  []*Client
	bots     map[string]*botState
	block    hostSet
	trust    hostSet
	pass     hostSet
	ca       *CA
	cpAddr   string
	budget   int64
	queryMin int
}

// NewState builds the gateway's state from an apply. A CA that does not
// load leaves decryption off: every TLS connection is passed through.
func NewState(a *v1.EgressApply) (*State, error) {
	s := &State{
		bots:     map[string]*botState{},
		block:    newHostSet(a.GetOperator().GetBlock()),
		trust:    newHostSet(a.GetOperator().GetTrust()),
		pass:     newHostSet(a.GetOperator().GetPass()),
		cpAddr:   a.GetCpAddr(),
		budget:   a.GetLimits().GetUploadBudget(),
		queryMin: int(a.GetLimits().GetQueryMin()),
	}
	if s.budget <= 0 {
		s.budget = DefaultUploadBudget
	}
	if s.queryMin <= 0 {
		s.queryMin = DefaultQueryMin
	}
	for _, c := range a.GetClients() {
		p, err := netip.ParsePrefix(c.GetSubnet())
		if err != nil {
			continue
		}
		s.clients = append(s.clients, &Client{Net: p.Masked(), Kind: c.GetKind(), BotID: c.GetBotId(), ConnectorID: c.GetConnectorId(), Name: c.GetName()})
	}
	for _, b := range a.GetBots() {
		bs := &botState{
			block:          newHostSet(b.GetLists().GetBlock()),
			trust:          newHostSet(b.GetLists().GetTrust()),
			pass:           newHostSet(b.GetLists().GetPass()),
			otherProtocols: b.GetOtherProtocols(),
		}
		for _, sg := range b.GetSurrogates() {
			// A stand-in is the real value's length (NewSurrogate keeps it), so
			// a body swapped as it streams keeps its Content-Length. One that is
			// not, or that is the value itself, is no stand-in.
			if len(sg.GetSurrogate()) < MinSurrogate || len(sg.GetSurrogate()) != len(sg.GetReal()) || sg.GetSurrogate() == sg.GetReal() {
				continue
			}
			bs.surrogates = append(bs.surrogates, surrogate{name: sg.GetName(), surrogate: sg.GetSurrogate(), real: sg.GetReal(), hosts: newHostSet(sg.GetHosts()), body: sg.GetBody()})
		}
		for _, v := range b.GetPlainSecrets() {
			bs.plain = append(bs.plain, variants(v)...)
		}
		s.bots[b.GetBotId()] = bs
	}
	if len(a.GetCaCert()) > 0 && len(a.GetCaKey()) > 0 {
		ca, err := LoadCA(a.GetCaCert(), a.GetCaKey())
		if err != nil {
			return s, err
		}
		s.ca = ca
	}
	return s, nil
}

// variants are the forms a value usually travels in.
func variants(v string) []string {
	if len(v) < 8 {
		return nil
	}
	out := []string{v, base64.StdEncoding.EncodeToString([]byte(v)), base64.RawURLEncoding.EncodeToString([]byte(v)), hex.EncodeToString([]byte(v))}
	if q := url.QueryEscape(v); q != v {
		out = append(out, q)
	}
	return out
}

// Client finds who a source address belongs to.
func (s *State) Client(remote netip.Addr) *Client {
	if s == nil {
		return nil
	}
	remote = remote.Unmap()
	for _, c := range s.clients {
		if c.Net.Contains(remote) {
			return c
		}
	}
	return nil
}

// ClientOf finds who a connection belongs to from both its ends: the source
// must be on a client's network and the connection must have arrived at the
// gateway's own address on that same network. A box whose root gives itself
// another client's address, or aims at the gateway's address on another
// network, is nobody.
func (s *State) ClientOf(remote, local netip.Addr) *Client {
	c := s.Client(remote)
	if c == nil || !c.Net.Contains(local.Unmap()) {
		return nil
	}
	return c
}

func (s *State) bot(c *Client) *botState {
	if s == nil || c == nil {
		return &botState{}
	}
	b := s.bots[c.BotID]
	if b == nil {
		return &botState{}
	}
	if c.Kind == "stdio" {
		// A STDIO sidecar is given the real values (a connector's
		// ${secrets.NAME} resolves to them), never stand-ins: swapping would
		// only corrupt what it holds, and watching for its own secrets would
		// raise an alert on every call it is there to make. It keeps the Bot's
		// lists and switch.
		return &botState{block: b.block, trust: b.trust, pass: b.pass, otherProtocols: b.otherProtocols}
	}
	return b
}

// blocked reports whether host is on a block list, the operator's first.
func (s *State) blocked(c *Client, host string) (string, bool) {
	if p, ok := s.block.match(host); ok {
		return "operator list: " + p, true
	}
	if p, ok := s.bot(c).block.match(host); ok {
		return "Bot list: " + p, true
	}
	return "", false
}

// trusted reports whether flags are silenced for host.
func (s *State) trusted(c *Client, host string) bool {
	if _, ok := s.trust.match(host); ok {
		return true
	}
	_, ok := s.bot(c).trust.match(host)
	return ok
}

// passThrough reports whether TLS to host is spliced, never decrypted.
func (s *State) passThrough(c *Client, host string) bool {
	if s == nil || s.ca == nil {
		return true
	}
	if _, ok := s.pass.match(host); ok {
		return true
	}
	_, ok := s.bot(c).pass.match(host)
	return ok
}

// OtherProtocols reports whether the client's Bot may use SOCKS for
// protocols that are not HTTP.
func (s *State) OtherProtocols(c *Client) bool { return s.bot(c).otherProtocols }

// leak reports a surrogate in s that must not go to host: one whose hosts do
// not cover it (insecure false), or one that would go to its own host over
// plain HTTP (insecure true), where the real value would cross the internet
// readable by anyone on the way. Either way the stand-in is not swapped and
// the request is refused.
func (b *botState) leak(s, host string, secure bool) (name string, insecure bool) {
	for _, sg := range b.surrogates {
		if !strings.Contains(s, sg.surrogate) {
			continue
		}
		if _, ok := sg.hosts.match(host); !ok {
			return sg.name, false
		}
		if !secure {
			return sg.name, true
		}
	}
	return "", false
}

// swapHeaders writes the real value over every surrogate in the request's
// headers that may go to host. Headers are where a credential belongs, and
// the one place it is swapped by default. The verdict has already refused a
// request carrying any other.
func (b *botState) swapHeaders(h http.Header, host string) {
	for k, vs := range h {
		for i, v := range vs {
			for _, sg := range b.surrogates {
				if _, ok := sg.hosts.match(host); ok && strings.Contains(v, sg.surrogate) {
					v = strings.ReplaceAll(v, sg.surrogate, sg.real)
				}
			}
			vs[i] = v
		}
		h[k] = vs
	}
}

// bodySwaps is how a request body to host is rewritten on its way out: a
// surrogate whose owner allowed bodies becomes the real value, one that may
// not go to host at all (or not unencrypted) ends the upload, and one that may
// go there but not in a body goes out as the stand-in, which is worthless
// outside the box.
func (b *botState) bodySwaps(host string, secure bool) []swap {
	var out []swap
	for _, sg := range b.surrogates {
		_, ok := sg.hosts.match(host)
		switch {
		case !ok || !secure:
			out = append(out, swap{old: sg.surrogate, name: sg.name, forbid: true})
		case sg.body:
			out = append(out, swap{old: sg.surrogate, new: sg.real, name: sg.name})
		}
	}
	return out
}

// backSwaps writes each surrogate over its real value in an answer, so one
// that echoes a key never teaches the box what it is.
func (b *botState) backSwaps() []swap {
	out := make([]swap, 0, len(b.surrogates))
	for _, sg := range b.surrogates {
		out = append(out, swap{old: sg.real, new: sg.surrogate, name: sg.name})
	}
	return out
}

// allowedTo reports whether the named surrogate may go to host.
func (b *botState) allowedTo(name, host string) bool {
	for _, sg := range b.surrogates {
		if sg.name == name {
			_, ok := sg.hosts.match(host)
			return ok
		}
	}
	return false
}

// swapBackHeader writes each surrogate over its real value in an answer's
// headers.
func (b *botState) swapBackHeader(h http.Header) {
	for _, vs := range h {
		for i, v := range vs {
			vs[i] = b.swapBack(v)
		}
	}
}

// swapBack writes each surrogate over its real value in s.
func (b *botState) swapBack(s string) string {
	for _, sg := range b.surrogates {
		if strings.Contains(s, sg.real) {
			s = strings.ReplaceAll(s, sg.real, sg.surrogate)
		}
	}
	return s
}

// withoutSurrogates is s with the Bot's stand-ins cut out, for the shape
// checks: a stand-in is made to look like the token it stands for, and one on
// its way to its own host is expected there, not a credential leaving.
func (b *botState) withoutSurrogates(s string) string {
	for _, sg := range b.surrogates {
		s = strings.ReplaceAll(s, sg.surrogate, "")
	}
	return s
}

// surrogateHost reports whether any surrogate may go to host, so its
// answers need reading for the real value.
func (b *botState) surrogateHost(host string) bool {
	for _, sg := range b.surrogates {
		if _, ok := sg.hosts.match(host); ok {
			return true
		}
	}
	return false
}

// plainSecret reports whether s carries one of the Bot's plain secrets.
func (b *botState) plainSecret(s string) bool {
	for _, v := range b.plain {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
