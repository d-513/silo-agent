package mailin

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-smtp"
)

type delivery struct {
	env Envelope
	raw string
}

// fakeBox is a Handler with a fixed set of mailboxes.
type fakeBox struct {
	mu    sync.Mutex
	boxes map[string]bool
	got   []delivery
	fail  error
}

func (f *fakeBox) Accept(rcpt string) bool { return f.boxes[rcpt] }

func (f *fakeBox) Deliver(_ context.Context, env Envelope, raw []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, delivery{env, string(raw)})
	return nil
}

func (f *fakeBox) deliveries() []delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]delivery(nil), f.got...)
}

func startServer(t *testing.T, cfg Config, boxes ...string) (*Server, *fakeBox) {
	t.Helper()
	h := &fakeBox{boxes: map[string]bool{}}
	for _, b := range boxes {
		h.boxes[b] = true
	}
	cfg.Addr = "127.0.0.1:0"
	if cfg.Domain == "" {
		cfg.Domain = "bots.test"
	}
	s, err := Listen(cfg, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, h
}

func dial(t *testing.T, s *Server) *smtp.Client {
	t.Helper()
	c, err := smtp.Dial(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Hello("sender.example"); err != nil {
		t.Fatal(err)
	}
	return c
}

const hello = "From: Ada <ada@example.com>\r\nTo: quiet-amber-heron@bots.test\r\nSubject: Hi\r\n\r\nHello there.\r\n"

// send runs one SMTP transaction and returns the first error.
func send(c *smtp.Client, from string, rcpts []string, body string) error {
	if err := c.Mail(from, nil); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r, nil); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	return w.Close()
}

func smtpCode(err error) int {
	var se *smtp.SMTPError
	if errors.As(err, &se) {
		return se.Code
	}
	return 0
}

func TestServerDeliversToAKnownMailbox(t *testing.T) {
	s, h := startServer(t, Config{}, "quiet-amber-heron@bots.test")
	c := dial(t, s)
	// The local part and domain are matched without regard to case.
	if err := send(c, "ada@example.com", []string{"Quiet-Amber-Heron@Bots.Test"}, hello); err != nil {
		t.Fatal(err)
	}
	got := h.deliveries()
	if len(got) != 1 {
		t.Fatalf("%d deliveries", len(got))
	}
	d := got[0]
	if d.env.From != "ada@example.com" || d.env.To != "quiet-amber-heron@bots.test" || d.env.Helo != "sender.example" || d.env.TLS {
		t.Fatalf("envelope = %+v", d.env)
	}
	if d.env.Remote == nil || !d.env.Remote.IsLoopback() {
		t.Fatalf("remote = %v", d.env.Remote)
	}
	// The message is handed over exactly as sent; the trace line a store
	// puts on top of it comes from the envelope.
	if d.raw != hello {
		t.Fatalf("message changed:\n%q", d.raw)
	}
	if rec := string(d.env.Received()); !strings.HasPrefix(rec, "Received: from sender.example (127.0.0.1)") || !strings.Contains(rec, "by bots.test with ESMTP;") || !strings.HasSuffix(rec, "\r\n") {
		t.Fatalf("Received = %q", rec)
	}
}

// Receive-only: an unknown mailbox is refused inside the session, so there is
// never a bounce to write (and none to forge a victim's address into).
func TestServerRefusesUnknownRecipients(t *testing.T) {
	s, h := startServer(t, Config{}, "quiet-amber-heron@bots.test")
	c := dial(t, s)
	for _, rcpt := range []string{"nobody@bots.test", "quiet-amber-heron@elsewhere.example", "ada@example.com"} {
		err := send(c, "ada@example.com", []string{rcpt}, hello)
		if smtpCode(err) != 550 {
			t.Fatalf("%s: %v, want 550", rcpt, err)
		}
		_ = c.Reset()
	}
	if n := len(h.deliveries()); n != 0 {
		t.Fatalf("%d deliveries", n)
	}
}

func TestServerDeliversOneCopyPerRecipient(t *testing.T) {
	s, h := startServer(t, Config{}, "a@bots.test", "b@bots.test")
	c := dial(t, s)
	// The same mailbox named twice is still one copy.
	if err := send(c, "ada@example.com", []string{"a@bots.test", "b@bots.test", "A@bots.test"}, hello); err != nil {
		t.Fatal(err)
	}
	got := h.deliveries()
	if len(got) != 2 || got[0].env.To != "a@bots.test" || got[1].env.To != "b@bots.test" {
		t.Fatalf("deliveries = %+v", got)
	}
}

func TestServerRefusesOversizeMail(t *testing.T) {
	s, h := startServer(t, Config{MaxBytes: 2048}, "a@bots.test")
	c := dial(t, s)
	big := hello + strings.Repeat("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\r\n", 64)
	if err := send(c, "ada@example.com", []string{"a@bots.test"}, big); smtpCode(err) != 552 {
		t.Fatalf("oversize: %v, want 552", err)
	}
	if n := len(h.deliveries()); n != 0 {
		t.Fatalf("%d deliveries", n)
	}
}

// A store that cannot take the message now answers 4xx: the sender keeps it
// and tries again, so a restart or a database blip loses nothing.
func TestServerAnswersTemporaryFailureWhenDeliveryFails(t *testing.T) {
	s, h := startServer(t, Config{}, "a@bots.test")
	h.fail = errors.New("database is down")
	c := dial(t, s)
	err := send(c, "ada@example.com", []string{"a@bots.test"}, hello)
	if code := smtpCode(err); code/100 != 4 {
		t.Fatalf("%v, want a 4xx", err)
	}
	if strings.Contains(err.Error(), "database") {
		t.Fatalf("internal error leaked to the sender: %v", err)
	}
}

func TestServerNeverOffersAuthAndOffersStartTLS(t *testing.T) {
	cert, err := SelfSigned("bots.test")
	if err != nil {
		t.Fatal(err)
	}
	s, h := startServer(t, Config{TLS: &tls.Config{Certificates: []tls.Certificate{cert}}}, "a@bots.test")
	c := dial(t, s)
	if ok, _ := c.Extension("AUTH"); ok {
		t.Fatal("AUTH offered: there is nothing to sign in to")
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		t.Fatal("STARTTLS not offered")
	}
	secure, err := smtp.DialStartTLS(s.Addr(), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	if err := send(secure, "ada@example.com", []string{"a@bots.test"}, hello); err != nil {
		t.Fatal(err)
	}
	got := h.deliveries()
	if len(got) != 1 || !got[0].env.TLS || !strings.Contains(string(got[0].env.Received()), "with ESMTPS") {
		t.Fatalf("deliveries = %+v", got)
	}
}

// Addresses are guessable only by trying them, so one client that keeps
// naming mailboxes that do not exist is cut off.
func TestServerCutsOffAddressProbing(t *testing.T) {
	s, _ := startServer(t, Config{}, "a@bots.test")
	c := dial(t, s)
	if err := c.Mail("probe@example.com", nil); err != nil {
		t.Fatal(err)
	}
	for i := range probeLimit {
		if code := smtpCode(c.Rcpt("guess-"+string(rune('a'+i))+"@bots.test", nil)); code != 550 {
			t.Fatalf("guess %d: code %d", i, code)
		}
	}
	// Even the real one is refused now, with a temporary code.
	if code := smtpCode(c.Rcpt("a@bots.test", nil)); code/100 != 4 {
		t.Fatalf("after the limit: code %d, want 4xx", code)
	}
}

func TestListenFailsOnABusyPort(t *testing.T) {
	s, _ := startServer(t, Config{})
	if _, err := Listen(Config{Addr: s.Addr(), Domain: "bots.test"}, &fakeBox{}); err == nil {
		t.Fatal("a second listener on the same port started")
	}
}
