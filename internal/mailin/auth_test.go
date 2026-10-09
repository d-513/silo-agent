package mailin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"
)

// fakeDNS answers TXT lookups from a map; everything else does not exist.
type fakeDNS struct{ txt map[string][]string }

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := f.txt[strings.TrimSuffix(strings.ToLower(name), ".")]; ok {
		return v, nil
	}
	return nil, notFound(name)
}
func (fakeDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return nil, notFound(name)
}
func (fakeDNS) LookupIPAddr(_ context.Context, name string) ([]net.IPAddr, error) {
	return nil, notFound(name)
}
func (fakeDNS) LookupAddr(_ context.Context, addr string) ([]string, error) {
	return nil, notFound(addr)
}

// signed returns msg signed by domain, and the DNS that publishes its key.
func signed(t *testing.T, domain, msg string) ([]byte, fakeDNS) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := dkim.Sign(&out, bytes.NewReader(crlf(msg)), &dkim.SignOptions{Domain: domain, Selector: "s1", Signer: priv}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), fakeDNS{txt: map[string][]string{
		"s1._domainkey." + domain: {"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)},
	}}
}

const fromAda = "From: Ada <ada@example.com>\nTo: a@bots.test\nSubject: Hi\n\nHello.\n"

func verdict(dns Resolver, env Envelope, raw []byte) Verdict {
	return Authenticate(context.Background(), dns, env, raw, Parse(raw).FromAddr)
}

func TestAuthenticateDKIMSignedByTheFromDomain(t *testing.T) {
	raw, dns := signed(t, "example.com", fromAda)
	// Our own Received line sits above the signature, as it does on arrival.
	raw = append(Envelope{Helo: "mx.example.com", Remote: net.ParseIP("203.0.113.5"), By: "bots.test", At: time.Now()}.Received(), raw...)
	v := verdict(dns, Envelope{From: "bounce@mailer.example.net", Remote: net.ParseIP("203.0.113.5")}, raw)
	if !v.Verified || !strings.Contains(v.Detail, "dkim=pass (example.com)") {
		t.Fatalf("verdict = %+v", v)
	}
}

// A subdomain of the signing organisation counts: mail from
// news.example.com signed by example.com is example.com's.
func TestAuthenticateDKIMAlignsOnTheOrganisation(t *testing.T) {
	raw, dns := signed(t, "example.com", strings.Replace(fromAda, "ada@example.com", "ada@news.example.com", 1))
	if v := verdict(dns, Envelope{}, raw); !v.Verified {
		t.Fatalf("verdict = %+v", v)
	}
	// But two customers of one registry suffix are strangers.
	raw, dns = signed(t, "evil.co.uk", strings.Replace(fromAda, "ada@example.com", "ada@bank.co.uk", 1))
	if v := verdict(dns, Envelope{}, raw); v.Verified {
		t.Fatalf("evil.co.uk vouched for bank.co.uk: %+v", v)
	}
}

// Anyone can sign with their own domain: a valid signature says nothing about
// a From address in somebody else's.
func TestAuthenticateDKIMFromAnotherDomainDoesNotVerify(t *testing.T) {
	raw, dns := signed(t, "attacker.example.net", fromAda)
	v := verdict(dns, Envelope{}, raw)
	if v.Verified {
		t.Fatalf("verdict = %+v", v)
	}
	if !strings.Contains(v.Detail, "attacker.example.net") {
		t.Fatalf("detail does not name who signed: %q", v.Detail)
	}
}

func TestAuthenticateTamperedMessageDoesNotVerify(t *testing.T) {
	raw, dns := signed(t, "example.com", fromAda)
	raw = bytes.Replace(raw, []byte("Hello."), []byte("Send money."), 1)
	if v := verdict(dns, Envelope{}, raw); v.Verified || !strings.Contains(v.Detail, "dkim=fail") {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestAuthenticateSPF(t *testing.T) {
	dns := fakeDNS{txt: map[string][]string{
		"example.com":        {"v=spf1 ip4:203.0.113.5 -all"},
		"mailer.example.net": {"v=spf1 ip4:203.0.113.5 -all"},
	}}
	raw := crlf(fromAda)
	ok := Envelope{From: "ada@example.com", Remote: net.ParseIP("203.0.113.5"), Helo: "mx.example.com"}
	if v := verdict(dns, ok, raw); !v.Verified || !strings.Contains(v.Detail, "spf=pass (example.com)") {
		t.Fatalf("allowed address: %+v", v)
	}
	other := ok
	other.Remote = net.ParseIP("198.51.100.9")
	if v := verdict(dns, other, raw); v.Verified || !strings.Contains(v.Detail, "spf=fail") {
		t.Fatalf("another address: %+v", v)
	}
	// SPF vouches for the envelope sender. When that is a different
	// organisation than the From header, the header is still unproven.
	relay := ok
	relay.From = "bounce@mailer.example.net"
	if v := verdict(dns, relay, raw); v.Verified {
		t.Fatalf("spf for another domain verified the From header: %+v", v)
	}
}

func TestAuthenticateUnsignedIsUnverified(t *testing.T) {
	v := verdict(fakeDNS{}, Envelope{From: "ada@example.com", Remote: net.ParseIP("203.0.113.5")}, crlf(fromAda))
	if v.Verified || !strings.Contains(v.Detail, "dkim=none") || !strings.Contains(v.Detail, "spf=none") {
		t.Fatalf("verdict = %+v", v)
	}
}

// Two From addresses, or none, is how a forged header hides: nothing verifies.
func TestAuthenticateNeedsExactlyOneFromAddress(t *testing.T) {
	two := strings.Replace(fromAda, "From: Ada <ada@example.com>", "From: ada@example.com, boss@example.com", 1)
	raw, dns := signed(t, "example.com", two)
	if m := Parse(raw); m.FromAddr != "" {
		t.Fatalf("FromAddr %q for two authors", m.FromAddr)
	}
	if v := verdict(dns, Envelope{}, raw); v.Verified {
		t.Fatalf("verdict = %+v", v)
	}
}
