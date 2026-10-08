package config

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestSignInDefaults(t *testing.T) {
	c := tunnelCfg(t, "")
	if !c.Auth.Password || !c.PasswordSignIn() {
		t.Fatal("password sign-in should default to on")
	}
	if c.OIDC.On() {
		t.Fatal("OIDC on with nothing configured")
	}
	if got := c.OIDC.ScopeList(); !slices.Equal(got, []string{"openid", "email", "profile"}) {
		t.Fatalf("default scopes %v", got)
	}
	if c.OIDC.ButtonLabel() != "Single sign-on" || c.OIDC.Claim() != "groups" {
		t.Fatalf("label %q claim %q", c.OIDC.ButtonLabel(), c.OIDC.Claim())
	}
	if len(c.TrustedProxies()) != 0 {
		t.Fatalf("trusted proxies %v", c.TrustedProxies())
	}
}

// Turning password sign-in off without a working alternative would lock
// everyone out, so it only counts while OIDC is configured.
func TestPasswordSignInOffNeedsOIDC(t *testing.T) {
	c := tunnelCfg(t, "auth:\n  password: false\n")
	if !c.PasswordSignIn() {
		t.Fatal("password sign-in went off with no OIDC")
	}
	c = tunnelCfg(t, "auth:\n  password: false\noidc:\n  issuer: https://id.example.com\n")
	if !c.PasswordSignIn() {
		t.Fatal("an issuer without a client id is not OIDC")
	}
	c = tunnelCfg(t, "auth:\n  password: false\noidc:\n  issuer: https://id.example.com\n  client_id: silo\n")
	if c.PasswordSignIn() || !c.OIDC.On() {
		t.Fatalf("password=%v oidc=%v", c.PasswordSignIn(), c.OIDC.On())
	}
}

func TestOIDCSettings(t *testing.T) {
	c := tunnelCfg(t, `oidc:
  issuer: https://id.example.com/
  client_id: silo
  scopes: "email groups,  email"
  label: "  Company login "
  groups_claim: roles
  allowed_domains: "Example.com, @corp.example"
`)
	if got := c.OIDC.ScopeList(); !slices.Equal(got, []string{"openid", "email", "groups"}) {
		t.Fatalf("scopes %v", got)
	}
	if c.OIDC.ButtonLabel() != "Company login" || c.OIDC.Claim() != "roles" {
		t.Fatalf("label %q claim %q", c.OIDC.ButtonLabel(), c.OIDC.Claim())
	}
	for email, want := range map[string]bool{
		"a@example.com":      true,
		"A@EXAMPLE.COM":      true,
		"b@corp.example":     true,
		"c@evil-example.com": false,
		"d@sub.example.com":  false,
		"no-at-sign":         false,
	} {
		if got := c.OIDC.DomainAllowed(email); got != want {
			t.Errorf("DomainAllowed(%q) = %v", email, got)
		}
	}
	if !tunnelCfg(t, "").OIDC.DomainAllowed("anyone@anywhere.example") {
		t.Fatal("no allowlist should allow every domain")
	}
}

func TestParseProxies(t *testing.T) {
	got, err := ParseProxies(" 127.0.0.1, 10.0.0.0/8  ::1 ,")
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ParseProxies = %v %v", got, err)
	}
	if got, err := ParseProxies(""); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v %v", got, err)
	}
	for _, bad := range []string{"proxy.local", "10.0.0.0/40", "1.2.3"} {
		if _, err := ParseProxies(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	c := tunnelCfg(t, "auth:\n  trusted_proxies: 10.0.0.0/8\n")
	if p := c.TrustedProxies(); len(p) != 1 || p[0].String() != "10.0.0.0/8" {
		t.Fatalf("TrustedProxies %v", p)
	}
}

func TestValidateSignInYAML(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, wantErr string }{
		"ok":             {"auth:\n  trusted_proxies: 10.0.0.0/8, 127.0.0.1\noidc:\n  issuer: https://id.example.com/realms/x\n", ""},
		"local issuer":   {"oidc:\n  issuer: http://localhost:5556/dex\n", ""},
		"bad proxy":      {"auth:\n  trusted_proxies: nope\n", "auth.trusted_proxies"},
		"proxy list":     {"auth:\n  trusted_proxies:\n    - 10.0.0.0/8\n", "auth.trusted_proxies"},
		"issuer scheme":  {"oidc:\n  issuer: id.example.com\n", "oidc.issuer"},
		"issuer query":   {"oidc:\n  issuer: https://id.example.com/?x=1\n", "oidc.issuer"},
		"bad domain":     {"oidc:\n  allowed_domains: \"exa mple\"\n", "oidc.allowed_domains"},
		"unknown key":    {"oidc:\n  client: silo\n", "oidc.client"},
		"unknown auth":   {"auth:\n  passwords: true\n", "auth.passwords"},
		"bad bool":       {"auth:\n  password: maybe\n", "auth.password"},
		"bad auto bool":  {"oidc:\n  auto_create: 2\n", "oidc.auto_create"},
		"good bool text": {"oidc:\n  auto_create: \"true\"\n", ""},
	} {
		err := validateYAML([]byte(tc.yaml))
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %v, want one naming %s", name, err, tc.wantErr)
		}
	}
}

func TestSignInKeysAreSettings(t *testing.T) {
	for _, k := range []string{"auth.password", "auth.trusted_proxies", "oidc.issuer", "oidc.client_id", "oidc.client_secret", "oidc.scopes", "oidc.label", "oidc.auto_create", "oidc.allowed_domains", "oidc.groups_claim", "oidc.admin_group"} {
		if !KnownKey(k) {
			t.Errorf("%s is not a known setting", k)
		}
	}
	s, err := FromYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Fields() {
		if f.Key == "oidc.client_secret" && !f.Secret {
			t.Fatal("oidc.client_secret is not marked secret")
		}
		if f.Key == "auth.password" && (f.Type != "bool" || f.Value != "true") {
			t.Fatalf("auth.password field %+v", f)
		}
	}
}
