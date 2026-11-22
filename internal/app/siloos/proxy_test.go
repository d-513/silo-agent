package siloos

import (
	"strings"
	"testing"

	"silo.agent/internal/caddy"
	"silo.agent/internal/config"
)

func TestSiteOfPicksTheIssuer(t *testing.T) {
	cfg := func(publicURL, mode string) config.Config {
		c := config.Config{PublicURL: publicURL, HTTPAddr: ":8080"}
		c.Caddy.Mode = mode
		return c
	}
	for _, tc := range []struct {
		url, mode   string
		host, issue string
		noted       bool
	}{
		{"https://silo.example.com", "", "silo.example.com", caddy.IssuerACME, false},
		{"https://silo.example.com", "internal", "silo.example.com", caddy.IssuerInternal, false},
		{"https://192.168.1.20", "auto", "192.168.1.20", caddy.IssuerInternal, false},
		{"https://silo.lan", "auto", "silo.lan", caddy.IssuerInternal, false},
		{"https://silo.lan", "acme", "", "", true},
		{"https://silo.example.com:8443", "auto", "", "", true},
		{"http://silo.example.com", "auto", "", "", true},
		{"", "auto", "", "", true},
	} {
		site, note := SiteOf(cfg(tc.url, tc.mode), nil)
		if site.Host != tc.host || (site.Host != "" && site.Issuer != tc.issue) || (note != "") != tc.noted {
			t.Errorf("%s %q: host %q issuer %q note %q", tc.url, tc.mode, site.Host, site.Issuer, note)
		}
	}

	// off serves nothing, the machine's own names included.
	if site, _ := SiteOf(cfg("https://silo.example.com", "off"), []string{"10.0.0.5"}); !site.Empty() {
		t.Errorf("off serves %v", site)
	}
	// The machine's names are served beside the domain, never twice.
	site, _ := SiteOf(cfg("https://silo-box", "auto"), []string{"silo-box", "10.0.0.5", "10.0.0.5"})
	if site.Host != "silo-box" || strings.Join(site.Local, ",") != "10.0.0.5" {
		t.Errorf("local = %v (host %q)", site.Local, site.Host)
	}

	// A DNS provider goes with Let's Encrypt only, with its own settings.
	c := cfg("https://silo.example.com", "auto")
	c.Caddy.DNSProvider = "cloudflare"
	c.Caddy.DNS = map[string]map[string]string{"cloudflare": {"api_token": " tok ", "nonsense": "x"}, "route53": {"secret_access_key": "no"}}
	site, _ = SiteOf(c, nil)
	if site.DNS == nil || site.DNS.Provider != "cloudflare" || site.DNS.Fields["api_token"] != "tok" || len(site.DNS.Fields) != 1 {
		t.Errorf("dns = %+v", site.DNS)
	}
	c.Caddy.Mode = "internal"
	if site, _ = SiteOf(c, nil); site.DNS != nil {
		t.Error("a DNS provider under Caddy's own authority")
	}
}
