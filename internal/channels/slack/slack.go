// Package slack is the built-in Slack adapter. It is a Slack app the owner
// makes in their own workspace, connected over Socket Mode: the Control Plane
// dials Slack (apps.connections.open, then a websocket) with the app-level
// token, so nothing has to reach Silo from the internet, and talks to the Web
// API with the bot token. Both tokens stay on the Control Plane.
//
// A channel is bound to exactly one Slack conversation: a channel the app is
// in, a group DM, or a DM someone opened with it. Inbound is live-only, as
// Socket Mode delivers: nothing sent while the app was offline is replayed.
// Answers go to the thread a message was written in, or (in a channel, unless
// the owner said otherwise) start one under it.
package slack

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const (
	// maxMessageRunes keeps clear of the 4000 characters Slack advises for a
	// message's text; turning Markdown into Slack's markup adds a little.
	maxMessageRunes = 3500
	// workingReaction is put on the message the Bot is working on (Slack has
	// no typing indicator for apps) and taken off when it is done.
	workingReaction = "eyes"
	// lookupTimeout bounds one name lookup made while reading a message, so a
	// slow Slack only costs a name, never the message.
	lookupTimeout = 10 * time.Second
	// maxDownload is a safety net under the host's own cap on what it saves.
	maxDownload = 256 << 20
	// filesPerMessage is how many uploads one message carries.
	filesPerMessage = 10
	// historyThreads is how many of the newest threads a history read opens:
	// with answers in threads, a conversation lives mostly in them.
	historyThreads = 5
)

//go:embed slack.svg
var slackLogo []byte

//go:embed GUIDE.md
var slackGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(slackLogo)
}

// Adapter holds one Socket Mode session per running channel, and what it has
// learned about the conversations it serves.
type Adapter struct {
	mu     sync.Mutex
	conns  map[string]*conn    // channelID -> live session
	names  map[string]string   // channelID/userID -> display name
	titles map[string]string   // channelID/conversation -> title
	last   map[string]string   // channelID/conversation -> ts of the newest message handed on
	marked map[string][]string // channelID/conversation -> messages wearing the working reaction
	seen   *channels.Seen      // channelID/conversation/ts handed on (Slack retries a slow ack)
	joined *channels.Seen      // channelID/conversation/thread_ts the app takes part in

	// apiURL is Slack's Web API (slackapi.APIURL); tests point it at a fake.
	apiURL string
}

// conn is one live app session.
type conn struct {
	api   *slackapi.Client
	self  string // the app's bot user (U…)
	botID string // the app's bot (B…)
	name  string // the bot user's handle
	team  string // the workspace's name
	mark  bool   // put the working reaction on (the "While working" setting)
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{
		conns:  map[string]*conn{},
		names:  map[string]string{},
		titles: map[string]string{},
		last:   map[string]string{},
		marked: map[string][]string{},
		seen:   channels.NewSeen(4096),
		joined: channels.NewSeen(4096),
		apiURL: slackapi.APIURL,
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "slack",
		Name:           "Slack",
		Description:    "A Slack app in your workspace, over Socket Mode.",
		Logo:           logoURL(),
		Guide:          slackGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_channels", Label: "Pick a conversation", Kind: channels.ActionPick, Description: "Choose the Slack channel, group DM or DM this channel talks to."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the connection and re-read the app's details."},
		},
		Fields: []channels.Field{
			{Key: "bot_token", Label: "Bot token", Type: channels.FieldSecret, Required: true, Description: "Bot User OAuth Token, xoxb-…, from OAuth & Permissions after installing the app."},
			{Key: "app_token", Label: "App-level token", Type: channels.FieldSecret, Required: true, Description: "xapp-…, from Basic Information → App-Level Tokens, with the connections:write scope."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
			{Key: "reply_in", Label: "Answer in", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "thread", Label: "A thread under the message"},
				{Value: "channel", Label: "The channel itself"},
			}},
			{Key: "working", Label: "While working", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "reaction", Label: "Mark the message with 👀"},
				{Value: "off", Label: "Show nothing"},
			}},
		},
	}
}

// checkTokens catches the usual mix-ups before Slack is asked: the two tokens
// swapped, or a user token, which would have the Bot speak as a person.
func checkTokens(cfg channels.Config) error {
	bot, app := strings.TrimSpace(cfg.Get("bot_token")), strings.TrimSpace(cfg.Get("app_token"))
	switch {
	case bot == "":
		return errors.New("bot_token is required")
	case strings.HasPrefix(bot, "xapp-"):
		return errors.New("the bot token field holds the app-level token: the Bot User OAuth Token starts with xoxb- (OAuth & Permissions)")
	case strings.HasPrefix(bot, "xoxp-"):
		return errors.New("that is a user token: use the Bot User OAuth Token (xoxb-), so the Bot speaks as itself, not as you")
	case !strings.HasPrefix(bot, "xoxb-"):
		return errors.New("the Bot User OAuth Token starts with xoxb- (OAuth & Permissions)")
	case app == "":
		return errors.New("app_token is required: Socket Mode needs an app-level token (xapp-)")
	case !strings.HasPrefix(app, "xapp-"):
		return errors.New("the app-level token starts with xapp- (Basic Information → App-Level Tokens, scope connections:write)")
	}
	return nil
}

func (a *Adapter) Validate(_ context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	if err := checkTokens(cfg); err != nil {
		return channels.State{Kind: channels.StateError, Message: err.Error()}, err
	}
	return channels.State{Kind: channels.StateInfo, Message: "Starting…"}, nil
}

// client is a Web API client on the config's tokens. Calls are bounded by
// their contexts, not a client timeout, so a large upload is not cut short.
func (a *Adapter) client(cfg channels.Config) *slackapi.Client {
	opts := []slackapi.Option{
		slackapi.OptionHTTPClient(&http.Client{}),
		slackapi.OptionAPIURL(a.apiURL),
	}
	if app := strings.TrimSpace(cfg.Get("app_token")); app != "" {
		opts = append(opts, slackapi.OptionAppLevelToken(app))
	}
	return slackapi.New(strings.TrimSpace(cfg.Get("bot_token")), opts...)
}

// session is the live session, or a fresh one from the config: the Web API
// needs no websocket, so a channel can send and read while its worker is down.
func (a *Adapter) session(ctx context.Context, ch *db.Channel, cfg channels.Config) (*conn, error) {
	if c := a.conn(ch.ID); c != nil {
		return c, nil
	}
	if strings.TrimSpace(cfg.Get("bot_token")) == "" {
		return nil, errors.New("slack channel has no bot token")
	}
	return a.login(ctx, a.client(cfg))
}

// login asks Slack who the bot token belongs to.
func (a *Adapter) login(ctx context.Context, api *slackapi.Client) (*conn, error) {
	auth, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, friendly(err)
	}
	return &conn{api: api, self: auth.UserID, botID: auth.BotID, name: auth.User, team: auth.Team}, nil
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	fail := func(err error) error {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	if err := checkTokens(cfg); err != nil {
		return fail(channels.Permanent(err))
	}
	api := a.client(cfg)
	c, err := a.login(ctx, api)
	if err != nil {
		return fail(err)
	}
	c.mark = cfg.Get("working") != "off"
	a.putConn(ch.ID, c)
	defer a.dropConn(ch.ID, c)

	sm := socketmode.New(api)
	run, stop := context.WithCancel(ctx)
	defer stop()
	ended := make(chan error, 1)
	go func() { ended <- sm.RunContext(run) }()

	// Every request is acknowledged as it arrives (Slack retries one not
	// acknowledged within 3 s); reading it, which may ask Slack for names,
	// happens in order on its own goroutine.
	work := make(chan inbound, 256)
	go func() {
		for {
			select {
			case <-run.Done():
				return
			case in := <-work:
				if err := a.onMessage(run, ch, cfg, host, c, in); err != nil && run.Err() == nil {
					log.Printf("slack channel %s: inbound: %v", ch.ID, err)
				}
			}
		}
	}()

	host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connecting to Slack…"})
	var fatal error
	for {
		select {
		case <-ctx.Done():
			stop()
			waitEnded(ended)
			return ctx.Err()
		case err := <-ended:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if fatal == nil {
				fatal = friendly(err)
			}
			return fail(fatal)
		case evt := <-sm.Events:
			if evt.Request != nil && evt.Request.EnvelopeID != "" {
				if err := sm.Ack(*evt.Request); err != nil {
					log.Printf("slack channel %s: ack: %v", ch.ID, err)
				}
			}
			switch evt.Type {
			case socketmode.EventTypeConnected:
				host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: connectedMessage(c)})
			case socketmode.EventTypeConnectionError:
				e, _ := evt.Data.(*slackapi.ConnectionErrorEvent)
				if e == nil {
					continue
				}
				if err := friendly(e.ErrorObj); channels.IsPermanent(err) {
					// Socket Mode retries anything it does not know is fatal,
					// a pasted bot token in the app-token field included.
					fatal = err
					stop()
					continue
				}
				host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: fmt.Sprintf("Cannot reach Slack, trying again in %s: %v", e.Backoff.Round(time.Second), e.ErrorObj)})
			case socketmode.EventTypeInvalidAuth:
				fatal = channels.Permanent(errors.New("Slack refused the app-level token — make a new one under Basic Information → App-Level Tokens (scope connections:write)"))
				stop()
			case socketmode.EventTypeEventsAPI:
				e, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				var raw json.RawMessage
				if evt.Request != nil {
					raw = evt.Request.Payload
				}
				if in, ok := readEvent(e, raw); ok {
					select {
					case work <- in:
					case <-run.Done():
					}
				}
			case socketmode.EventTypeIncomingError, socketmode.EventTypeErrorWriteFailed, socketmode.EventTypeErrorBadMessage:
				log.Printf("slack channel %s: socket: %v", ch.ID, evt.Data)
			}
		}
	}
}

// waitEnded gives the socket a moment to close after a stop.
func waitEnded(ended <-chan error) {
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
	}
}

func connectedMessage(c *conn) string {
	msg := "Connected as @" + c.name
	if c.team != "" {
		msg += " in " + c.team
	}
	return msg
}

// fatalCodes are Slack's answers that trying again will not change.
var fatalCodes = map[string]string{
	"invalid_auth":            "Slack rejected a token — copy the Bot User OAuth Token (xoxb-) and the app-level token (xapp-) again",
	"not_authed":              "Slack got no token — fill in the Bot User OAuth Token and the app-level token",
	"account_inactive":        "the Slack app's token belongs to a deactivated account or workspace",
	"token_revoked":           "the Slack token was revoked — reinstall the app and copy the new tokens",
	"token_expired":           "the Slack token expired — turn off token rotation for this app, or reinstall it and copy the new tokens",
	"not_allowed_token_type":  "Slack refused a token of the wrong kind — the bot token starts with xoxb- and the app-level token with xapp-",
	"app_uninstalled":         "the Slack app was uninstalled from the workspace — reinstall it and copy the new bot token",
	"team_disabled":           "the Slack workspace is disabled",
	"invalid_app_level_token": "Slack refused the app-level token — make a new one with the connections:write scope",
}

// friendly words a Slack refusal in terms of the setup, and marks the ones
// that starting again will not fix.
func friendly(err error) error {
	if err == nil {
		return nil
	}
	code := strings.TrimSpace(err.Error())
	var se slackapi.SlackErrorResponse
	if errors.As(err, &se) {
		code = se.Err
	}
	if msg, ok := fatalCodes[code]; ok {
		return channels.Permanent(errors.New(msg))
	}
	switch code {
	case "missing_scope":
		need := "a scope"
		if se.Needed != "" {
			need = se.Needed
		}
		return fmt.Errorf("the Slack app lacks the %s permission — add it under OAuth & Permissions, reinstall the app, and copy the bot token again", need)
	case "not_in_channel":
		return errors.New("the Slack app is not in that channel — invite it there with /invite and the app's name")
	case "channel_not_found":
		return errors.New("that Slack conversation does not exist or the app cannot see it — pick it again in Set up")
	case "is_archived":
		return errors.New("that Slack channel is archived")
	case "restricted_action", "restricted_action_read_only_channel", "restricted_action_thread_only_channel", "restricted_action_non_threadable_channel":
		return fmt.Errorf("the workspace does not let the app post there (%s)", code)
	case "ekm_access_denied":
		return errors.New("the workspace's key management refused access to that message")
	}
	return fmt.Errorf("slack: %w", err)
}

// --- inbound ---

// inbound is one message event, message or app_mention, as this adapter reads
// it.
type inbound struct {
	user, bot, subtype string
	text               string
	ts, thread         string // thread is the root's ts, "" at the top level
	channel, kind      string // kind is Slack's channel_type: channel, group, im, mpim
	files              []slackapi.File
	voice              map[string]bool // file ids that are voice clips
	mention            bool            // an app_mention event: the app was named
}

// readEvent picks the messages out of an Events API callback. raw is the
// request as Slack sent it, for what the library does not decode.
func readEvent(e slackevents.EventsAPIEvent, raw json.RawMessage) (inbound, bool) {
	if e.Type != slackevents.CallbackEvent {
		return inbound{}, false
	}
	voice := voiceClips(raw)
	switch ev := e.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		in := inbound{
			user: ev.User, bot: ev.BotID, subtype: ev.SubType, text: ev.Text,
			ts: ev.TimeStamp, thread: ev.ThreadTimeStamp, channel: ev.Channel, kind: ev.ChannelType, voice: voice,
		}
		if ev.Message != nil {
			in.files = ev.Message.Files
		}
		return in, true
	case *slackevents.AppMentionEvent:
		return inbound{
			user: ev.User, bot: ev.BotID, text: ev.Text, ts: ev.TimeStamp, thread: ev.ThreadTimeStamp,
			channel: ev.Channel, files: ev.Files, voice: voice, mention: true,
		}, true
	}
	return inbound{}, false
}

// voiceClips reads which attached files are voice clips (subtype slack_audio),
// which the library's File does not carry.
func voiceClips(raw json.RawMessage) map[string]bool {
	if len(raw) == 0 {
		return nil
	}
	var p struct {
		Event struct {
			Files []struct {
				ID      string `json:"id"`
				Subtype string `json:"subtype"`
			} `json:"files"`
		} `json:"event"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, f := range p.Event.Files {
		if f.Subtype == "slack_audio" {
			out[f.ID] = true
		}
	}
	return out
}

// slackbot is Slack's own bot user: reminders and notices, never a person.
const slackbot = "USLACKBOT"

// spoken are the message subtypes a person writes; the rest are joins, edits,
// deletions, topic changes and the like.
var spoken = map[string]bool{"": true, "file_share": true, "thread_broadcast": true, "me_message": true}

// onMessage turns a message into an Inbound for the bound conversation.
func (a *Adapter) onMessage(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, c *conn, m inbound) error {
	if m.user == "" || m.user == c.self || m.user == slackbot || m.bot != "" {
		return nil // never answer an app (this one included, and Slackbot): that is a loop
	}
	if !spoken[m.subtype] || m.ts == "" {
		return nil
	}
	key := ch.ID + "/" + m.channel + "/" + m.ts
	if a.seen.Has(key) {
		return nil // the message and app_mention events of one message, or a retry
	}
	// Re-read the row so a target picked after Start takes effect now.
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	if m.channel == "" || m.channel != targetID(live.ExternalID) {
		return nil
	}
	dm := isDM(m.channel, m.kind)
	addressed := dm || m.mention || mentions(m.text, c.self) ||
		(m.thread != "" && a.joined.Has(threadKey(ch.ID, m.channel, m.thread)))
	if !addressed && cfg.Get("reply_mode") != "always" {
		return nil
	}
	text := strings.TrimSpace(a.readable(ctx, c, ch.ID, m.text))
	files := a.mediaOf(c, m)
	if text == "" && len(files) == 0 {
		return nil
	}
	a.seen.Add(key)
	reply := m.thread
	if reply == "" && !dm && cfg.Get("reply_in") != "channel" {
		reply = m.ts // start a thread under the message
	}
	if reply != "" {
		// A thread the app answers in is one it takes part in: what is said
		// there next is said to it.
		a.joined.Add(threadKey(ch.ID, m.channel, reply))
	}
	a.setLast(ch.ID, m.channel, m.ts)
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: m.channel,
		Title:      a.title(ctx, c, ch.ID, m.channel),
		Author:     a.userName(ctx, c, ch.ID, m.user),
		AuthorID:   m.user,
		Text:       text,
		ReplyTo:    reply,
		Files:      files,
	})
}

func threadKey(channelID, conversation, thread string) string {
	return channelID + "/" + conversation + "/" + thread
}

// isDM reports whether a conversation is a direct message with the app. An
// app_mention carries no channel_type; DM ids start with D.
func isDM(conversation, kind string) bool {
	if kind != "" {
		return kind == "im"
	}
	return strings.HasPrefix(conversation, "D")
}

func mentions(text, self string) bool {
	return self != "" && (strings.Contains(text, "<@"+self+">") || strings.Contains(text, "<@"+self+"|"))
}

// mediaOf is what a message has attached, fetched with the bot token (files:read).
func (a *Adapter) mediaOf(c *conn, m inbound) []channels.Media {
	var out []channels.Media
	for _, f := range m.files {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = strings.TrimSpace(f.Title)
		}
		if name == "" {
			name = "file"
		}
		out = append(out, channels.Media{
			Name: name, Mime: f.Mimetype, Size: int64(f.Size), Voice: m.voice[f.ID],
			Fetch: func(ctx context.Context) ([]byte, error) { return download(ctx, c.api, f) },
		})
	}
	return out
}

// download fetches one file. The library sends the token only to Slack's own
// hosts, so a file linked from elsewhere (an external file) is refused, not
// leaked to.
func download(ctx context.Context, api *slackapi.Client, f slackapi.File) ([]byte, error) {
	switch f.Mode {
	case "hidden_by_limit":
		return nil, errors.New("the workspace's plan hides this file")
	case "tombstone":
		return nil, errors.New("the file was deleted")
	}
	url := f.URLPrivateDownload
	if url == "" {
		url = f.URLPrivate
	}
	if url == "" {
		return nil, errors.New("Slack gave no address for this file")
	}
	var buf capped
	if err := api.GetFileContext(ctx, url, &buf); err != nil {
		return nil, friendly(err)
	}
	return buf.Bytes(), nil
}

// capped is a buffer that refuses to grow past maxDownload.
type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if c.Len()+len(p) > maxDownload {
		return 0, fmt.Errorf("file is over %d MB", maxDownload>>20)
	}
	return c.Buffer.Write(p)
}

// readable is a message's text with Slack's markup read out: names for
// mentions, words for links.
func (a *Adapter) readable(ctx context.Context, c *conn, channelID, text string) string {
	return fromSlack(text, func(id string) string { return a.userName(ctx, c, channelID, id) },
		func(id string) string {
			name, _, _ := strings.Cut(a.title(ctx, c, channelID, id), " · ")
			return strings.TrimPrefix(name, "#")
		})
}

// userName is how a person shows in Slack: their display name, else their
// full name, else their handle.
func (a *Adapter) userName(ctx context.Context, c *conn, channelID, user string) string {
	if user == "" {
		return ""
	}
	key := channelID + "/" + user
	a.mu.Lock()
	cached := a.names[key]
	a.mu.Unlock()
	if cached != "" {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	u, err := c.api.GetUserInfoContext(ctx, user)
	if err != nil || u == nil {
		return user
	}
	name := displayName(u)
	a.mu.Lock()
	if len(a.names) > 10000 {
		a.names = map[string]string{}
	}
	a.names[key] = name
	a.mu.Unlock()
	return name
}

func displayName(u *slackapi.User) string {
	for _, n := range []string{u.Profile.DisplayName, u.RealName, u.Profile.RealName, u.Name} {
		if n = strings.TrimSpace(n); n != "" {
			return n
		}
	}
	return u.ID
}

// title names a conversation: "#general · Acme", "DM · Alice" or
// "Group DM · alice, bob".
func (a *Adapter) title(ctx context.Context, c *conn, channelID, conversation string) string {
	key := channelID + "/" + conversation
	a.mu.Lock()
	cached := a.titles[key]
	a.mu.Unlock()
	if cached != "" {
		return cached
	}
	lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	info, err := c.api.GetConversationInfoContext(lctx, &slackapi.GetConversationInfoInput{ChannelID: conversation})
	cancel()
	if err != nil || info == nil {
		return conversation
	}
	title := conversationTitle(info, c.team, func(id string) string { return a.userName(ctx, c, channelID, id) })
	a.mu.Lock()
	if len(a.titles) > 10000 {
		a.titles = map[string]string{}
	}
	a.titles[key] = title
	a.mu.Unlock()
	return title
}

func conversationTitle(info *slackapi.Channel, team string, user func(string) string) string {
	switch {
	case info.IsIM:
		return "DM · " + user(info.User)
	case info.IsMpIM:
		return "Group DM · " + groupMembers(info.Name)
	}
	title := "#" + info.Name
	if team != "" {
		title += " · " + team
	}
	return title
}

// groupMembers reads the handles out of a group DM's name, mpdm-alice--bob-1.
func groupMembers(name string) string {
	name = strings.TrimPrefix(name, "mpdm-")
	if i := strings.LastIndex(name, "-"); i > 0 && !strings.HasSuffix(name[:i], "-") {
		name = name[:i]
	}
	return strings.Join(strings.Split(name, "--"), ", ")
}

// --- sessions and working marks ---

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

func (a *Adapter) setLast(channelID, conversation, ts string) {
	a.mu.Lock()
	a.last[channelID+"/"+conversation] = ts
	a.mu.Unlock()
}
