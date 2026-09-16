// Package telegram is the built-in Telegram adapter. It talks MTProto via
// gotd/td, logging in as the Bot token. Per-channel api_id/api_hash, bot_token,
// and a session file on the Control Plane keep credentials off the Bot.
//
// Inbound is live-only: messages older than the moment Start connected are
// ignored, so a Bot that was offline does not replay a backlog of runs.
package telegram

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
)

const maxMessageLen = 4000

//go:embed telegram.svg
var telegramLogo []byte

//go:embed GUIDE.md
var telegramGuide string

func logoURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(telegramLogo)
}

type peerRef struct {
	Kind       string `json:"kind"` // user | chat | channel
	ID         int64  `json:"id"`
	AccessHash int64  `json:"access_hash,omitempty"`
	Username   string `json:"username,omitempty"`
	Title      string `json:"title,omitempty"`
}

func (p peerRef) externalID() string { return p.Kind + ":" + strconv.FormatInt(p.ID, 10) }

func (p peerRef) inputPeer() tg.InputPeerClass {
	switch p.Kind {
	case "user":
		return &tg.InputPeerUser{UserID: p.ID, AccessHash: p.AccessHash}
	case "chat":
		return &tg.InputPeerChat{ChatID: p.ID}
	case "channel":
		return &tg.InputPeerChannel{ChannelID: p.ID, AccessHash: p.AccessHash}
	}
	return nil
}

type Adapter struct {
	mu        sync.Mutex
	clients   map[string]*telegram.Client
	selfs     map[string]*tg.User
	peers     map[string]map[string]peerRef // channelID -> externalID -> ref
	seen      map[string]map[int]struct{}   // channelID -> message IDs
	noHistory map[string]bool               // channelID -> server rejected getHistory
}

func init() {
	channels.Register(&Adapter{
		clients:   map[string]*telegram.Client{},
		selfs:     map[string]*tg.User{},
		peers:     map[string]map[string]peerRef{},
		seen:      map[string]map[int]struct{}{},
		noHistory: map[string]bool{},
	})
}

func (a *Adapter) Descriptor() channels.Descriptor {
	return channels.Descriptor{
		Slug:           "telegram",
		Name:           "Telegram",
		Description:    "A Telegram bot over MTProto.",
		Logo:           logoURL(),
		RequiresTarget: true,
		Actions: []channels.Action{
			{Key: "list_dialogs", Label: "Pick a chat", Kind: channels.ActionPick, Description: "Choose the Telegram chat this channel talks to."},
			{Key: "refresh", Label: "Reconnect", Kind: channels.ActionRun, Description: "Check the connection and re-read the bot's details."},
		},
		Guide: telegramGuide,
		Fields: []channels.Field{
			{Key: "api_id", Label: "API ID", Type: channels.FieldNumber, Required: true, Description: "App api_id from my.telegram.org (not a login)."},
			{Key: "api_hash", Label: "API Hash", Type: channels.FieldSecret, Required: true, Description: "App api_hash from my.telegram.org."},
			{Key: "bot_token", Label: "Bot token", Type: channels.FieldSecret, Required: true, Description: "From @BotFather. This is the bot account."},
			{Key: "reply_mode", Label: "Reply to", Type: channels.FieldSelect, Options: []channels.Option{
				{Value: "always", Label: "Every message"},
				{Value: "mentions", Label: "Only when mentioned"},
			}},
		},
	}
}

func (a *Adapter) Validate(_ context.Context, _ *db.Channel, cfg channels.Config) (channels.State, error) {
	if strings.TrimSpace(cfg.Get("api_id")) == "" || strings.TrimSpace(cfg.Get("api_hash")) == "" || strings.TrimSpace(cfg.Get("bot_token")) == "" {
		return channels.State{Kind: channels.StateError, Message: "api_id, api_hash and bot_token are required"}, fmt.Errorf("missing credentials")
	}
	return channels.State{Kind: channels.StateInfo, Message: "Starting…"}, nil
}

func (a *Adapter) Start(ctx context.Context, ch *db.Channel, cfg channels.Config, host channels.Host) error {
	appID, err := strconv.Atoi(strings.TrimSpace(cfg.Get("api_id")))
	if err != nil || appID == 0 {
		return fmt.Errorf("api_id must be a number")
	}
	dir := filepath.Join(host.DataDir(), "channels", ch.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	liveSince := time.Now().Unix()
	a.loadPeers(ch)

	dispatcher := tg.NewUpdateDispatcher()
	client := telegram.NewClient(appID, strings.TrimSpace(cfg.Get("api_hash")), telegram.Options{
		SessionStorage: &session.FileStorage{Path: filepath.Join(dir, "session.json")},
		UpdateHandler:  dispatcher,
	})
	a.putClient(ch.ID, client)
	defer a.dropClient(ch.ID)

	dispatcher.OnNewMessage(a.onMessage(ch, cfg, liveSince, host))

	err = client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !status.Authorized {
			if _, err := client.Auth().Bot(ctx, strings.TrimSpace(cfg.Get("bot_token"))); err != nil {
				return fmt.Errorf("telegram login: %w", err)
			}
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.selfs[ch.ID] = self
		a.mu.Unlock()
		host.PublishState(ch.ID, channels.State{Kind: channels.StateInfo, Message: "Connected as @" + self.Username, Values: a.peersState(ch.ID)})
		<-ctx.Done()
		return ctx.Err()
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && err != context.Canceled {
		host.PublishState(ch.ID, channels.State{Kind: channels.StateError, Message: err.Error()})
		return err
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, ch *db.Channel, cfg channels.Config, msg channels.Outbound) error {
	client := a.client(ch.ID)
	if client == nil {
		return fmt.Errorf("telegram channel is not connected")
	}
	sender := message.NewSender(client.API())
	req := a.target(sender, ch, msg.ExternalID)
	if req == nil {
		return fmt.Errorf("telegram chat %q is not known yet — pick it again in Set up, or have someone message the bot once", strings.TrimSpace(ch.ExternalID))
	}
	text := strings.TrimSpace(msg.Text)
	if text != "" {
		for _, part := range splitMessage(text, maxMessageLen) {
			if _, err := req.Text(ctx, part); err != nil {
				return err
			}
		}
	}
	for _, f := range msg.Files {
		if len(f.Data) == 0 {
			continue
		}
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = "file"
		}
		file, err := uploader.NewUploader(client.API()).FromBytes(ctx, name, f.Data)
		if err != nil {
			return err
		}
		// ForceFile + an explicit filename, or Telegram shows a generated
		// "file<random>.undefined" name.
		doc := message.UploadedDocument(file).Filename(name).ForceFile(true)
		if f.Mime != "" {
			doc = doc.MIME(f.Mime)
		}
		if _, err := req.Media(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}

// History reads the platform's recent messages for the bound chat, so the Bot
// can see what happened before this process started.
func (a *Adapter) History(ctx context.Context, ch *db.Channel, _ channels.Config, externalID string, limit int) ([]channels.Message, error) {
	client := a.client(ch.ID)
	if client == nil {
		return nil, fmt.Errorf("telegram channel is not connected")
	}
	a.mu.Lock()
	denied := a.noHistory[ch.ID]
	a.mu.Unlock()
	if denied {
		return nil, fmt.Errorf("history is not available for bot accounts")
	}
	external := strings.TrimSpace(externalID)
	if external == "" {
		external = strings.TrimSpace(ch.ExternalID)
	}
	peer := a.inputPeerFor(ch.ID, external)
	if peer == nil {
		peer = a.resolveInputPeer(ctx, ch.ID, client, external)
	}
	if peer == nil {
		return nil, fmt.Errorf("chat %q is not known yet", external)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	res, err := client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: limit})
	if err != nil {
		if strings.Contains(err.Error(), "BOT_METHOD_INVALID") {
			a.mu.Lock()
			a.noHistory[ch.ID] = true
			a.mu.Unlock()
			return nil, fmt.Errorf("history is not available for bot accounts")
		}
		return nil, err
	}
	users := historyUsers(res)
	var out []channels.Message
	for _, m := range historyMessages(res) {
		msg, ok := m.(*tg.Message)
		if !ok {
			continue
		}
		out = append(out, channels.Message{
			Author: historyAuthor(users, msg),
			Text:   msg.Message,
			At:     time.Unix(int64(msg.Date), 0),
			Out:    msg.Out,
		})
	}
	slices.Reverse(out) // API returns newest first
	return out, nil
}

func historyMessages(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch v := res.(type) {
	case *tg.MessagesMessages:
		return v.Messages
	case *tg.MessagesMessagesSlice:
		return v.Messages
	case *tg.MessagesChannelMessages:
		return v.Messages
	}
	return nil
}

func historyUsers(res tg.MessagesMessagesClass) map[int64]*tg.User {
	var users tg.UserClassArray
	switch v := res.(type) {
	case *tg.MessagesMessages:
		users = v.Users
	case *tg.MessagesMessagesSlice:
		users = v.Users
	case *tg.MessagesChannelMessages:
		users = v.Users
	}
	return users.UserToMap()
}

func historyAuthor(users map[int64]*tg.User, m *tg.Message) string {
	if m.Out {
		return "Bot"
	}
	if pu, ok := m.FromID.(*tg.PeerUser); ok {
		if u := users[pu.UserID]; u != nil {
			return displayName(u)
		}
	}
	if pf, ok := m.FromID.(*tg.PeerChannel); ok {
		return fmt.Sprintf("channel %d", pf.ChannelID)
	}
	return ""
}

// inputPeerFor rebuilds an InputPeer from peers learned via updates or the
// dialog list.
func (a *Adapter) inputPeerFor(channelID, external string) tg.InputPeerClass {
	external = strings.TrimSpace(external)
	if external == "" {
		return nil
	}
	if ref, ok := a.peer(channelID, external); ok {
		return ref.inputPeer()
	}
	if ref, ok := a.peerByID(channelID, external); ok {
		return ref.inputPeer()
	}
	return nil
}

// resolveInputPeer turns a manually entered @username, domain, or t.me link
// into an InputPeer, so a chat can be bound before any update arrives.
func (a *Adapter) resolveInputPeer(ctx context.Context, channelID string, client *telegram.Client, external string) tg.InputPeerClass {
	domain := normalizeDomain(external)
	if domain == "" {
		return nil
	}
	ip, err := peer.DefaultResolver(client.API()).ResolveDomain(ctx, domain)
	if err != nil || ip == nil {
		return nil
	}
	if ref, ok := refFromInput(ip); ok {
		a.putPeer(channelID, ref)
	}
	return ip
}

// matchesTarget accepts either the stored peer id or a manually entered
// @username for the bound chat.
func matchesTarget(target string, ref peerRef) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if target == ref.externalID() {
		return true
	}
	want := strings.ToLower(normalizeDomain(target))
	if want == "" {
		return false
	}
	if ref.Username != "" && strings.ToLower(ref.Username) == want {
		return true
	}
	return false
}

// normalizeDomain accepts @name, name, t.me/name, https://t.me/name.
func normalizeDomain(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, p)
	}
	for _, p := range []string{"t.me/", "telegram.me/", "tg://resolve?domain="} {
		if i := strings.Index(s, p); i >= 0 {
			s = s[i+len(p):]
		}
	}
	s = strings.TrimPrefix(s, "@")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// target resolves where an outbound message goes: a known peer, a username or
// link, or the channel's configured target.
func (a *Adapter) target(sender *message.Sender, ch *db.Channel, external string) *message.RequestBuilder {
	external = strings.TrimSpace(external)
	if external == "" {
		external = strings.TrimSpace(ch.ExternalID)
	}
	if external == "" {
		return nil
	}
	if ref, ok := a.peer(ch.ID, external); ok {
		if ip := ref.inputPeer(); ip != nil {
			return sender.To(ip)
		}
	}
	if looksLikePeerID(external) {
		if ref, ok := a.peerByID(ch.ID, external); ok {
			if ip := ref.inputPeer(); ip != nil {
				return sender.To(ip)
			}
		}
	}
	// A synthetic id (user:123) cannot be resolved without the access hash we
	// learn from an update or the picker; a username/link/deeplink can.
	if isSyntheticPeer(external) {
		return nil
	}
	return sender.Resolve(external)
}

// isSyntheticPeer reports whether an external id is one of our derived peer
// keys rather than something the server can resolve.
func isSyntheticPeer(s string) bool {
	for _, p := range []string{"user:", "chat:", "channel:"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (a *Adapter) Action(ctx context.Context, ch *db.Channel, cfg channels.Config, action string, _ map[string]string) (channels.State, error) {
	switch action {
	case "list_dialogs":
		return a.listDialogs(ctx, ch)
	case "refresh":
		client := a.client(ch.ID)
		if client == nil {
			return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
		}
		a.mu.Lock()
		self := a.selfs[ch.ID]
		a.mu.Unlock()
		msg := "Connected"
		if self != nil {
			msg = "Connected as @" + self.Username
		}
		return channels.State{Kind: channels.StateInfo, Message: msg, Values: a.peersState(ch.ID)}, nil
	default:
		return channels.State{}, fmt.Errorf("unknown action %q", action)
	}
}

func (a *Adapter) listDialogs(ctx context.Context, ch *db.Channel) (channels.State, error) {
	client := a.client(ch.ID)
	if client == nil {
		return channels.State{Kind: channels.StateError, Message: "not connected"}, nil
	}
	a.loadPeers(ch)
	res, err := client.API().MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err == nil {
		type dialogSet struct {
			ent peer.Entities
			ds  []tg.DialogClass
		}
		var sets []dialogSet
		switch v := res.(type) {
		case *tg.MessagesDialogs:
			sets = append(sets, dialogSet{peer.EntitiesFromResult(v), v.Dialogs})
		case *tg.MessagesDialogsSlice:
			sets = append(sets, dialogSet{peer.EntitiesFromResult(v), v.Dialogs})
		}
		for _, set := range sets {
			for _, d := range set.ds {
				dd, ok := d.(*tg.Dialog)
				if !ok || dd.Peer == nil {
					continue
				}
				ip, ierr := set.ent.ExtractPeer(dd.Peer)
				if ierr != nil {
					continue
				}
				if ref, ok := refFromInput(ip); ok {
					if c := chatOf(res, dd.Peer); c != "" {
						ref.Title = c
					}
					a.putPeer(ch.ID, ref)
				}
			}
		}
	}
	// Merge peers learned from updates too; a bot's dialog list can be
	// partial, but anything it has seen should still be pickable.
	a.mu.Lock()
	refs := make([]peerRef, 0, len(a.peers[ch.ID]))
	for _, r := range a.peers[ch.ID] {
		refs = append(refs, r)
	}
	a.mu.Unlock()
	seen := map[string]bool{}
	var options []channels.Option
	for _, r := range refs {
		if seen[r.externalID()] {
			continue
		}
		seen[r.externalID()] = true
		label := r.Title
		if label == "" && r.Username != "" {
			label = "@" + r.Username
		}
		if label == "" {
			label = r.externalID()
		}
		options = append(options, channels.Option{Value: r.externalID(), Label: label})
	}
	if len(options) == 0 {
		// Bot accounts cannot enumerate dialogs (Telegram answers
		// BOT_METHOD_INVALID), so there may simply be nothing discovered yet.
		return channels.State{
			Kind:    channels.StateSelect,
			Message: "No chats discovered yet. Send the bot a message or add it to a group, then try again — or enter a chat below.",
		}, nil
	}
	slices.SortFunc(options, func(x, y channels.Option) int {
		return strings.Compare(strings.ToLower(x.Label), strings.ToLower(y.Label))
	})
	return channels.State{
		Kind:    channels.StateSelect,
		Message: fmt.Sprintf("%d chats", len(options)),
		Options: options,
		Values:  a.peersState(ch.ID),
	}, nil
}

// loadPeers restores chats learned in earlier runs from the channel state, so a
// reused bot keeps its pickable chat list even before updates arrive.
func (a *Adapter) loadPeers(ch *db.Channel) {
	a.mu.Lock()
	if a.peers[ch.ID] == nil {
		a.peers[ch.ID] = map[string]peerRef{}
	}
	a.mu.Unlock()
	if strings.TrimSpace(ch.StateJSON) == "" {
		return
	}
	var st channels.State
	if json.Unmarshal([]byte(ch.StateJSON), &st) != nil {
		return
	}
	for k, v := range st.Values {
		if !strings.HasPrefix(k, "peer:") {
			continue
		}
		var ref peerRef
		if json.Unmarshal([]byte(v), &ref) == nil && ref.Kind != "" {
			a.putPeer(ch.ID, ref)
		}
	}
}

// peersState serializes known chats into state values for persistence.
func (a *Adapter) peersState(channelID string) map[string]string {
	out := map[string]string{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ref := range a.peers[channelID] {
		if b, err := json.Marshal(ref); err == nil {
			out["peer:"+ref.externalID()] = string(b)
		}
	}
	return out
}

func (a *Adapter) onMessage(ch *db.Channel, cfg channels.Config, liveSince int64, host channels.Host) func(context.Context, tg.Entities, *tg.UpdateNewMessage) error {
	return func(ctx context.Context, uctx tg.Entities, u *tg.UpdateNewMessage) error {
		m, ok := u.Message.(*tg.Message)
		if !ok || m.Out {
			return nil
		}
		if int64(m.Date) < liveSince {
			return nil // live-only: never replay what was missed while offline
		}
		if a.markSeen(ch.ID, m.ID) {
			return nil
		}
		ref, ok := refFromUpdate(uctx, m.PeerID)
		if !ok {
			return nil
		}
		// Re-read the row so a target picked after Start takes effect now.
		live := ch
		if cur, ok := host.CurrentChannel(ch.ID); ok {
			live = cur
		}
		if a.putPeer(live.ID, ref) {
			// A newly seen chat becomes pickable and its access hash must
			// survive restarts, so persist it now.
			host.PublishState(live.ID, channels.State{Values: a.peersState(live.ID)})
		}
		// One channel is one chat; ignore everything else.
		if !matchesTarget(live.ExternalID, ref) {
			return nil
		}
		// Private chats always get a reply; reply_mode only gates groups and
		// channels, where being mentioned is the safe default.
		if ref.Kind != "user" && cfg.Get("reply_mode") != "always" && !a.mentionsBot(live.ID, m) {
			return nil
		}
		text := strings.TrimSpace(m.Message)
		if text == "" {
			text = "[media]"
		}
		title := ref.Title
		if title == "" {
			title = ref.externalID()
		}
		return host.DeliverInbound(ctx, live, channels.Inbound{
			ExternalID: ref.externalID(),
			Title:      title,
			Author:     authorName(uctx, m),
			Text:       text,
		})
	}
}

func (a *Adapter) mentionsBot(channelID string, m *tg.Message) bool {
	a.mu.Lock()
	self := a.selfs[channelID]
	a.mu.Unlock()
	text := strings.ToLower(m.Message)
	if strings.HasPrefix(strings.TrimSpace(m.Message), "/") {
		return true
	}
	for _, e := range m.Entities {
		switch v := e.(type) {
		case *tg.MessageEntityBotCommand:
			return true
		case *tg.MessageEntityMentionName:
			if self != nil && v.UserID == self.ID {
				return true
			}
		}
	}
	if self != nil && self.Username != "" && strings.Contains(text, "@"+strings.ToLower(self.Username)) {
		return true
	}
	return false
}

// --- helpers ---

func (a *Adapter) putClient(id string, c *telegram.Client) {
	a.mu.Lock()
	a.clients[id] = c
	if a.peers[id] == nil {
		a.peers[id] = map[string]peerRef{}
	}
	a.mu.Unlock()
}

func (a *Adapter) dropClient(id string) {
	a.mu.Lock()
	delete(a.clients, id)
	delete(a.selfs, id)
	delete(a.seen, id)
	delete(a.noHistory, id)
	a.mu.Unlock()
}

func (a *Adapter) client(id string) *telegram.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients[id]
}

// putPeer records a peer and reports whether it is newly learned, so callers
// can persist the pickable list at the moment it appears.
func (a *Adapter) putPeer(channelID string, ref peerRef) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.peers[channelID] == nil {
		a.peers[channelID] = map[string]peerRef{}
	}
	key := ref.externalID()
	_, existed := a.peers[channelID][key]
	a.peers[channelID][key] = ref
	return !existed
}

func (a *Adapter) peer(channelID, external string) (peerRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ref, ok := a.peers[channelID][external]
	return ref, ok
}

func (a *Adapter) peerByID(channelID, external string) (peerRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ref := range a.peers[channelID] {
		if ref.externalID() == external || strconv.FormatInt(ref.ID, 10) == external {
			return ref, true
		}
	}
	return peerRef{}, false
}

// markSeen records a message id and reports whether it was already handled.
func (a *Adapter) markSeen(channelID string, id int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.seen[channelID] == nil {
		a.seen[channelID] = map[int]struct{}{}
	}
	if _, ok := a.seen[channelID][id]; ok {
		return true
	}
	if len(a.seen[channelID]) > 4096 {
		a.seen[channelID] = map[int]struct{}{}
	}
	a.seen[channelID][id] = struct{}{}
	return false
}

func refFromUpdate(uctx tg.Entities, pc tg.PeerClass) (peerRef, bool) {
	switch p := pc.(type) {
	case *tg.PeerUser:
		u, ok := uctx.Users[p.UserID]
		if !ok {
			return peerRef{}, false
		}
		return peerRef{Kind: "user", ID: u.ID, AccessHash: u.AccessHash, Username: u.Username, Title: displayName(u)}, true
	case *tg.PeerChat:
		c, ok := uctx.Chats[p.ChatID]
		if !ok {
			return peerRef{}, false
		}
		return peerRef{Kind: "chat", ID: c.ID, Title: c.Title}, true
	case *tg.PeerChannel:
		c, ok := uctx.Channels[p.ChannelID]
		if !ok {
			return peerRef{}, false
		}
		return peerRef{Kind: "channel", ID: c.ID, AccessHash: c.AccessHash, Username: c.Username, Title: c.Title}, true
	}
	return peerRef{}, false
}

func refFromInput(ip tg.InputPeerClass) (peerRef, bool) {
	switch p := ip.(type) {
	case *tg.InputPeerUser:
		return peerRef{Kind: "user", ID: p.UserID, AccessHash: p.AccessHash}, true
	case *tg.InputPeerChat:
		return peerRef{Kind: "chat", ID: p.ChatID}, true
	case *tg.InputPeerChannel:
		return peerRef{Kind: "channel", ID: p.ChannelID, AccessHash: p.AccessHash}, true
	}
	return peerRef{}, false
}

func chatOf(res tg.MessagesDialogsClass, pc tg.PeerClass) string {
	var users tg.UserClassArray
	var chats tg.ChatClassArray
	switch v := res.(type) {
	case *tg.MessagesDialogs:
		users, chats = v.Users, v.Chats
	case *tg.MessagesDialogsSlice:
		users, chats = v.Users, v.Chats
	default:
		return ""
	}
	switch p := pc.(type) {
	case *tg.PeerUser:
		if u := users.UserToMap()[p.UserID]; u != nil {
			return displayName(u)
		}
	case *tg.PeerChat:
		if c := chats.ChatToMap()[p.ChatID]; c != nil {
			return c.Title
		}
	case *tg.PeerChannel:
		if c := chats.ChannelToMap()[p.ChannelID]; c != nil {
			return c.Title
		}
	}
	return ""
}

func authorName(uctx tg.Entities, m *tg.Message) string {
	from := m.FromID
	if from == nil {
		if p, ok := m.PeerID.(*tg.PeerUser); ok {
			if u := uctx.Users[p.UserID]; u != nil {
				return displayName(u)
			}
		}
		return ""
	}
	if pu, ok := from.(*tg.PeerUser); ok {
		if u := uctx.Users[pu.UserID]; u != nil {
			return displayName(u)
		}
	}
	return ""
}

func displayName(u *tg.User) string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" && u.Username != "" {
		name = "@" + u.Username
	}
	return name
}

func looksLikePeerID(s string) bool {
	_, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return err == nil
}

// splitMessage cuts text into chunks <= n bytes, preferring paragraph and line
// boundaries.
func splitMessage(text string, n int) []string {
	if len(text) <= n {
		return []string{text}
	}
	var out []string
	for len(text) > n {
		cut := strings.LastIndex(text[:n], "\n\n")
		if cut < n/2 {
			cut = strings.LastIndex(text[:n], "\n")
		}
		if cut < n/2 {
			cut = strings.LastIndex(text[:n], " ")
		}
		if cut <= 0 {
			cut = n
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		text = text[cut:]
	}
	if t := strings.TrimSpace(text); t != "" {
		out = append(out, t)
	}
	return out
}

// PeerSnapshot is used by tests.
func (a *Adapter) PeerSnapshot(channelID string) map[string]peerRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]peerRef{}
	for k, v := range a.peers[channelID] {
		out[k] = v
	}
	return out
}
