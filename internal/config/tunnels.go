package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Tunnels configures the reverse proxy into Bots: a tunnel is served at
// <name>.<host>, so the CP answers any one-label subdomain of Host.
type Tunnels struct {
	// Enabled is the master switch: off, the proxy, the Bot tools and the tab
	// are gone.
	Enabled bool `koanf:"enabled"`
	// Host is the domain suffix, host[:port], e.g. tunnels.example.com. Unset,
	// local development derives localhost:<http_addr port>; anything else needs
	// it spelled out (wildcard DNS and TLS for *.<host> are the operator's).
	Host string `koanf:"host"`
	// Scheme builds the links: http or https. Unset it follows public_url.
	Scheme string `koanf:"scheme"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeTunnelHost validates a tunnel domain suffix (host with an optional
// :port, no scheme, path or wildcard) and returns it lower-cased.
func NormalizeTunnelHost(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", errors.New("is empty")
	}
	if strings.ContainsAny(s, "/?#@* \t") || strings.Contains(s, "://") {
		return "", errors.New("must be a bare host like tunnels.example.com, without a scheme, path, wildcard or spaces")
	}
	name, port, hasPort := strings.Cut(s, ":")
	if hasPort {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return "", fmt.Errorf("port %q is not 1-65535", port)
		}
	}
	if name == "" || len(name) > 253 {
		return "", errors.New("is not a valid host name")
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return "", errors.New("must be a domain name: tunnels are subdomains, which an IP address cannot have")
	}
	for _, label := range strings.Split(name, ".") {
		if !dnsLabel.MatchString(label) {
			return "", fmt.Errorf("%q is not a valid host name", name)
		}
	}
	return s, nil
}

func isLocalHost(h string) bool {
	h = strings.ToLower(h)
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

// publicURL parses public_url; nil when unset or not a URL.
func (c Config) publicURL() *url.URL {
	u, err := url.Parse(strings.TrimSpace(c.PublicURL))
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// TunnelHost is the domain suffix tunnels are served under, or "" when there
// is none (not configured, or the configured one is unusable).
func (c Config) TunnelHost() string {
	if raw := strings.TrimSpace(c.Tunnels.Host); raw != "" {
		h, err := NormalizeTunnelHost(raw)
		if err != nil {
			return ""
		}
		// The CP's own origin is never a tunnel suffix: a tunnelled app could
		// set cookies for the whole domain and reach the CP with them.
		if u := c.publicURL(); u != nil && strings.EqualFold(u.Host, h) {
			return ""
		}
		return h
	}
	// Local development: public_url is on this machine, so *.localhost (which
	// browsers resolve to loopback) on the CP's own port needs no DNS. This is
	// the CP port, not Vite's: Vite would answer a subdomain with the SPA.
	if u := c.publicURL(); u != nil && isLocalHost(u.Hostname()) {
		port := "8080"
		if _, p, err := net.SplitHostPort(strings.TrimSpace(c.HTTPAddr)); err == nil && p != "" {
			port = p
		}
		return "localhost:" + port
	}
	return ""
}

// TunnelsOn reports whether tunnels are enabled and have a domain to live on.
func (c Config) TunnelsOn() bool {
	return c.Tunnels.Enabled && c.TunnelHost() != ""
}

// TunnelScheme is the scheme links use: tunnels.scheme, else public_url's.
func (c Config) TunnelScheme() string {
	switch s := strings.ToLower(strings.TrimSpace(c.Tunnels.Scheme)); s {
	case "http", "https":
		return s
	}
	if u := c.publicURL(); u != nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme
	}
	return "http"
}

// TunnelURL is the address of the tunnel named name.
func (c Config) TunnelURL(name string) string {
	return c.TunnelScheme() + "://" + name + "." + c.TunnelHost()
}

// TunnelNameFromHost extracts the tunnel name from a request Host header: the
// single label in front of the tunnel domain. It matches nothing when tunnels
// are off, and requires the port to match when the domain carries one.
func (c Config) TunnelNameFromHost(host string) (string, bool) {
	if !c.TunnelsOn() {
		return "", false
	}
	want := c.TunnelHost()
	wantName, wantPort, _ := strings.Cut(want, ":")
	host = strings.ToLower(strings.TrimSpace(host))
	name, port := host, ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		name, port = h, p
	}
	if wantPort != "" && port != wantPort {
		return "", false
	}
	label, ok := strings.CutSuffix(name, "."+wantName)
	if !ok || !dnsLabel.MatchString(label) {
		return "", false
	}
	return label, true
}

// validateTunnels checks the tunnels.* keys of a YAML document being written.
func validateTunnels(host, scheme, publicURL string) error {
	if host = strings.TrimSpace(host); host != "" {
		n, err := NormalizeTunnelHost(host)
		if err != nil {
			return fmt.Errorf("tunnels.host: %w", err)
		}
		if u, err := url.Parse(strings.TrimSpace(publicURL)); err == nil && u.Host != "" && strings.EqualFold(u.Host, n) {
			return fmt.Errorf("tunnels.host %q is the public_url host: use a separate domain so tunnelled apps cannot set cookies for the control plane", n)
		}
	}
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "", "http", "https":
	default:
		return fmt.Errorf("tunnels.scheme must be http or https, not %q", scheme)
	}
	return nil
}
