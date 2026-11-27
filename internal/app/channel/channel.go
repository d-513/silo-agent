// Package channel is a Bot's messaging channels: the rows an owner configures,
// the adapter worker behind each (Telegram and friends, see internal/channels),
// the conversations an inbound message opens, the channel and chats tools, and
// the section delivery back out.
package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/app/voice"
	"silo.agent/internal/app/workspace"
	"silo.agent/internal/channels"
	"silo.agent/internal/config"
	"silo.agent/internal/masker"
	"silo.agent/internal/prompts"

	// Register built-in adapters.
	_ "silo.agent/internal/channels/discord"
	_ "silo.agent/internal/channels/email"
	_ "silo.agent/internal/channels/matrix"
	_ "silo.agent/internal/channels/mattermost"
	_ "silo.agent/internal/channels/signal"
	_ "silo.agent/internal/channels/slack"
	_ "silo.agent/internal/channels/teams"
	_ "silo.agent/internal/channels/telegram"
	_ "silo.agent/internal/channels/whatsapp"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Host is what the channels need from the App around them.
type Host interface {
	host.Authorizer
	// Wake starts the Bot's machine if it is down and waits for its worker;
	// false when it did not come up in time.
	Wake(ctx context.Context, botID string) bool
}

// Service owns the channel rows, their adapter workers and conversations.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	engine run.Engine
	host   Host
	mask   func(botID string) *masker.Masker
	ws     *workspace.Service
	voice  *voice.Service

	// mu guards workers (the running adapter workers) and states (their
	// dynamic state). chatMu serializes find-or-create of a channel
	// conversation.
	mu      sync.Mutex
	workers map[string]*worker
	states  map[string]channels.State
	chatMu  sync.Mutex

	lanes   lanes
	typing  typists
	replies replies
	told    told
	refused refusals

	// Refused, when set, notes a message the channel did not answer because
	// its sender is not on the channel's list (the App files it in the Audit).
	Refused func(ch *db.Channel, who string)
}

func (s *Service) logf(format string, args ...any) { log.Printf(format, args...) }

func New(gdb *gorm.DB, cfg func() config.Config, e run.Engine, h Host, mask func(botID string) *masker.Masker, ws *workspace.Service, v *voice.Service) *Service {
	return &Service{db: gdb, cfg: cfg, engine: e, host: h, mask: mask, ws: ws, voice: v,
		workers: map[string]*worker{}, states: map[string]channels.State{},
		lanes: lanes{busy: map[string][]func(){}}, typing: typists{every: typingEvery, max: typingMax, live: map[string]*typist{}},
		replies: replies{to: map[string]string{}}}
}

// adapterHost is what an adapter sees: channels.Host, backed by the Service.
// w is the worker it was handed to (nil outside a worker: an action, a
// removal); once that worker is stopped or replaced, what it publishes may
// still persist durable values but no longer sets the channel's status.
type adapterHost struct {
	s *Service
	w *worker
}

func (h adapterHost) GetSecret(botID, name string) (string, error) {
	var sec db.Secret
	if err := h.s.db.First(&sec, "bot_id = ? AND name = ?", botID, name).Error; err != nil {
		return "", fmt.Errorf("unknown secret")
	}
	h.s.mask(botID).Add(sec.Value)
	return sec.Value, nil
}

func (h adapterHost) DeliverInbound(ctx context.Context, ch *db.Channel, in channels.Inbound) error {
	return h.s.deliverInbound(ctx, ch, in)
}

func (h adapterHost) PublishState(channelID string, st channels.State) {
	if h.w != nil && !h.s.current(channelID, h.w) {
		h.s.publishValues(channelID, st.Values)
		return
	}
	h.s.publishChannelState(channelID, st)
}

func (h adapterHost) CurrentChannel(id string) (*db.Channel, bool) {
	var ch db.Channel
	if h.s.db.First(&ch, "id = ?", id).Error != nil {
		return nil, false
	}
	return &ch, true
}

func (h adapterHost) DataDir() string { return h.s.cfg().DataDir }

func (h adapterHost) DatabaseURL() string { return h.s.cfg().DatabaseURL }

// --- config ---

func (s *Service) Enabled(botID string) []db.Channel {
	var out []db.Channel
	s.db.Where("bot_id = ? AND enabled = ?", botID, true).Order("created_at").Find(&out)
	return out
}

func (s *Service) All(botID string) []db.Channel {
	var out []db.Channel
	s.db.Where("bot_id = ?", botID).Order("created_at").Find(&out)
	return out
}

// channelConfig resolves non-secret config plus decrypted secret fields.
func (s *Service) channelConfig(ch *db.Channel) channels.Config {
	cfg := channels.Config{Values: map[string]string{}, Net: s.cfg().OutboundPolicy(), HookURL: s.hookURL(ch.ID)}
	if strings.TrimSpace(ch.ConfigJSON) != "" {
		_ = json.Unmarshal([]byte(ch.ConfigJSON), &cfg.Values)
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return cfg
	}
	for _, f := range ad.Descriptor().Fields {
		if f.Type != channels.FieldSecret {
			continue
		}
		var sec db.Secret
		if s.db.First(&sec, "bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Error == nil {
			cfg.Values[f.Key] = sec.Value
			s.mask(ch.BotID).Add(sec.Value)
		}
	}
	return cfg
}

func nonSecretConfig(ch *db.Channel) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(ch.ConfigJSON) != "" {
		_ = json.Unmarshal([]byte(ch.ConfigJSON), &out)
	}
	return out
}

func (s *Service) channelSecretsSet(ch *db.Channel) []string {
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return nil
	}
	var out []string
	for _, f := range ad.Descriptor().Fields {
		if f.Type != channels.FieldSecret {
			continue
		}
		var n int64
		s.db.Model(&db.Secret{}).Where("bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Count(&n)
		if n > 0 {
			out = append(out, f.Key)
		}
	}
	return out
}

// saveChannelConfig validates required fields, writes non-secrets to the row
// and secrets to the Bot secret store.
func (s *Service) saveChannelConfig(ch *db.Channel, ad channels.Adapter, config, secrets map[string]string) error {
	desc := ad.Descriptor()
	values := map[string]string{}
	for _, f := range s.fieldsOf(desc) {
		if f.Type == channels.FieldSecret {
			continue
		}
		if v, ok := config[f.Key]; ok {
			values[f.Key] = v
		}
	}
	if err := s.checkHostConfig(values); err != nil {
		return err
	}
	if len(values) > 0 {
		b, err := json.Marshal(values)
		if err != nil {
			return err
		}
		ch.ConfigJSON = string(b)
	} else {
		ch.ConfigJSON = ""
	}
	for _, f := range desc.Fields {
		if f.Type != channels.FieldSecret {
			continue
		}
		v, ok := secrets[f.Key]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		name := channels.SecretName(ch.ID, f.Key)
		var sec db.Secret
		if s.db.First(&sec, "bot_id = ? AND name = ?", ch.BotID, name).Error == nil {
			sec.Value = v
			s.db.Save(&sec)
		} else {
			s.db.Create(&db.Secret{ID: ids.New(), BotID: ch.BotID, Name: name, Value: v, CreatedAt: time.Now()})
		}
		s.mask(ch.BotID).Add(v)
	}
	// Required check (after writes, so a first-time save passes).
	for _, f := range desc.Fields {
		if !f.Required {
			continue
		}
		if f.Type == channels.FieldSecret {
			var n int64
			s.db.Model(&db.Secret{}).Where("bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Count(&n)
			if n == 0 {
				return fmt.Errorf("%s is required", f.Label)
			}
			continue
		}
		if strings.TrimSpace(values[f.Key]) == "" {
			return fmt.Errorf("%s is required", f.Label)
		}
	}
	return nil
}

// --- lifecycle ---

func (s *Service) Reconcile() {
	if s.db == nil {
		return
	}
	var chans []db.Channel
	// A suspended Bot (no enabled manager) does not listen; Resume starts it
	// again.
	s.db.Where("enabled = ? AND bot_id NOT IN (?)", true, access.SuspendedBots(s.db)).Find(&chans)
	for i := range chans {
		c := chans[i]
		s.startChannel(&c)
	}
}

// Suspend stops a Bot's channel workers without touching their rows, for a Bot
// whose owner was disabled. Resume starts the enabled ones again.
func (s *Service) Suspend(botID string) {
	for _, c := range s.All(botID) {
		s.stopChannel(c.ID)
		if c.Enabled {
			s.setChannelStatus(c.ID, "stopped", "the Bot's owner is disabled")
		}
	}
}

func (s *Service) Resume(botID string) {
	for _, c := range s.All(botID) {
		if c.Enabled {
			s.startChannel(&c)
		}
	}
}

// worker is one running adapter Start. done closes once Start has returned,
// so the next worker of the same channel starts only after the old one let go
// of whatever it held (a client in the adapter's map, a platform session).
type worker struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// Start retries: an adapter whose Start fails (the network, the platform
// being down) is started again after RetryDelay(attempt), up to StartRetries
// times, unless the error is channels.Permanent. Each start is live-only, as
// always: nothing missed in between is fetched.
var (
	StartRetries = 8
	RetryDelay   = func(attempt int) time.Duration {
		d := 30 * time.Second << attempt
		if d > 15*time.Minute || d <= 0 {
			d = 15 * time.Minute
		}
		return d
	}
	// handover is how long a new worker waits for the old one to return.
	handover = 30 * time.Second
)

// startChannel (re)starts an adapter worker. It is safe to call when already
// running: the old worker is cancelled first, and the new one starts once the
// old one has returned. Concurrent calls leave exactly one worker.
func (s *Service) startChannel(ch *db.Channel) {
	if !ch.Enabled {
		s.stopChannel(ch.ID)
		s.setChannelStatus(ch.ID, "stopped", "")
		return
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		s.stopChannel(ch.ID)
		s.setChannelStatus(ch.ID, "error", "adapter not found")
		return
	}
	cfg := s.channelConfig(ch)
	c := *ch
	ctx, cancel := context.WithCancel(context.Background())
	w := &worker{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	old := s.workers[ch.ID]
	s.workers[ch.ID] = w
	delete(s.states, ch.ID)
	s.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	s.setChannelStatus(ch.ID, "starting", "")
	go func() {
		defer close(w.done)
		// Wait even when this one is replaced in the meantime: its done is
		// what the next worker waits on, so it must not close before the
		// worker it replaced has returned.
		if old != nil {
			select {
			case <-old.done:
			case <-time.After(handover):
				log.Printf("channel %s (%s): the old worker did not stop in %s", c.ID, c.Adapter, handover)
			}
		}
		if ctx.Err() != nil {
			return
		}
		s.runWorker(w, &c, ad, cfg)
	}()
}

// runWorker runs one worker's adapter until it is stopped, retrying a failed
// start with a backoff.
func (s *Service) runWorker(w *worker, ch *db.Channel, ad channels.Adapter, cfg channels.Config) {
	ctx := w.ctx
	// status writes only while this is still the channel's worker.
	status := func(st, detail string) {
		if ctx.Err() == nil && s.current(ch.ID, w) {
			s.setChannelStatus(ch.ID, st, detail)
		}
	}
	for attempt := 0; ; attempt++ {
		if _, err := ad.Validate(ctx, ch, cfg); err != nil {
			status("error", err.Error())
			return
		}
		err := ad.Start(ctx, ch, cfg, adapterHost{s, w})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			status("stopped", "")
			return
		}
		log.Printf("channel %s (%s): %v", ch.ID, ch.Adapter, err)
		if channels.IsPermanent(err) || attempt >= StartRetries {
			status("error", err.Error())
			return
		}
		wait := RetryDelay(attempt)
		status("error", fmt.Sprintf("%v (trying again in %s)", err, wait.Round(time.Second)))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		status("starting", "")
	}
}

// current reports whether w is still the channel's running worker.
func (s *Service) current(id string, w *worker) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workers[id] == w && w.ctx.Err() == nil
}

// StopAll stops every running adapter worker (Shutdown).
func (s *Service) StopAll() {
	s.mu.Lock()
	for id, w := range s.workers {
		w.cancel()
		delete(s.workers, id)
	}
	s.mu.Unlock()
}

// stopChannel cancels the channel's worker and forgets its live state, so a
// stopped channel does not go on showing how it last stood.
func (s *Service) stopChannel(id string) {
	s.mu.Lock()
	w := s.workers[id]
	delete(s.workers, id)
	delete(s.states, id)
	s.mu.Unlock()
	if w != nil {
		w.cancel()
	}
}

func (s *Service) setChannelStatus(id, status, detail string) {
	s.db.Model(&db.Channel{}).Where("id = ?", id).Updates(map[string]any{"status": status, "status_detail": detail})
}

func (s *Service) publishChannelState(channelID string, st channels.State) {
	// Merge values with the previously stored state. Adapters stash durable
	// keys in Values (the Telegram adapter keeps peer access hashes under
	// peer:…), and a plain status update must not wipe them.
	var prev channels.State
	var ch db.Channel
	if s.db.First(&ch, "id = ?", channelID).Error == nil && strings.TrimSpace(ch.StateJSON) != "" {
		_ = json.Unmarshal([]byte(ch.StateJSON), &prev)
	}
	if len(prev.Values) > 0 {
		merged := make(map[string]string, len(prev.Values)+len(st.Values))
		for k, v := range prev.Values {
			merged[k] = v
		}
		for k, v := range st.Values {
			merged[k] = v
		}
		st.Values = merged
	}
	s.mu.Lock()
	s.states[channelID] = st
	s.mu.Unlock()
	updates := map[string]any{}
	if b, err := json.Marshal(st); err == nil {
		updates["state_json"] = string(b)
	}
	switch st.Kind {
	case channels.StateError:
		updates["status"] = "error"
		updates["status_detail"] = st.Message
	case channels.StateQR, channels.StateAuth:
		updates["status"] = "pending_auth"
		updates["status_detail"] = ""
	case channels.StateInfo, channels.StateSelect:
		updates["status"] = "connected"
		updates["status_detail"] = ""
	}
	if len(updates) > 0 {
		s.db.Model(&db.Channel{}).Where("id = ?", channelID).Updates(updates)
	}
}

// publishValues persists only the durable values of a state (a stopped
// worker's last word: a paired JID, peer hashes), leaving the status and the
// live state to the worker that replaced it.
func (s *Service) publishValues(channelID string, values map[string]string) {
	if len(values) == 0 {
		return
	}
	var ch db.Channel
	if s.db.First(&ch, "id = ?", channelID).Error != nil {
		return
	}
	var st channels.State
	if strings.TrimSpace(ch.StateJSON) != "" {
		_ = json.Unmarshal([]byte(ch.StateJSON), &st)
	}
	if st.Values == nil {
		st.Values = map[string]string{}
	}
	for k, v := range values {
		st.Values[k] = v
	}
	if b, err := json.Marshal(st); err == nil {
		s.db.Model(&db.Channel{}).Where("id = ?", channelID).Update("state_json", string(b))
	}
}

func (s *Service) channelState(channelID string) channels.State {
	s.mu.Lock()
	st, ok := s.states[channelID]
	s.mu.Unlock()
	if ok {
		return st
	}
	var ch db.Channel
	if s.db.First(&ch, "id = ?", channelID).Error == nil && strings.TrimSpace(ch.StateJSON) != "" {
		_ = json.Unmarshal([]byte(ch.StateJSON), &st)
	}
	return st
}

// Origin is the run origin for a channel conversation: its sections
// are delivered back through the adapter.
func (s *Service) Origin(ch *db.Channel, external string) *run.Origin {
	return &run.Origin{
		Channel:  ch,
		External: external,
		Deliver: func(msg channels.Outbound) error {
			ad, ok := channels.Lookup(ch.Adapter)
			if !ok {
				return fmt.Errorf("adapter %q is not available", ch.Adapter)
			}
			if msg.ExternalID == "" {
				msg.ExternalID = external
			}
			if msg.ReplyTo == "" && msg.ExternalID == external {
				msg.ReplyTo = s.replies.get(ch.ID, external)
			}
			return ad.Send(context.Background(), ch, s.channelConfig(ch), msg)
		},
		Working: func() func() { return s.showTyping(ch, external) },
	}
}

// deliverInbound is the bridge from an adapter to the execution engine: find or
// create the conversation's Chat, then inject or start a run.
func (s *Service) deliverInbound(ctx context.Context, ch *db.Channel, in channels.Inbound) error {
	if !ch.Enabled || !ch.Inbound {
		return nil
	}
	if strings.TrimSpace(ch.ExternalID) == "" {
		return nil // no chat picked yet
	}
	external := strings.TrimSpace(in.ExternalID)
	if external == "" {
		return nil
	}
	if !answers(allowList(ch), in) {
		s.refuse(ch, in)
		return nil
	}
	c, err := s.findOrCreateConversation(ch, external, in.Title)
	if err != nil {
		return err
	}
	// Files take their time and the adapter's event loop must not wait for
	// them; whatever arrives behind one waits its turn, so a message typed
	// after a voice memo is not read before it.
	if len(in.Files) == 0 && !s.lanes.occupied(c.ID) {
		return s.hand(ch, c, external, in)
	}
	done := s.showTyping(ch, external)
	s.lanes.run(c.ID, func() {
		defer done()
		if err := s.hand(ch, c, external, in); err != nil {
			log.Printf("channel %s: inbound: %v", ch.ID, err)
		}
	})
	return nil
}

// hand gives one message to the conversation: a command runs, anything else
// joins the live run or starts one.
func (s *Service) hand(ch *db.Channel, c *db.Chat, external string, in channels.Inbound) error {
	s.replies.set(ch.ID, external, in.ReplyTo)
	origin := s.Origin(ch, external)
	if reply, ok := s.engine.Command(ch.BotID, c.ID, in.Text); ok {
		if strings.TrimSpace(reply) != "" {
			return origin.Deliver(channels.Outbound{Text: reply})
		}
		return nil
	}
	text, ex := in.Text, run.Extra{Sender: in.Author}
	if len(in.Files) > 0 {
		text, ex.Files = s.readFiles(ch.BotID, in)
	}
	if _, err := s.engine.StartOrInject(ch.BotID, c.ID, text, ex, origin); err != nil {
		return err
	}
	s.db.Model(c).Update("updated_at", time.Now())
	return nil
}

// findOrCreateConversation maps an external conversation to a Chat row.
func (s *Service) findOrCreateConversation(ch *db.Channel, external, title string) (*db.Chat, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	var c db.Chat
	s.db.Where("bot_id = ? AND channel_id = ? AND external_id = ?", ch.BotID, ch.ID, external).Limit(1).Find(&c)
	if c.ID != "" {
		return &c, nil
	}
	if strings.TrimSpace(title) == "" {
		title = ch.Name
	}
	c = db.Chat{
		ID: ids.New(), BotID: ch.BotID, ChannelID: ch.ID, ExternalID: external,
		Title: title, Model: s.channelModel(ch), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.db.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// --- proto / RPC ---

func protoChannelField(f channels.Field) *v1.ChannelField {
	out := &v1.ChannelField{
		Key: f.Key, Label: f.Label, Description: f.Description, Type: string(f.Type),
		Required: f.Required, Secret: f.Type == channels.FieldSecret,
	}
	for _, o := range f.Options {
		out.Options = append(out.Options, &v1.ChannelFieldOption{Value: o.Value, Label: o.Label})
	}
	return out
}

// protoChannelAdapter is an adapter as the form draws it: fields are its own
// followed by the host's (Service.fieldsOf).
func protoChannelAdapter(d channels.Descriptor, fields []channels.Field) *v1.ChannelAdapter {
	out := &v1.ChannelAdapter{Slug: d.Slug, Name: d.Name, Description: d.Description, Logo: d.Logo, Guide: d.Guide, RequiresTarget: d.RequiresTarget}
	for _, f := range fields {
		out.Fields = append(out.Fields, protoChannelField(f))
	}
	for _, a := range d.Actions {
		out.Actions = append(out.Actions, &v1.ChannelAdapterAction{Key: a.Key, Label: a.Label, Description: a.Description, Kind: string(a.Kind)})
	}
	return out
}

func protoChannelState(st channels.State) *v1.ChannelState {
	out := &v1.ChannelState{Kind: string(st.Kind), Message: st.Message, Qr: st.QR, Values: st.Values}
	for _, o := range st.Options {
		out.Options = append(out.Options, &v1.ChannelFieldOption{Value: o.Value, Label: o.Label})
	}
	return out
}

func (s *Service) protoChannel(ch *db.Channel) *v1.Channel {
	adapterName := ch.Adapter
	if ad, ok := channels.Lookup(ch.Adapter); ok {
		adapterName = ad.Descriptor().Name
	}
	return &v1.Channel{
		Id: ch.ID, BotId: ch.BotID, Adapter: ch.Adapter, AdapterName: adapterName,
		Name: ch.Name, Enabled: ch.Enabled, Inbound: ch.Inbound, Prompt: ch.Prompt,
		Config: nonSecretConfig(ch), SecretsSet: s.channelSecretsSet(ch),
		Status: ch.Status, StatusDetail: ch.StatusDetail,
		State:      protoChannelState(s.shownState(ch)),
		ExternalId: ch.ExternalID, TargetTitle: ch.TargetTitle,
	}
}

// shownState is the state the page shows: a stopped channel keeps its durable
// values but no longer says how it last stood ("Connected as …").
func (s *Service) shownState(ch *db.Channel) channels.State {
	st := s.channelState(ch.ID)
	if ch.Status == "stopped" {
		return channels.State{Values: st.Values}
	}
	return st
}

func (s *Service) ListChannelAdapters(ctx context.Context, _ *connect.Request[v1.ListChannelAdaptersRequest]) (*connect.Response[v1.ListChannelAdaptersResponse], error) {
	if access.User(ctx) == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	out := &v1.ListChannelAdaptersResponse{}
	for _, d := range channels.Descriptors() {
		out.Adapters = append(out.Adapters, protoChannelAdapter(d, s.fieldsOf(d)))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListBotChannels(ctx context.Context, req *connect.Request[v1.ListBotChannelsRequest]) (*connect.Response[v1.ListBotChannelsResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Use); err != nil {
		return nil, err
	}
	out := &v1.ListBotChannelsResponse{}
	for i, ch := range s.All(req.Msg.GetBotId()) {
		_ = i
		c := ch
		out.Channels = append(out.Channels, s.protoChannel(&c))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateChannel(ctx context.Context, req *connect.Request[v1.CreateChannelRequest]) (*connect.Response[v1.Channel], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	ad, ok := channels.Lookup(req.Msg.GetAdapter())
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown adapter"))
	}
	botID := req.Msg.GetBotId()
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		name = ad.Descriptor().Name
	}
	name = s.uniqueChannelName(botID, name, "")
	ch := db.Channel{
		ID: ids.New(), BotID: botID, Adapter: ad.Descriptor().Slug, Name: name,
		Enabled: req.Msg.GetEnabled(), Inbound: req.Msg.GetInbound(),
		Prompt: strings.TrimSpace(req.Msg.GetPrompt()), Status: "stopped", CreatedAt: time.Now(),
		ExternalID: strings.TrimSpace(req.Msg.GetExternalId()), TargetTitle: strings.TrimSpace(req.Msg.GetTargetTitle()),
	}
	if err := s.saveChannelConfig(&ch, ad, req.Msg.GetConfig(), req.Msg.GetSecrets()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.db.Create(&ch).Error; err != nil {
		return nil, err
	}
	if ch.Enabled {
		s.startChannel(&ch)
	}
	return connect.NewResponse(s.protoChannel(&ch)), nil
}

func (s *Service) UpdateChannel(ctx context.Context, req *connect.Request[v1.UpdateChannelRequest]) (*connect.Response[v1.Channel], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := s.db.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("adapter not available"))
	}
	if name := strings.TrimSpace(req.Msg.GetName()); name != "" && name != ch.Name {
		ch.Name = s.uniqueChannelName(ch.BotID, name, ch.ID)
	}
	ch.Enabled = req.Msg.GetEnabled()
	ch.Inbound = req.Msg.GetInbound()
	ch.Prompt = strings.TrimSpace(req.Msg.GetPrompt())
	if next := strings.TrimSpace(req.Msg.GetExternalId()); next != ch.ExternalID {
		// Re-picking the chat starts a fresh conversation.
		s.clearChannelChats(ch.ID)
		ch.ExternalID = next
	}
	ch.TargetTitle = strings.TrimSpace(req.Msg.GetTargetTitle())
	modelWas := s.channelModel(&ch)
	if err := s.saveChannelConfig(&ch, ad, req.Msg.GetConfig(), req.Msg.GetSecrets()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	// A new model for the channel is the model of the conversations it has.
	if m := s.channelModel(&ch); m != modelWas {
		s.db.Model(&db.Chat{}).Where("bot_id = ? AND channel_id = ?", ch.BotID, ch.ID).Update("model", m)
	}
	// Only the columns this edits: the adapter writes state_json and the
	// status while it runs (WhatsApp's paired JID is there), and a whole-row
	// save would put back what was read above.
	if err := s.db.Model(&db.Channel{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"name": ch.Name, "enabled": ch.Enabled, "inbound": ch.Inbound, "prompt": ch.Prompt,
		"external_id": ch.ExternalID, "target_title": ch.TargetTitle, "config_json": ch.ConfigJSON,
	}).Error; err != nil {
		return nil, err
	}
	if err := s.db.First(&ch, "id = ?", ch.ID).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	s.startChannel(&ch)
	return connect.NewResponse(s.protoChannel(&ch)), nil
}

func (s *Service) DeleteChannel(ctx context.Context, req *connect.Request[v1.DeleteChannelRequest]) (*connect.Response[v1.DeleteChannelResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := s.db.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	s.Delete(&ch)
	return connect.NewResponse(&v1.DeleteChannelResponse{}), nil
}

func (s *Service) clearChannelChats(channelID string) {
	var convs []db.Chat
	s.db.Where("channel_id = ?", channelID).Find(&convs)
	for _, c := range convs {
		var runs []db.Run
		s.db.Where("chat_id = ?", c.ID).Find(&runs)
		for _, r := range runs {
			s.db.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
		}
		s.db.Where("chat_id = ?", c.ID).Delete(&db.Run{})
	}
	s.db.Where("channel_id = ?", channelID).Delete(&db.Chat{})
}

func (s *Service) Delete(ch *db.Channel) {
	s.removeSession(ch)
	s.stopChannel(ch.ID)
	s.clearChannelChats(ch.ID)
	s.db.Where("bot_id = ? AND name LIKE ?", ch.BotID, "channel."+ch.ID+".%").Delete(&db.Secret{})
	s.db.Where("bot_id = ? AND connector = ? AND action = ?", ch.BotID, "channels", ch.ID).Delete(&db.Rule{})
	s.db.Delete(ch)
	s.mu.Lock()
	delete(s.states, ch.ID)
	s.mu.Unlock()
	s.replies.drop(ch.ID)
}

// removeSession lets an adapter end its own session before the channel goes
// (WhatsApp unlinks the device from the owner's phone and drops its keys). It
// runs before the worker stops, while the session is still connected. A failure
// is logged and never keeps the owner from deleting the channel.
func (s *Service) removeSession(ch *db.Channel) {
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return
	}
	rm, ok := ad.(channels.Remover)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := rm.Remove(ctx, ch, adapterHost{s: s}); err != nil {
		log.Printf("channel %s (%s): end session: %v", ch.ID, ch.Adapter, err)
	}
}

func (s *Service) ChannelAction(ctx context.Context, req *connect.Request[v1.ChannelActionRequest]) (*connect.Response[v1.ChannelActionResponse], error) {
	if _, err := access.Bot(ctx, s.db, req.Msg.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := s.db.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("adapter not available"))
	}
	// set_target is host-level: adapters only list choices, the app persists
	// which conversation the channel is bound to.
	if req.Msg.GetAction() == "set_target" {
		value := strings.TrimSpace(req.Msg.GetPayload()["value"])
		if value == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("value required"))
		}
		title := strings.TrimSpace(req.Msg.GetPayload()["label"])
		if title == "" {
			title = value
		}
		if value != ch.ExternalID {
			s.clearChannelChats(ch.ID)
			ch.ExternalID = value
		}
		ch.TargetTitle = title
		if err := s.db.Model(&db.Channel{}).Where("id = ?", ch.ID).Updates(map[string]any{
			"external_id": ch.ExternalID, "target_title": ch.TargetTitle,
		}).Error; err != nil {
			return nil, err
		}
		// Ask the adapter to republish its state so the peer access hash it
		// just used is persisted before the next restart.
		if rst, rerr := ad.Action(ctx, &ch, s.channelConfig(&ch), "refresh", nil); rerr == nil && rst.Kind != channels.StateError {
			s.publishChannelState(ch.ID, rst)
		}
		st := channels.State{Kind: channels.StateInfo, Message: "Chat: " + title}
		return connect.NewResponse(&v1.ChannelActionResponse{State: protoChannelState(st)}), nil
	}
	st, err := ad.Action(ctx, &ch, s.channelConfig(&ch), req.Msg.GetAction(), req.Msg.GetPayload())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	s.publishChannelState(ch.ID, st)
	return connect.NewResponse(&v1.ChannelActionResponse{State: protoChannelState(st)}), nil
}

func (s *Service) uniqueChannelName(botID, name, exceptID string) string {
	base := name
	for i := 2; ; i++ {
		var n int64
		q := s.db.Model(&db.Channel{}).Where("bot_id = ? AND name = ?", botID, name)
		if exceptID != "" {
			q = q.Where("id <> ?", exceptID)
		}
		q.Count(&n)
		if n == 0 {
			return name
		}
		name = fmt.Sprintf("%s %d", base, i)
	}
}

// ListPrompt is the session-tier system-prompt note naming the channels a Bot
// can send to; current marks the conversation the run came from. "" when there
// are none.
func ListPrompt(all []db.Channel, current *db.Channel) string {
	if len(all) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("This Bot is reachable through these channels. From Python, `silo_runtime.send_channel(channel, text)` sends a message to one and `silo_runtime.read_chats(chat=None, limit=20)` reads a chat's or a channel's history (name a chat or a channel; no chat lists them).\n")
	for i := range all {
		c := &all[i]
		line := fmt.Sprintf("- %s (adapter: %s)", c.Name, c.Adapter)
		if current != nil && current.ID == c.ID {
			line += " — this conversation"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// ConversationPrompt is the per-run note for a run that came from channel ch:
// which channel it is, the owner's own prompt for it, and the section delivery
// contract. "" for a run that did not come from a channel.
func ConversationPrompt(ch *db.Channel) string {
	if ch == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This conversation is the %s channel “%s”.\n\n", ch.Adapter, ch.Name)
	if p := strings.TrimSpace(ch.Prompt); p != "" {
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.TrimSpace(prompts.Channel))
	return b.String()
}

// HookPath is where a channel's webhook is served: HookPath + the channel's id.
const HookPath = "/channels/hook/"

// hookURL is the public address of a channel's webhook, "" with no public_url.
func (s *Service) hookURL(channelID string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg().PublicURL), "/")
	if base == "" {
		return ""
	}
	return base + HookPath + channelID
}

// ServeHook hands a request at a channel's webhook to its adapter
// (channels.Webhook). The id in the path is no secret and nobody is signed in:
// the adapter proves who is calling. An unknown, disabled or suspended channel
// and an adapter with no webhook are all the same 404.
func (s *Service) ServeHook(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, HookPath)
	var ch db.Channel
	if id != "" && !strings.Contains(id, "/") {
		s.db.Where("id = ? AND enabled = ?", id, true).Limit(1).Find(&ch)
	}
	if ch.ID == "" || access.Suspended(s.db, ch.BotID) {
		http.NotFound(w, r)
		return
	}
	ad, _ := channels.Lookup(ch.Adapter)
	hook, ok := ad.(channels.Webhook)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	wk := s.workers[ch.ID]
	s.mu.Unlock()
	hook.Hook(w, r, &ch, s.channelConfig(&ch), adapterHost{s, wk})
}
