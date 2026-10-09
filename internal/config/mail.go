package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Mail configures the receive-only mailbox every Bot has: the control plane
// runs an SMTP listener and files what arrives for <name>@<domain> in that
// Bot's inbox. Nothing is ever sent, so there is no relay, queue or bounce.
type Mail struct {
	// Enabled is the master switch: off, the listener, the Bot tools and the
	// Mail page are gone.
	Enabled bool `koanf:"enabled"`
	// Domain is what follows the @, e.g. bots.example.com. Its MX record must
	// point at this machine. Unset, there is no mail: an address nothing
	// delivers to would only look real.
	Domain string `koanf:"domain"`
	// Addr is where the SMTP listener binds. The default is unprivileged; the
	// world delivers to port 25, so publish or forward 25 to it.
	Addr string `koanf:"addr"`
	// MaxSizeMB caps one message, attachments included.
	MaxSizeMB int `koanf:"max_size_mb"`
	// TLSCert and TLSKey are PEM files for STARTTLS. Unset, a self-signed
	// certificate is made at start, which opportunistic TLS accepts.
	TLSCert string `koanf:"tls_cert"`
	TLSKey  string `koanf:"tls_key"`
}

const (
	// DefaultMailAddr is the SMTP listener's address when mail.addr is unset.
	DefaultMailAddr = ":2525"
	// DefaultMailSizeMB and MailMaxSizeMB bound mail.max_size_mb.
	DefaultMailSizeMB = 10
	MailMaxSizeMB     = 50
)

// NormalizeMailDomain validates a mail domain (a bare host name: no port,
// scheme, address or wildcard) and returns it lower-cased.
func NormalizeMailDomain(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", errors.New("is empty")
	}
	if strings.ContainsAny(s, "/?#@*: \t") {
		return "", errors.New("must be a bare domain like bots.example.com, without a scheme, port, user or wildcard")
	}
	if len(s) > 253 {
		return "", errors.New("is not a valid domain")
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return "", errors.New("must be a domain name, not an IP address")
	}
	for _, label := range strings.Split(s, ".") {
		if !dnsLabel.MatchString(label) {
			return "", fmt.Errorf("%q is not a valid domain", s)
		}
	}
	return s, nil
}

// MailDomain is the domain Bots' addresses live under, or "" when there is
// none (not configured, or the configured one is unusable).
func (c Config) MailDomain() string {
	d, err := NormalizeMailDomain(c.Mail.Domain)
	if err != nil {
		return ""
	}
	return d
}

// MailOn reports whether mail is enabled and has a domain to receive for.
func (c Config) MailOn() bool {
	return c.Mail.Enabled && c.MailDomain() != ""
}

// MailAddress is the address of the mailbox named name.
func (c Config) MailAddress(name string) string {
	return name + "@" + c.MailDomain()
}

// ListenAddr is where the SMTP listener binds.
func (m Mail) ListenAddr() string {
	if a := strings.TrimSpace(m.Addr); a != "" {
		return a
	}
	return DefaultMailAddr
}

// MaxBytes is the largest message accepted.
func (m Mail) MaxBytes() int64 {
	mb := m.MaxSizeMB
	if mb <= 0 {
		mb = DefaultMailSizeMB
	}
	return int64(min(mb, MailMaxSizeMB)) << 20
}

// validateMail checks the mail.* keys of a YAML document being written.
func validateMail(k *koanf.Koanf) error {
	if d := strings.TrimSpace(k.String("mail.domain")); d != "" {
		if _, err := NormalizeMailDomain(d); err != nil {
			return fmt.Errorf("mail.domain: %w", err)
		}
	}
	if a := strings.TrimSpace(k.String("mail.addr")); a != "" {
		_, port, err := net.SplitHostPort(a)
		if err != nil {
			return fmt.Errorf("mail.addr must be host:port or :port, like :2525")
		}
		// Port 0 is "any free port", which only a test wants.
		if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("mail.addr: port %q is not 1-65535", port)
		}
	}
	if raw := strings.TrimSpace(k.String("mail.max_size_mb")); raw != "" {
		if n, err := strconv.Atoi(raw); err != nil || n < 1 || n > MailMaxSizeMB {
			return fmt.Errorf("mail.max_size_mb must be a whole number from 1 to %d", MailMaxSizeMB)
		}
	}
	cert, key := strings.TrimSpace(k.String("mail.tls_cert")), strings.TrimSpace(k.String("mail.tls_key"))
	if (cert == "") != (key == "") {
		return errors.New("mail.tls_cert and mail.tls_key go together: set both or neither")
	}
	return nil
}
