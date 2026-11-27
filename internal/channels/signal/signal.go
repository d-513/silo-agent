// Package signal is the built-in Signal adapter. Signal has no bot API, so it
// talks to a signal-cli daemon the operator runs (signal-cli daemon --http):
// the daemon holds the account and its keys, and the Control Plane only speaks
// its HTTP JSON-RPC and reads its event stream. Nothing of the account is
// kept here.
//
// A channel is bound to exactly one conversation of that account: a group or
// one person. Inbound is live-only, as the event stream delivers; Signal
// keeps no history to read back.
package signal

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// queueSlack is how far before the start a message may be dated and still
	// count as live: the daemon hands over what queued up while nobody read.
	queueSlack = 5 * time.Second
	// lookupTimeout bounds one name lookup made while reading a message, so a
	// slow daemon only costs a name, never the message.
	lookupTimeout = 10 * time.Second
	// retryMin and retryMax are the waits between tries to reopen a dropped
	// event stream.
	retryMin = time.Second
	retryMax = time.Minute
	// mentionMark stands in a message's text where someone is mentioned.
	mentionMark = '￼'
)

//go:embed signal.svg
var signalLogo []byte

//go:embed GUIDE.md
var signalGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(signalLogo)
}

// Adapter holds one daemon session per running channel.
type Adapter struct {
	mu    sync.Mutex
	conns map[string]*conn // channelID -> live session
	seen  *channels.Seen   // channelID/sender/timestamp handed on (a reconnect replays)

	// retryMin and retryMax bound the reconnect backoff; tests shorten them.
	retryMin, retryMax time.Duration
}

// conn is one live session: the daemon, and what it has told us about people.
type conn struct {
	d     *daemon
	since int64 // ms; messages dated before it were queued, not live

	mu     sync.Mutex
	people map[string]person // number or uuid -> who that is; a miss is kept too
}

// person is one Signal user as the daemon knows them.
type person struct {
	number, uuid, name string
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{
		conns:    map[string]*conn{},
		seen:     channels.NewSeen(4096),
		retryMin: retryMin,
		retryMax: retryMax,
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "signal",
		Name:           "Signal",
		Description:    "A Signal account, through a signal-cli daemon you run.",
		Logo:           logoURL(),
		Guide:          signalGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_chats", Label: "Pick a conversation", Kind: channels.ActionPick, Description: "Choose the group or person this channel talks to, or type a phone number or a group id."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Ask the signal-cli daemon whether it is up and has the account."},
		},
		Fields: []channels.Field{
			{Key: "daemon_url", Label: "Daemon address", Type: channels.FieldText, Required: true, Description: "Where the signal-cli daemon listens (its --http option), e.g. http://127.0.0.1:8080."},
			{Key: "account", Label: "Account", Type: channels.FieldText, Required: true, Description: "The phone number the daemon is registered or linked as, in international form: +491701234567."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
		},
	}
}

func connectedMessage(d *daemon) string { return "Connected as " + d.account }

func (a *Adapter) Validate(ctx context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	d, err := newDaemon(cfg)
	if err == nil {
		defer d.close()
		err = d.check(ctx)
	}
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, err
	}
	return channels.State{Kind: channels.StateInfo, Message: connectedMessage(d)}, nil
}

// errOtherAccount ends a Start whose daemon turned out to run another number.
type errOtherAccount struct{ want, got string }

func (e errOtherAccount) Error() string {
	return fmt.Sprintf("the signal-cli daemon runs the account %s, not %s — correct the number, or point this channel at the daemon that has it", e.got, e.want)
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	fail := func(err error) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	d, err := newDaemon(cfg)
	if err != nil {
		return fail(channels.Permanent(err))
	}
	defer d.close()
	if err := d.check(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail(err)
	}
	c := &conn{d: d, since: time.Now().Add(-queueSlack).UnixMilli(), people: map[string]person{}}
	a.putConn(ch.ID, c)
	defer a.dropConn(ch.ID, c)

	run, stop := context.WithCancel(ctx)
	defer stop()
	var fatal error
	onEvent := func(ev event) {
		if err := a.onEvent(run, ch, cfg, host, c, ev); err != nil {
			var other errOtherAccount
			if errors.As(err, &other) {
				fatal = channels.Permanent(err)
				stop()
				return
			}
			if run.Err() == nil {
				log.Printf("signal channel %s: inbound: %v", ch.ID, err)
			}
		}
	}

	// A stream that drops is reopened here, with a growing wait: the daemon
	// restarting is not the channel failing. Last-Event-ID has the daemon
	// send what arrived while the stream was down.
	wait, last := a.retryMin, ""
	for {
		err := d.events(run, last, func() {
			wait = a.retryMin
			host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: connectedMessage(d)})
		}, func(ev event) {
			if ev.ID != "" {
				last = ev.ID
			}
			onEvent(ev)
		})
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case fatal != nil:
			return fail(fatal)
		case channels.IsPermanent(err):
			return fail(err)
		}
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: fmt.Sprintf("Lost the signal-cli daemon, trying again in %s: %v", wait.Round(time.Second), err)})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, a.retryMax)
	}
}

// --- targets ---

// target is the one conversation a channel is bound to: a group, or a person
// by number or uuid.
type target struct {
	group  string // base64 group id
	person string // +number, or a lower-case uuid
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// parseTarget reads a conversation as the picker stores it (group:<id>, a
// number, a uuid) or as the owner types it: a number with spaces, or a bare
// group id.
func parseTarget(s string) (target, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 6 && strings.EqualFold(s[:6], "group:") {
		id := strings.TrimSpace(s[6:])
		if raw, err := base64.StdEncoding.DecodeString(id); err != nil || len(raw) == 0 {
			return target{}, false
		}
		return target{group: id}, true
	}
	if uuidRe.MatchString(s) {
		return target{person: strings.ToLower(s)}, true
	}
	if n := cleanNumber(s); numberRe.MatchString(n) {
		return target{person: n}, true
	}
	// A group id is 16 or 32 bytes of base64; a number without its + is not.
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) >= 16 {
		return target{group: s}, true
	}
	return target{}, false
}

func (t target) String() string {
	if t.group != "" {
		return "group:" + t.group
	}
	return t.person
}

// address is the target as the RPC's parameters name it.
func (t target) address() map[string]any {
	if t.group != "" {
		return map[string]any{"groupId": []string{t.group}}
	}
	return map[string]any{"recipient": []string{t.person}}
}

// --- inbound ---

// envelope is one received message as signal-cli's JSON output shapes it.
// Only a dataMessage is a message someone wrote here: a syncMessage is the
// account's own doing on another device, and receipts, typing, edits, stories
// and calls have fields of their own that are not read.
type envelope struct {
	SourceNumber string       `json:"sourceNumber"`
	SourceUUID   string       `json:"sourceUuid"`
	SourceName   string       `json:"sourceName"`
	Timestamp    int64        `json:"timestamp"`
	DataMessage  *dataMessage `json:"dataMessage"`
}

type dataMessage struct {
	Message   string `json:"message"`
	GroupInfo *struct {
		GroupID   string `json:"groupId"`
		GroupName string `json:"groupName"`
		Type      string `json:"type"` // DELIVER, or UPDATE for a change to the group
	} `json:"groupInfo"`
	Mentions    []mention    `json:"mentions"`
	Attachments []attachment `json:"attachments"`
}

// mention marks where in the text someone is named; start and length count
// UTF-16 code units.
type mention struct {
	Number string `json:"number"`
	UUID   string `json:"uuid"`
	Start  int    `json:"start"`
	Length int    `json:"length"`
}

type attachment struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	IsVoiceNote bool   `json:"isVoiceNote"`
}

// onEvent turns one event of the stream into an Inbound for the bound
// conversation, or into nothing.
func (a *Adapter) onEvent(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, c *conn, ev event) error {
	if ev.Name != "" && ev.Name != "receive" {
		return nil
	}
	// The stream carries the receive payload itself; the JSON-RPC socket
	// wraps the same thing in a notification's params. Read either.
	var msg struct {
		Account  string    `json:"account"`
		Envelope *envelope `json:"envelope"`
		Params   *struct {
			Account  string    `json:"account"`
			Envelope *envelope `json:"envelope"`
		} `json:"params"`
	}
	if err := json.Unmarshal(ev.Data, &msg); err != nil {
		return fmt.Errorf("unreadable event: %w", err)
	}
	if msg.Envelope == nil && msg.Params != nil {
		msg.Account, msg.Envelope = msg.Params.Account, msg.Params.Envelope
	}
	// A single-account daemon cannot be asked which number it runs; what it
	// receives says so.
	if strings.HasPrefix(msg.Account, "+") && msg.Account != c.d.account {
		return errOtherAccount{want: c.d.account, got: msg.Account}
	}
	env := msg.Envelope
	if env == nil || env.DataMessage == nil {
		return nil
	}
	dm := env.DataMessage
	sender := env.SourceNumber
	if sender == "" {
		sender = strings.ToLower(env.SourceUUID)
	}
	if sender == "" || sender == c.d.account {
		return nil
	}
	if env.Timestamp < c.since {
		return nil // queued while nobody was reading: never backfill
	}
	if dm.GroupInfo != nil && dm.GroupInfo.Type == "UPDATE" {
		return nil
	}
	// Re-read the row so a target picked after Start takes effect now.
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	tgt, ok := parseTarget(live.ExternalID)
	if !ok {
		return nil
	}
	direct := tgt.group == ""
	switch {
	case direct && (dm.GroupInfo != nil || !c.isPeer(ctx, tgt.person, env)):
		return nil
	case !direct && (dm.GroupInfo == nil || dm.GroupInfo.GroupID != tgt.group):
		return nil
	}
	if !direct && cfg.Get("reply_mode") != "always" && !c.mentioned(ctx, dm.Mentions) {
		return nil
	}
	text := strings.TrimSpace(c.readable(ctx, dm.Message, dm.Mentions))
	files := c.mediaOf(tgt, sender, dm.Attachments)
	if text == "" && len(files) == 0 {
		return nil // a reaction, a sticker, a removed message
	}
	if a.seen.Add(fmt.Sprintf("%s/%s/%d", ch.ID, sender, env.Timestamp)) {
		return nil
	}
	// Off the read loop: a receipt is a courtesy and must not hold up the
	// next message.
	go c.markRead(sender, env.Timestamp)

	author := strings.TrimSpace(env.SourceName)
	if author == "" {
		author = sender
	}
	title := "Signal · " + author
	if !direct {
		title = "Group · " + strings.TrimSpace(dm.GroupInfo.GroupName)
	}
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: tgt.String(),
		Title:      title,
		Author:     author,
		AuthorID:   sender,
		Text:       text,
		Files:      files,
	})
}

// isPeer reports whether a direct message came from the bound person. They
// are bound by number or by uuid, and a message may name its sender by
// either; when it names only the other one, the daemon is asked who that is.
func (c *conn) isPeer(ctx context.Context, bound string, env *envelope) bool {
	number, uuid := env.SourceNumber, strings.ToLower(env.SourceUUID)
	if bound == number || bound == uuid {
		return true
	}
	if number != "" && uuid != "" {
		return false
	}
	p := c.who(ctx, bound)
	return (number != "" && p.number == number) || (uuid != "" && p.uuid == uuid)
}

// mentioned reports whether the account is named in a group message.
func (c *conn) mentioned(ctx context.Context, ms []mention) bool {
	self, asked := "", false
	for _, m := range ms {
		if m.Number == c.d.account {
			return true
		}
		if m.Number != "" || m.UUID == "" {
			continue
		}
		// A mention by uuid alone: learn the account's own, once.
		if !asked {
			self, asked = c.who(ctx, c.d.account).uuid, true
		}
		if self != "" && strings.ToLower(m.UUID) == self {
			return true
		}
	}
	return false
}

// readable is a message's text with each mention's placeholder replaced by
// the name of who is mentioned.
func (c *conn) readable(ctx context.Context, text string, ms []mention) string {
	if len(ms) == 0 || !strings.ContainsRune(text, mentionMark) {
		return text
	}
	units := utf16.Encode([]rune(text))
	ms = append([]mention(nil), ms...)
	// Last first, so an earlier mention's offsets still hold.
	sort.Slice(ms, func(i, j int) bool { return ms[i].Start > ms[j].Start })
	for _, m := range ms {
		end := m.Start + m.Length
		if m.Start < 0 || m.Length <= 0 || end > len(units) || units[m.Start] != uint16(mentionMark) {
			continue
		}
		id := m.Number
		if id == "" {
			id = strings.ToLower(m.UUID)
		}
		name := utf16.Encode([]rune("@" + c.nameOf(ctx, id)))
		units = append(units[:m.Start:m.Start], append(name, units[end:]...)...)
	}
	return string(utf16.Decode(units))
}

func (c *conn) nameOf(ctx context.Context, id string) string {
	p := c.who(ctx, id)
	switch {
	case p.name != "":
		return p.name
	case p.number != "":
		return p.number
	}
	return id
}

// who asks the daemon about one person, by number or uuid, once per session.
// A miss is remembered too, so an unknown sender is not looked up per message.
func (c *conn) who(ctx context.Context, id string) person {
	if id == "" {
		return person{}
	}
	c.mu.Lock()
	p, ok := c.people[id]
	c.mu.Unlock()
	if ok {
		return p
	}
	var found []contact
	if err := c.d.call(ctx, lookupTimeout, "listContacts", map[string]any{"recipient": []string{id}}, &found); err == nil && len(found) > 0 {
		p = found[0].person()
	}
	c.mu.Lock()
	if len(c.people) > 10000 {
		c.people = map[string]person{}
	}
	c.people[id] = p
	c.mu.Unlock()
	return p
}

// contact is one entry of listContacts.
type contact struct {
	Number     string `json:"number"`
	UUID       string `json:"uuid"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	NickName   string `json:"nickName"`
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	IsBlocked  bool   `json:"isBlocked"`
	Profile    *struct {
		GivenName  string `json:"givenName"`
		FamilyName string `json:"familyName"`
	} `json:"profile"`
}

// person names a contact the way the account's owner sees them: the name
// they gave the contact, else the one the person gave themselves.
func (ct contact) person() person {
	names := []string{ct.NickName, ct.Name, ct.GivenName + " " + ct.FamilyName}
	if ct.Profile != nil {
		names = append(names, ct.Profile.GivenName+" "+ct.Profile.FamilyName)
	}
	names = append(names, ct.Username)
	p := person{number: ct.Number, uuid: strings.ToLower(ct.UUID)}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			p.name = n
			break
		}
	}
	return p
}

// extensions name a file that came without a name, by its type.
var extensions = map[string]string{
	"audio/aac": ".aac", "audio/mp4": ".m4a", "audio/mpeg": ".mp3", "audio/ogg": ".ogg", "audio/wav": ".wav",
	"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp",
	"video/mp4": ".mp4", "application/pdf": ".pdf", "text/plain": ".txt",
}

// mediaOf is what a message has attached. The daemon downloads attachments
// as they arrive and hands one over, by id, as base64.
func (c *conn) mediaOf(tgt target, sender string, as []attachment) []channels.Media {
	var out []channels.Media
	for _, at := range as {
		if at.ID == "" {
			continue
		}
		kind, _, _ := strings.Cut(at.ContentType, ";")
		kind = strings.ToLower(strings.TrimSpace(kind))
		name := strings.TrimSpace(at.Filename)
		if name == "" {
			name = "attachment"
			if at.IsVoiceNote {
				name = "voice-message"
			}
			name += extensions[kind]
		}
		params := map[string]any{"id": at.ID}
		if tgt.group != "" {
			params["groupId"] = tgt.group
		} else {
			params["recipient"] = sender
		}
		out = append(out, channels.Media{
			Name: name, Mime: kind, Size: at.Size, Voice: at.IsVoiceNote,
			Fetch: func(ctx context.Context) ([]byte, error) {
				var got struct {
					Data string `json:"data"`
				}
				if err := c.d.call(ctx, fileTimeout, "getAttachment", params, &got); err != nil {
					return nil, err
				}
				data, err := base64.StdEncoding.DecodeString(got.Data)
				if err != nil {
					return nil, errors.New("the daemon's copy of the attachment could not be read")
				}
				return data, nil
			},
		})
	}
	return out
}

// markRead tells the sender their message was read. Best effort.
func (c *conn) markRead(sender string, timestamp int64) {
	err := c.d.call(context.Background(), rpcTimeout, "sendReceipt", map[string]any{
		"recipient": sender, "targetTimestamp": []int64{timestamp}, "type": "read",
	}, nil)
	if err != nil {
		log.Printf("signal: read receipt: %v", err)
	}
}

// --- sessions ---

func (a *Adapter) putConn(id string, c *conn) {
	a.mu.Lock()
	a.conns[id] = c
	a.mu.Unlock()
}

// dropConn forgets c, only while it is still the channel's: a restarted
// worker may already have put its own, which the old one must not remove.
func (a *Adapter) dropConn(id string, c *conn) {
	a.mu.Lock()
	if a.conns[id] == c {
		delete(a.conns, id)
	}
	a.mu.Unlock()
}

func (a *Adapter) conn(id string) *conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[id]
}
