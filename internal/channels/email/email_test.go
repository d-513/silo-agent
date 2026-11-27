package email

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-msgauth/dkim"

	"silo.agent/internal/builtin/email/emailtest"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// --- fakes ---

// fakeHost plays the Control Plane for the adapter.
type fakeHost struct {
	mu        sync.Mutex
	current   *db.Channel
	delivered []channels.Inbound
	states    []channels.State
}

func (h *fakeHost) GetSecret(_, _ string) (string, error) { return "", errors.New("unused") }
func (h *fakeHost) DeliverInbound(_ context.Context, _ *db.Channel, in channels.Inbound) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, in)
	return nil
}
func (h *fakeHost) PublishState(_ string, st channels.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.states = append(h.states, st)
}
func (h *fakeHost) CurrentChannel(string) (*db.Channel, bool) { return h.current, h.current != nil }
func (h *fakeHost) DataDir() string                           { return "" }
func (h *fakeHost) DatabaseURL() string                       { return "" }

func (h *fakeHost) inbound() []channels.Inbound {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.delivered)
}

func (h *fakeHost) said(part string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, st := range h.states {
		if strings.Contains(st.Message, part) {
			return true
		}
	}
	return false
}

// signer signs mail for domains whose keys its DNS publishes, so the DKIM
// check runs for real and asks no network.
type signer struct {
	t   *testing.T
	mu  sync.Mutex
	txt map[string][]string
	key map[string]ed25519.PrivateKey
}

func newSigner(t *testing.T) *signer {
	return &signer{t: t, txt: map[string][]string{}, key: map[string]ed25519.PrivateKey{}}
}

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (s *signer) LookupTXT(_ context.Context, name string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.txt[strings.TrimSuffix(strings.ToLower(name), ".")]; ok {
		return v, nil
	}
	return nil, notFound(name)
}
func (*signer) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	return nil, notFound(name)
}
func (*signer) LookupIPAddr(_ context.Context, name string) ([]net.IPAddr, error) {
	return nil, notFound(name)
}
func (*signer) LookupAddr(_ context.Context, addr string) ([]string, error) {
	return nil, notFound(addr)
}

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// sign returns msg signed by domain.
func (s *signer) sign(domain, msg string) string {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	priv, ok := s.key[domain]
	if !ok {
		pub, p, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			s.t.Fatal(err)
		}
		priv, s.key[domain] = p, p
		s.txt["s1._domainkey."+domain] = []string{"v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub)}
	}
	var out bytes.Buffer
	if err := dkim.Sign(&out, strings.NewReader(crlf(msg)), &dkim.SignOptions{Domain: domain, Selector: "s1", Signer: priv}); err != nil {
		s.t.Fatal(err)
	}
	return out.String()
}

const (
	ada = "ada@example.org"
	bot = emailtest.Address
)

// rig is one channel on the test servers.
type rig struct {
	t    *testing.T
	a    *Adapter
	srv  *emailtest.Server
	host *fakeHost
	ch   *db.Channel
	cfg  channels.Config
	dns  *signer
}

func newRig(t *testing.T, extra map[string]string) *rig {
	t.Helper()
	srv := emailtest.Start(t)
	dns := newSigner(t)
	a := New()
	a.plain, a.dns = true, dns
	a.retry = 20 * time.Millisecond
	// The in-memory server never reports mail that came just before an IDLE
	// began, so the tests lean on the look the adapter takes anyway.
	a.idle = 150 * time.Millisecond
	ch := &db.Channel{ID: "ch1", Name: "Ada by mail", Adapter: "email", ExternalID: ada}
	values := map[string]string{
		"imap_host": "127.0.0.1", "imap_port": strconv.Itoa(srv.IMAPPort),
		"smtp_host": "127.0.0.1", "smtp_port": strconv.Itoa(srv.SMTPPort),
		"username": bot, "password": emailtest.Password,
	}
	for k, v := range extra {
		values[k] = v
	}
	return &rig{t: t, a: a, srv: srv, host: &fakeHost{current: ch}, ch: ch, cfg: channels.Config{Values: values}, dns: dns}
}

// start runs the channel's worker until the test ends, and returns once the
// mailbox is open.
func (r *rig) start() {
	r.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.a.Start(ctx, r.ch, r.cfg, r.host) }()
	r.t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			r.t.Error("Start did not return after its context ended")
		}
	})
	r.wait("the mailbox to open", func() bool { return r.host.said("Connected as " + bot) })
}

func (r *rig) wait(what string, ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.host.mu.Lock()
			defer r.host.mu.Unlock()
			r.t.Fatalf("timed out waiting for %s (states: %+v)", what, r.host.states)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// arrive puts a message in the inbox, signed by its sender's domain.
func (r *rig) arrive(domain, msg string) uint32 {
	r.t.Helper()
	return r.srv.Deliver(r.t, "INBOX", r.dns.sign(domain, msg))
}

// sentinel delivers a plain signed mail from the bound address and waits for
// it: whatever arrived before it has been decided by then.
func (r *rig) sentinel() channels.Inbound {
	r.t.Helper()
	before := len(r.host.inbound())
	r.arrive("example.org", "From: Ada <ada@example.org>\nTo: me@example.com\nSubject: Re: done\nIn-Reply-To: <x@example.com>\nMessage-ID: <sentinel-"+strconv.Itoa(before)+"@example.org>\n\nsentinel\n")
	r.wait("the sentinel mail", func() bool { return len(r.host.inbound()) > before })
	got := r.host.inbound()
	last := got[len(got)-1]
	if last.Text != "sentinel" {
		r.t.Fatalf("delivered %q where only the sentinel was expected (all: %+v)", last.Text, got)
	}
	if len(got) != before+1 {
		r.t.Fatalf("delivered %d messages, want only the sentinel", len(got)-before)
	}
	return last
}

// sent is one captured mail taken apart.
type sent struct {
	header mail.Header
	parts  map[string]string // content type -> body
	files  map[string][]byte // file name -> bytes
}

func (r *rig) lastSent() sent {
	r.t.Helper()
	all := r.srv.Sent()
	if len(all) == 0 {
		r.t.Fatal("nothing was sent")
	}
	m := all[len(all)-1]
	if m.From != bot || len(m.To) != 1 || m.To[0] != ada {
		r.t.Fatalf("envelope %s -> %v, want %s -> %s", m.From, m.To, bot, ada)
	}
	mr, err := mail.CreateReader(bytes.NewReader(m.Data))
	if err != nil {
		r.t.Fatal(err)
	}
	out := sent{header: mr.Header, parts: map[string]string{}, files: map[string][]byte{}}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			r.t.Fatal(err)
		}
		body, _ := io.ReadAll(p.Body)
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			out.parts[ct] = string(body)
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			out.files[name] = body
		}
	}
	return out
}

// --- inbound ---

func TestStartDeliversOnlyNewMailFromTheBoundAddress(t *testing.T) {
	r := newRig(t, nil)
	r.arrive("example.org", "From: Ada <ada@example.org>\nTo: me@example.com\nSubject: Old\nMessage-ID: <old@example.org>\n\nThis was here before the channel started.\n")
	r.start()

	r.arrive("example.net", "From: Bob <bob@example.net>\nTo: me@example.com\nSubject: Hello\nMessage-ID: <bob@example.net>\n\nNot the bound address.\n")
	uid := r.arrive("example.org", strings.Join([]string{
		"From: Ada Lovelace <Ada@Example.org>",
		"To: me@example.com",
		"Subject: Lunch",
		"Message-ID: <m1@example.org>",
		"Date: Mon, 02 Nov 2026 10:00:00 +0000",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="b"`,
		"",
		"--b",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"See you at noon.",
		"--b",
		"Content-Type: application/octet-stream",
		`Content-Disposition: attachment; filename="../../notes.bin"`,
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString([]byte("the notes")),
		"--b--",
		"",
	}, "\n"))
	r.wait("the new mail", func() bool { return len(r.host.inbound()) == 1 })

	in := r.host.inbound()[0]
	if in.Text != "Subject: Lunch\n\nSee you at noon." {
		t.Errorf("text = %q", in.Text)
	}
	if in.ExternalID != ada || in.AuthorID != ada || in.Author != "Ada Lovelace" {
		t.Errorf("external %q author %q (%q)", in.ExternalID, in.Author, in.AuthorID)
	}
	an, ok := readAnchor(in.ReplyTo)
	if !ok || an.ID != "m1@example.org" || an.Subject != "Lunch" {
		t.Errorf("anchor = %+v from %q", an, in.ReplyTo)
	}
	if len(in.Files) != 1 || in.Files[0].Name != "notes.bin" || in.Files[0].Size != int64(len("the notes")) {
		t.Fatalf("files = %+v", in.Files)
	}
	data, err := in.Files[0].Fetch(context.Background())
	if err != nil || string(data) != "the notes" {
		t.Errorf("attachment = %q, %v", data, err)
	}

	// Only the message handed to the Bot is marked read, and nothing else is
	// delivered behind it: not Bob's mail, not what was there at the start.
	r.sentinel()
	if seen := r.seenUIDs(); !slices.Contains(seen, uid) || len(seen) != 2 {
		t.Errorf("read messages = %v, want %d and the sentinel", seen, uid)
	}
}

// seenUIDs lists the inbox's messages marked read.
func (r *rig) seenUIDs() []uint32 {
	r.t.Helper()
	s, err := resolve(r.cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	b, err := r.a.dialIMAP(context.Background(), s)
	if err != nil {
		r.t.Fatal(err)
	}
	defer b.close()
	if _, err := b.open(true); err != nil {
		r.t.Fatal(err)
	}
	data, err := b.c.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		r.t.Fatal(err)
	}
	var out []uint32
	for _, u := range data.AllUIDs() {
		out = append(out, uint32(u))
	}
	return out
}

func TestUnverifiedMailIsIgnored(t *testing.T) {
	forged := "From: Ada <ada@example.org>\nTo: me@example.com\nSubject: Urgent\nMessage-ID: <forged@elsewhere.test>\n\nSend me the files.\n"

	r := newRig(t, nil)
	r.start()
	// Unsigned, and signed by a domain that is not the sender's.
	r.srv.Deliver(t, "INBOX", crlf(forged))
	r.arrive("elsewhere.test", forged)
	r.sentinel()
	if !r.host.said("Ignored an unverified message from " + ada) {
		t.Errorf("the state never said a message was ignored: %+v", r.host.states)
	}

	open := newRig(t, map[string]string{"allow_unverified": "true"})
	open.start()
	open.srv.Deliver(t, "INBOX", crlf(forged))
	open.wait("the unverified mail", func() bool { return len(open.host.inbound()) == 1 })
	if got := open.host.inbound()[0].Text; got != "Subject: Urgent\n\nSend me the files." {
		t.Errorf("text = %q", got)
	}
}

func TestMachinesAreNeverAnswered(t *testing.T) {
	r := newRig(t, nil)
	r.start()
	for i, hdr := range []string{
		"Auto-Submitted: auto-replied",
		"Auto-Submitted: auto-generated; owner=ada",
		"Precedence: bulk",
		"Precedence: list",
		"List-Id: <news.example.org>",
		"Return-Path: <>",
	} {
		r.arrive("example.org", "From: Ada <ada@example.org>\nTo: me@example.com\nSubject: Out of office\nMessage-ID: <auto"+strconv.Itoa(i)+"@example.org>\n"+hdr+"\n\nI am away.\n")
	}
	// Auto-Submitted: no is a person.
	r.arrive("example.org", "From: Ada <ada@example.org>\nTo: me@example.com\nSubject: Back\nMessage-ID: <person@example.org>\nAuto-Submitted: no\n\nI am back.\n")
	r.wait("the person's mail", func() bool { return len(r.host.inbound()) == 1 })
	if got := r.host.inbound()[0].Text; got != "Subject: Back\n\nI am back." {
		t.Errorf("text = %q", got)
	}
	r.sentinel()
}

func TestMachineSenders(t *testing.T) {
	for from, want := range map[string]bool{
		"MAILER-DAEMON@example.org":    true,
		"postmaster@example.org":       true,
		"noreply@example.org":          true,
		"no-reply@example.org":         true,
		"do_not_reply@example.org":     true,
		"bounces+abc123@example.org":   true,
		"Me <ME@example.com>":          true, // the Bot's own address
		"ada@example.org":              false,
		"Norah Eply <nor@example.org>": false,
	} {
		h := headerOf([]byte(crlf("From: " + from + "\n\n")))
		if got := machine(h, bot); got != want {
			t.Errorf("machine(%s) = %v, want %v", from, got, want)
		}
	}
}

func TestQuotedTailIsCut(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"reply over a quote": {
			"Yes, noon works.\n\nOn Mon, 2 Nov 2026 at 10:00, Silo <me@example.com> wrote:\n> Is noon fine?\n>\n> > Lunch?\n",
			"Yes, noon works.",
		},
		"wrapped attribution": {
			"Fine.\n\nOn Mon, 2 Nov 2026 at 10:00, Silo\n<me@example.com> wrote:\n> Is noon fine?\n",
			"Fine.",
		},
		"quote without attribution": {
			"Fine.\n> Is noon fine?",
			"Fine.",
		},
		"answers between the quotes stay": {
			"> Is noon fine?\nYes.\n> And Tuesday?\nNo.\n",
			"> Is noon fine?\nYes.\n> And Tuesday?\nNo.",
		},
		"a line ending in a colon is not an attribution": {
			"Here is the list:\n> one\n",
			"Here is the list:",
		},
		"only a quote is kept": {
			"> forwarded for you\n",
			"> forwarded for you\n",
		},
		"nothing quoted": {
			"Hello.\n\nBye.\n",
			"Hello.\n\nBye.",
		},
	} {
		if got := cutQuoted(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestRejectedLoginIsPermanentAndKeepsThePassword(t *testing.T) {
	r := newRig(t, map[string]string{"password": "wrong-secret"})
	st, err := r.a.Validate(context.Background(), r.ch, r.cfg)
	if err == nil || st.Kind != channels.StateError {
		t.Fatalf("Validate = %+v, %v; want a refusal", st, err)
	}
	if strings.Contains(err.Error(), "wrong-secret") || !strings.Contains(err.Error(), "refused the username or password") {
		t.Errorf("error = %q", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.a.Start(ctx, r.ch, r.cfg, r.host); !channels.IsPermanent(err) {
		t.Errorf("Start = %v, want a permanent error", err)
	}
}

// --- outbound ---

func TestSendIsAThreadedReply(t *testing.T) {
	r := newRig(t, nil)
	reply := anchor{ID: "m2@example.org", Refs: []string{"m1@example.com"}, Subject: "Lunch"}.String()
	err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{
		ExternalID: ada,
		Text:       "Noon is **fine**.\n\n- bring the notes",
		ReplyTo:    reply,
		Files:      []channels.Attachment{{Name: "plan.pdf", Mime: "application/pdf", Data: []byte("%PDF-plan")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := r.lastSent()
	for k, want := range map[string]string{
		"Subject":                  "Re: Lunch",
		"In-Reply-To":              "<m2@example.org>",
		"References":               "<m1@example.com> <m2@example.org>",
		"Auto-Submitted":           "auto-replied",
		"X-Auto-Response-Suppress": "All",
		"To":                       "<" + ada + ">",
		"From":                     "<" + bot + ">",
	} {
		if got := m.header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if id, _ := m.header.MessageID(); !strings.HasSuffix(id, "@example.com") {
		t.Errorf("Message-ID = %q, want one at the from address's domain", id)
	}
	if d, err := m.header.Date(); err != nil || time.Since(d) > time.Minute {
		t.Errorf("Date = %v, %v", d, err)
	}
	if got := m.parts["text/plain"]; got != "Noon is **fine**.\r\n\r\n- bring the notes" {
		t.Errorf("plain part = %q", got)
	}
	if got := m.parts["text/html"]; !strings.Contains(got, "<strong>fine</strong>") || !strings.Contains(got, "<li>bring the notes</li>") {
		t.Errorf("html part = %q", got)
	}
	if got := m.files["plan.pdf"]; string(got) != "%PDF-plan" {
		t.Errorf("attachment = %q (files %v)", got, m.files)
	}

	// A subject that already says Re: is not given another.
	again := anchor{ID: "m3@example.org", Subject: "RE: Lunch"}.String()
	if err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{Text: "Ok.", ReplyTo: again}); err != nil {
		t.Fatal(err)
	}
	m = r.lastSent()
	if got := m.header.Get("Subject"); got != "RE: Lunch" {
		t.Errorf("subject = %q", got)
	}
}

func TestSendWritesFirst(t *testing.T) {
	r := newRig(t, nil)
	if err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{Text: "## The **report** is ready\n\nIt is attached to nothing."}); err != nil {
		t.Fatal(err)
	}
	m := r.lastSent()
	if got := m.header.Get("Subject"); got != "The report is ready" {
		t.Errorf("subject = %q", got)
	}
	if got := m.header.Get("Auto-Submitted"); got != "auto-generated" {
		t.Errorf("Auto-Submitted = %q", got)
	}
	if m.header.Has("In-Reply-To") || m.header.Has("References") {
		t.Errorf("a first message names a thread: %v", m.header)
	}
	if ct, _, _ := m.header.ContentType(); ct != "multipart/alternative" {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(m.parts["text/html"], "<h2>") || !strings.Contains(m.parts["text/plain"], "## The") {
		t.Errorf("parts = %v", m.parts)
	}

	// Files alone are named after the channel.
	if err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{Files: []channels.Attachment{{Name: "a.txt", Data: []byte("a")}}}); err != nil {
		t.Fatal(err)
	}
	m = r.lastSent()
	if got := m.header.Get("Subject"); got != "Ada by mail" {
		t.Errorf("subject = %q", got)
	}

	if err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{ExternalID: "bob@example.net", Text: "hi"}); err == nil {
		t.Error("a mail to another address was sent")
	}
}

func TestAStrangersAnchorCannotWriteHeaders(t *testing.T) {
	r := newRig(t, nil)
	bad := anchor{ID: "x@y>\r\nBcc: eve@example.net", Refs: []string{"ok@example.org", "a b"}, Subject: "Hi\r\nBcc: eve@example.net"}.String()
	if err := r.a.Send(context.Background(), r.ch, r.cfg, channels.Outbound{Text: "Ok.", ReplyTo: bad}); err != nil {
		t.Fatal(err)
	}
	m := r.lastSent()
	if m.header.Has("Bcc") || m.header.Has("In-Reply-To") {
		t.Errorf("headers = %v", m.header)
	}
	if got := m.header.Get("Subject"); got != "Re: Hi Bcc: eve@example.net" {
		t.Errorf("subject = %q", got)
	}
}

// --- history and setup ---

func TestHistoryReadsTheBoundAddressOldestFirst(t *testing.T) {
	r := newRig(t, nil)
	r.arrive("example.org", "From: Ada <ada@example.org>\nSubject: First\nMessage-ID: <h1@example.org>\nDate: Mon, 02 Nov 2026 10:00:00 +0000\n\nOne.\n")
	r.arrive("example.net", "From: Bob <bob@example.net>\nSubject: Other\nMessage-ID: <h2@example.net>\n\nNot Ada.\n")
	r.srv.Deliver(t, "INBOX", crlf("From: Ada <ada@example.org>\nSubject: Forged\nMessage-ID: <h3@example.org>\n\nUnsigned.\n"))
	r.arrive("example.org", "From: Ada <ada@example.org>\nSubject: Re: First\nIn-Reply-To: <x@example.com>\nMessage-ID: <h4@example.org>\n\nTwo.\n\n> One.\n")

	got, err := r.a.History(context.Background(), r.ch, r.cfg, ada, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "Subject: First\n\nOne." || got[1].Text != "Two." || got[0].Author != "Ada" || got[0].Out {
		t.Fatalf("history = %+v", got)
	}
	if want := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC); !got[0].At.Equal(want) {
		t.Errorf("date = %v", got[0].At)
	}
	if got, err := r.a.History(context.Background(), r.ch, r.cfg, ada, 1); err != nil || len(got) != 1 || got[0].Text != "Two." {
		t.Errorf("newest one = %+v, %v", got, err)
	}
}

func TestPickListsSendersAndRefreshChecksBothServers(t *testing.T) {
	r := newRig(t, nil)
	r.srv.Deliver(t, "INBOX", crlf("From: Ada <ada@example.org>\nSubject: a\n\n1\n"))
	r.srv.Deliver(t, "INBOX", crlf("From: News <noreply@shop.test>\nSubject: b\n\n2\n"))
	r.srv.Deliver(t, "INBOX", crlf("From: bob@example.net\nSubject: c\n\n3\n"))
	r.srv.Deliver(t, "INBOX", crlf("From: Ada <ADA@example.org>\nSubject: d\n\n4\n"))

	st, err := r.a.Action(context.Background(), r.ch, r.cfg, "list_senders", nil)
	if err != nil || st.Kind != channels.StateSelect {
		t.Fatalf("list_senders = %+v, %v", st, err)
	}
	want := []channels.Option{{Value: ada, Label: "Ada <" + ada + ">"}, {Value: "bob@example.net", Label: "bob@example.net"}}
	if !slices.Equal(st.Options, want) {
		t.Errorf("options = %+v", st.Options)
	}

	st, err = r.a.Action(context.Background(), r.ch, r.cfg, "refresh", nil)
	if err != nil || st.Kind != channels.StateInfo || st.Message != "Connected as "+bot {
		t.Errorf("refresh = %+v, %v", st, err)
	}
	if len(r.srv.Sent()) != 0 {
		t.Error("checking the connection sent mail")
	}
}

func TestTypedTargets(t *testing.T) {
	for in, want := range map[string]string{
		"Ada@Example.org":                   ada,
		"Ada Lovelace <ada@example.org>":    ada,
		" mailto:ada@example.org?subject=x": ada,
		"not an address":                    "",
		"":                                  "",
	} {
		if got := targetAddr(in); got != want {
			t.Errorf("targetAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

// cuttable forwards to the IMAP server and can drop every connection, as a
// network or a restarting server does.
type cuttable struct {
	ln    net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func newCuttable(t *testing.T, port int) *cuttable {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &cuttable{ln: ln}
	t.Cleanup(func() { ln.Close(); c.cut() })
	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
			if err != nil {
				in.Close()
				continue
			}
			c.mu.Lock()
			c.conns = append(c.conns, in, out)
			c.mu.Unlock()
			go func() { _, _ = io.Copy(out, in); out.Close() }()
			go func() { _, _ = io.Copy(in, out); in.Close() }()
		}
	}()
	return c
}

func (c *cuttable) cut() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		conn.Close()
	}
	c.conns = nil
}

func TestADroppedConnectionIsReconnected(t *testing.T) {
	r := newRig(t, nil)
	link := newCuttable(t, r.srv.IMAPPort)
	r.cfg.Values["imap_port"] = strconv.Itoa(link.ln.Addr().(*net.TCPAddr).Port)
	r.start()

	link.cut()
	r.wait("the loss to show", func() bool { return r.host.said("Lost the mailbox") })
	// Mail that comes while the connection is down is read when it is back.
	r.sentinel()
	r.wait("the state to recover", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		return r.host.states[len(r.host.states)-1].Kind == channels.StateInfo
	})
}
