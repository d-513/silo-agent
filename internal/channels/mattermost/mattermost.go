// Package mattermost is the built-in Mattermost adapter: a bot account (or a
// personal access token) on a server the owner names, over REST API v4 and its
// websocket. The Control Plane dials the server, so nothing has to reach Silo,
// and the token stays here.
//
// A channel is bound to exactly one Mattermost channel: a public or private
// channel the bot is a member of, a group message, or a direct message.
// Inbound is live-only, as the websocket delivers: nothing posted while the
// bot was offline is replayed. Answers go to the thread a post was written in,
// or (in a channel, unless the owner said otherwise) start one under it.
package mattermost

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// maxMessageRunes keeps clear of the 16383 characters a post may hold.
	maxMessageRunes = 16000
	// filesPerPost is the server's default cap on a post's attachments.
	filesPerPost = 5
	// lookupTimeout bounds one lookup made while reading a post, so a slow
	// server only costs a name, never the message.
	lookupTimeout = 10 * time.Second
	// maxDownload is a safety net under the host's own cap on what it saves.
	maxDownload = 256 << 20
	// maxFrame is the largest websocket event read: a full post is 16383
	// characters, carried as a JSON string inside JSON.
	maxFrame = 4 << 20
	// dialTimeout bounds the websocket handshake.
	dialTimeout = 30 * time.Second
	// maxRetry is the longest wait between two tries to reconnect.
	maxRetry = time.Minute
)

//go:embed mattermost.svg
var logo []byte

//go:embed GUIDE.md
var guide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(logo)
}

// Adapter holds one session per running channel, and what it has learned
// about the people and threads it serves.
type Adapter struct {
	mu     sync.Mutex
	conns  map[string]*conn  // channelID -> live session
	users  map[string]user   // channelID/userID -> who that is
	thread map[string]string // channelID/conversation -> thread of the newest post handed on
	seen   *channels.Seen    // channelID/postID handed on
	joined *channels.Seen    // channelID/rootID threads the bot takes part in

	// retry is the first wait before reconnecting (doubling up to maxRetry)
	// and pingEvery how often a live socket is pinged; tests shorten both.
	retry     time.Duration
	pingEvery time.Duration
}

// conn is one signed-in session.
type conn struct {
	api  *api
	self string // the bot's user id
	name string // its username
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{
		conns:     map[string]*conn{},
		users:     map[string]user{},
		thread:    map[string]string{},
		seen:      channels.NewSeen(4096),
		joined:    channels.NewSeen(4096),
		retry:     time.Second,
		pingEvery: 30 * time.Second,
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "mattermost",
		Name:           "Mattermost",
		Description:    "A bot account on your Mattermost server.",
		Logo:           logoURL(),
		Guide:          guide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_channels", Label: "Pick a channel", Kind: channels.ActionPick, Description: "Choose the Mattermost channel, group message or direct message this channel talks to."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the connection and re-read the bot's details."},
		},
		Fields: []channels.Field{
			{Key: "server_url", Label: "Server address", Type: channels.FieldText, Required: true, Description: "Where your Mattermost is, e.g. https://chat.example.com."},
			{Key: "bot_token", Label: "Access token", Type: channels.FieldSecret, Required: true, Description: "The bot account's token (System Console → Integrations → Bot Accounts), or a personal access token."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
			{Key: "reply_in", Label: "Answer in", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "thread", Label: "A thread under the message"},
				{Value: "channel", Label: "The channel itself"},
			}},
		},
	}
}

func (a *Adapter) Validate(ctx context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	fail := func(err error) (channels.State, error) {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, err
	}
	cl, err := newAPI(cfg)
	if err != nil {
		return fail(err)
	}
	defer cl.close()
	c, err := login(ctx, cl)
	if err != nil {
		return fail(err)
	}
	return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)}, nil
}

// login asks the server who the token belongs to.
func login(ctx context.Context, cl *api) (*conn, error) {
	var me user
	if err := cl.get(ctx, "/users/me", &me); err != nil {
		return nil, friendly(err)
	}
	if me.ID == "" {
		return nil, errors.New("the server did not say who the token belongs to — is the address a Mattermost server?")
	}
	return &conn{api: cl, self: me.ID, name: me.Username}, nil
}

func connectedMessage(c *conn) string { return "Connected as @" + c.name }

// session is the live session, or a fresh one from the config: the REST API
// needs no websocket, so a channel can send and read while its worker is
// down. done lets go of a fresh one's connections.
func (a *Adapter) session(ctx context.Context, ch *db.Channel, cfg channels.Config) (c *conn, done func(), err error) {
	if c := a.conn(ch.ID); c != nil {
		return c, func() {}, nil
	}
	cl, err := newAPI(cfg)
	if err != nil {
		return nil, nil, err
	}
	c, err = login(ctx, cl)
	if err != nil {
		cl.close()
		return nil, nil, err
	}
	return c, cl.close, nil
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	fail := func(err error) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	cl, err := newAPI(cfg)
	if err != nil {
		return fail(channels.Permanent(err))
	}
	defer cl.close()
	c, err := login(ctx, cl)
	if err != nil {
		return fail(err)
	}
	a.putConn(ch.ID, c)
	defer a.dropConn(ch.ID, c)

	// Reading a post may ask the server for names and file details, so it
	// happens in order on its own goroutine: the socket's reader must stay
	// free to answer pings.
	run, stop := context.WithCancel(ctx)
	defer stop()
	work := make(chan posted, 256)
	go func() {
		for {
			select {
			case <-run.Done():
				return
			case p := <-work:
				if err := a.onPost(run, ch, cfg, host, c, p); err != nil && run.Err() == nil {
					log.Printf("mattermost channel %s: inbound: %v", ch.ID, err)
				}
			}
		}
	}()

	host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connecting to Mattermost…"})
	wait := a.retry
	for {
		authed, err := a.listen(run, ch, host, c, work)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if authed {
			wait = a.retry
		}
		// The server hangs up on a token it does not know without a word, so
		// ask it over REST whether the token is still good before trying again.
		if _, lerr := login(ctx, cl); channels.IsPermanent(lerr) {
			return fail(lerr)
		}
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: fmt.Sprintf("Lost the connection to Mattermost, trying again in %s: %v", wait.Round(time.Second), err)})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(wait*2, maxRetry)
	}
}

// frame is one websocket message: an event, or the reply to a request.
type frame struct {
	Event    string                     `json:"event"`
	Status   string                     `json:"status"`
	SeqReply int64                      `json:"seq_reply"`
	Data     map[string]json.RawMessage `json:"data"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// str reads a string field of an event's data, "" when it is anything else.
func (f frame) str(key string) string {
	var s string
	_ = json.Unmarshal(f.Data[key], &s)
	return s
}

// authSeq numbers the authentication request, so its reply is known.
const authSeq = 1

// listen holds one websocket until it ends. authed says whether the server
// took the token on it, which is what makes the next wait short again.
func (a *Adapter) listen(ctx context.Context, ch *db.Channel, host channels.Host, c *conn, work chan<- posted) (authed bool, err error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	ws, _, err := websocket.Dial(dctx, c.api.base+"/api/v4/websocket", &websocket.DialOptions{HTTPClient: c.api.hc})
	cancel()
	if err != nil {
		return false, err
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(maxFrame)

	challenge, _ := json.Marshal(map[string]any{
		"seq": authSeq, "action": "authentication_challenge", "data": map[string]string{"token": c.api.token},
	})
	wctx, cancel := context.WithTimeout(ctx, dialTimeout)
	err = ws.Write(wctx, websocket.MessageText, challenge)
	cancel()
	if err != nil {
		return false, err
	}

	run, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		// A socket that stops answering pings is closed, which ends the read
		// below and so brings a new one.
		tick := time.NewTicker(a.pingEvery)
		defer tick.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-tick.C:
				pctx, cancel := context.WithTimeout(run, lookupTimeout)
				err := ws.Ping(pctx)
				cancel()
				if err != nil {
					_ = ws.CloseNow()
					return
				}
			}
		}
	}()

	for {
		_, data, err := ws.Read(run)
		if err != nil {
			if !authed {
				return false, fmt.Errorf("the server closed the connection before signing the bot in: %w", err)
			}
			return true, err
		}
		var f frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch {
		case f.SeqReply == authSeq && f.Status != "OK":
			reason := "no reason given"
			if f.Error != nil && f.Error.Message != "" {
				reason = f.Error.Message
			}
			return false, fmt.Errorf("the server did not sign the bot in: %s", reason)
		case f.SeqReply == authSeq, f.Event == "hello":
			if !authed {
				authed = true
				host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)})
			}
		case f.Event == "posted":
			p := posted{kind: f.str("channel_type"), title: f.str("channel_display_name")}
			if json.Unmarshal([]byte(f.str("post")), &p.post) != nil {
				continue
			}
			_ = json.Unmarshal([]byte(f.str("mentions")), &p.mentions)
			select {
			case work <- p:
			case <-run.Done():
			}
		}
	}
}

// --- inbound ---

// post is a Mattermost post as the API and the websocket carry it.
type post struct {
	ID        string         `json:"id"`
	CreateAt  int64          `json:"create_at"`
	UserID    string         `json:"user_id"`
	ChannelID string         `json:"channel_id"`
	RootID    string         `json:"root_id"`
	Message   string         `json:"message"`
	Type      string         `json:"type"`
	Props     map[string]any `json:"props"`
	FileIDs   []string       `json:"file_ids"`
	Metadata  struct {
		Files []fileInfo `json:"files"`
	} `json:"metadata"`
}

type fileInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// user is a Mattermost account.
type user struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Nickname  string `json:"nickname"`
}

// display is how a person shows: their full name, else their nickname, else
// their username.
func (u user) display() string {
	full := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	for _, n := range []string{full, strings.TrimSpace(u.Nickname), u.Username} {
		if n != "" {
			return n
		}
	}
	return u.ID
}

// posted is one "posted" event: the post, and what the event says around it.
type posted struct {
	post     post
	kind     string   // the channel's type: O public, P private, D direct, G group
	title    string   // the channel's display name
	mentions []string // user ids the post mentions
}

// private reports whether a channel type is a direct or group message, where
// everything is said to the bot.
func private(kind string) bool { return kind == "D" || kind == "G" }

// fromApp reports whether a post was made by a bot account or a webhook.
func fromApp(p post) bool {
	for _, key := range []string{"from_bot", "from_webhook"} {
		if v, _ := p.Props[key].(string); v == "true" {
			return true
		}
	}
	return false
}

// onPost turns a post into an Inbound for the bound channel.
func (a *Adapter) onPost(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, c *conn, ev posted) error {
	p := ev.post
	if p.ID == "" || p.UserID == "" || p.UserID == c.self || fromApp(p) {
		return nil // never answer a bot (this one included): that is a loop
	}
	if p.Type != "" {
		return nil // joins, header changes and the other system posts
	}
	key := ch.ID + "/" + p.ID
	if a.seen.Has(key) {
		return nil
	}
	// Re-read the row so a target picked after Start takes effect now.
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	if p.ChannelID == "" || p.ChannelID != targetID(live.ExternalID) {
		return nil
	}
	dm := private(ev.kind)
	addressed := dm || slices.Contains(ev.mentions, c.self) || atName(p.Message, c.name) >= 0 ||
		(p.RootID != "" && a.joined.Has(ch.ID+"/"+p.RootID))
	if !addressed && cfg.Get("reply_mode") != "always" {
		return nil
	}
	text := strings.TrimSpace(withoutLeadingMention(p.Message, c.name))
	files := a.mediaOf(ctx, c, p)
	if text == "" && len(files) == 0 {
		return nil
	}
	a.seen.Add(key)
	reply := p.RootID
	if reply == "" && !dm && cfg.Get("reply_in") != "channel" {
		reply = p.ID // start a thread under the post
	}
	if reply != "" {
		// A thread the bot answers in is one it takes part in: what is said
		// there next is said to it.
		a.joined.Add(ch.ID + "/" + reply)
	}
	a.setThread(ch.ID, p.ChannelID, reply)
	author := a.user(ctx, c, ch.ID, p.UserID).display()
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: p.ChannelID,
		Title:      conversationTitle(ev.kind, ev.title, author),
		Author:     author,
		AuthorID:   p.UserID,
		Text:       text,
		ReplyTo:    reply,
		Files:      files,
	})
}

// conversationTitle names a conversation: the channel's name, "DM · Alice" or
// "Group message · alice, bob".
func conversationTitle(kind, title, author string) string {
	switch kind {
	case "D":
		return "DM · " + author
	case "G":
		return "Group message · " + title
	}
	return title
}

// nameRune reports whether r can be part of a Mattermost username.
func nameRune(r byte) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_'
}

// nameGoesOn reports whether rest, the text right after a name, is more of
// that name. A dot or dash that ends a word is punctuation, not the name's.
func nameGoesOn(rest string) bool {
	if rest == "" || !nameRune(rest[0]) {
		return false
	}
	if rest[0] == '.' || rest[0] == '-' {
		return len(rest) > 1 && nameRune(rest[1])
	}
	return true
}

// atName finds the first @name in text that is that name and not the start of
// a longer one, -1 when there is none. Usernames match in any case.
func atName(text, name string) int {
	if name == "" {
		return -1
	}
	low, want := strings.ToLower(text), "@"+strings.ToLower(name)
	for from := 0; ; {
		i := strings.Index(low[from:], want)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(want)
		if !nameGoesOn(low[end:]) {
			return i
		}
		from = end
	}
}

// withoutLeadingMention drops the "@bot" a message opens with: it is how the
// bot was called, not part of what was said.
func withoutLeadingMention(text, name string) string {
	t := strings.TrimSpace(text)
	if atName(t, name) != 0 {
		return text
	}
	return strings.TrimLeft(t[len(name)+1:], " \t,:")
}

// mediaOf is what a post has attached. The event usually carries the files'
// details; the server is asked for the ones it does not.
func (a *Adapter) mediaOf(ctx context.Context, c *conn, p post) []channels.Media {
	known := map[string]fileInfo{}
	for _, f := range p.Metadata.Files {
		known[f.ID] = f
	}
	var out []channels.Media
	for _, id := range p.FileIDs {
		f, ok := known[id]
		if !ok {
			lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
			_ = c.api.get(lctx, "/files/"+id+"/info", &f)
			cancel()
		}
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = "file"
		}
		out = append(out, channels.Media{
			Name: name, Mime: f.MimeType, Size: f.Size,
			Fetch: func(ctx context.Context) ([]byte, error) {
				data, err := c.api.call(ctx, "GET", "/files/"+id, "", nil)
				return data, friendly(err)
			},
		})
	}
	return out
}

// user looks an account up, once per channel.
func (a *Adapter) user(ctx context.Context, c *conn, channelID, id string) user {
	key := channelID + "/" + id
	a.mu.Lock()
	cached, ok := a.users[key]
	a.mu.Unlock()
	if ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	u := user{ID: id}
	if err := c.api.get(ctx, "/users/"+id, &u); err != nil {
		return user{ID: id}
	}
	a.mu.Lock()
	if len(a.users) > 10000 {
		a.users = map[string]user{}
	}
	a.users[key] = u
	a.mu.Unlock()
	return u
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

// setThread remembers where the newest post handed on is answered, which is
// where the bot shows as typing.
func (a *Adapter) setThread(channelID, conversation, thread string) {
	a.mu.Lock()
	a.thread[channelID+"/"+conversation] = thread
	a.mu.Unlock()
}
