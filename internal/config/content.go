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

// Tunnels switches the reverse proxy into Bots on and off. A tunnel is served
// on the content host (Content), at tunnel-<name>.<content.host>.
type Tunnels struct {
	// Enabled is the master switch: off, the proxy, the Bot tools and the tab
	// are gone.
	Enabled bool `koanf:"enabled"`
}

// Content is the domain that serves what Bots made rather than the control
// plane itself: tunnels into a Bot (tunnel-<name>), live app artifacts
// (app-<name>) and the owner's public links (share-<name>), each a one-label
// subdomain of Host. It must be a domain separate from public_url's, so a page
// served there cannot set cookies for the control plane.
type Content struct {
	// Host is the domain suffix, host[:port], e.g. content.example.com. Unset,
	// local development derives localhost:<http_addr port>; anything else needs
	// it spelled out (wildcard DNS and TLS for *.<host> are the operator's).
	Host string `koanf:"host"`
	// Scheme builds the links: http or https. Unset it follows public_url.
	Scheme string `koanf:"scheme"`
}

// The kinds of content, by the prefix of their subdomain.
const (
	ContentTunnel = "tunnel"
	ContentApp    = "app"
	ContentShare  = "share"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeContentHost validates a content domain suffix (host with an
// optional :port, no scheme, path or wildcard) and returns it lower-cased.
func NormalizeContentHost(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", errors.New("is empty")
	}
	if strings.ContainsAny(s, "/?#@* \t") || strings.Contains(s, "://") {
		return "", errors.New("must be a bare host like content.example.com, without a scheme, path, wildcard or spaces")
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
		return "", errors.New("must be a domain name: content is served on subdomains, which an IP address cannot have")
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

// ContentHost is the domain suffix content is served under, or "" when there
// is none (not configured, or the configured one is unusable).
func (c Config) ContentHost() string {
	if raw := strings.TrimSpace(c.Content.Host); raw != "" {
		h, err := NormalizeContentHost(raw)
		if err != nil {
			return ""
		}
		// The CP's own origin is never a content suffix: a page served there
		// could set cookies for the whole domain and reach the CP with them.
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
	return c.Tunnels.Enabled && c.ContentHost() != ""
}

// ContentScheme is the scheme links use: content.scheme, else public_url's.
func (c Config) ContentScheme() string {
	switch s := strings.ToLower(strings.TrimSpace(c.Content.Scheme)); s {
	case "http", "https":
		return s
	}
	if u := c.publicURL(); u != nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme
	}
	return "http"
}

// ContentURL is the origin of the content of a kind named name.
func (c Config) ContentURL(kind, name string) string {
	return c.ContentScheme() + "://" + kind + "-" + name + "." + c.ContentHost()
}

// TunnelURL is the address of the tunnel named name.
func (c Config) TunnelURL(name string) string {
	return c.ContentURL(ContentTunnel, name)
}

// ContentFromHost reads a request Host header as content: the kind and the
// name of the one label in front of the content domain (tunnel-quiet-amber-
// heron is tunnel, quiet-amber-heron). It matches nothing without a content
// host, and requires the port to match when the domain carries one.
func (c Config) ContentFromHost(host string) (kind, name string, ok bool) {
	want := c.ContentHost()
	if want == "" {
		return "", "", false
	}
	wantName, wantPort, _ := strings.Cut(want, ":")
	host = strings.ToLower(strings.TrimSpace(host))
	h, port := host, ""
	if hh, p, err := net.SplitHostPort(host); err == nil {
		h, port = hh, p
	}
	if wantPort != "" && port != wantPort {
		return "", "", false
	}
	label, found := strings.CutSuffix(h, "."+wantName)
	if !found || !dnsLabel.MatchString(label) {
		return "", "", false
	}
	kind, name, found = strings.Cut(label, "-")
	if !found || name == "" {
		return "", "", false
	}
	switch kind {
	case ContentTunnel, ContentApp, ContentShare:
		return kind, name, true
	}
	return "", "", false
}

// validateContent checks the content.* keys of a YAML document being written.
func validateContent(host, scheme, publicURL string) error {
	if host = strings.TrimSpace(host); host != "" {
		n, err := NormalizeContentHost(host)
		if err != nil {
			return fmt.Errorf("content.host: %w", err)
		}
		if u, err := url.Parse(strings.TrimSpace(publicURL)); err == nil && u.Host != "" && strings.EqualFold(u.Host, n) {
			return fmt.Errorf("content.host %q is the public_url host: use a separate domain so pages served there cannot set cookies for the control plane", n)
		}
	}
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "", "http", "https":
	default:
		return fmt.Errorf("content.scheme must be http or https, not %q", scheme)
	}
	return nil
}
