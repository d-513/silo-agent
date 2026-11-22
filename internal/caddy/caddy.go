// Package caddy configures a Caddy server (silo-caddy, caddyimage/) in front
// of the control plane through Caddy's admin API (caddy.admin): HTTPS for the
// control plane's own name and its content host, from Let's Encrypt (over
// HTTP, or through a DNS provider, DNSProviders) or from Caddy's own
// authority, and for a SiloOS machine's local names and addresses always
// from the latter. Over HTTP Let's Encrypt has no wildcards, so content names
// then get a certificate each on demand, which Caddy asks the control plane
// about first (AskPath).
//
// The control plane is where this configuration lives: Caddy keeps what it
// was last given (`caddy run --resume`), and the control plane compares and
// pushes the whole of it again whenever it differs (Admin.Sync), so a Caddy
// that restarted, or that someone changed by hand, is put right.
package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// AskPath is the control plane's endpoint Caddy asks before it gets a
// certificate on demand (?domain=<name>; 200 means yes).
const AskPath = "/caddy/ask"

// Issuers: where a certificate comes from.
const (
	// IssuerACME is Let's Encrypt.
	IssuerACME = "acme"
	// IssuerInternal is Caddy's own certificate authority: browsers warn
	// until its root is trusted (Admin.RootCA hands it out), but it certifies
	// any name, IP addresses and names on the local network included.
	IssuerInternal = "internal"
)

// DNS is a DNS provider Caddy answers the ACME DNS challenge through: its
// module id and its settings, as the libdns provider names them.
type DNS struct {
	Provider string
	Fields   map[string]string
}

// Site is what Caddy serves.
type Site struct {
	// Host is the control plane's own name (public_url's), certified by
	// Issuer. Empty serves none.
	Host   string
	Issuer string
	// Email is the ACME account's address; empty is none.
	Email string
	// DNS, set, answers Let's Encrypt through the DNS provider instead of on
	// port 80: the machine needs no inbound connection, and the content
	// host gets one wildcard certificate.
	DNS *DNS
	// ContentHost is the content suffix: *.<ContentHost> is served under
	// Host's issuer. Empty serves none.
	ContentHost string
	// Local are the machine's own names and addresses (a SiloOS machine's
	// host name and IPs), always certified by Caddy's own authority, so the
	// console is HTTPS on the local network before it has a domain.
	Local []string
	// Upstream is the control plane's address from Caddy (127.0.0.1:8080).
	Upstream string
	// Ask is the URL of AskPath on the control plane, from Caddy.
	Ask string
}

// Empty reports whether the site serves nothing.
func (s Site) Empty() bool { return s.Host == "" && len(s.Local) == 0 }

// OnDemand reports whether content names get certificates one by one, each
// asked about first: Let's Encrypt over HTTP, which has no wildcards.
func (s Site) OnDemand() bool {
	return s.Host != "" && s.ContentHost != "" && s.Issuer == IssuerACME && s.DNS == nil
}

// UsesInternal reports whether Caddy's own authority certifies anything.
func (s Site) UsesInternal() bool {
	return len(s.Local) > 0 || (s.Host != "" && s.Issuer == IssuerInternal)
}

func (s Site) issuer() map[string]any {
	if s.Issuer == IssuerInternal {
		return map[string]any{"module": "internal"}
	}
	acme := map[string]any{"module": "acme"}
	if s.Email != "" {
		acme["email"] = s.Email
	}
	if s.DNS != nil {
		provider := map[string]any{"name": s.DNS.Provider}
		for k, v := range s.DNS.Fields {
			if v != "" {
				provider[k] = v
			}
		}
		acme["challenges"] = map[string]any{"dns": map[string]any{"provider": provider}}
	}
	return acme
}

// Config is Caddy's JSON configuration for site. A site with nothing to
// serve is an empty configuration.
func Config(site Site) map[string]any {
	if site.Empty() {
		return map[string]any{}
	}
	proxy := []any{map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": []any{map[string]any{"dial": site.Upstream}},
		// Streams (the run stream, a call's socket) are passed on as they
		// come, not buffered.
		"flush_interval": -1,
	}}
	route := func(hosts ...string) map[string]any {
		hs := make([]any, len(hosts))
		for i, h := range hosts {
			hs[i] = h
		}
		return map[string]any{"match": []any{map[string]any{"host": hs}}, "handle": proxy, "terminal": true}
	}
	own := append([]string{}, site.Local...)
	if site.Host != "" {
		own = append([]string{site.Host}, own...)
	}
	routes := []any{route(own...)}
	server := map[string]any{"listen": []any{":443"}}
	var policies []any
	subjects := func(names ...string) []any {
		out := make([]any, len(names))
		for i, n := range names {
			out[i] = n
		}
		return out
	}
	if site.Host != "" {
		policies = append(policies, map[string]any{"subjects": subjects(site.Host), "issuers": []any{site.issuer()}})
	}
	tls := map[string]any{}
	if site.Host != "" && site.ContentHost != "" {
		wild := "*." + site.ContentHost
		routes = append(routes, route(wild))
		if site.OnDemand() {
			// No wildcard over HTTP: one certificate per name, made at the
			// first visit, only for names the control plane knows.
			server["automatic_https"] = map[string]any{"skip_certificates": []any{wild}}
			policies = append(policies, map[string]any{"subjects": subjects(wild), "issuers": []any{site.issuer()}, "on_demand": true})
			tls["automation"] = map[string]any{"on_demand": map[string]any{"permission": map[string]any{"module": "http", "endpoint": site.Ask}}}
		} else {
			policies = append(policies, map[string]any{"subjects": subjects(wild), "issuers": []any{site.issuer()}})
		}
	}
	if len(site.Local) > 0 {
		policies = append(policies, map[string]any{"subjects": subjects(site.Local...), "issuers": []any{map[string]any{"module": "internal"}}})
		// A browser opening https://<address> names no server: answer it
		// with the first address's certificate.
		for _, l := range site.Local {
			if net.ParseIP(l) != nil {
				server["tls_connection_policies"] = []any{map[string]any{"default_sni": l}}
				break
			}
		}
	}
	server["routes"] = routes
	if len(policies) > 0 {
		auto, _ := tls["automation"].(map[string]any)
		if auto == nil {
			auto = map[string]any{}
		}
		auto["policies"] = policies
		tls["automation"] = auto
	}
	apps := map[string]any{"http": map[string]any{"servers": map[string]any{"silo": server}}, "tls": tls}
	if site.UsesInternal() {
		// The authority's root lives in Caddy's volume; installing it into
		// the container's own trust store is pointless and fails.
		apps["pki"] = map[string]any{"certificate_authorities": map[string]any{"local": map[string]any{"install_trust": false}}}
	}
	return map[string]any{"apps": apps}
}

// Admin is a client of Caddy's admin API at an address: unix//path/to.sock,
// or http://host:port.
type Admin struct {
	base string
	http *http.Client
}

// NewAdmin is a client of the admin API at addr.
func NewAdmin(addr string) (*Admin, error) {
	addr = strings.TrimSpace(addr)
	if path, ok := strings.CutPrefix(addr, "unix/"); ok && strings.HasPrefix(path, "/") {
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}
		// Caddy checks the Host of a request on a unix socket: 127.0.0.1 is
		// one it takes.
		return &Admin{base: "http://127.0.0.1", http: &http.Client{Transport: tr, Timeout: 30 * time.Second}}, nil
	}
	if strings.HasPrefix(addr, "http://") {
		return &Admin{base: strings.TrimSuffix(addr, "/"), http: &http.Client{Timeout: 30 * time.Second}}, nil
	}
	return nil, fmt.Errorf("caddy.admin is unix//path/to/admin.sock or http://host:port, not %q", addr)
}

// Current is the configuration Caddy runs now.
func (a *Admin) Current(ctx context.Context) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/config/", nil)
	if err != nil {
		return nil, err
	}
	body, err := a.do(req)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("caddy: %w", err)
	}
	return v, nil
}

// Load replaces Caddy's whole configuration.
func (a *Admin) Load(ctx context.Context, cfg map[string]any) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/load", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = a.do(req)
	return err
}

// RootCA is the root certificate of Caddy's own authority, PEM, for people
// to trust on their devices.
func (a *Admin) RootCA(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/pki/ca/local", nil)
	if err != nil {
		return "", err
	}
	body, err := a.do(req)
	if err != nil {
		return "", err
	}
	var ca struct {
		Root string `json:"root_certificate"`
	}
	if err := json.Unmarshal(body, &ca); err != nil {
		return "", fmt.Errorf("caddy: %w", err)
	}
	return ca.Root, nil
}

// Sync loads cfg unless Caddy runs it already, and reports whether it did.
func (a *Admin) Sync(ctx context.Context, cfg map[string]any) (bool, error) {
	cur, err := a.Current(ctx)
	if err != nil {
		return false, err
	}
	if Same(cur, cfg) {
		return false, nil
	}
	return true, a.Load(ctx, cfg)
}

// Same reports whether Caddy's current configuration is cfg. An empty one
// reads back as null.
func Same(cur any, cfg map[string]any) bool {
	raw, _ := json.Marshal(cfg)
	var want any
	_ = json.Unmarshal(raw, &want)
	if cur == nil {
		return len(cfg) == 0
	}
	return reflect.DeepEqual(cur, want)
}

func (a *Admin) do(req *http.Request) ([]byte, error) {
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy is not answering: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("caddy: %s", e.Error)
		}
		return nil, fmt.Errorf("caddy: %s", resp.Status)
	}
	return body, nil
}
