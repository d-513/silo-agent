package config

import (
	"strings"
	"testing"
)

func mailCfg(t *testing.T, yaml string) Config {
	t.Helper()
	s, err := FromYAML([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return s.Config()
}

func TestMailDefaultOnButNeedsADomain(t *testing.T) {
	c := mailCfg(t, "public_url: http://localhost:5173\n")
	if !c.Mail.Enabled {
		t.Fatal("mail.enabled should default to true")
	}
	// Nothing is guessed from public_url: an address that no MX record points
	// at would look real and receive nothing.
	if c.MailDomain() != "" || c.MailOn() {
		t.Fatalf("domain %q on=%v with no mail.domain", c.MailDomain(), c.MailOn())
	}
	if c.Mail.ListenAddr() != ":2525" || c.Mail.MaxBytes() != 10<<20 {
		t.Fatalf("defaults: addr %q max %d", c.Mail.ListenAddr(), c.Mail.MaxBytes())
	}
}

func TestMailDomainIsNormalizedAndBuildsAddresses(t *testing.T) {
	c := mailCfg(t, "mail:\n  domain: \"  Bots.Example.COM \"\n")
	if c.MailDomain() != "bots.example.com" || !c.MailOn() {
		t.Fatalf("domain %q on=%v", c.MailDomain(), c.MailOn())
	}
	if got := c.MailAddress("quiet-amber-heron"); got != "quiet-amber-heron@bots.example.com" {
		t.Fatalf("address %q", got)
	}
}

func TestMailOffHasNoAddresses(t *testing.T) {
	c := mailCfg(t, "mail:\n  enabled: false\n  domain: bots.example.com\n")
	if c.MailOn() {
		t.Fatal("mail.enabled=false must turn it off")
	}
	// The domain is still known, so the settings page can show it.
	if c.MailDomain() != "bots.example.com" {
		t.Fatalf("domain %q", c.MailDomain())
	}
}

func TestNormalizeMailDomainRejectsWhatIsNotADomain(t *testing.T) {
	for _, bad := range []string{"", "bots.example.com:25", "https://bots.example.com", "user@example.com", "*.example.com", "10.0.0.1", "exa mple.com", "-bad.example.com"} {
		if got, err := NormalizeMailDomain(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
	for in, want := range map[string]string{"Example.com": "example.com", " bots.example.com ": "bots.example.com", "silo.localhost": "silo.localhost"} {
		if got, err := NormalizeMailDomain(in); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestMailMaxBytesIsClamped(t *testing.T) {
	for yaml, want := range map[string]int64{
		"mail:\n  max_size_mb: 3\n":    3 << 20,
		"mail:\n  max_size_mb: 0\n":    10 << 20,
		"mail:\n  max_size_mb: -4\n":   10 << 20,
		"mail:\n  max_size_mb: 9000\n": MailMaxSizeMB << 20,
	} {
		if got := mailCfg(t, yaml).Mail.MaxBytes(); got != want {
			t.Errorf("%q: %d, want %d", yaml, got, want)
		}
	}
}

func TestValidateYAMLChecksMail(t *testing.T) {
	for yaml, want := range map[string]string{
		"mail:\n  domain: \"bots.example.com:25\"\n":           "mail.domain",
		"mail:\n  addr: nonsense\n":                            "mail.addr",
		"mail:\n  addr: \":99999\"\n":                          "mail.addr",
		"mail:\n  max_size_mb: lots\n":                         "mail.max_size_mb",
		"mail:\n  max_size_mb: 500\n":                          "mail.max_size_mb",
		"mail:\n  tls_cert: /etc/silo/mail.crt\n":              "mail.tls_cert and mail.tls_key",
		"mail:\n  tls_key: /etc/silo/mail.key\n":               "mail.tls_cert and mail.tls_key",
		"mail:\n  domain: bots.example.com\n  addr: \":25\"\n": "",
		"mail:\n  addr: 127.0.0.1:2525\n  max_size_mb: 25\n":   "",
		"mail:\n  tls_cert: /a.crt\n  tls_key: /a.key\n":       "",
	} {
		err := validateYAML([]byte(yaml))
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: %v", yaml, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: error %v, want one naming %q", yaml, err, want)
		}
	}
}

func TestMailKeysAreKnownSettings(t *testing.T) {
	for _, k := range []string{"mail.enabled", "mail.domain", "mail.addr", "mail.max_size_mb", "mail.tls_cert", "mail.tls_key"} {
		if !KnownKey(k) {
			t.Errorf("%s is not a known setting: the admin form could not save it", k)
		}
	}
}
