package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Auth configures signing in with a password.
type Auth struct {
	// Password is the password form. Off, only OIDC signs people in; see
	// Config.PasswordSignIn.
	Password bool `koanf:"password"`
	// TrustedProxies lists the reverse proxies in front of the control plane,
	// as comma-separated IPs or CIDRs. X-Forwarded-For is believed only from
	// them; unset, the address of the connection is the client.
	TrustedProxies string `koanf:"trusted_proxies"`
}

// OIDC configures the one OpenID Connect provider people may sign in with.
type OIDC struct {
	Issuer       string `koanf:"issuer"`
	ClientID     string `koanf:"client_id"`
	ClientSecret string `koanf:"client_secret"`
	// Scopes are space- or comma-separated; openid is always asked for.
	Scopes string `koanf:"scopes"`
	// Label is the text of the button on the sign-in page.
	Label string `koanf:"label"`
	// AutoCreate makes a Silo user the first time someone signs in. Off, only
	// people who already have an account (by verified email) get in.
	AutoCreate bool `koanf:"auto_create"`
	// AllowedDomains limits AutoCreate to these email domains.
	AllowedDomains string `koanf:"allowed_domains"`
	// GroupsClaim is the ID token claim that lists a person's groups, and
	// AdminGroup the group whose members are Silo admins. With AdminGroup unset
	// the provider has no say in who is an admin.
	GroupsClaim string `koanf:"groups_claim"`
	AdminGroup  string `koanf:"admin_group"`
}

const (
	DefaultOIDCScopes = "openid email profile"
	DefaultOIDCLabel  = "Single sign-on"
	DefaultOIDCClaim  = "groups"
)

// On reports whether OIDC sign-in is configured.
func (o OIDC) On() bool {
	return strings.TrimSpace(o.Issuer) != "" && strings.TrimSpace(o.ClientID) != ""
}

// ScopeList is the scopes to request, openid first.
func (o OIDC) ScopeList() []string {
	raw := o.Scopes
	if strings.TrimSpace(raw) == "" {
		raw = DefaultOIDCScopes
	}
	out := []string{"openid"}
	for _, s := range splitList(raw) {
		if !contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// ButtonLabel is the sign-in button's text.
func (o OIDC) ButtonLabel() string {
	if l := strings.TrimSpace(o.Label); l != "" {
		return l
	}
	return DefaultOIDCLabel
}

// Claim is the groups claim's name.
func (o OIDC) Claim() string {
	if c := strings.TrimSpace(o.GroupsClaim); c != "" {
		return c
	}
	return DefaultOIDCClaim
}

// DomainAllowed reports whether an account may be made for email: always when
// there is no allowlist, otherwise only for an exact domain on it.
func (o OIDC) DomainAllowed(email string) bool {
	domains := domainList(o.AllowedDomains)
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" {
		return false
	}
	return len(domains) == 0 || contains(domains, domain)
}

// PasswordSignIn reports whether the password form is offered. Turning it off
// only counts while OIDC is configured, so a typo cannot lock everyone out.
func (c Config) PasswordSignIn() bool {
	return c.Auth.Password || !c.OIDC.On()
}

// TrustedProxies is auth.trusted_proxies as prefixes; a bad entry (possible
// only from the environment, which is not validated) trusts nothing.
func (c Config) TrustedProxies() []netip.Prefix {
	p, err := ParseProxies(c.Auth.TrustedProxies)
	if err != nil {
		return nil
	}
	return p
}

// ParseProxies reads a comma- or space-separated list of IPs and CIDRs.
func ParseProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range splitList(s) {
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR", item)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

func domainList(s string) []string {
	var out []string
	for _, d := range splitList(strings.ToLower(s)) {
		out = append(out, strings.TrimPrefix(d, "@"))
	}
	return out
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var signInKeys = map[string]bool{
	"auth.password": true, "auth.trusted_proxies": true,
	"oidc.issuer": true, "oidc.client_id": true, "oidc.client_secret": true, "oidc.scopes": true, "oidc.label": true,
	"oidc.auto_create": true, "oidc.allowed_domains": true, "oidc.groups_claim": true, "oidc.admin_group": true,
}

// validateSignIn checks the auth.* and oidc.* settings of a YAML document.
func validateSignIn(k *koanf.Koanf) error {
	for _, key := range k.Keys() {
		if (strings.HasPrefix(key, "auth.") || strings.HasPrefix(key, "oidc.")) && !signInKeys[key] {
			return fmt.Errorf("unknown setting %q", key)
		}
	}
	for _, key := range []string{"auth.password", "oidc.auto_create"} {
		switch v := k.Get(key).(type) {
		case nil, bool:
		case string:
			if v != "true" && v != "false" && v != "" {
				return fmt.Errorf("%s must be true or false", key)
			}
		default:
			return fmt.Errorf("%s must be true or false", key)
		}
	}
	switch k.Get("auth.trusted_proxies").(type) {
	case nil, string:
	default:
		return fmt.Errorf("auth.trusted_proxies must be one comma-separated string of IPs and CIDRs")
	}
	if _, err := ParseProxies(k.String("auth.trusted_proxies")); err != nil {
		return fmt.Errorf("auth.trusted_proxies: %w", err)
	}
	if iss := strings.TrimSpace(k.String("oidc.issuer")); iss != "" {
		u, err := url.Parse(iss)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("oidc.issuer must be an http(s) URL with no query, such as https://id.example.com")
		}
	}
	for _, d := range domainList(k.String("oidc.allowed_domains")) {
		if !strings.Contains(d, ".") || strings.ContainsAny(d, "@/:") {
			return fmt.Errorf("oidc.allowed_domains: %q is not a domain", d)
		}
	}
	return nil
}
