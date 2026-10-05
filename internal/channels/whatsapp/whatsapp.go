// Package whatsapp is the built-in WhatsApp adapter. It links the Control
// Plane to a WhatsApp account as a "linked device" (the WhatsApp Web
// protocol) with whatsmeow. The owner scans a QR code once; the device's keys
// live in the Control Plane's Postgres, never in the Bot.
//
// A channel is bound to one chat: a person, a group, or the owner's own "message
// yourself" chat. Inbound is live-only: what arrived while the Control Plane
// was offline is not replayed into runs.
package whatsapp

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"rsc.io/qr"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// maxMessageRunes keeps one message readable on a phone. WhatsApp itself takes
// far more, but a wall of text is no kinder there than anywhere else.
const maxMessageRunes = 4000

//go:embed whatsapp.svg
var whatsappLogo []byte

//go:embed GUIDE.md
var whatsappGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(whatsappLogo)
}

// errLoggedOut ends a session when the phone (or WhatsApp) unlinks the device.
var errLoggedOut = errors.New("WhatsApp unlinked this device")

// --- seams ---

// waAPI is what the adapter needs from a live whatsmeow client. *whatsmeow.Client
// satisfies it; tests supply a recorder.
type waAPI interface {
	SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	Upload(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	SendChatPresence(ctx context.Context, jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error
	MarkRead(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error
	GetJoinedGroups(ctx context.Context) ([]*types.GroupInfo, error)
	GenerateMessageID() types.MessageID
}

// directory is the part of the device store used to put names and numbers to
// JIDs. A new device has no stores until it is paired, so it is read lazily.
type directory interface {
	AllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error)
	PhoneForLID(ctx context.Context, lid types.JID) (types.JID, error)
}

type deviceDirectory struct{ dev *store.Device }

func (d deviceDirectory) AllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	if d.dev.Contacts == nil {
		return nil, nil
	}
	return d.dev.Contacts.GetAllContacts(ctx)
}

func (d deviceDirectory) PhoneForLID(ctx context.Context, lid types.JID) (types.JID, error) {
	if d.dev.LIDs == nil {
		return types.EmptyJID, nil
	}
	return d.dev.LIDs.GetPNForLID(ctx, lid)
}

// --- adapter ---

// Adapter holds the shared session store and one run per channel.
type Adapter struct {
	mu       sync.Mutex
	store    *sqlstore.Container
	storeDSN string
	runs     map[string]*run
	seen     *channels.Seen
}

// run is one channel's lifetime, across sessions (a logout starts a new one).
type run struct {
	// relink wakes a run that is waiting for the owner to ask for a new QR
	// code. Unbuffered on purpose: a press nobody is waiting for is dropped,
	// never remembered and acted on later.
	relink chan struct{}

	mu   sync.Mutex
	c    *client // the current session, nil between sessions
	last channels.State
}

func newRun() *run { return &run{relink: make(chan struct{})} }

func (r *run) client() *client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.c
}

func (r *run) setClient(c *client) {
	r.mu.Lock()
	r.c = c
	r.mu.Unlock()
}

// publish sends a state to the host and remembers it, so an action can hand the
// current QR code back unchanged. A state with no Kind only carries values.
func (r *run) publish(host channels.Host, channelID string, st channels.State) {
	if st.Kind != channels.StateNone {
		r.mu.Lock()
		r.last = st
		r.mu.Unlock()
	}
	host.PublishState(channelID, st)
}

func (r *run) lastState() channels.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{runs: map[string]*run{}, seen: channels.NewSeen(4096)}
}

var _ channels.Remover = (*Adapter)(nil)

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "whatsapp",
		Name:           "WhatsApp",
		Description:    "A linked WhatsApp account, paired by QR code.",
		Logo:           logoURL(),
		Guide:          whatsappGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "link", Label: "Link device", Kind: channels.ActionQR, Description: "Show a QR code to scan in WhatsApp → Linked devices."},
			{Key: "list_chats", Label: "Pick a chat", Kind: channels.ActionPick, Description: "Choose the WhatsApp chat this channel talks to."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the connection and re-read the account."},
		},
		Fields: []channels.Field{
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
		},
	}
}

func (a *Adapter) Validate(_ context.Context, _ *db.Channel, _ channels.Config) (channels.State, error) {
	return channels.State{Kind: channels.StateInfo, Message: "Starting…"}, nil
}

// --- lifecycle ---

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	r := newRun()
	a.mu.Lock()
	a.runs[ch.ID] = r
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.runs, ch.ID)
		a.mu.Unlock()
	}()

	container, err := a.container(ctx, host.DatabaseURL())
	if err != nil {
		r.publish(host, ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	for {
		err := a.session(ctx, ch, cfg, host, container, r)
		if !errors.Is(err, errLoggedOut) {
			return err
		}
		// The device is gone. Forget it, then wait for the owner to link again
		// rather than putting up a QR code nobody asked for.
		r.publish(host, ch.ID, channels.State{
			Kind:    channels.StateError,
			Message: "WhatsApp unlinked this device. Press Link device to scan a new code.",
			Values:  map[string]string{"jid": ""},
		})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.relink:
		}
	}
}

// container opens the whatsmeow session store once and shares it between
// channels. It lives in the Control Plane's Postgres (whatsmeow_* tables), so a
// WhatsApp session is backed up and restored with everything else.
func (a *Adapter) container(ctx context.Context, dsn string) (*sqlstore.Container, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("WhatsApp keeps its session in Postgres, but no database_url is configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil && a.storeDSN == dsn {
		return a.store, nil
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("whatsapp session store: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	c := sqlstore.NewWithDB(sqlDB, "postgres", logger{mod: "store"})
	if err := c.Upgrade(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("whatsapp session store: %w", err)
	}
	a.store, a.storeDSN = c, dsn
	return c, nil
}

// session is one device's life: pair if needed, connect, serve until the
// context ends or something fatal happens.
func (a *Adapter) session(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, container *sqlstore.Container, r *run) error {
	cur := ch
	if c, ok := host.CurrentChannel(ch.ID); ok {
		cur = c
	}
	dev, err := loadDevice(ctx, container, stateJID(cur))
	if err != nil {
		r.publish(host, ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	wa := whatsmeow.NewClient(dev, logger{mod: "client", id: ch.ID})
	wa.EnableAutoReconnect = true
	wa.InitialAutoReconnect = true // a network blip at boot must not end the channel

	c := &client{
		api:       wa,
		dir:       deviceDirectory{dev},
		sent:      channels.NewSeen(1024),
		liveSince: time.Now().Add(-5 * time.Second), // slack for clock skew; older is a backlog
		failed:    make(chan error, 1),
		logout:    wa.Logout,
		spawn:     func(f func()) { go f() },
		names:     map[types.JID]string{},
	}
	r.setClient(c)
	defer func() {
		r.setClient(nil)
		wa.Disconnect()
	}()
	wa.AddEventHandler(func(evt any) { a.handleEvent(ctx, ch, cfg, host, r, c, evt) })

	if wa.Store.ID == nil {
		if err := a.pair(ctx, ch, host, r, wa); err != nil {
			return err
		}
	} else {
		c.setSelf(*wa.Store.ID, wa.Store.LID)
		if err := wa.Connect(); err != nil {
			r.publish(host, ch.ID, channels.State{Kind: channels.StateError, Message: "WhatsApp connect: " + err.Error()})
			return err
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.failed:
		if !errors.Is(err, errLoggedOut) {
			r.publish(host, ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		}
		return err
	}
}

// loadDevice reloads the device a channel was paired as, or makes a new,
// unpaired one (nothing is stored until pairing succeeds).
func loadDevice(ctx context.Context, c *sqlstore.Container, jid string) (*store.Device, error) {
	if parsed, err := types.ParseJID(strings.TrimSpace(jid)); err == nil && !parsed.IsEmpty() {
		dev, err := c.GetDevice(ctx, parsed)
		if err != nil {
			return nil, fmt.Errorf("whatsapp session store: %w", err)
		}
		if dev != nil {
			return dev, nil
		}
	}
	return c.NewDevice(), nil
}

// stateJID reads the paired device's JID from a channel's saved state.
func stateJID(ch *db.Channel) string {
	var st channels.State
	if strings.TrimSpace(ch.StateJSON) == "" || json.Unmarshal([]byte(ch.StateJSON), &st) != nil {
		return ""
	}
	return st.Values["jid"]
}

// pair shows a QR code until it is scanned. Each attempt lasts about two
// minutes; after a failure it waits for Link device instead of looping.
func (a *Adapter) pair(ctx context.Context, ch *db.Channel, host channels.Host, r *run, wa *whatsmeow.Client) error {
	for {
		err := func() error {
			items, err := wa.GetQRChannel(ctx)
			if err != nil {
				return err
			}
			if err := wa.Connect(); err != nil {
				return err
			}
			return consumeQR(ctx, items, func(st channels.State) { r.publish(host, ch.ID, st) })
		}()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wa.Disconnect()
		r.publish(host, ch.ID, channels.State{Kind: channels.StateError, Message: err.Error() + " — press Link device to get a new code."})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.relink:
		}
	}
}

// consumeQR turns whatsmeow's pairing events into channel states: a fresh QR
// image per code, nil once linked, an actionable error otherwise.
func consumeQR(ctx context.Context, items <-chan whatsmeow.QRChannelItem, publish func(channels.State)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-items:
			if !ok {
				return errors.New("WhatsApp closed the pairing")
			}
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				img, err := qrDataURL(item.Code)
				if err != nil {
					return err
				}
				publish(channels.State{
					Kind:    channels.StateQR,
					Message: "Open WhatsApp on your phone → Settings → Linked devices → Link a device, and scan this code. It refreshes by itself.",
					QR:      img,
				})
			case whatsmeow.QRChannelSuccess.Event:
				publish(channels.State{Kind: channels.StateInfo, Message: "Linked. Connecting…"})
				return nil
			case whatsmeow.QRChannelTimeout.Event:
				return errors.New("The code expired before it was scanned")
			case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
				return errors.New("That WhatsApp does not support linked devices — update the app on your phone")
			case whatsmeow.QRChannelClientOutdated.Event:
				return errors.New("WhatsApp no longer accepts this client version — update Silo")
			case whatsmeow.QRChannelEventError:
				return fmt.Errorf("pairing failed: %w", item.Error)
			default:
				return fmt.Errorf("pairing stopped (%s)", item.Event)
			}
		}
	}
}

// qrDataURL draws a pairing code as a PNG data: URL for the Setup page.
func qrDataURL(code string) (string, error) {
	q, err := qr.Encode(code, qr.M)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(q.PNG()), nil
}

// handleEvent is the whatsmeow event handler.
func (a *Adapter) handleEvent(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, r *run, c *client, evt any) {
	switch e := evt.(type) {
	case *events.Message:
		if err := a.onMessage(ctx, ch, cfg, host, c, e); err != nil && ctx.Err() == nil {
			log.Printf("whatsapp channel %s: inbound: %v", ch.ID, err)
		}
	case *events.PairSuccess:
		c.setSelf(e.ID, e.LID)
		// Persist who we paired as before anything else can fail: the next
		// start finds its keys by this JID.
		r.publish(host, ch.ID, channels.State{Values: map[string]string{"jid": e.ID.String()}})
	case *events.Connected:
		st := channels.State{Kind: channels.StateInfo, Message: connectedMessage(c.selfJID())}
		if id := c.deviceJID(); !id.IsEmpty() { // never blank out a JID we already saved
			st.Values = map[string]string{"jid": id.String()}
		}
		r.publish(host, ch.ID, st)
	case *events.LoggedOut:
		c.fail(errLoggedOut)
	case *events.StreamReplaced:
		c.fail(errors.New("WhatsApp opened this session somewhere else — a linked device can only be online from one place"))
	case *events.ClientOutdated:
		c.fail(errors.New("WhatsApp no longer accepts this client version — update Silo"))
	case *events.TemporaryBan:
		c.fail(errors.New(e.String()))
	case *events.ConnectFailure:
		if !e.Reason.IsLoggedOut() { // a LoggedOut event follows and says it better
			c.fail(fmt.Errorf("WhatsApp refused the connection (%d %s)", int(e.Reason), e.Message))
		}
	}
}

func connectedMessage(self types.JID) string {
	if self.IsEmpty() {
		return "Connected"
	}
	return "Connected as +" + self.User
}

// Remove is called when the channel is deleted: unlink the device from the
// owner's phone and drop its keys, so nothing stale is left on either side.
func (a *Adapter) Remove(ctx context.Context, ch *db.Channel, host channels.Host) error {
	a.mu.Lock()
	r := a.runs[ch.ID]
	a.mu.Unlock()
	if r != nil {
		if c := r.client(); c != nil && c.logout != nil && !c.selfJID().IsEmpty() {
			if err := c.logout(ctx); err == nil {
				return nil // the library also deletes the device's rows
			}
		}
	}
	jid := stateJID(ch)
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		jid = stateJID(cur)
	}
	if strings.TrimSpace(jid) == "" {
		return nil
	}
	container, err := a.container(ctx, host.DatabaseURL())
	if err != nil {
		return err
	}
	dev, err := loadDevice(ctx, container, jid)
	if err != nil || dev.ID == nil {
		return err // an unpaired device has nothing stored
	}
	return dev.Delete(ctx)
}

// --- outbound ---

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, _ channels.Config, msg channels.Outbound) error {
	c := a.liveClient(ch.ID)
	if c == nil {
		return errors.New("whatsapp channel is not connected")
	}
	external := strings.TrimSpace(msg.ExternalID)
	if external == "" {
		external = ch.ExternalID
	}
	to, ok := normalizeTarget(external)
	if !ok {
		return fmt.Errorf("%q is not a WhatsApp chat — pick it again in Set up, or enter a phone number", strings.TrimSpace(external))
	}
	return c.send(ctx, to, msg)
}

func (a *Adapter) liveClient(channelID string) *client {
	a.mu.Lock()
	r := a.runs[channelID]
	a.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.client()
}

// History: a linked device cannot ask the phone for old messages on demand (it
// only gets a one-off sync at pairing), so the Bot reads its own run log.
func (a *Adapter) History(context.Context, *db.Channel, channels.Config, string, int) ([]channels.Message, error) {
	return nil, errors.New("history is not available over WhatsApp")
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, _ channels.Config, action string, _ map[string]string) (channels.State, error) {
	a.mu.Lock()
	r := a.runs[ch.ID]
	a.mu.Unlock()
	if r == nil {
		return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
	}
	c := r.client()
	switch action {
	case "link":
		select {
		case r.relink <- struct{}{}:
			return channels.State{Kind: channels.StateAuth, Message: "Asking WhatsApp for a new code…"}, nil
		default: // nobody is waiting: already linked, or a code is already showing
		}
		if st := r.lastState(); st.Kind == channels.StateQR {
			return st, nil
		}
		if c != nil && !c.selfJID().IsEmpty() {
			return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c.selfJID()) + " — already linked."}, nil
		}
		return channels.State{Kind: channels.StateAuth, Message: "Waiting for WhatsApp…"}, nil
	case "refresh":
		if c != nil && !c.selfJID().IsEmpty() {
			return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c.selfJID())}, nil
		}
		if st := r.lastState(); st.Kind != channels.StateNone {
			return st, nil
		}
		return channels.State{Kind: channels.StateAuth, Message: "Waiting to be linked."}, nil
	case "list_chats":
		if c == nil || c.selfJID().IsEmpty() {
			return channels.State{Kind: channels.StateError, Message: "Link the device first (Link device), then pick a chat."}, nil
		}
		return c.listChats(ctx)
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// --- the live session ---

// client is one connected WhatsApp session.
type client struct {
	api       waAPI
	dir       directory
	sent      *channels.Seen // ids the bot sent: its echoes in the self chat are not input
	liveSince time.Time
	failed    chan error // the first fatal event ends the session
	logout    func(context.Context) error
	spawn     func(func())         // runs best-effort work off the event loop
	names     map[types.JID]string // chat title cache, guarded by mu

	mu       sync.Mutex
	self     types.JID // the account's phone-number JID
	selfLID  types.JID
	deviceID types.JID // with the device suffix: how the keys are found again
}

func (c *client) setSelf(id, lid types.JID) {
	c.mu.Lock()
	c.deviceID = id
	c.self = id.ToNonAD()
	c.selfLID = lid.ToNonAD()
	c.mu.Unlock()
}

func (c *client) selfJID() types.JID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.self
}

func (c *client) deviceJID() types.JID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deviceID
}

func (c *client) isSelf(j types.JID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	j = j.ToNonAD()
	return !j.IsEmpty() && (sameUser(j, c.self) || sameUser(j, c.selfLID))
}

// fail records the first fatal event; later ones are dropped.
func (c *client) fail(err error) {
	select {
	case c.failed <- err:
	default:
	}
}

func sameUser(a, b types.JID) bool {
	return a.User != "" && a.User == b.User && a.Server == b.Server
}

// sameChat reports whether a message belongs to the target chat. Direct chats
// can arrive under a phone number or a LID, so both are tried, through the
// alternate address WhatsApp sends along and the library's LID↔number map.
func (c *client) sameChat(ctx context.Context, target types.JID, info types.MessageInfo) bool {
	cands := []types.JID{info.Chat.ToNonAD()}
	if !info.IsGroup {
		alt := info.SenderAlt
		if info.IsFromMe {
			alt = info.RecipientAlt // my own alt would match my own number
		}
		cands = append(cands, alt.ToNonAD())
	}
	for _, cand := range cands {
		if cand.IsEmpty() {
			continue
		}
		if sameUser(cand, target) {
			return true
		}
		if c.dir == nil {
			continue
		}
		switch {
		case cand.Server == types.HiddenUserServer && target.Server == types.DefaultUserServer:
			if pn, err := c.dir.PhoneForLID(ctx, cand); err == nil && sameUser(pn.ToNonAD(), target) {
				return true
			}
		case cand.Server == types.DefaultUserServer && target.Server == types.HiddenUserServer:
			if pn, err := c.dir.PhoneForLID(ctx, target); err == nil && sameUser(pn.ToNonAD(), cand) {
				return true
			}
		}
	}
	return false
}

// mentionsBot: an @mention of the account, or a reply to one of its messages.
func (c *client) mentionsBot(m *waE2E.Message) bool {
	ci := contextInfo(m)
	if ci == nil {
		return false
	}
	for _, s := range append(slices.Clone(ci.GetMentionedJID()), ci.GetParticipant()) {
		if j, err := types.ParseJID(s); err == nil && c.isSelf(j) {
			return true
		}
	}
	return false
}

// --- inbound ---

func (a *Adapter) onMessage(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, c *client, e *events.Message) error {
	if e == nil || e.Message == nil || e.IsEdit {
		return nil
	}
	info := e.Info
	switch info.Chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
	default:
		return nil // status updates, broadcast lists, newsletters
	}
	if info.Timestamp.Before(c.liveSince) {
		return nil // live-only: never replay what was missed while offline
	}
	// Re-read the row so a chat picked after Start takes effect now.
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	target, ok := normalizeTarget(live.ExternalID)
	if !ok || !c.sameChat(ctx, target, info) {
		return nil
	}
	if info.IsFromMe {
		// Only the "message yourself" chat takes input from the owner's own
		// account, and never the bot's own replies echoing back into it.
		if !c.isSelf(target) || c.sent.Has(info.ID) {
			return nil
		}
	}
	if a.seen.Add(ch.ID + "/" + info.ID) {
		return nil
	}
	text := messageText(e.Message)
	if text == "" {
		return nil // a reaction, a receipt-like protocol message, a poll vote …
	}
	// Direct chats always get a reply; reply_mode only gates groups, where
	// being mentioned is the safe default.
	if info.IsGroup && cfg.Get("reply_mode") != "always" && !c.mentionsBot(e.Message) {
		return nil
	}
	if !info.IsFromMe {
		c.spawn(func() { c.ack(info) })
	}
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: target.String(),
		Title:      c.title(ctx, target, info),
		Author:     c.author(info),
		Text:       text,
	})
}

// ack tells the sender the message was seen: a read receipt, then "typing…".
// Best effort and off the event loop, which must not wait on the network.
func (c *client) ack(info types.MessageInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.api.MarkRead(ctx, []types.MessageID{info.ID}, info.Timestamp, info.Chat, info.Sender)
	_ = c.api.SendChatPresence(ctx, info.Chat, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

func (c *client) author(info types.MessageInfo) string {
	switch {
	case info.IsFromMe:
		return "You"
	case info.PushName != "":
		return info.PushName
	case info.Sender.User != "":
		return "+" + info.Sender.User
	}
	return ""
}

// title names the conversation: the group's subject, or the contact's name.
func (c *client) title(ctx context.Context, target types.JID, info types.MessageInfo) string {
	c.mu.Lock()
	cached := c.names[target]
	c.mu.Unlock()
	if cached != "" {
		return cached
	}
	name := ""
	switch {
	case c.isSelf(target):
		name = "Notes to self"
	case target.Server == types.GroupServer:
		if groups, err := c.api.GetJoinedGroups(ctx); err == nil {
			for _, g := range groups {
				if sameUser(g.JID, target) {
					name = g.Name
				}
			}
		}
	default:
		name = info.PushName
		if info.IsFromMe || name == "" {
			name = c.contactName(ctx, target)
		}
	}
	if name == "" {
		name = "+" + target.User
		if target.Server == types.GroupServer {
			name = "WhatsApp group"
		}
	}
	c.mu.Lock()
	c.names[target] = name
	c.mu.Unlock()
	return name
}

// contactName is the name the owner saved the number under, if any.
func (c *client) contactName(ctx context.Context, jid types.JID) string {
	if c.dir == nil {
		return ""
	}
	all, err := c.dir.AllContacts(ctx)
	if err != nil {
		return ""
	}
	return bestName(all[jid])
}

func bestName(ci types.ContactInfo) string {
	for _, n := range []string{ci.FullName, ci.FirstName, ci.PushName, ci.BusinessName} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return ""
}

// messageText is what the Bot reads. Media is not downloaded: it arrives as a
// labelled placeholder (plus its caption), as it does over Telegram.
func messageText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	labelled := func(label, caption string) string {
		if caption = strings.TrimSpace(caption); caption != "" {
			return "[" + label + "]\n" + caption
		}
		return "[" + label + "]"
	}
	switch {
	case strings.TrimSpace(m.GetConversation()) != "":
		return strings.TrimSpace(m.GetConversation())
	case m.GetExtendedTextMessage() != nil:
		return strings.TrimSpace(m.GetExtendedTextMessage().GetText())
	case m.GetImageMessage() != nil:
		return labelled("image", m.GetImageMessage().GetCaption())
	case m.GetVideoMessage() != nil:
		return labelled("video", m.GetVideoMessage().GetCaption())
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		label := "document"
		if n := strings.TrimSpace(d.GetFileName()); n != "" {
			label += ": " + n
		}
		return labelled(label, d.GetCaption())
	case m.GetAudioMessage() != nil:
		if m.GetAudioMessage().GetPTT() {
			return "[voice message]"
		}
		return "[audio]"
	case m.GetStickerMessage() != nil:
		return "[sticker]"
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		s := fmt.Sprintf("[location: %.5f, %.5f]", l.GetDegreesLatitude(), l.GetDegreesLongitude())
		if n := strings.TrimSpace(l.GetName()); n != "" {
			s += "\n" + n
		}
		return s
	case m.GetContactMessage() != nil:
		return "[contact: " + strings.TrimSpace(m.GetContactMessage().GetDisplayName()) + "]"
	}
	return ""
}

// contextInfo is where mentions and quoted-message details live, whichever
// kind of message carries them.
func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	}
	return nil
}

// --- target ---

// normalizeTarget reads a chat from what the owner picked or typed: a JID, a
// phone number in any common notation, or a wa.me link.
func normalizeTarget(s string) (types.JID, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "wa.me/"); i >= 0 {
		s = s[i+len("wa.me/"):]
		if j := strings.IndexAny(s, "?#/"); j >= 0 {
			s = s[:j]
		}
	}
	if s == "" {
		return types.EmptyJID, false
	}
	if strings.Contains(s, "@") {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" {
			return types.EmptyJID, false
		}
		switch j.Server {
		case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
			return j.ToNonAD(), true
		}
		return types.EmptyJID, false
	}
	var digits strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case strings.ContainsRune("+ -().", r):
		default:
			return types.EmptyJID, false
		}
	}
	if n := digits.Len(); n < 7 || n > 15 { // E.164 numbers are at most 15 digits
		return types.EmptyJID, false
	}
	return types.NewJID(digits.String(), types.DefaultUserServer), true
}

// listChats offers yourself, the groups the account is in, and the contacts
// saved on the phone. Anyone else can be entered by phone number.
func (c *client) listChats(ctx context.Context) (channels.State, error) {
	var options []channels.Option
	if self := c.selfJID(); !self.IsEmpty() {
		options = append(options, channels.Option{Value: self.String(), Label: "You (message yourself)"})
	}
	groups, err := c.api.GetJoinedGroups(ctx)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: "Could not read the groups: " + err.Error()}, nil
	}
	var rest []channels.Option
	for _, g := range groups {
		rest = append(rest, channels.Option{Value: g.JID.String(), Label: "Group · " + g.Name})
	}
	if c.dir != nil {
		if all, err := c.dir.AllContacts(ctx); err == nil {
			for jid, ci := range all {
				// Saved contacts only: a push name alone is a stranger from a group.
				if jid.Server != types.DefaultUserServer || (strings.TrimSpace(ci.FullName) == "" && strings.TrimSpace(ci.FirstName) == "") {
					continue
				}
				rest = append(rest, channels.Option{Value: jid.String(), Label: bestName(ci) + " · +" + jid.User})
			}
		}
	}
	slices.SortFunc(rest, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	const maxOptions = 400 // the picker is a scrolling list, not a search box
	truncated := len(rest) > maxOptions
	options = append(options, rest[:min(len(rest), maxOptions)]...)

	msg := fmt.Sprintf("%d chats. Someone not listed? Enter their phone number below, with the country code.", len(options))
	if truncated {
		msg += " The list is cut at " + fmt.Sprint(maxOptions) + "."
	}
	return channels.State{Kind: channels.StateSelect, Message: msg, Options: options}, nil
}

// --- sending ---

func (c *client) send(ctx context.Context, to types.JID, msg channels.Outbound) error {
	// ReplyTo is not used: a WhatsApp quote carries the quoted message's body,
	// which the Control Plane does not keep.
	post := func(m *waE2E.Message) error {
		id := c.api.GenerateMessageID()
		c.sent.Add(id) // before sending: the echo can beat SendMessage's return
		_, err := c.api.SendMessage(ctx, to, m, whatsmeow.SendRequestExtra{ID: id})
		return err
	}
	sentAny := false
	for _, part := range channels.Chunk(toWhatsApp(msg.Text), maxMessageRunes) {
		if err := post(&waE2E.Message{Conversation: proto.String(part)}); err != nil {
			return err
		}
		sentAny = true
	}
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		m, err := c.mediaMessage(ctx, f)
		if err != nil {
			return err
		}
		if err := post(m); err != nil {
			return err
		}
		sentAny = true
	}
	if sentAny { // stop the "typing…" the inbound ack started
		_ = c.api.SendChatPresence(ctx, to, types.ChatPresencePaused, types.ChatPresenceMediaText)
	}
	return nil
}

// mediaMessage uploads a file and wraps it as the message type WhatsApp shows
// best: photos inline, video and audio playable, everything else a document.
func (c *client) mediaMessage(ctx context.Context, f channels.Attachment) (*waE2E.Message, error) {
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = "file"
	}
	mime := strings.TrimSpace(f.Mime)
	if mime == "" {
		mime = http.DetectContentType(f.Data)
	}
	kind := whatsmeow.MediaDocument
	switch {
	case mime == "image/png" || mime == "image/jpeg" || mime == "image/webp":
		kind = whatsmeow.MediaImage
	case strings.HasPrefix(mime, "video/"):
		kind = whatsmeow.MediaVideo
	case strings.HasPrefix(mime, "audio/"):
		kind = whatsmeow.MediaAudio
	}
	up, err := c.api.Upload(ctx, f.Data, kind)
	if err != nil {
		return nil, fmt.Errorf("upload %s: %w", name, err)
	}
	switch kind {
	case whatsmeow.MediaImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, Mimetype: &mime,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}}, nil
	case whatsmeow.MediaVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, Mimetype: &mime,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}}, nil
	case whatsmeow.MediaAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, Mimetype: &mime,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		}}, nil
	}
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, Mimetype: &mime,
		FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		FileName: &name, Title: &name,
	}}, nil
}

// --- logging ---

// logger routes whatsmeow's logs to the Control Plane's log: warnings and
// errors only, since the library is chatty at info.
type logger struct{ id, mod string }

func (l logger) out(level, format string, args ...any) {
	log.Printf("whatsapp[%s] %s %s: %s", l.id, l.mod, level, fmt.Sprintf(format, args...))
}
func (l logger) Warnf(format string, args ...any)  { l.out("warn", format, args...) }
func (l logger) Errorf(format string, args ...any) { l.out("error", format, args...) }
func (l logger) Infof(string, ...any)              {}
func (l logger) Debugf(string, ...any)             {}
func (l logger) Sub(mod string) waLog.Logger       { return logger{id: l.id, mod: mod} }
