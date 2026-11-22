package siloos

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/caddy"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

// localEvery is how often the machine's names and addresses are asked of
// siloosd again.
const localEvery = time.Minute

// ProxyStatus is how the last comparison with Caddy went.
func (s *Service) ProxyStatus() *v1.ProxyStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return proto.Clone(s.proxy).(*v1.ProxyStatus)
}

func (s *Service) setProxy(p *v1.ProxyStatus) {
	s.mu.Lock()
	s.proxy = p
	s.mu.Unlock()
}

// SyncProxy gives Caddy the configuration the settings call for, unless it
// runs it already.
func (s *Service) SyncProxy(ctx context.Context) {
	cfg := s.cfg()
	addr := strings.TrimSpace(cfg.Caddy.Admin)
	if addr == "" {
		s.setProxy(&v1.ProxyStatus{})
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	site, note := SiteOf(cfg, s.localNames(ctx))
	st := &v1.ProxyStatus{Configured: true, Host: site.Host, ContentHost: site.ContentHost, Local: site.Local, Note: note}
	if site.Host != "" {
		st.Issuer = site.Issuer
		if site.DNS != nil && site.Issuer == caddy.IssuerACME {
			st.DnsProvider = site.DNS.Provider
		}
	}
	admin, err := caddy.NewAdmin(addr)
	if err != nil {
		st.State, st.Error = "error", err.Error()
		s.setProxy(st)
		return
	}
	changed, err := admin.Sync(ctx, caddy.Config(site))
	switch {
	case err != nil:
		st.State, st.Error = "error", err.Error()
	case site.Empty():
		st.State = "off"
	default:
		st.State = "ok"
	}
	if err == nil {
		st.SyncedAt = time.Now().UTC().Format(time.RFC3339)
		if changed {
			log.Printf("caddy: loaded the configuration for %q (content host %q, issuer %q, local %v)", site.Host, site.ContentHost, site.Issuer, site.Local)
		}
		if site.UsesInternal() {
			st.RootCa = s.root(ctx, admin)
		}
	}
	s.setProxy(st)
}

// root is Caddy's authority's root certificate, asked each time (a new
// Caddy volume is a new authority), the last one known when Caddy will not
// say.
func (s *Service) root(ctx context.Context, admin *caddy.Admin) string {
	pem, err := admin.RootCA(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && strings.Contains(pem, "BEGIN CERTIFICATE") {
		s.rootCA = pem
	}
	return s.rootCA
}

// localNames are a SiloOS machine's host name and IPv4 addresses, asked of
// siloosd at most once a minute; none anywhere else.
func (s *Service) localNames(ctx context.Context) []string {
	d := s.daemon()
	if d == nil {
		return nil
	}
	s.mu.Lock()
	if time.Since(s.localAt) < localEvery {
		out := s.local
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()
	var out []string
	if sys, err := d.GetSystem(ctx, connect.NewRequest(&v1.GetSystemRequest{})); err == nil {
		if h := strings.ToLower(sys.Msg.GetHostname()); h != "" && h != "localhost" {
			out = append(out, h)
		}
	}
	if n, err := d.GetNetwork(ctx, connect.NewRequest(&v1.GetNetworkRequest{})); err == nil {
		for _, dev := range n.Msg.GetDevices() {
			for _, a := range dev.GetAddresses() {
				ip, _, _ := strings.Cut(a, "/")
				if p := net.ParseIP(ip); p != nil && !p.IsLoopback() && !slices.Contains(out, ip) {
					out = append(out, ip)
				}
			}
		}
	}
	s.mu.Lock()
	s.local, s.localAt = out, time.Now()
	s.mu.Unlock()
	return out
}

// SiteOf is what Caddy serves for the settings and the machine's own names
// (local), and, when the control plane's own name is not served, why.
//
// The control plane's name is public_url's, when it is https on port 443.
// Its issuer is caddy.mode's: Let's Encrypt (acme), Caddy's own authority
// (internal), or for auto Let's Encrypt when the name is a public domain and
// Caddy's own authority when it is an address or a local name. The content
// host follows it. The machine's own names are always Caddy's own authority.
func SiteOf(cfg config.Config, local []string) (caddy.Site, string) {
	mode := cfg.CaddyMode()
	if mode == config.CaddyOff {
		return caddy.Site{}, "caddy.mode is off."
	}
	up := upstream(cfg.HTTPAddr)
	site := caddy.Site{Upstream: up, Ask: "http://" + up + caddy.AskPath, Email: strings.TrimSpace(cfg.Caddy.Email)}
	host, note := publicHost(cfg.PublicURL)
	if host != "" {
		public := publicName(host)
		switch {
		case mode == config.CaddyInternal, mode == config.CaddyAuto && !public:
			site.Issuer = caddy.IssuerInternal
		case !public:
			host, note = "", "Let's Encrypt cannot certify "+host+": it is an address or a name on the local network. Use internal or auto."
		default:
			site.Issuer = caddy.IssuerACME
		}
	}
	if host != "" {
		site.Host = host
		if c := cfg.ContentHost(); c != "" && !strings.Contains(c, ":") {
			site.ContentHost = c
		}
		if site.Issuer == caddy.IssuerACME {
			site.DNS = dnsOf(cfg)
		}
	}
	for _, l := range local {
		if l != site.Host && !slices.Contains(site.Local, l) {
			site.Local = append(site.Local, l)
		}
	}
	return site, note
}

// publicHost is public_url's host when Caddy can serve it, else why not.
func publicHost(raw string) (string, string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", "public_url is not set."
	}
	if u.Scheme != "https" {
		return "", "public_url is not an https address."
	}
	if p := u.Port(); p != "" && p != "443" {
		return "", "public_url has a port; Caddy serves HTTPS on 443."
	}
	return strings.ToLower(u.Hostname()), ""
}

// localSuffixes are names that only mean something on a local network.
var localSuffixes = []string{".local", ".lan", ".home", ".internal", ".intranet", ".corp", ".home.arpa", ".localhost", ".test", ".example", ".invalid"}

// publicName reports whether Let's Encrypt can certify a name: a domain
// with a public suffix, not an address or a local network's name.
func publicName(host string) bool {
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") || host == "localhost" {
		return false
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(host, s) {
			return false
		}
	}
	return true
}

// dnsOf is the DNS provider the settings name, with its settings, or nil.
func dnsOf(cfg config.Config) *caddy.DNS {
	id := strings.TrimSpace(cfg.Caddy.DNSProvider)
	p, ok := caddy.DNSProviderOf(id)
	if !ok {
		return nil
	}
	fields := map[string]string{}
	for _, f := range p.Settings {
		if v := strings.TrimSpace(cfg.Caddy.DNS[id][f.Key]); v != "" {
			fields[f.Key] = v
		}
	}
	return &caddy.DNS{Provider: id, Fields: fields}
}

// upstream is the control plane's own address from a Caddy on the same host.
func upstream(httpAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(httpAddr))
	if err != nil || port == "" {
		return "127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// ServeAsk answers Caddy before it gets a certificate on demand (Let's
// Encrypt over HTTP, for content names): yes (200) for the control plane's
// own name and for a tunnel, live app or share that exists, no (404) for
// everything else, so nobody can make Caddy ask Let's Encrypt for names at
// will. Only Caddy on this machine may ask.
func (s *Service) ServeAsk(w http.ResponseWriter, r *http.Request) {
	if !loopback(r.RemoteAddr) {
		http.NotFound(w, r)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	cfg := s.cfg()
	host, _ := publicHost(cfg.PublicURL)
	if domain != "" && (domain == host || s.contentKnown(cfg, domain)) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
}

func (s *Service) contentKnown(cfg config.Config, domain string) bool {
	kind, name, ok := cfg.ContentFromHost(domain)
	if !ok {
		return false
	}
	var n int64
	switch kind {
	case config.ContentTunnel:
		if !cfg.TunnelsOn() {
			return false
		}
		s.db.Model(&db.Tunnel{}).Where("name = ?", name).Count(&n)
	case config.ContentApp:
		s.db.Model(&db.Artifact{}).Where("label = ?", name).Count(&n)
	case config.ContentShare:
		s.db.Model(&db.ArtifactShare{}).Where("label = ?", name).Count(&n)
	}
	return n > 0
}

func loopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
