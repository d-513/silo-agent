// Package discord is the built-in Discord adapter. It logs in as a bot with
// discordgo (gateway websocket for inbound, REST for outbound and history).
// The bot token stays on the Control Plane.
//
// A channel is bound to exactly one Discord channel: a text channel the bot
// can see, or a DM it has been messaged in. Inbound is live-only, as the
// gateway delivers: nothing sent while the bot was offline is replayed.
package discord

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

// maxMessageRunes keeps clear of Discord's 2000-character message limit.
const maxMessageRunes = 1900

// filesPerMessage is Discord's cap on attachments in one message.
const filesPerMessage = 10

// invitePermissions is what the bot needs in a server: see and write in
// channels and threads, read history, upload files, unfurl links.
const invitePermissions = discordgo.PermissionViewChannel |
	discordgo.PermissionSendMessages |
	discordgo.PermissionSendMessagesInThreads |
	discordgo.PermissionReadMessageHistory |
	discordgo.PermissionAttachFiles |
	discordgo.PermissionEmbedLinks

//go:embed discord.svg
var discordLogo []byte

//go:embed GUIDE.md
var discordGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(discordLogo)
}

// Adapter holds one gateway session per running channel.
type Adapter struct {
	mu     sync.Mutex
	conns  map[string]*conn             // channelID -> live session
	dms    map[string]map[string]string // channelID -> DM channel id -> title
	titles map[string]string            // Discord channel id -> display title
	seen   *channels.Seen
}

// conn is one live bot session.
type conn struct {
	sess *discordgo.Session
	// limited: the Message Content intent is not enabled for this bot, so the
	// gateway only shows messages that DM or @mention it.
	limited bool
}

func init() { channels.Register(New()) }

// New makes an Adapter with no sessions.
func New() *Adapter {
	return &Adapter{
		conns:  map[string]*conn{},
		dms:    map[string]map[string]string{},
		titles: map[string]string{},
		seen:   channels.NewSeen(4096),
	}
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "discord",
		Name:           "Discord",
		Description:    "A Discord bot over the gateway.",
		Logo:           logoURL(),
		Guide:          discordGuide,
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_channels", Label: "Pick a channel", Kind: channels.ActionPick, Description: "Choose the Discord channel (or DM) this channel talks to."},
			{Key: "invite", Label: "Invite link", Kind: channels.ActionRun, Description: "Get the link that adds the bot to a server."},
			{Key: "refresh", Label: "Check connection", Kind: channels.ActionRun, Description: "Check the connection and re-read the bot's details."},
		},
		Fields: []channels.Field{
			{Key: "bot_token", Label: "Bot token", Type: channels.FieldSecret, Required: true, Description: "From the Discord Developer Portal → Bot → Reset Token."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "mentions", Label: "Only when mentioned"},
				{Value: "always", Label: "Every message"},
			}},
		},
	}
}

func (a *Adapter) Validate(_ context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	if strings.TrimSpace(cfg.Get("bot_token")) == "" {
		return channels.State{Kind: channels.StateError, Message: "bot_token is required"}, fmt.Errorf("missing bot token")
	}
	return channels.State{Kind: channels.StateInfo, Message: "Starting…"}, nil
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	token := strings.TrimSpace(cfg.Get("bot_token"))
	a.loadDMs(ch)

	handler := func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if err := a.onMessage(ctx, ch, cfg, host, s, m.Message); err != nil && ctx.Err() == nil {
			log.Printf("discord channel %s: inbound: %v", ch.ID, err)
		}
	}
	c, err := dial(token, true, handler)
	limited := false
	if isDisallowedIntent(err) {
		// Message Content is a privileged intent the owner may not have
		// switched on. Without it the bot still sees DMs and @mentions, which
		// is enough for the default reply mode, so run limited, not dead.
		c, err = dial(token, false, handler)
		limited = true
	}
	if err != nil {
		err = friendlyOpen(err)
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	c.limited = limited
	a.putConn(ch.ID, c)
	defer func() {
		a.dropConn(ch.ID)
		_ = c.sess.Close()
	}()

	host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: connectedMessage(c), Values: a.dmState(ch.ID)})
	<-ctx.Done()
	return ctx.Err()
}

// dial opens a gateway session. wantContent asks for the privileged Message
// Content intent, which is what lets the bot read every message in a channel.
func dial(token string, wantContent bool, handler func(*discordgo.Session, *discordgo.MessageCreate)) (*conn, error) {
	sess, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	sess.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	if wantContent {
		sess.Identify.Intents |= discordgo.IntentMessageContent
	}
	// The bot only needs guilds and channels in state (to label chats and check
	// its own permissions); members, presences and voice are dead weight.
	sess.State.TrackMembers = false
	sess.State.TrackPresences = false
	sess.State.TrackEmojis = false
	sess.State.TrackVoice = false
	sess.AddHandler(handler)
	if err := sess.Open(); err != nil {
		_ = sess.Close()
		return nil, err
	}
	return &conn{sess: sess}, nil
}

func isDisallowedIntent(err error) bool {
	return err != nil && strings.Contains(err.Error(), "4014")
}

// friendlyOpen turns a gateway close code into something the owner can act on.
func friendlyOpen(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "4004"):
		return errors.New("Discord rejected the bot token — copy a fresh one from the Developer Portal (Bot → Reset Token)")
	case strings.Contains(s, "4014"):
		return errors.New("Discord refused a requested intent — enable Message Content Intent under Bot in the Developer Portal")
	}
	return fmt.Errorf("discord login: %w", err)
}

func connectedMessage(c *conn) string {
	name := "the bot"
	if u := c.sess.State.User; u != nil {
		name = "@" + u.Username
	}
	msg := "Connected as " + name
	if c.limited {
		msg += " — Message Content Intent is off, so the bot only sees DMs and messages that @mention it. Turn it on in the Developer Portal (Bot) to read every message."
	}
	return msg
}

// --- inbound ---

// onMessage turns a gateway message into an Inbound for the bound channel.
func (a *Adapter) onMessage(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host, s *discordgo.Session, m *discordgo.Message) error {
	self := s.State.User
	if self == nil || m == nil || m.Author == nil {
		return nil
	}
	if m.Author.ID == self.ID || m.Author.Bot || m.WebhookID != "" {
		return nil // never answer a bot (itself included): that is a loop
	}
	if m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply {
		return nil // joins, pins, boosts …
	}
	if a.seen.Add(ch.ID + "/" + m.ID) {
		return nil
	}
	// Re-read the row so a target picked after Start takes effect now.
	live := ch
	if cur, ok := host.CurrentChannel(ch.ID); ok {
		live = cur
	}
	dm := m.GuildID == ""
	if dm && a.putDM(live.ID, m.ChannelID, "DM · "+authorName(m)) {
		// A DM becomes pickable the moment it exists, and must survive restarts.
		host.PublishState(live.ID, channels.State{Values: a.dmState(live.ID)})
	}
	// One channel is one Discord channel; ignore everything else.
	if m.ChannelID != targetID(live.ExternalID) {
		return nil
	}
	// DMs always get a reply; reply_mode only gates servers, where being
	// mentioned is the safe default.
	if !dm && cfg.Get("reply_mode") != "always" && !mentionsBot(m, self) {
		return nil
	}
	text := messageText(m)
	if text == "" {
		return nil
	}
	return host.DeliverInbound(ctx, live, channels.Inbound{
		ExternalID: m.ChannelID,
		Title:      a.title(s, m),
		Author:     authorName(m),
		Text:       text,
	})
}

func mentionsBot(m *discordgo.Message, self *discordgo.User) bool {
	if slices.ContainsFunc(m.Mentions, func(u *discordgo.User) bool { return u != nil && u.ID == self.ID }) {
		return true
	}
	// A reply to the bot counts even when the sender turned the ping off.
	ref := m.ReferencedMessage
	return ref != nil && ref.Author != nil && ref.Author.ID == self.ID
}

// messageText is what the Bot reads: the text with mentions turned into names,
// then one line per attachment so it can fetch the file.
func messageText(m *discordgo.Message) string {
	lines := []string{}
	if t := strings.TrimSpace(m.ContentWithMentionsReplaced()); t != "" {
		lines = append(lines, t)
	}
	for _, at := range m.Attachments {
		kind := ""
		if at.ContentType != "" {
			kind = " (" + at.ContentType + ")"
		}
		lines = append(lines, fmt.Sprintf("[attachment: %s%s %s]", at.Filename, kind, at.URL))
	}
	return strings.Join(lines, "\n")
}

func authorName(m *discordgo.Message) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author == nil {
		return ""
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}

// title names the conversation: "#general · My Server" or "DM · alice".
func (a *Adapter) title(s *discordgo.Session, m *discordgo.Message) string {
	if m.GuildID == "" {
		return "DM · " + authorName(m)
	}
	a.mu.Lock()
	cached := a.titles[m.ChannelID]
	a.mu.Unlock()
	if cached != "" {
		return cached
	}
	name := ""
	if c, err := s.State.Channel(m.ChannelID); err == nil {
		name = c.Name
	} else if c, err := s.Channel(m.ChannelID); err == nil {
		name = c.Name
	}
	if name == "" {
		return m.ChannelID
	}
	title := "#" + name
	if g, err := s.State.Guild(m.GuildID); err == nil && g.Name != "" {
		title += " · " + g.Name
	}
	a.mu.Lock()
	a.titles[m.ChannelID] = title
	a.mu.Unlock()
	return title
}

// --- outbound ---

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, _ channels.Config, msg channels.Outbound) error {
	c := a.conn(ch.ID)
	if c == nil {
		return fmt.Errorf("discord channel is not connected")
	}
	external := strings.TrimSpace(msg.ExternalID)
	if external == "" {
		external = ch.ExternalID
	}
	target := targetID(external)
	if target == "" {
		return fmt.Errorf("discord channel %q is not a channel id — pick it again in Set up", strings.TrimSpace(external))
	}
	return send(ctx, c.sess, target, msg)
}

// noPings lets the bot @mention people but never @everyone, @here or a role: the
// model writes what it likes, and a stray "@everyone" must not page a server.
var noPings = &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{discordgo.AllowedMentionTypeUsers}}

func send(ctx context.Context, sess *discordgo.Session, target string, msg channels.Outbound) error {
	parts := channels.Chunk(msg.Text, maxMessageRunes)
	var files []*discordgo.File
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = "file"
		}
		files = append(files, &discordgo.File{Name: name, ContentType: f.Mime, Reader: bytes.NewReader(f.Data)})
	}
	if len(parts) == 0 && len(files) == 0 {
		return nil
	}
	if len(parts) == 0 {
		parts = []string{""}
	}
	post := func(content string, fs []*discordgo.File, first bool) error {
		out := &discordgo.MessageSend{Content: content, Files: fs, AllowedMentions: noPings}
		if first && msg.ReplyTo != "" {
			out.Reference = &discordgo.MessageReference{MessageID: msg.ReplyTo, ChannelID: target}
		}
		_, err := sess.ChannelMessageSendComplex(target, out, discordgo.WithContext(ctx))
		return friendlySend(err)
	}
	// Text first, the first batch of files riding on the last piece so they
	// land together; any files past Discord's per-message cap follow alone.
	for i, p := range parts {
		var fs []*discordgo.File
		if i == len(parts)-1 {
			fs = files[:min(len(files), filesPerMessage)]
		}
		if err := post(p, fs, i == 0); err != nil {
			return err
		}
	}
	for rest := files; len(rest) > filesPerMessage; {
		rest = rest[filesPerMessage:]
		if err := post("", rest[:min(len(rest), filesPerMessage)], false); err != nil {
			return err
		}
	}
	return nil
}

// friendlySend names the usual Discord REST refusals in terms of the setup.
func friendlySend(err error) error {
	var rest *discordgo.RESTError
	if !errors.As(err, &rest) || rest.Message == nil {
		return err
	}
	switch rest.Message.Code {
	case discordgo.ErrCodeMissingPermissions, discordgo.ErrCodeMissingAccess:
		return fmt.Errorf("the bot cannot write in that Discord channel — give it View Channel, Send Messages and Attach Files there (%s)", rest.Message.Message)
	case discordgo.ErrCodeUnknownChannel:
		return fmt.Errorf("that Discord channel no longer exists or the bot was removed from it — pick it again in Set up")
	}
	return err
}

// History reads the bound channel's recent messages, oldest first. Unlike a
// Telegram bot, a Discord bot may read history (it needs Read Message History).
func (a *Adapter) History(ctx context.Context, ch *db.Channel, _ channels.Config, externalID string, limit int) ([]channels.Message, error) {
	c := a.conn(ch.ID)
	if c == nil {
		return nil, fmt.Errorf("discord channel is not connected")
	}
	external := strings.TrimSpace(externalID)
	if external == "" {
		external = ch.ExternalID
	}
	target := targetID(external)
	if target == "" {
		return nil, fmt.Errorf("chat %q is not a Discord channel id", external)
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 100)
	msgs, err := c.sess.ChannelMessages(target, limit, "", "", "", discordgo.WithContext(ctx))
	if err != nil {
		return nil, friendlySend(err)
	}
	self := c.sess.State.User
	out := make([]channels.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Author == nil {
			continue
		}
		isOut := self != nil && m.Author.ID == self.ID
		author := authorName(m)
		if isOut {
			author = "Bot"
		}
		out = append(out, channels.Message{Author: author, Text: messageText(m), At: m.Timestamp, Out: isOut})
	}
	slices.Reverse(out) // the API returns newest first
	return out, nil
}

// --- setup actions ---

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, _ channels.Config, action string, _ map[string]string) (channels.State, error) {
	c := a.conn(ch.ID)
	if c == nil {
		return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
	}
	switch action {
	case "list_channels":
		return a.listChannels(ctx, ch, c)
	case "invite":
		self := c.sess.State.User
		if self == nil {
			return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
		}
		return channels.State{Kind: channels.StateInfo, Message: "Open this link to add the bot to a server: " + inviteURL(self.ID), Values: a.dmState(ch.ID)}, nil
	case "refresh":
		return channels.State{Kind: channels.StateInfo, Message: connectedMessage(c), Values: a.dmState(ch.ID)}, nil
	}
	return channels.State{}, fmt.Errorf("unknown action %q", action)
}

// inviteURL is the OAuth link that adds the bot to a server. A bot's user id is
// its application id.
func inviteURL(botID string) string {
	q := url.Values{}
	q.Set("client_id", botID)
	q.Set("scope", "bot")
	q.Set("permissions", fmt.Sprint(int64(invitePermissions)))
	return "https://discord.com/oauth2/authorize?" + q.Encode()
}

// listChannels offers every text channel the bot can write in, across its
// servers, plus the DMs it has seen. A bot can enumerate servers and channels
// (unlike a Telegram bot), so the picker is complete, not just "seen so far".
func (a *Adapter) listChannels(ctx context.Context, ch *db.Channel, c *conn) (channels.State, error) {
	sess := c.sess
	guilds, err := sess.UserGuilds(200, "", "", false, discordgo.WithContext(ctx))
	if err != nil {
		return channels.State{}, err
	}
	var self string
	if u := sess.State.User; u != nil {
		self = u.ID
	}
	const need = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages
	var options []channels.Option
	for _, g := range guilds {
		chans, err := sess.GuildChannels(g.ID, discordgo.WithContext(ctx))
		if err != nil {
			return channels.State{}, err
		}
		for _, gc := range chans {
			if gc.Type != discordgo.ChannelTypeGuildText && gc.Type != discordgo.ChannelTypeGuildNews {
				continue
			}
			// State knows the bot's roles; when it cannot say, offer the channel.
			if perms, err := sess.State.UserChannelPermissions(self, gc.ID); err == nil && perms&need != need {
				continue
			}
			options = append(options, channels.Option{Value: gc.ID, Label: g.Name + " › #" + gc.Name})
		}
	}
	a.mu.Lock()
	for id, title := range a.dms[ch.ID] {
		options = append(options, channels.Option{Value: id, Label: title})
	}
	a.mu.Unlock()
	if len(options) == 0 {
		return channels.State{
			Kind:    channels.StateSelect,
			Message: "The bot is not in any server yet. Use Invite link to add it, or send the bot a DM — then try again. You can also enter a channel id below.",
		}, nil
	}
	slices.SortFunc(options, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return channels.State{
		Kind:    channels.StateSelect,
		Message: fmt.Sprintf("%d channels", len(options)),
		Options: options,
		Values:  a.dmState(ch.ID),
	}, nil
}

// --- target ---

var (
	snowflakeRe  = regexp.MustCompile(`^\d{5,25}$`)
	channelMenRe = regexp.MustCompile(`^<#(\d+)>$`)
)

// targetID reads a channel id from what the owner entered or picked: a bare id,
// a <#id> mention, or a discord.com/channels/<server>/<channel> link.
func targetID(s string) string {
	s = strings.TrimSpace(s)
	if m := channelMenRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if snowflakeRe.MatchString(s) {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) >= 3 && segs[0] == "channels" && snowflakeRe.MatchString(segs[2]) {
		return segs[2] // …/channels/<server or @me>/<channel>[/<message>]
	}
	return ""
}

// --- sessions and DMs ---

func (a *Adapter) putConn(id string, c *conn) {
	a.mu.Lock()
	a.conns[id] = c
	a.mu.Unlock()
}

func (a *Adapter) dropConn(id string) {
	a.mu.Lock()
	delete(a.conns, id)
	a.mu.Unlock()
}

func (a *Adapter) conn(id string) *conn {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conns[id]
}

// putDM records a DM channel and reports whether it is new.
func (a *Adapter) putDM(channelID, dmID, title string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dms[channelID] == nil {
		a.dms[channelID] = map[string]string{}
	}
	_, existed := a.dms[channelID][dmID]
	a.dms[channelID][dmID] = title
	return !existed
}

// dmState serializes known DMs into state values for persistence.
func (a *Adapter) dmState(channelID string) map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]string{}
	for id, title := range a.dms[channelID] {
		out["dm:"+id] = title
	}
	return out
}

// loadDMs restores DMs learned in earlier runs from the channel's saved state.
func (a *Adapter) loadDMs(ch *db.Channel) {
	var st channels.State
	if strings.TrimSpace(ch.StateJSON) == "" || json.Unmarshal([]byte(ch.StateJSON), &st) != nil {
		return
	}
	for k, v := range st.Values {
		if id, ok := strings.CutPrefix(k, "dm:"); ok {
			a.putDM(ch.ID, id, v)
		}
	}
}
