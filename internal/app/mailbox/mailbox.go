// Package mailbox gives every Bot a receive-only email address. The Control
// Plane runs one SMTP listener (internal/mailin); a message for
// <name>@<mail.domain> is filed in that Bot's inbox, where the Bot reads it
// with list_mail/read_mail and the owner on the Mail page. Nothing is ever
// sent. This is a feature of the Bot itself, not a connector: the Email
// connector is a client of somebody's IMAP/SMTP account.
package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mailin"
	"silo.agent/internal/names"
	"silo.agent/internal/textx"
)

const (
	// Keep is how many messages a mailbox holds; the oldest fall off.
	Keep = 200
	// Tick is how often the listener is checked against the settings.
	Tick = 15 * time.Second

	textMax    = 200000
	headerMax  = 500
	previewMax = 160
)

// State values of Mailbox.state.
const (
	StateOK       = "ok"
	StateOff      = "off"       // mail.enabled is false
	StateNoDomain = "no_domain" // enabled, but no domain to receive for
)

// Host is what the mailbox needs from the App around it.
type Host interface {
	host.Authorizer
	host.Runs
}

// Service owns the mailboxes: the listener, the rows, the RPCs and the Bot's
// tools.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	host   Host
	engine run.Engine
	ws     *workspace.Service
	// DNS answers the DKIM and SPF lookups; nil is the system resolver. Tests
	// set it so no check leaves the machine.
	DNS mailin.Resolver

	mu      sync.Mutex
	srv     *mailin.Server
	running string // the settings the running listener was started with
	problem string // why the listener is not up, when it should be
}

func New(gdb *gorm.DB, cfg func() config.Config, h Host, engine run.Engine, ws *workspace.Service) *Service {
	return &Service{db: gdb, cfg: cfg, host: h, engine: engine, ws: ws}
}

// State reports whether mail can be received.
func State(c config.Config) string {
	switch {
	case !c.Mail.Enabled:
		return StateOff
	case c.MailDomain() == "":
		return StateNoDomain
	}
	return StateOK
}

// Usable reports whether mail is on and has a domain: the Bot's mail tools are
// offered, and the prompt gives its address, only then.
func (s *Service) Usable() bool { return State(s.cfg()) == StateOK }

// usable is nil when mail can be received right now.
func (s *Service) usable() error {
	switch State(s.cfg()) {
	case StateOff:
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("mail is turned off by the operator (mail.enabled)"))
	case StateNoDomain:
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("mail has no domain yet: the operator sets mail.domain"))
	}
	return nil
}

// --- the listener ---

// Sync makes the listener match the settings: started when mail is on, moved
// when the address, domain, size cap or certificate changed, stopped when mail
// is off. It runs at start and on every Tick, so a change in Admin → Settings
// needs no restart, and a port that was busy is tried again.
func (s *Service) Sync() {
	c := s.cfg()
	want := ""
	if c.MailOn() {
		want = strings.Join([]string{c.Mail.ListenAddr(), c.MailDomain(), strconv.FormatInt(c.Mail.MaxBytes(), 10), c.Mail.TLSCert, c.Mail.TLSKey}, "\x00")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if want == s.running && (want == "" || s.srv != nil) {
		return
	}
	if s.srv != nil {
		_ = s.srv.Close()
		s.srv = nil
		log.Printf("mail: listener stopped")
	}
	s.running = want
	if want == "" {
		s.problem = ""
		return
	}
	srv, err := s.listen(c)
	if err != nil {
		// Said once; the next tick tries again quietly until it differs.
		if msg := err.Error(); msg != s.problem {
			s.problem = msg
			log.Printf("mail: listener %s did not start: %v", c.Mail.ListenAddr(), err)
		}
		return
	}
	s.srv, s.problem = srv, ""
	log.Printf("mail: receiving for @%s on %s", c.MailDomain(), srv.Addr())
}

func (s *Service) listen(c config.Config) (*mailin.Server, error) {
	tlsCfg, err := mailin.TLSConfig(c.MailDomain(), strings.TrimSpace(c.Mail.TLSCert), strings.TrimSpace(c.Mail.TLSKey))
	if err != nil {
		return nil, err
	}
	return mailin.Listen(mailin.Config{Addr: c.Mail.ListenAddr(), Domain: c.MailDomain(), MaxBytes: c.Mail.MaxBytes(), TLS: tlsCfg}, s)
}

// Addr is where the listener is bound, or "" while it is not running.
func (s *Service) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return ""
	}
	return s.srv.Addr()
}

// Problem is why the listener is not running although mail is on, or "".
func (s *Service) Problem() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.problem
}

// Close stops the listener (Shutdown).
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		_ = s.srv.Close()
		s.srv = nil
	}
	s.running = ""
}

// --- mailboxes ---

// Box returns the Bot's mailbox, making it on first use.
func (s *Service) Box(botID string) (*db.Mailbox, error) {
	for range 6 {
		var box db.Mailbox
		res := s.db.Where("bot_id = ?", botID).Limit(1).Find(&box)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected > 0 {
			return &box, nil
		}
		// A name taken meanwhile fails the unique index and the next pass
		// picks another; a concurrent call for this Bot is simply found.
		s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.Mailbox{BotID: botID, Name: names.New(), CreatedAt: time.Now()})
	}
	return nil, errors.New("could not allocate a mailbox name")
}

// Address is the Bot's address, or "" while mail cannot be received.
func (s *Service) Address(botID string) string {
	c := s.cfg()
	if !c.MailOn() {
		return ""
	}
	box, err := s.Box(botID)
	if err != nil {
		return ""
	}
	return c.MailAddress(box.Name)
}

// Rotate gives the Bot a new address. Mail to the old one is refused from
// then on; what already arrived stays.
func (s *Service) Rotate(botID string) (*db.Mailbox, error) {
	box, err := s.Box(botID)
	if err != nil {
		return nil, err
	}
	for range 6 {
		name := names.New()
		if name == box.Name {
			continue
		}
		if err := s.db.Model(box).Update("name", name).Error; err == nil {
			box.Name = name
			return box, nil
		}
	}
	return nil, errors.New("could not allocate a mailbox name")
}

// Drop removes a Bot's mailbox and everything in it (DeleteBot).
func (s *Service) Drop(botID string) {
	s.db.Where("bot_id = ?", botID).Delete(&db.MailBody{})
	s.db.Where("bot_id = ?", botID).Delete(&db.MailMessage{})
	s.db.Where("bot_id = ?", botID).Delete(&db.Mailbox{})
}

// byAddress finds the mailbox an address belongs to; nil when there is none.
// name+anything@domain is name's mailbox, so the Bot can give each service its
// own address and see who passed it on.
func (s *Service) byAddress(rcpt string) *db.Mailbox {
	c := s.cfg()
	if !c.MailOn() {
		return nil
	}
	local, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(rcpt)), "@")
	if !ok || domain != c.MailDomain() {
		return nil
	}
	local, _, _ = strings.Cut(local, "+")
	if local == "" {
		return nil
	}
	var box db.Mailbox
	if res := s.db.Where("name = ?", local).Limit(1).Find(&box); res.Error != nil || res.RowsAffected == 0 {
		return nil
	}
	return &box
}

// --- mailin.Handler ---

// Accept reports whether rcpt is a Bot's address.
func (s *Service) Accept(rcpt string) bool { return s.byAddress(rcpt) != nil }

// Deliver files one message in the mailbox env.To names. A message the mailbox
// already holds (the sender retried) is not filed twice.
func (s *Service) Deliver(ctx context.Context, env mailin.Envelope, raw []byte) error {
	box := s.byAddress(env.To)
	if box == nil {
		// Rotated or deleted between RCPT and DATA: nobody to keep it for.
		return nil
	}
	hash := ids.Hash(string(raw))
	var dup int64
	if err := s.db.Model(&db.MailMessage{}).Where("bot_id = ? AND hash = ?", box.BotID, hash).Count(&dup).Error; err != nil {
		return err
	}
	if dup > 0 {
		return nil
	}
	m := mailin.Parse(raw)
	v := mailin.Authenticate(ctx, s.DNS, env, raw, m.FromAddr)
	atts, _ := json.Marshal(m.Attachments)
	row := db.MailMessage{
		ID: ids.New(), BotID: box.BotID, Hash: hash,
		Sender: textx.ClipRunes(m.From, headerMax), SenderAddr: m.FromAddr, EnvelopeFrom: textx.ClipRunes(env.From, headerMax),
		Recipients: textx.ClipRunes(m.To, 2*headerMax), Subject: textx.ClipRunes(oneLine(m.Subject), headerMax),
		Text: textx.CapRunes(strings.TrimSpace(m.Text), textMax), Size: len(raw), Attachments: string(atts),
		Verified: v.Verified, AuthDetail: v.Detail, CreatedAt: time.Now(),
	}
	if len(m.Attachments) == 0 {
		row.Attachments = ""
	}
	if !m.Date.IsZero() {
		row.SentAt = &m.Date
	}
	stored := append(env.Received(), raw...)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error // a concurrent copy of the same message won
		}
		return tx.Create(&db.MailBody{ID: row.ID, BotID: box.BotID, Data: stored}).Error
	})
	if err != nil {
		return err
	}
	s.trim(box.BotID)
	s.wake(box, &row)
	return nil
}

// trim keeps a mailbox at its newest Keep messages.
func (s *Service) trim(botID string) {
	var old []string
	s.db.Model(&db.MailMessage{}).Where("bot_id = ?", botID).Order("created_at desc, id").Offset(Keep).Limit(1000).Pluck("id", &old)
	if len(old) == 0 {
		return
	}
	s.db.Where("id IN ?", old).Delete(&db.MailBody{})
	s.db.Where("id IN ?", old).Delete(&db.MailMessage{})
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// attachments decodes a row's attachment list.
func attachments(m *db.MailMessage) []mailin.Attachment {
	if m.Attachments == "" {
		return nil
	}
	var out []mailin.Attachment
	_ = json.Unmarshal([]byte(m.Attachments), &out)
	return out
}

// raw loads a message's bytes as they arrived.
func (s *Service) raw(id string) ([]byte, error) {
	var b db.MailBody
	res := s.db.Where("id = ?", id).Limit(1).Find(&b)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("the message's original is gone")
	}
	return b.Data, nil
}
