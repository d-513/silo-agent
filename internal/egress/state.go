package egress

import (
	"encoding/base64"
	"encoding/hex"
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
			if len(sg.GetSurrogate()) < 8 || sg.GetReal() == "" {
				continue
			}
			bs.surrogates = append(bs.surrogates, surrogate{name: sg.GetName(), surrogate: sg.GetSurrogate(), real: sg.GetReal(), hosts: newHostSet(sg.GetHosts())})
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

// Client finds who a connection that arrived on local belongs to.
func (s *State) Client(local netip.Addr) *Client {
	if s == nil {
		return nil
	}
	local = local.Unmap()
	for _, c := range s.clients {
		if c.Net.Contains(local) {
			return c
		}
	}
	return nil
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

// swapOut writes the real value over every surrogate in s that may go to
// host. A surrogate headed anywhere else is a leak: its name comes back.
func (b *botState) swapOut(s, host string) (out, leaked string) {
	out = s
	for _, sg := range b.surrogates {
		if !strings.Contains(out, sg.surrogate) {
			continue
		}
		if _, ok := sg.hosts.match(host); !ok {
			return s, sg.name
		}
		out = strings.ReplaceAll(out, sg.surrogate, sg.real)
	}
	return out, ""
}

// swapBack writes each surrogate over its real value, so an answer that
// echoes a key never teaches the box what it is.
func (b *botState) swapBack(s string) string {
	for _, sg := range b.surrogates {
		if strings.Contains(s, sg.real) {
			s = strings.ReplaceAll(s, sg.real, sg.surrogate)
		}
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
