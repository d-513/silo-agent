package config

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/knadh/koanf/v2"

	"silo.agent/internal/egress"
)

// Egress configures the network filter: one silo-egress gateway per container
// engine that every Bot box and STDIO sidecar reaches the internet through.
// Turning it off leaves today's unfiltered network, and Admin warns.
type Egress struct {
	Enabled bool   `koanf:"enabled"`
	Image   string `koanf:"image"`
	// SubnetPool is the pool the CP carves each client's own /24 from. It is
	// rarely changed (a collision with a real network is the reason to).
	SubnetPool string `koanf:"subnet_pool"`
	// The operator's lists: the floor every Bot stands on. An owner may add
	// their own on top (the owner layer) but never below this.
	Block []string `koanf:"block"`
	Trust []string `koanf:"trust"`
	Pass  []string `koanf:"pass"`
	// Feeds are the threat lists the gateway downloads and refreshes itself.
	// Empty means the built-in set.
	Feeds []EgressFeed `koanf:"feeds"`
	// DNS names the resolvers the gateway looks outside names up with:
	// comma-separated IPs, each optionally with a port. Empty means the
	// system's, as the control plane's own resolv.conf names them.
	DNS string `koanf:"dns"`
}

// EgressFeed is one threat list the gateway refreshes on its own.
type EgressFeed struct {
	ID     string `koanf:"id"`
	URL    string `koanf:"url"`
	Format string `koanf:"format"`
}

const (
	// DefaultEgressImage is the gateway container image.
	DefaultEgressImage = "localhost/silo-egress:v1"
	// DefaultEgressPool is the pool the CP carves a /24 per client from.
	DefaultEgressPool = "10.213.0.0/16"
	// EgressSubnetBits is the size of one client's network: a /24.
	EgressSubnetBits = 24
)

// DefaultEgressFeeds is the built-in threat list set, used when the operator
// names none.
var DefaultEgressFeeds = []EgressFeed{
	{ID: "urlhaus", URL: "https://urlhaus.abuse.ch/downloads/hostfile/", Format: egress.FormatHosts},
	{ID: "threatfox", URL: "https://threatfox.abuse.ch/downloads/hostfile/", Format: egress.FormatHosts},
	{ID: "spamhaus_drop", URL: "https://www.spamhaus.org/drop/drop_v4.json", Format: egress.FormatCIDRJSON},
	{ID: "et_open", URL: "https://rules.emergingthreats.net/blockrules/compromised-ips.txt", Format: egress.FormatIPs},
	{ID: "hagezi_tif", URL: "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/tif.mini-onlydomains.txt", Format: egress.FormatDomains},
}

// EgressImage is the gateway image to run.
func (c Config) EgressImage() string {
	if s := strings.TrimSpace(c.Egress.Image); s != "" {
		return s
	}
	return DefaultEgressImage
}

// EgressPool is the prefix the CP carves client subnets from.
func (c Config) EgressPool() netip.Prefix {
	if p, err := netip.ParsePrefix(strings.TrimSpace(c.Egress.SubnetPool)); err == nil {
		return p.Masked()
	}
	p, _ := netip.ParsePrefix(DefaultEgressPool)
	return p
}

// EgressFeeds is the threat list set in use: the operator's, or the built-in
// defaults when none are named.
func (c Config) EgressFeeds() []EgressFeed {
	if len(c.Egress.Feeds) > 0 {
		return c.Egress.Feeds
	}
	return DefaultEgressFeeds
}

// EgressDNS is the resolvers the operator named for the gateway. A bad entry
// (possible only from the environment, which is not validated) is skipped.
func (c Config) EgressDNS() []string {
	servers, _ := ParseDNSServers(c.Egress.DNS)
	return servers
}

// ParseDNSServers reads a comma-separated list of resolvers, each an IP or
// IP:port, into host:port strings (port 53 when none is given). It keeps the
// entries it can read and reports the first it cannot.
func ParseDNSServers(s string) ([]string, error) {
	var out []string
	var bad error
	for _, f := range splitList(s) {
		ap, err := parseDNSServer(f)
		if err != nil {
			if bad == nil {
				bad = err
			}
			continue
		}
		out = append(out, ap.String())
	}
	return out, bad
}

func parseDNSServer(s string) (netip.AddrPort, error) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		if ap.Port() == 0 || ap.Addr().IsUnspecified() {
			return netip.AddrPort{}, fmt.Errorf("%q is not a resolver address", s)
		}
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), nil
	}
	a, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil || a.IsUnspecified() {
		return netip.AddrPort{}, fmt.Errorf("%q is not an IP address (names are not taken: the gateway would need DNS to find its DNS)", s)
	}
	return netip.AddrPortFrom(a.Unmap(), 53), nil
}

// validateEgress checks the egress.* keys of a YAML document being written.
func validateEgress(k *koanf.Koanf) error {
	known := map[string]bool{
		"egress.enabled": true, "egress.image": true, "egress.subnet_pool": true,
		"egress.block": true, "egress.trust": true, "egress.pass": true, "egress.feeds": true,
		"egress.dns": true,
	}
	for _, key := range k.Keys() {
		if strings.HasPrefix(key, "egress.") && !known[key] {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	switch v := k.Get("egress.enabled").(type) {
	case nil, bool:
	case string:
		if v != "true" && v != "false" && v != "" {
			return fmt.Errorf("egress.enabled must be true or false")
		}
	default:
		return fmt.Errorf("egress.enabled must be true or false")
	}
	if pool := strings.TrimSpace(k.String("egress.subnet_pool")); pool != "" {
		p, err := netip.ParsePrefix(pool)
		if err != nil {
			return fmt.Errorf("egress.subnet_pool must be a CIDR: %w", err)
		}
		if p.Bits() > EgressSubnetBits {
			return fmt.Errorf("egress.subnet_pool must be no smaller than a /%d", EgressSubnetBits)
		}
	}
	switch k.Get("egress.dns").(type) {
	case nil, string:
	default:
		return fmt.Errorf("egress.dns must be one comma-separated string of IP addresses")
	}
	if _, err := ParseDNSServers(k.String("egress.dns")); err != nil {
		return fmt.Errorf("egress.dns: %w", err)
	}
	for _, list := range []string{"egress.block", "egress.trust", "egress.pass"} {
		for _, p := range k.Strings(list) {
			if !egress.ValidPattern(p) {
				return fmt.Errorf("%s: %q is not a host pattern", list, p)
			}
		}
	}
	feeds, _ := k.Get("egress.feeds").([]any)
	for _, f := range feeds {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		url, _ := m["url"].(string)
		format, _ := m["format"].(string)
		if strings.TrimSpace(id) == "" || strings.TrimSpace(url) == "" {
			return fmt.Errorf("egress.feeds: each feed needs an id and a url")
		}
		switch format {
		case egress.FormatHosts, egress.FormatDomains, egress.FormatIPs, egress.FormatCIDRJSON:
		default:
			return fmt.Errorf("egress.feeds: unknown format %q", format)
		}
	}
	return nil
}
