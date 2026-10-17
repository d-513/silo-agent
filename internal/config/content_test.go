package config

import (
	"os"
	"strings"
	"testing"
)

func tunnelCfg(t *testing.T, yaml string) Config {
	t.Helper()
	s, err := FromYAML([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return s.Config()
}

func TestTunnelsDefaultOnButNeedsAHost(t *testing.T) {
	c := tunnelCfg(t, "")
	if !c.Tunnels.Enabled {
		t.Fatal("tunnels.enabled should default to true")
	}
	// No public_url and no host: nothing to build links from.
	if c.ContentHost() != "" || c.TunnelsOn() {
		t.Fatalf("host %q on=%v with nothing configured", c.ContentHost(), c.TunnelsOn())
	}
}

func TestTunnelHostDerivedForLocalDev(t *testing.T) {
	for _, tc := range []struct{ yaml, host, scheme string }{
		{"public_url: http://127.0.0.1:5173\n", "localhost:8080", "http"},
		{"public_url: http://localhost:5173\n", "localhost:8080", "http"},
		{"public_url: http://[::1]:5173\nhttp_addr: \":9000\"\n", "localhost:9000", "http"},
		{"public_url: http://localhost:5173\nhttp_addr: 127.0.0.1:8123\n", "localhost:8123", "http"},
	} {
		c := tunnelCfg(t, tc.yaml)
		if c.ContentHost() != tc.host || c.ContentScheme() != tc.scheme || !c.TunnelsOn() {
			t.Errorf("%q: host=%q scheme=%q on=%v, want %q %q", tc.yaml, c.ContentHost(), c.ContentScheme(), c.TunnelsOn(), tc.host, tc.scheme)
		}
	}
}

// A real deployment must name its tunnel domain: guessing one from public_url
// would hand out links that do not resolve.
func TestTunnelHostNotDerivedForARealDomain(t *testing.T) {
	c := tunnelCfg(t, "public_url: https://silo.example.com\n")
	if c.ContentHost() != "" || c.TunnelsOn() {
		t.Fatalf("host %q on=%v", c.ContentHost(), c.TunnelsOn())
	}
	if c.ContentScheme() != "https" {
		t.Fatalf("scheme %q follows public_url", c.ContentScheme())
	}
}

func TestTunnelExplicitHostWinsAndIsNormalized(t *testing.T) {
	c := tunnelCfg(t, "public_url: https://silo.example.com\ncontent:\n  host: \"  Tunnels.Example.COM \"\n")
	if c.ContentHost() != "tunnels.example.com" || !c.TunnelsOn() {
		t.Fatalf("host %q on=%v", c.ContentHost(), c.TunnelsOn())
	}
	if got := c.TunnelURL("quiet-amber-heron"); got != "https://tunnel-quiet-amber-heron.tunnels.example.com" {
		t.Fatalf("url %q", got)
	}
}

func TestTunnelSchemeOverride(t *testing.T) {
	// TLS ends at a proxy and public_url is the internal origin.
	c := tunnelCfg(t, "public_url: http://silo.internal\ncontent:\n  host: t.example.com\n  scheme: https\n")
	if got := c.TunnelURL("x"); got != "https://tunnel-x.t.example.com" {
		t.Fatalf("url %q", got)
	}
}

func TestTunnelsDisabled(t *testing.T) {
	c := tunnelCfg(t, "public_url: http://localhost:5173\ntunnels:\n  enabled: false\n")
	if c.TunnelsOn() {
		t.Fatal("tunnels.enabled=false must turn tunnels off even with a derivable host")
	}
}

// The tunnel router must never shadow the CP's own origin.
func TestTunnelHostEqualToPublicHostIsIgnored(t *testing.T) {
	c := tunnelCfg(t, "public_url: https://silo.example.com\ncontent:\n  host: silo.example.com\n")
	if c.TunnelsOn() {
		t.Fatalf("host %q equal to the public host is live", c.ContentHost())
	}
}

func TestContentFromHost(t *testing.T) {
	c := tunnelCfg(t, "content:\n  host: content.example.com\n")
	for host, want := range map[string][2]string{
		"tunnel-quiet-amber-heron.content.example.com":      {"tunnel", "quiet-amber-heron"},
		"Tunnel-Quiet-Amber-Heron.Content.Example.COM":      {"tunnel", "quiet-amber-heron"},
		"tunnel-quiet-amber-heron.content.example.com:8443": {"tunnel", "quiet-amber-heron"},
		"app-brisk-teal-otter.content.example.com":          {"app", "brisk-teal-otter"},
		"share-calm-red-fox.content.example.com":            {"share", "calm-red-fox"},
	} {
		if kind, name, ok := c.ContentFromHost(host); !ok || kind != want[0] || name != want[1] {
			t.Errorf("%q -> %q %q, %v; want %v", host, kind, name, ok, want)
		}
	}
	for _, host := range []string{
		"", "content.example.com", "example.com", "a.b.content.example.com", // one label only
		"evil-content.example.com", ".content.example.com", "tunnel-x.content.example.org",
		"quiet-amber-heron.content.example.com", "tunnel-.content.example.com", "app.content.example.com",
	} {
		if kind, name, ok := c.ContentFromHost(host); ok {
			t.Errorf("%q matched as %q %q", host, kind, name)
		}
	}
}

func TestContentFromHostWithPort(t *testing.T) {
	c := tunnelCfg(t, "content:\n  host: localhost:8080\n")
	if _, got, ok := c.ContentFromHost("tunnel-brisk-teal-otter.localhost:8080"); !ok || got != "brisk-teal-otter" {
		t.Fatalf("got %q, %v", got, ok)
	}
	// A different port is a different origin.
	if _, _, ok := c.ContentFromHost("tunnel-brisk-teal-otter.localhost:5173"); ok {
		t.Fatal("matched another port")
	}
	if _, _, ok := c.ContentFromHost("tunnel-brisk-teal-otter.localhost"); ok {
		t.Fatal("matched with the port missing")
	}
}

func TestTunnelsOffKeepsTheContentHost(t *testing.T) {
	// Tunnels off turns tunnels off, not apps and shared links.
	c := tunnelCfg(t, "tunnels:\n  enabled: false\ncontent:\n  host: t.example.com\n")
	if c.TunnelsOn() {
		t.Fatal("a disabled tunnel feature is on")
	}
	if _, _, ok := c.ContentFromHost("app-x.t.example.com"); !ok {
		t.Fatal("the content host stopped routing with tunnels off")
	}
}

func TestNormalizeContentHost(t *testing.T) {
	good := map[string]string{
		"tunnels.example.com":   "tunnels.example.com",
		" TUNNELS.Example.com ": "tunnels.example.com",
		"localhost:8080":        "localhost:8080",
		"t.example.com:8443":    "t.example.com:8443",
		"a-b.c1.io":             "a-b.c1.io",
	}
	for in, want := range good {
		got, err := NormalizeContentHost(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   ",
		"https://t.example.com", "t.example.com/path", "t.example.com?x=1",
		"*.t.example.com", "t example.com", "-bad.example.com", "bad-.example.com",
		"t..example.com", ".example.com", "example.com.", "t.example.com:", "t.example.com:0",
		"t.example.com:99999", "t.example.com:abc", "ünicode.example.com", "a@b.example.com",
		strings.Repeat("a", 64) + ".example.com",
	} {
		if got, err := NormalizeContentHost(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestTunnelSettingsAreValidatedOnWrite(t *testing.T) {
	for yaml, wantErr := range map[string]string{
		"content:\n  host: https://t.example.com\n":                                  "content.host",
		"content:\n  host: \"*.t.example.com\"\n":                                    "content.host",
		"content:\n  scheme: ftp\n":                                                  "content.scheme",
		"public_url: https://silo.example.com\ncontent:\n  host: silo.example.com\n": "public_url",
	} {
		err := validateYAML([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: err = %v, want mention of %q", yaml, err, wantErr)
		}
	}
	for _, yaml := range []string{
		"content:\n  host: tunnels.example.com\n  scheme: https\n",
		"tunnels:\n  enabled: false\n",
		"content:\n  host: localhost:8080\npublic_url: http://localhost:5173\n",
		"content:\n  scheme: \"\"\n",
	} {
		if err := validateYAML([]byte(yaml)); err != nil {
			t.Errorf("%q rejected: %v", yaml, err)
		}
	}
}

func TestContentKeysAreKnownEditableSettings(t *testing.T) {
	for _, k := range []string{"tunnels.enabled", "content.host", "content.scheme", "artifacts.max_mb"} {
		if !KnownKey(k) {
			t.Errorf("%s is not a known key", k)
		}
	}
	if EnvName("content.host") != "SILO_CONTENT__HOST" {
		t.Fatalf("env name %q", EnvName("content.host"))
	}
}

func TestTunnelHostEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := writeFile(dir+"/silo.yaml", "content:\n  host: from-yaml.example.com\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SILO_CONTENT__HOST", "From-Env.example.com")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Config().ContentHost(); got != "from-env.example.com" {
		t.Fatalf("host %q", got)
	}
	if s.Source("content.host") != SourceEnv {
		t.Fatalf("source %v", s.Source("content.host"))
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
