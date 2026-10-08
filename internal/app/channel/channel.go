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
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/host"
	"silo.agent/internal/app/run"
	"silo.agent/internal/channels"
	"silo.agent/internal/config"
	"silo.agent/internal/masker"
	"silo.agent/internal/prompts"

	// Register built-in adapters.
	_ "silo.agent/internal/channels/discord"
	_ "silo.agent/internal/channels/telegram"
	_ "silo.agent/internal/channels/whatsapp"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Host is what the channels need from the App around them.
type Host interface {
	host.Authorizer
}

// Service owns the channel rows, their adapter workers and conversations.
type Service struct {
	db     *gorm.DB
	cfg    func() config.Config
	engine run.Engine
	host   Host
	mask   func(botID string) *masker.Masker

	// mu guards cancel (the running adapter workers) and states (their
	// dynamic state). chatMu serializes find-or-create of a channel
	// conversation.
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	states map[string]channels.State
	chatMu sync.Mutex
}

func New(gdb *gorm.DB, cfg func() config.Config, e run.Engine, h Host, mask func(botID string) *masker.Masker) *Service {
	return &Service{db: gdb, cfg: cfg, engine: e, host: h, mask: mask,
		cancel: map[string]context.CancelFunc{}, states: map[string]channels.State{}}
}

// adapterHost is what an adapter sees: channels.Host, backed by the Service.
type adapterHost struct{ s *Service }

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
	cfg := channels.Config{Values: map[string]string{}}
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
	for _, f := range desc.Fields {
		if f.Type == channels.FieldSecret {
			continue
		}
		if v, ok := config[f.Key]; ok {
			values[f.Key] = v
		}
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
	// A disabled user's Bots do not listen; Resume starts them again.
	paused := s.db.Model(&db.Bot{}).Select("bots.id").Joins("JOIN users ON users.id = bots.user_id").Where("users.disabled = ?", true)
	s.db.Where("enabled = ? AND bot_id NOT IN (?)", true, paused).Find(&chans)
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

// startChannel (re)starts an adapter worker. It is safe to call when already
// running: the old worker is cancelled first.
func (s *Service) startChannel(ch *db.Channel) {
	s.stopChannel(ch.ID)
	if !ch.Enabled {
		s.setChannelStatus(ch.ID, "stopped", "")
		return
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		s.setChannelStatus(ch.ID, "error", "adapter not found")
		return
	}
	cfg := s.channelConfig(ch)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel[ch.ID] = cancel
	s.mu.Unlock()
	s.setChannelStatus(ch.ID, "starting", "")
	go func() {
		if _, err := ad.Validate(ctx, ch, cfg); err != nil {
			s.setChannelStatus(ch.ID, "error", err.Error())
			return
		}
		err := ad.Start(ctx, ch, cfg, adapterHost{s})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.setChannelStatus(ch.ID, "error", err.Error())
			log.Printf("channel %s (%s): %v", ch.ID, ch.Adapter, err)
			return
		}
		s.setChannelStatus(ch.ID, "stopped", "")
	}()
}

// StopAll stops every running adapter worker (Shutdown).
func (s *Service) StopAll() {
	s.mu.Lock()
	for id, cancel := range s.cancel {
		cancel()
		delete(s.cancel, id)
	}
	s.mu.Unlock()
}

func (s *Service) stopChannel(id string) {
	s.mu.Lock()
	cancel := s.cancel[id]
	delete(s.cancel, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
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
	case channels.StateInfo, channels.StateSelect:
		updates["status"] = "connected"
		updates["status_detail"] = ""
	}
	if len(updates) > 0 {
		s.db.Model(&db.Channel{}).Where("id = ?", channelID).Updates(updates)
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
			return ad.Send(context.Background(), ch, s.channelConfig(ch), msg)
		},
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
	c, err := s.findOrCreateConversation(ch, external, in.Title)
	if err != nil {
		return err
	}
	_, err = s.engine.StartOrInject(ch.BotID, c.ID, in.Text, nil, s.Origin(ch, external))
	if err != nil {
		return err
	}
	s.db.Model(&c).Update("updated_at", time.Now())
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
		Title: title, CreatedAt: time.Now(), UpdatedAt: time.Now(),
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

func protoChannelAdapter(d channels.Descriptor) *v1.ChannelAdapter {
	out := &v1.ChannelAdapter{Slug: d.Slug, Name: d.Name, Description: d.Description, Logo: d.Logo, Guide: d.Guide, RequiresTarget: d.RequiresTarget}
	for _, f := range d.Fields {
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
		State:      protoChannelState(s.channelState(ch.ID)),
		ExternalId: ch.ExternalID, TargetTitle: ch.TargetTitle,
	}
}

func (s *Service) ListChannelAdapters(ctx context.Context, _ *connect.Request[v1.ListChannelAdaptersRequest]) (*connect.Response[v1.ListChannelAdaptersResponse], error) {
	if access.User(ctx) == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	out := &v1.ListChannelAdaptersResponse{}
	for _, d := range channels.Descriptors() {
		out.Adapters = append(out.Adapters, protoChannelAdapter(d))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListBotChannels(ctx context.Context, req *connect.Request[v1.ListBotChannelsRequest]) (*connect.Response[v1.ListBotChannelsResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
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
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
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
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
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
	if err := s.saveChannelConfig(&ch, ad, req.Msg.GetConfig(), req.Msg.GetSecrets()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.db.Save(&ch).Error; err != nil {
		return nil, err
	}
	s.startChannel(&ch)
	return connect.NewResponse(s.protoChannel(&ch)), nil
}

func (s *Service) DeleteChannel(ctx context.Context, req *connect.Request[v1.DeleteChannelRequest]) (*connect.Response[v1.DeleteChannelResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
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
	if err := rm.Remove(ctx, ch, adapterHost{s}); err != nil {
		log.Printf("channel %s (%s): end session: %v", ch.ID, ch.Adapter, err)
	}
}

func (s *Service) ChannelAction(ctx context.Context, req *connect.Request[v1.ChannelActionRequest]) (*connect.Response[v1.ChannelActionResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
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
		if err := s.db.Save(&ch).Error; err != nil {
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
	b.WriteString("This Bot is reachable through these channels. Use the `channel` tool to send a message to one (it defaults to the current conversation) and the `chats` tool to read chat history.\n")
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
