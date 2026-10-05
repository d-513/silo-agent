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
	if c.TunnelHost() != "" || c.TunnelsOn() {
		t.Fatalf("host %q on=%v with nothing configured", c.TunnelHost(), c.TunnelsOn())
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
		if c.TunnelHost() != tc.host || c.TunnelScheme() != tc.scheme || !c.TunnelsOn() {
			t.Errorf("%q: host=%q scheme=%q on=%v, want %q %q", tc.yaml, c.TunnelHost(), c.TunnelScheme(), c.TunnelsOn(), tc.host, tc.scheme)
		}
	}
}

// A real deployment must name its tunnel domain: guessing one from public_url
// would hand out links that do not resolve.
func TestTunnelHostNotDerivedForARealDomain(t *testing.T) {
	c := tunnelCfg(t, "public_url: https://silo.example.com\n")
	if c.TunnelHost() != "" || c.TunnelsOn() {
		t.Fatalf("host %q on=%v", c.TunnelHost(), c.TunnelsOn())
	}
	if c.TunnelScheme() != "https" {
		t.Fatalf("scheme %q follows public_url", c.TunnelScheme())
	}
}

func TestTunnelExplicitHostWinsAndIsNormalized(t *testing.T) {
	c := tunnelCfg(t, "public_url: https://silo.example.com\ntunnels:\n  host: \"  Tunnels.Example.COM \"\n")
	if c.TunnelHost() != "tunnels.example.com" || !c.TunnelsOn() {
		t.Fatalf("host %q on=%v", c.TunnelHost(), c.TunnelsOn())
	}
	if got := c.TunnelURL("quiet-amber-heron"); got != "https://quiet-amber-heron.tunnels.example.com" {
		t.Fatalf("url %q", got)
	}
}

func TestTunnelSchemeOverride(t *testing.T) {
	// TLS ends at a proxy and public_url is the internal origin.
	c := tunnelCfg(t, "public_url: http://silo.internal\ntunnels:\n  host: t.example.com\n  scheme: https\n")
	if got := c.TunnelURL("x"); got != "https://x.t.example.com" {
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
	c := tunnelCfg(t, "public_url: https://silo.example.com\ntunnels:\n  host: silo.example.com\n")
	if c.TunnelsOn() {
		t.Fatalf("host %q equal to the public host is live", c.TunnelHost())
	}
}

func TestTunnelNameFromHost(t *testing.T) {
	c := tunnelCfg(t, "tunnels:\n  host: tunnels.example.com\n")
	for host, want := range map[string]string{
		"quiet-amber-heron.tunnels.example.com":      "quiet-amber-heron",
		"Quiet-Amber-Heron.Tunnels.Example.COM":      "quiet-amber-heron",
		"quiet-amber-heron.tunnels.example.com:8443": "quiet-amber-heron",
	} {
		if got, ok := c.TunnelNameFromHost(host); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", host, got, ok, want)
		}
	}
	for _, host := range []string{
		"", "tunnels.example.com", "example.com", "a.b.tunnels.example.com", // one label only
		"evil-tunnels.example.com", ".tunnels.example.com", "x.tunnels.example.org",
	} {
		if got, ok := c.TunnelNameFromHost(host); ok {
			t.Errorf("%q matched as %q", host, got)
		}
	}
}

func TestTunnelNameFromHostWithPort(t *testing.T) {
	c := tunnelCfg(t, "tunnels:\n  host: localhost:8080\n")
	if got, ok := c.TunnelNameFromHost("brisk-teal-otter.localhost:8080"); !ok || got != "brisk-teal-otter" {
		t.Fatalf("got %q, %v", got, ok)
	}
	// A different port is a different origin.
	if _, ok := c.TunnelNameFromHost("brisk-teal-otter.localhost:5173"); ok {
		t.Fatal("matched another port")
	}
	if _, ok := c.TunnelNameFromHost("brisk-teal-otter.localhost"); ok {
		t.Fatal("matched with the port missing")
	}
}

func TestTunnelsOffNeverMatchesAHost(t *testing.T) {
	c := tunnelCfg(t, "tunnels:\n  enabled: false\n  host: t.example.com\n")
	if _, ok := c.TunnelNameFromHost("x.t.example.com"); ok {
		t.Fatal("a disabled tunnel domain still routes")
	}
}

func TestNormalizeTunnelHost(t *testing.T) {
	good := map[string]string{
		"tunnels.example.com":   "tunnels.example.com",
		" TUNNELS.Example.com ": "tunnels.example.com",
		"localhost:8080":        "localhost:8080",
		"t.example.com:8443":    "t.example.com:8443",
		"a-b.c1.io":             "a-b.c1.io",
	}
	for in, want := range good {
		got, err := NormalizeTunnelHost(in)
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
		if got, err := NormalizeTunnelHost(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestTunnelSettingsAreValidatedOnWrite(t *testing.T) {
	for yaml, wantErr := range map[string]string{
		"tunnels:\n  host: https://t.example.com\n":                                  "tunnels.host",
		"tunnels:\n  host: \"*.t.example.com\"\n":                                    "tunnels.host",
		"tunnels:\n  scheme: ftp\n":                                                  "tunnels.scheme",
		"public_url: https://silo.example.com\ntunnels:\n  host: silo.example.com\n": "public_url",
	} {
		err := validateYAML([]byte(yaml))
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: err = %v, want mention of %q", yaml, err, wantErr)
		}
	}
	for _, yaml := range []string{
		"tunnels:\n  host: tunnels.example.com\n  scheme: https\n",
		"tunnels:\n  enabled: false\n",
		"tunnels:\n  host: localhost:8080\npublic_url: http://localhost:5173\n",
		"tunnels:\n  scheme: \"\"\n",
	} {
		if err := validateYAML([]byte(yaml)); err != nil {
			t.Errorf("%q rejected: %v", yaml, err)
		}
	}
}

func TestTunnelKeysAreKnownEditableSettings(t *testing.T) {
	for _, k := range []string{"tunnels.enabled", "tunnels.host", "tunnels.scheme"} {
		if !KnownKey(k) {
			t.Errorf("%s is not a known key", k)
		}
	}
	if EnvName("tunnels.host") != "SILO_TUNNELS__HOST" {
		t.Fatalf("env name %q", EnvName("tunnels.host"))
	}
}

func TestTunnelHostEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := writeFile(dir+"/silo.yaml", "tunnels:\n  host: from-yaml.example.com\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SILO_TUNNELS__HOST", "From-Env.example.com")
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Config().TunnelHost(); got != "from-env.example.com" {
		t.Fatalf("host %q", got)
	}
	if s.Source("tunnels.host") != SourceEnv {
		t.Fatalf("source %v", s.Source("tunnels.host"))
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
