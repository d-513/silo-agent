// Package email is the built-in Email adapter: a Bot holding a conversation by
// mail with one correspondent, from a mailbox its owner gives it. The Control
// Plane reads the mailbox over IMAP and answers over SMTP; the password stays
// here.
//
// It is neither the Email connector (a tool for working an inbox) nor the
// Bot's receive-only Mail address. A channel is bound to exactly one address:
// a mail from it starts or continues the conversation, and the Bot's answers
// go back as replies in the same thread. Inbound is live-only: nothing that
// was in the mailbox before the channel started is ever handed on.
//
// Two rules keep it safe. A From header is forgeable, so a mail is believed
// only when its From domain signed it (DKIM), unless the owner turns that off.
// And a machine is never answered (auto-replies, lists, bounces, the Bot's own
// address), so two auto-responders cannot write to each other forever.
package email

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	netmail "net/mail"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/mailin"
	"silo.agent/internal/netguard"
)

const (
	// opTimeout bounds one burst of commands on a connection: a server that
	// stops answering must not hang the channel.
	opTimeout = 2 * time.Minute
	// idleFor is how long one IDLE waits before the mailbox is looked at
	// anyway: some servers offer IDLE and then say nothing.
	idleFor = 5 * time.Minute
	// pollEvery is the wait between looks at a mailbox whose server has no IDLE.
	pollEvery = 30 * time.Second
	// maxMessage is the largest mail read; a larger one is ignored and the
	// state says so.
	maxMessage = 40 << 20
	// pickFrom is how many of the newest messages the sender picker reads.
	pickFrom = 50
	// historyMax caps one history read: every message is downloaded whole.
	historyMax = 50
	// retryMin and retryMax bound the wait before reconnecting.
	retryMin = 5 * time.Second
	retryMax = 5 * time.Minute
)

//go:embed email.svg
var emailLogo []byte

//go:embed GUIDE.md
var emailGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(emailLogo)
}

// Adapter keeps nothing between calls but what it has already handed on: every
// read and send opens its own connection.
type Adapter struct {
	seen *channels.Seen // channelID/Message-ID handed on (a copy delivered twice)

	// dns answers the DKIM key lookups; nil is the system resolver.
	dns mailin.Resolver
	// plain talks to the servers without TLS. Only tests set it: their servers
	// are on loopback.
	plain bool
	// poll, idle and retry are pollEvery, idleFor and retryMin; tests shorten them.
	poll, idle, retry time.Duration

	mu    sync.Mutex
	notes map[string]string // channelID -> why the last message was ignored
}

func init() { channels.Register(New()) }

// New makes an Adapter.
func New() *Adapter {
	return &Adapter{
		seen:  channels.NewSeen(4096),
		poll:  pollEvery,
		idle:  idleFor,
		retry: retryMin,
		notes: map[string]string{},
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "email",
		Name:           "Email",
		Description:    "A conversation by email with one person, from a mailbox of the Bot's own.",
		Logo:           logoURL(),
		Guide:          emailGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_senders", Label: "Pick a correspondent", Kind: channels.ActionPick, Description: "Choose the address this channel writes with: someone who wrote to the mailbox lately, or type an address."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Sign in to the mailbox again, for reading and for sending."},
		},
		Fields: []channels.Field{
			{Key: "imap_host", Label: "IMAP server", Type: channels.FieldText, Required: true, Description: "Where mail is read, e.g. imap.fastmail.com."},
			{Key: "imap_port", Label: "IMAP port", Type: channels.FieldNumber, Description: "993 (TLS) unless your provider says otherwise; any other port uses STARTTLS."},
			{Key: "smtp_host", Label: "SMTP server", Type: channels.FieldText, Required: true, Description: "Where mail is sent, e.g. smtp.fastmail.com."},
			{Key: "smtp_port", Label: "SMTP port", Type: channels.FieldNumber, Description: "587 (STARTTLS) unless your provider says otherwise; 465 uses TLS from the start."},
			{Key: "username", Label: "Username", Type: channels.FieldText, Required: true, Description: "Usually the mailbox's full address."},
			{Key: "password", Label: "Password", Type: channels.FieldSecret, Required: true, Description: "An app password where the provider has them (Gmail, iCloud, Fastmail)."},
			{Key: "from_address", Label: "From address", Type: channels.FieldText, Description: "The address the Bot writes from. Empty uses the username."},
			{Key: "allow_unverified", Label: "Allow unverified mail", Type: channels.FieldToggle, Description: "Off, a mail is read only when its sender's domain signed it (DKIM). On, anyone who can forge the address can write to the Bot."},
		},
	}
}

// settings is a channel's config read and checked.
type settings struct {
	imapHost, imapAddr string
	imapTLS            bool // TLS from the first byte (993); otherwise STARTTLS
	smtpHost, smtpAddr string
	smtpTLS            bool // TLS from the first byte (465); otherwise STARTTLS
	username, password string
	from               *netmail.Address
	self               string // from's address, lower-cased
	allowUnverified    bool
	net                netguard.Policy
}

func resolve(cfg channels.Config) (settings, error) {
	s := settings{
		imapHost:        strings.TrimSpace(cfg.Get("imap_host")),
		smtpHost:        strings.TrimSpace(cfg.Get("smtp_host")),
		username:        strings.TrimSpace(cfg.Get("username")),
		password:        cfg.Get("password"),
		allowUnverified: cfg.Bool("allow_unverified"),
		net:             cfg.Net,
	}
	switch {
	case s.imapHost == "":
		return s, errors.New("set the IMAP server")
	case s.smtpHost == "":
		return s, errors.New("set the SMTP server")
	case s.username == "":
		return s, errors.New("set the username")
	case s.password == "":
		return s, errors.New("set the password")
	}
	imapPort, err := port(cfg.Get("imap_port"), 993, "IMAP")
	if err != nil {
		return s, err
	}
	smtpPort, err := port(cfg.Get("smtp_port"), 587, "SMTP")
	if err != nil {
		return s, err
	}
	s.imapTLS, s.smtpTLS = imapPort == 993, smtpPort == 465
	s.imapAddr = net.JoinHostPort(s.imapHost, strconv.Itoa(imapPort))
	s.smtpAddr = net.JoinHostPort(s.smtpHost, strconv.Itoa(smtpPort))

	from := strings.TrimSpace(cfg.Get("from_address"))
	if from == "" {
		from = s.username
	}
	if s.from, err = netmail.ParseAddress(from); err != nil {
		if cfg.Get("from_address") == "" {
			return s, errors.New("the username is not an email address: fill in From address")
		}
		return s, errors.New("From address is not an email address")
	}
	s.self = strings.ToLower(s.from.Address)
	return s, nil
}

func port(v string, def int, what string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("the %s port must be a number from 1 to 65535", what)
	}
	return n, nil
}

// targetAddr reads the bound correspondent out of what the owner picked or
// typed: an address, "Name <address>" or a mailto: link.
func targetAddr(external string) string {
	v := strings.TrimSpace(external)
	if len(v) > 7 && strings.EqualFold(v[:7], "mailto:") {
		v = v[7:]
		if i := strings.IndexByte(v, '?'); i >= 0 {
			v = v[:i]
		}
	}
	if a, err := netmail.ParseAddress(v); err == nil {
		return strings.ToLower(a.Address)
	}
	return ""
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return "localhost"
}

func (a *Adapter) Validate(ctx context.Context, ch *db.Channel, cfg channels.Config) (channels.State, error) {
	st, err := a.check(ctx, ch, cfg)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, err
	}
	return st, nil
}

// check signs in to both servers and sends nothing.
func (a *Adapter) check(ctx context.Context, ch *db.Channel, cfg channels.Config) (channels.State, error) {
	s, err := resolve(cfg)
	if err != nil {
		return channels.State{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := a.dialIMAP(ctx, s)
	if err != nil {
		return channels.State{}, err
	}
	b.close()
	c, err := a.dialSMTP(ctx, s)
	if err != nil {
		return channels.State{}, err
	}
	_ = c.Quit()
	if to := targetAddr(ch.ExternalID); to != "" && to == s.self {
		return channels.State{}, errors.New("the channel is bound to the Bot's own address: pick the person it writes with")
	}
	return channels.State{Kind: channels.StateInfo, Message: "Connected as " + s.from.Address}, nil
}

// --- inbound ---

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	fail := func(err error) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	s, err := resolve(cfg)
	if err != nil {
		return fail(channels.Permanent(err))
	}
	a.setNote(ch.ID, "")
	host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connecting to the mailbox…"})

	// mark is where reading resumes: nothing below it is ever delivered. It is
	// kept across reconnects, so mail that came while the connection was down
	// is read when it is back.
	var mark watermark
	wait := a.retry
	for {
		connected, err := a.session(ctx, ch, s, host, &mark)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if channels.IsPermanent(err) {
			return fail(err)
		}
		if !mark.set {
			// Never connected: let the host's own retry decide.
			return fail(err)
		}
		if connected {
			wait = a.retry
		}
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: fmt.Sprintf("Lost the mailbox, trying again in %s: %v", wait.Round(time.Second), err)})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		if wait *= 2; wait > retryMax {
			wait = retryMax
		}
	}
}

// watermark is the first UID not yet looked at, in one incarnation of INBOX.
type watermark struct {
	set      bool
	validity uint32
	next     uint32
}

// session reads the mailbox on one connection until it breaks or ctx ends.
// connected says whether it got as far as signing in and opening INBOX.
func (a *Adapter) session(ctx context.Context, ch *db.Channel, s settings, host channels.Host, mark *watermark) (connected bool, err error) {
	b, err := a.dialIMAP(ctx, s)
	if err != nil {
		return false, err
	}
	defer b.close()
	sel, err := b.open(false)
	if err != nil {
		return false, err
	}
	// A new UIDVALIDITY means the server renumbered the mailbox: the old mark
	// names nothing, and starting at the end is the only way never to deliver
	// old mail.
	if !mark.set || mark.validity != sel.UIDValidity {
		*mark = watermark{set: true, validity: sel.UIDValidity, next: uint32(sel.UIDNext)}
	}
	host.PublishState(ch.ID, a.connected(ch.ID, s))
	for {
		if err := a.readNew(ctx, ch, s, host, b, mark); err != nil {
			return true, err
		}
		if err := b.await(ctx, a.idle, a.poll); err != nil {
			return true, err
		}
	}
}

// connected is the channel's state while its mailbox is being read, with why
// the last message was ignored, so an owner sees why nothing happened.
func (a *Adapter) connected(channelID string, s settings) channels.State {
	msg := "Connected as " + s.from.Address
	a.mu.Lock()
	note := a.notes[channelID]
	a.mu.Unlock()
	if note != "" {
		msg += ". " + note
	}
	return channels.State{Kind: channels.StateInfo, Message: msg}
}

func (a *Adapter) setNote(channelID, note string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if note == "" {
		delete(a.notes, channelID)
		return
	}
	a.notes[channelID] = note
}

// readNew hands on every message from the mark up, oldest first.
func (a *Adapter) readNew(ctx context.Context, ch *db.Channel, s settings, host channels.Host, b *box, mark *watermark) error {
	heads, err := b.headsFrom(mark.next)
	if err != nil {
		return err
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].uid < heads[j].uid })
	for _, h := range heads {
		if err := a.onMessage(ctx, ch, s, host, b, h); err != nil {
			return err
		}
		mark.next = h.uid + 1
	}
	return nil
}

// onMessage decides about one new message and hands it to the host. An error
// is the connection's: a message that is not for the Bot is simply passed over.
func (a *Adapter) onMessage(ctx context.Context, ch *db.Channel, s settings, host channels.Host, b *box, h head) error {
	live, ok := host.CurrentChannel(ch.ID)
	if !ok {
		live = ch
	}
	bound := targetAddr(live.ExternalID)
	// The headers alone settle most mail, so a newsletter is never downloaded.
	if bound == "" || fromOf(h.header) != bound || machine(h.header, s.self) {
		return nil
	}
	ignore := func(why string) {
		a.setNote(ch.ID, why)
		host.PublishState(ch.ID, a.connected(ch.ID, s))
	}
	if h.size > maxMessage {
		ignore(fmt.Sprintf("Ignored a message from %s: it is over %d MB.", bound, maxMessage>>20))
		return nil
	}
	raw, err := b.fetch(h.uid)
	if err != nil {
		return err
	}
	if raw == nil {
		return nil // gone since it was listed
	}
	in, ok := read(raw)
	// The whole message is what is believed, not the header fetch before it.
	if !ok || in.from != bound || machine(in.header, s.self) {
		return nil
	}
	if in.text() == "" && len(in.files) == 0 {
		return nil
	}
	if !s.allowUnverified {
		v := mailin.Authenticate(ctx, a.dns, mailin.Envelope{}, raw, in.from)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !v.Verified {
			ignore(fmt.Sprintf("Ignored an unverified message from %s: its domain did not sign it (%s), so anyone could have written it. Allow unverified mail reads such mail anyway.", bound, dkimOf(v.Detail)))
			return nil
		}
	}
	if in.id != "" && a.seen.Add(live.ID+"/"+in.id) {
		return nil
	}
	a.setNote(ch.ID, "")
	if err := host.DeliverInbound(ctx, live, in.inbound(bound, raw)); err != nil {
		log.Printf("email channel %s: inbound: %v", ch.ID, err)
		return nil
	}
	// Read means the Bot was handed it; the owner sees the same in the mailbox.
	if err := b.markSeen(h.uid); err != nil {
		return err
	}
	host.PublishState(ch.ID, a.connected(ch.ID, s))
	return nil
}

// dkimOf keeps the DKIM half of a verdict's evidence: an IMAP reader has no
// envelope, so the SPF half always says none.
func dkimOf(detail string) string {
	if i := strings.Index(detail, ";"); i >= 0 {
		detail = detail[:i]
	}
	return detail
}

// --- history and setup ---

func (a *Adapter) History(ctx context.Context, ch *db.Channel, cfg channels.Config, externalID string, limit int) ([]channels.Message, error) {
	s, err := resolve(cfg)
	if err != nil {
		return nil, err
	}
	bound := targetAddr(ch.ExternalID)
	if bound == "" {
		return nil, errors.New("the channel has no correspondent yet: pick one in Set up")
	}
	if want := targetAddr(externalID); want != "" && want != bound {
		return nil, errors.New("this channel only reads mail from " + bound)
	}
	if limit <= 0 || limit > historyMax {
		limit = historyMax
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := a.dialIMAP(ctx, s)
	if err != nil {
		return nil, err
	}
	defer b.close()
	if _, err := b.open(true); err != nil {
		return nil, err
	}
	heads, err := b.headsOf(bound, limit)
	if err != nil {
		return nil, err
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].uid < heads[j].uid })
	out := []channels.Message{}
	for _, h := range heads {
		// The same tests as a live message: history must not be a way around them.
		if fromOf(h.header) != bound || machine(h.header, s.self) || h.size > maxMessage {
			continue
		}
		raw, err := b.fetch(h.uid)
		if err != nil {
			return nil, err
		}
		in, ok := read(raw)
		if !ok || in.from != bound {
			continue
		}
		if !s.allowUnverified && !mailin.Authenticate(ctx, a.dns, mailin.Envelope{}, raw, in.from).Verified {
			continue
		}
		out = append(out, channels.Message{Author: in.author, Text: in.text(), At: in.date})
	}
	return out, nil
}

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	switch action {
	case "list_senders":
		return a.listSenders(ctx, cfg)
	case "refresh":
		st, err := a.check(ctx, ch, cfg)
		if err != nil {
			return channels.State{Kind: channels.StateError, Message: err.Error()}, nil
		}
		return st, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// listSenders offers the people who wrote to the mailbox lately, newest first.
// Machines and the Bot's own address are left out: neither is ever answered.
func (a *Adapter) listSenders(ctx context.Context, cfg channels.Config) (channels.State, error) {
	s, err := resolve(cfg)
	if err != nil {
		return channels.State{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := a.dialIMAP(ctx, s)
	if err != nil {
		return channels.State{}, err
	}
	defer b.close()
	if _, err := b.open(true); err != nil {
		return channels.State{}, err
	}
	heads, err := b.newest(pickFrom)
	if err != nil {
		return channels.State{}, err
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].uid > heads[j].uid })
	var options []channels.Option
	listed := map[string]bool{}
	for _, h := range heads {
		addr := fromOf(h.header)
		if addr == "" || listed[addr] || machine(h.header, s.self) {
			continue
		}
		listed[addr] = true
		label := addr
		if name := nameOf(h.header); name != "" {
			label = name + " <" + addr + ">"
		}
		options = append(options, channels.Option{Value: addr, Label: label})
	}
	msg := "Pick who this channel writes with, or type their address."
	if len(options) == 0 {
		msg = "Nobody has written to this mailbox lately. Type the address this channel writes with."
	}
	return channels.State{Kind: channels.StateSelect, Message: msg, Options: options}, nil
}
