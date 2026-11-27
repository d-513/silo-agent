// Package matrix is the built-in Matrix adapter: a bot account on any
// homeserver, spoken to through the client-server API. The Control Plane
// long-polls /sync with the account's access token, so nothing has to reach
// Silo from the internet, and the token stays on the Control Plane.
//
// A channel is bound to exactly one room. Inbound is live-only: the first sync
// only finds the position to listen from, and nothing said before it, or while
// the homeserver could not be reached for long, is delivered. End-to-end
// encryption is not supported: an encrypted room cannot be read, and the
// channel says so instead of staying silent.
package matrix

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// maxMessageRunes keeps a message's Markdown and the HTML made from it
	// well inside the 65536 bytes a homeserver takes for one event.
	maxMessageRunes = 4000
	// syncTimeout is how long the homeserver holds a sync open with no news.
	syncTimeout = 30 * time.Second
	// syncTimeline is how many events of a room one sync carries. A room that
	// said more between two syncs loses the oldest, which is the live-only rule.
	syncTimeline = 50
	// lookupTimeout bounds one lookup made while reading a message, so a slow
	// homeserver only costs a name, never the message.
	lookupTimeout = 10 * time.Second
	// membersFresh is how long a room's member list is believed when no
	// membership event said it changed.
	membersFresh = 10 * time.Minute
	// staleAfter is how long the homeserver may be out of reach before the
	// sync position is given up: resuming it would deliver what was said
	// meanwhile, which a live-only channel never does.
	staleAfter = 5 * time.Minute
	// maxDownload is a safety net under the host's own cap on what it saves.
	maxDownload = 256 << 20

	encryptedMessage = "This room is encrypted; Silo cannot read it. Matrix end-to-end encryption is not supported: pick a room without encryption, or make a new one with encryption off."
)

//go:embed matrix.svg
var matrixLogo []byte

//go:embed GUIDE.md
var matrixGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(matrixLogo)
}

// Adapter holds one sync session per running channel, and what it has learned
// about the threads it serves.
type Adapter struct {
	mu     sync.Mutex
	conns  map[string]*conn  // channelID -> live session
	last   map[string]string // channelID/room/thread -> newest event there
	seen   *channels.Seen    // channelID/event handed on
	joined *channels.Seen    // channelID/room/thread the bot takes part in

	// retryMin is the first wait after a failed sync; tests shorten it.
	retryMin time.Duration
}

// conn is one signed-in account and what it knows of its rooms.
type conn struct {
	api  *client
	self string // the account's MXID

	mu      sync.Mutex
	name    string            // the account's display name
	rooms   map[string]*room  // by room id
	aliases map[string]string // #alias:server -> room id
}

// room is what a session knows of one room.
type room struct {
	synced    bool // its state came with a sync, so encrypted is known
	name      string
	alias     string
	encrypted bool
	members   map[string]string // joined MXID -> display name; nil until asked
	asked     time.Time
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{
		conns:    map[string]*conn{},
		last:     map[string]string{},
		seen:     channels.NewSeen(4096),
		joined:   channels.NewSeen(4096),
		retryMin: 2 * time.Second,
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "matrix",
		Name:           "Matrix",
		Description:    "A bot account on any Matrix homeserver.",
		Logo:           logoURL(),
		Guide:          matrixGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_rooms", Label: "Pick a room", Kind: channels.ActionPick, Description: "Choose the Matrix room or direct chat this channel talks to."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the connection and re-read the account's details."},
		},
		Fields: []channels.Field{
			{Key: "homeserver_url", Label: "Homeserver URL", Type: channels.FieldText, Required: true, Description: "Where the bot's account lives, e.g. https://matrix.org."},
			{Key: "access_token", Label: "Access token", Type: channels.FieldSecret, Required: true, Description: "The bot account's access token."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
		},
	}
}

func (a *Adapter) Validate(ctx context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	c, err := login(ctx, cfg)
	if err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, err
	}
	return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)}, nil
}

func connectedMessage(c *conn) string { return "Connected as " + c.self }

// login asks the homeserver whose token this is. An address that does not
// answer as a homeserver is taken as a server name and looked up the way
// clients do (.well-known), so "example.org" works where the API lives on
// matrix.example.org.
func login(ctx context.Context, cfg channels.Config) (*conn, error) {
	api, err := newClient(cfg)
	if err != nil {
		return nil, channels.Permanent(err)
	}
	var who struct {
		UserID string `json:"user_id"`
	}
	err = api.call(ctx, "GET", "/_matrix/client/v3/account/whoami", nil, nil, &who)
	if notMatrix(err) {
		if base := api.discover(ctx); base != "" && base != api.base {
			api.base = base
			err = api.call(ctx, "GET", "/_matrix/client/v3/account/whoami", nil, nil, &who)
		}
	}
	var ae *apiError
	switch {
	case errors.As(err, &ae) && ae.Status == 403:
		return nil, channels.Permanent(fmt.Errorf("the homeserver refused the access token (%s) — copy the bot account's token again", ae))
	case notMatrix(err):
		return nil, fmt.Errorf("%s does not answer as a Matrix homeserver (%s) — check the homeserver URL", api.base, ae)
	case err != nil:
		return nil, friendly(err)
	case who.UserID == "":
		return nil, errors.New("the homeserver did not say whose token this is")
	}
	c := &conn{api: api, self: who.UserID, rooms: map[string]*room{}, aliases: map[string]string{}}
	c.name = c.profileName(ctx, c.self)
	return c, nil
}

// session is the live session, or a fresh one from the config: nothing but
// listening needs the sync loop, so a channel can send and read while its
// worker is down.
func (a *Adapter) session(ctx context.Context, ch *db.Channel, cfg channels.Config) (*conn, error) {
	if c := a.conn(ch.ID); c != nil {
		return c, nil
	}
	return login(ctx, cfg)
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	c, err := login(ctx, cfg)
	if err != nil {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	a.putConn(ch.ID, c)
	defer a.dropConn(ch.ID, c)

	var (
		since  string
		shown  string // the state last published, so one is published when it changes
		lastOK = time.Now()
		wait   = a.retryMin
	)
	for {
		resp, err := c.sync(ctx, since)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			err = friendly(err)
			if channels.IsPermanent(err) {
				host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
				return err
			}
			shown = fmt.Sprintf("Cannot reach the homeserver, trying again in %s: %v", wait.Round(time.Second), err)
			host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: shown})
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			wait = min(wait*2, time.Minute)
			if time.Since(lastOK) > staleAfter {
				since = ""
			}
			continue
		}
		lastOK, wait = time.Now(), a.retryMin
		first := since == ""
		since = resp.NextBatch

		// Re-read the row so a room picked after Start takes effect now.
		live := ch
		if cur, ok := host.CurrentChannel(ch.ID); ok {
			live = cur
		}
		a.accept(ctx, c, resp)
		c.learn(resp)
		bound := ""
		if strings.TrimSpace(live.ExternalID) != "" {
			if bound, err = c.target(ctx, live.ExternalID); err != nil {
				log.Printf("matrix channel %s: %v", ch.ID, err)
			}
		}
		if !first && bound != "" {
			for _, ev := range resp.Rooms.Join[bound].Timeline.Events {
				if ev.Type != "m.room.message" {
					continue
				}
				if err := a.onMessage(ctx, live, cfg, host, c, bound, ev); err != nil && ctx.Err() == nil {
					log.Printf("matrix channel %s: inbound: %v", ch.ID, err)
				}
			}
		}
		st := channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)}
		if bound != "" && c.knownEncrypted(bound) {
			st = channels.State{Kind: channels.StateError, Message: encryptedMessage}
		}
		if st.Message != shown {
			shown = st.Message
			host.PublishState(ch.ID, st)
		}
	}
}

// accept joins the rooms the account is invited to, so they can be picked.
// The bot still talks only in the room its channel is bound to.
func (a *Adapter) accept(ctx context.Context, c *conn, resp *syncResponse) {
	for id := range resp.Rooms.Invite {
		jctx, cancel := context.WithTimeout(ctx, lookupTimeout)
		err := c.api.call(jctx, "POST", "/_matrix/client/v3/join/"+url.PathEscape(id), nil, struct{}{}, nil)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("matrix: joining %s as %s: %v", id, c.self, err)
		}
	}
}

// --- sync ---

// event is one room event, as much of it as this adapter reads.
type event struct {
	Type     string          `json:"type"`
	ID       string          `json:"event_id"`
	Sender   string          `json:"sender"`
	StateKey *string         `json:"state_key"`
	TS       int64           `json:"origin_server_ts"`
	Content  json.RawMessage `json:"content"`
}

// content is the union of the event contents read: a message's, and the three
// state events a room is named and judged by.
type content struct {
	MsgType       string `json:"msgtype"`
	Body          string `json:"body"`
	FormattedBody string `json:"formatted_body"`
	Filename      string `json:"filename"`
	URL           string `json:"url"`
	Info          struct {
		Mimetype string  `json:"mimetype"`
		Size     float64 `json:"size"`
	} `json:"info"`
	Voice    json.RawMessage `json:"org.matrix.msc3245.voice"`
	Mentions struct {
		UserIDs []string `json:"user_ids"`
	} `json:"m.mentions"`
	Relates struct {
		RelType   string `json:"rel_type"`
		EventID   string `json:"event_id"`
		InReplyTo struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
	} `json:"m.relates_to"`

	Name  string `json:"name"`  // m.room.name
	Alias string `json:"alias"` // m.room.canonical_alias
}

type syncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join map[string]struct {
			State struct {
				Events []event `json:"events"`
			} `json:"state"`
			Timeline struct {
				Events []event `json:"events"`
			} `json:"timeline"`
		} `json:"join"`
		Invite map[string]json.RawMessage `json:"invite"`
	} `json:"rooms"`
}

// syncFilter keeps a sync to what is read: the state a room is named and
// judged by, and a short timeline. Members are asked for when needed.
func syncFilter(timeline int) string {
	b, _ := json.Marshal(map[string]any{
		"presence":     map[string]any{"types": []string{}},
		"account_data": map[string]any{"types": []string{}},
		"room": map[string]any{
			"account_data": map[string]any{"types": []string{}},
			"ephemeral":    map[string]any{"types": []string{}},
			"state": map[string]any{
				"types":             []string{"m.room.name", "m.room.canonical_alias", "m.room.encryption"},
				"lazy_load_members": true,
			},
			"timeline": map[string]any{"limit": timeline},
		},
	})
	return string(b)
}

// sync asks for what happened after since. With no since it only reads where
// the account's rooms stand now and returns at once.
func (c *conn) sync(ctx context.Context, since string) (*syncResponse, error) {
	q := url.Values{}
	if since == "" {
		q.Set("timeout", "0")
		q.Set("filter", syncFilter(1))
	} else {
		q.Set("since", since)
		q.Set("timeout", fmt.Sprint(syncTimeout.Milliseconds()))
		q.Set("filter", syncFilter(syncTimeline))
	}
	ctx, cancel := context.WithTimeout(ctx, 2*syncTimeout+15*time.Second)
	defer cancel()
	var resp syncResponse
	if err := c.api.call(ctx, "GET", "/_matrix/client/v3/sync", q, nil, &resp); err != nil {
		return nil, err
	}
	if resp.NextBatch == "" {
		return nil, errors.New("the homeserver's sync answer names no position to continue from")
	}
	return &resp, nil
}

// learn keeps the room cache current with a sync's state and timelines.
func (c *conn) learn(resp *syncResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, jr := range resp.Rooms.Join {
		r := c.room(id)
		r.synced = true
		for _, ev := range slices.Concat(jr.State.Events, jr.Timeline.Events) {
			if ev.Type == "m.room.encrypted" {
				r.encrypted = true
				continue
			}
			if ev.StateKey == nil {
				continue
			}
			var m content
			_ = json.Unmarshal(ev.Content, &m)
			switch ev.Type {
			case "m.room.name":
				r.name = strings.TrimSpace(m.Name)
			case "m.room.canonical_alias":
				r.alias = strings.TrimSpace(m.Alias)
			case "m.room.encryption":
				r.encrypted = true // encryption cannot be turned off again
			case "m.room.member":
				r.members = nil
			}
		}
	}
}

// room is the cache entry of a room, made when missing. Callers hold c.mu.
func (c *conn) room(id string) *room {
	r := c.rooms[id]
	if r == nil {
		r = &room{}
		c.rooms[id] = r
	}
	return r
}

func (c *conn) knownEncrypted(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.rooms[id]
	return r != nil && r.encrypted
}

// encrypted reports whether a room has encryption on, asking the homeserver
// when no sync has told this session.
func (c *conn) encrypted(ctx context.Context, id string) (bool, error) {
	c.mu.Lock()
	r := c.rooms[id]
	if r != nil && (r.synced || r.encrypted) {
		defer c.mu.Unlock()
		return r.encrypted, nil
	}
	c.mu.Unlock()
	err := c.api.call(ctx, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(id)+"/state/m.room.encryption", nil, nil, nil)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == 404 {
		return false, nil
	}
	if err != nil {
		return false, friendly(err)
	}
	c.mu.Lock()
	c.room(id).encrypted = true
	c.mu.Unlock()
	return true, nil
}

// --- inbound ---

// fileTypes are the message types that carry a file.
var fileTypes = map[string]bool{"m.image": true, "m.file": true, "m.audio": true, "m.video": true}

// onMessage turns an m.room.message of the bound room into an Inbound.
func (a *Adapter) onMessage(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, c *conn, roomID string, ev event) error {
	if ev.ID == "" || ev.Sender == "" || ev.Sender == c.self {
		return nil
	}
	var m content
	if err := json.Unmarshal(ev.Content, &m); err != nil {
		return nil // a redacted or malformed event says nothing
	}
	// m.notice is how other bots speak, and answering one is a loop; an edit
	// (m.replace) is not a new message.
	text, file := m.MsgType == "m.text" || m.MsgType == "m.emote", fileTypes[m.MsgType]
	if (!text && !file) || m.Relates.RelType == "m.replace" {
		return nil
	}
	key := ch.ID + "/" + ev.ID
	if a.seen.Has(key) {
		return nil
	}
	thread := ""
	if m.Relates.RelType == "m.thread" {
		thread = m.Relates.EventID
	}
	body := m.Body
	if m.Relates.InReplyTo.EventID != "" {
		body = withoutReplyQuote(body)
	}
	members := c.members(ctx, roomID)
	// A room of two is a direct chat. With the members unknown it is taken
	// for a group, which errs towards staying quiet.
	dm := len(members) > 0 && len(members) <= 2
	addressed := dm || c.mentioned(m, body, members[c.self]) ||
		(thread != "" && a.joined.Has(threadKey(ch.ID, roomID, thread)))
	if !addressed && cfg.Get("reply_mode") != "always" {
		return nil
	}
	author := c.memberName(ctx, roomID, ev.Sender)
	in := channels.Inbound{
		ExternalID: roomID,
		Title:      c.title(ctx, roomID),
		Author:     author,
		AuthorID:   ev.Sender,
		ReplyTo:    thread,
	}
	switch {
	case file:
		media, ok := c.mediaOf(m)
		if !ok {
			return nil
		}
		in.Files = []channels.Media{media}
		// With a filename, the body is a caption; without, it is the name.
		if m.Filename != "" && m.Filename != body {
			in.Text = strings.TrimSpace(body)
		}
	case m.MsgType == "m.emote":
		in.Text = "* " + author + " " + strings.TrimSpace(body)
	default:
		in.Text = strings.TrimSpace(body)
	}
	if in.Text == "" && len(in.Files) == 0 {
		return nil
	}
	a.seen.Add(key)
	if thread != "" {
		// A thread the bot answers in is one it takes part in: what is said
		// there next is said to it.
		a.joined.Add(threadKey(ch.ID, roomID, thread))
		a.setLast(ch.ID, roomID, thread, ev.ID)
	}
	return host.DeliverInbound(ctx, ch, in)
}

func threadKey(channelID, roomID, thread string) string {
	return channelID + "/" + roomID + "/" + thread
}

// withoutReplyQuote drops the quoted original older clients put at the head of
// a reply's body ("> <@user:server> …", then a blank line): it is not what the
// sender wrote, and it names whoever is replied to.
func withoutReplyQuote(body string) string {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) && strings.HasPrefix(lines[i], "> ") {
		i++
	}
	if i == 0 || i >= len(lines) || strings.TrimSpace(lines[i]) != "" {
		return body
	}
	return strings.Join(lines[i+1:], "\n")
}

// mentioned reports whether a message names the bot: in m.mentions, as clients
// have sent since Matrix 1.7, or in its words, as older ones and people do.
func (c *conn) mentioned(m content, body, roomName string) bool {
	if slices.Contains(m.Mentions.UserIDs, c.self) {
		return true
	}
	if strings.Contains(body, c.self) || strings.Contains(m.FormattedBody, c.self) {
		return true
	}
	c.mu.Lock()
	profile := c.name
	c.mu.Unlock()
	return named(body, roomName) || named(body, profile) || named(body, localpart(c.self))
}

// named reports whether name stands in text as a word of its own, whatever
// its case: "silo" is in "hey Silo," and not in "silos".
func named(text, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	text = strings.ToLower(text)
	wordRune := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	for from := 0; ; {
		i := strings.Index(text[from:], name)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(name)
		before, after := []rune(text[:start]), []rune(text[end:])
		if (len(before) == 0 || !wordRune(before[len(before)-1])) && (len(after) == 0 || !wordRune(after[0])) {
			return true
		}
		from = end
	}
}

// localpart is the name in @name:server.
func localpart(mxid string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(mxid, "@"), ":")
	return name
}

// mediaOf is the file a message carries, fetched when the host asks.
func (c *conn) mediaOf(m content) (channels.Media, bool) {
	server, id, ok := parseMXC(m.URL)
	if !ok {
		return channels.Media{}, false // no address, or a file encrypted for someone else
	}
	name := strings.TrimSpace(m.Filename)
	if name == "" {
		name = strings.TrimSpace(m.Body)
	}
	if name == "" {
		name = "file"
	}
	return channels.Media{
		Name: name, Mime: m.Info.Mimetype, Size: int64(m.Info.Size),
		Voice: m.MsgType == "m.audio" && len(m.Voice) > 0,
		Fetch: func(ctx context.Context) ([]byte, error) { return c.api.download(ctx, server, id) },
	}, true
}

// parseMXC splits mxc://server/mediaId.
func parseMXC(uri string) (server, id string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(uri), "mxc://")
	if !found {
		return "", "", false
	}
	server, id, _ = strings.Cut(rest, "/")
	return server, id, server != "" && id != "" && !strings.Contains(id, "/")
}

// --- names ---

// members is who has joined a room, by MXID, with the names they show there.
// nil when the homeserver would not say.
func (c *conn) members(ctx context.Context, roomID string) map[string]string {
	c.mu.Lock()
	if r := c.rooms[roomID]; r != nil && r.members != nil && time.Since(r.asked) < membersFresh {
		defer c.mu.Unlock()
		return r.members
	}
	c.mu.Unlock()
	var resp struct {
		Joined map[string]struct {
			DisplayName string `json:"display_name"`
		} `json:"joined"`
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	if err := c.api.call(ctx, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/joined_members", nil, nil, &resp); err != nil {
		return nil
	}
	members := make(map[string]string, len(resp.Joined))
	for id, m := range resp.Joined {
		members[id] = strings.TrimSpace(m.DisplayName)
	}
	c.mu.Lock()
	r := c.room(roomID)
	r.members, r.asked = members, time.Now()
	c.mu.Unlock()
	return members
}

// memberName is how a person shows in a room: the name they set there, else
// their profile's, else the name in their MXID.
func (c *conn) memberName(ctx context.Context, roomID, user string) string {
	members := c.members(ctx, roomID)
	if name := members[user]; name != "" {
		return name
	}
	if _, joined := members[user]; !joined {
		if name := c.profileName(ctx, user); name != "" {
			return name
		}
	}
	return localpart(user)
}

// profileName is a user's global display name, "" when unset or not told.
func (c *conn) profileName(ctx context.Context, user string) string {
	var p struct {
		DisplayName string `json:"displayname"`
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	if err := c.api.call(ctx, "GET", "/_matrix/client/v3/profile/"+url.PathEscape(user)+"/displayname", nil, nil, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.DisplayName)
}

// title names a room: its name, else its alias, else who is in it
// ("DM · Alice" for a direct chat).
func (c *conn) title(ctx context.Context, roomID string) string {
	c.mu.Lock()
	var name, alias string
	if r := c.rooms[roomID]; r != nil {
		name, alias = r.name, r.alias
	}
	c.mu.Unlock()
	if name != "" {
		return name
	}
	if alias != "" {
		return alias
	}
	var others []string
	for id, shown := range c.members(ctx, roomID) {
		if id == c.self {
			continue
		}
		if shown == "" {
			shown = localpart(id)
		}
		others = append(others, shown)
	}
	sort.Strings(others)
	switch {
	case len(others) == 0:
		return roomID
	case len(others) == 1:
		return "DM · " + others[0]
	case len(others) > 3:
		return fmt.Sprintf("%s and %d more", strings.Join(others[:3], ", "), len(others)-3)
	}
	return strings.Join(others, ", ")
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

// setLast remembers a thread's newest event, which a reply there points at.
func (a *Adapter) setLast(channelID, roomID, thread, eventID string) {
	a.mu.Lock()
	if len(a.last) > 10000 {
		a.last = map[string]string{}
	}
	a.last[threadKey(channelID, roomID, thread)] = eventID
	a.mu.Unlock()
}

func (a *Adapter) lastIn(channelID, roomID, thread string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last[threadKey(channelID, roomID, thread)]
}
