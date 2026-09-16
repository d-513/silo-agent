package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/channels"
	// Register built-in adapters.
	_ "silo.agent/internal/channels/telegram"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// channelHost adapts *App to the channels.Host interface (the App's own
// GetSecret signature is the worker RPC).
type channelHost struct{ app *App }

func (h channelHost) GetSecret(botID, name string) (string, error) {
	var sec db.Secret
	if err := h.app.DB.First(&sec, "bot_id = ? AND name = ?", botID, name).Error; err != nil {
		return "", fmt.Errorf("unknown secret")
	}
	h.app.Mask(botID).Add(sec.Value)
	return sec.Value, nil
}

func (h channelHost) DeliverInbound(ctx context.Context, ch *db.Channel, in channels.Inbound) error {
	return h.app.deliverInbound(ctx, ch, in)
}

func (h channelHost) PublishState(channelID string, st channels.State) {
	h.app.publishChannelState(channelID, st)
}

func (h channelHost) CurrentChannel(id string) (*db.Channel, bool) {
	var ch db.Channel
	if h.app.DB.First(&ch, "id = ?", id).Error != nil {
		return nil, false
	}
	return &ch, true
}

func (h channelHost) DataDir() string { return h.app.cfg().DataDir }

// --- config ---

func (a *App) enabledChannels(botID string) []db.Channel {
	var out []db.Channel
	a.DB.Where("bot_id = ? AND enabled = ?", botID, true).Order("created_at").Find(&out)
	return out
}

func (a *App) allChannels(botID string) []db.Channel {
	var out []db.Channel
	a.DB.Where("bot_id = ?", botID).Order("created_at").Find(&out)
	return out
}

// channelConfig resolves non-secret config plus decrypted secret fields.
func (a *App) channelConfig(ch *db.Channel) channels.Config {
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
		if a.DB.First(&sec, "bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Error == nil {
			cfg.Values[f.Key] = sec.Value
			a.Mask(ch.BotID).Add(sec.Value)
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

func (a *App) channelSecretsSet(ch *db.Channel) []string {
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
		a.DB.Model(&db.Secret{}).Where("bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Count(&n)
		if n > 0 {
			out = append(out, f.Key)
		}
	}
	return out
}

// saveChannelConfig validates required fields, writes non-secrets to the row
// and secrets to the Bot secret store.
func (a *App) saveChannelConfig(ch *db.Channel, ad channels.Adapter, config, secrets map[string]string) error {
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
		if a.DB.First(&sec, "bot_id = ? AND name = ?", ch.BotID, name).Error == nil {
			sec.Value = v
			a.DB.Save(&sec)
		} else {
			a.DB.Create(&db.Secret{ID: ids.New(), BotID: ch.BotID, Name: name, Value: v, CreatedAt: time.Now()})
		}
		a.Mask(ch.BotID).Add(v)
	}
	// Required check (after writes, so a first-time save passes).
	for _, f := range desc.Fields {
		if !f.Required {
			continue
		}
		if f.Type == channels.FieldSecret {
			var n int64
			a.DB.Model(&db.Secret{}).Where("bot_id = ? AND name = ?", ch.BotID, channels.SecretName(ch.ID, f.Key)).Count(&n)
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

func (a *App) reconcileChannels() {
	if a.DB == nil {
		return
	}
	var chans []db.Channel
	a.DB.Where("enabled = ?", true).Find(&chans)
	for i := range chans {
		c := chans[i]
		a.startChannel(&c)
	}
}

// startChannel (re)starts an adapter worker. It is safe to call when already
// running: the old worker is cancelled first.
func (a *App) startChannel(ch *db.Channel) {
	a.stopChannel(ch.ID)
	if !ch.Enabled {
		a.setChannelStatus(ch.ID, "stopped", "")
		return
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		a.setChannelStatus(ch.ID, "error", "adapter not found")
		return
	}
	cfg := a.channelConfig(ch)
	ctx, cancel := context.WithCancel(context.Background())
	a.chanMu.Lock()
	a.chanCancel[ch.ID] = cancel
	a.chanMu.Unlock()
	a.setChannelStatus(ch.ID, "starting", "")
	go func() {
		if _, err := ad.Validate(ctx, ch, cfg); err != nil {
			a.setChannelStatus(ch.ID, "error", err.Error())
			return
		}
		err := ad.Start(ctx, ch, cfg, channelHost{a})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.setChannelStatus(ch.ID, "error", err.Error())
			log.Printf("channel %s (%s): %v", ch.ID, ch.Adapter, err)
			return
		}
		a.setChannelStatus(ch.ID, "stopped", "")
	}()
}

func (a *App) stopChannel(id string) {
	a.chanMu.Lock()
	cancel := a.chanCancel[id]
	delete(a.chanCancel, id)
	a.chanMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) setChannelStatus(id, status, detail string) {
	a.DB.Model(&db.Channel{}).Where("id = ?", id).Updates(map[string]any{"status": status, "status_detail": detail})
}

func (a *App) publishChannelState(channelID string, st channels.State) {
	// Merge values with the previously stored state. Adapters stash durable
	// keys in Values (the Telegram adapter keeps peer access hashes under
	// peer:…), and a plain status update must not wipe them.
	var prev channels.State
	var ch db.Channel
	if a.DB.First(&ch, "id = ?", channelID).Error == nil && strings.TrimSpace(ch.StateJSON) != "" {
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
	a.chanMu.Lock()
	a.chanStates[channelID] = st
	a.chanMu.Unlock()
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
		a.DB.Model(&db.Channel{}).Where("id = ?", channelID).Updates(updates)
	}
}

func (a *App) channelState(channelID string) channels.State {
	a.chanMu.Lock()
	st, ok := a.chanStates[channelID]
	a.chanMu.Unlock()
	if ok {
		return st
	}
	var ch db.Channel
	if a.DB.First(&ch, "id = ?", channelID).Error == nil && strings.TrimSpace(ch.StateJSON) != "" {
		_ = json.Unmarshal([]byte(ch.StateJSON), &st)
	}
	return st
}

// deliverInbound is the bridge from an adapter to the execution engine: find or
// create the conversation's Chat, then inject or start a run.
func (a *App) deliverInbound(ctx context.Context, ch *db.Channel, in channels.Inbound) error {
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
	c, err := a.findOrCreateConversation(ch, external, in.Title)
	if err != nil {
		return err
	}
	origin := &runOrigin{
		channel:  ch,
		external: external,
		deliver: func(msg channels.Outbound) error {
			ad, ok := channels.Lookup(ch.Adapter)
			if !ok {
				return fmt.Errorf("adapter %q is not available", ch.Adapter)
			}
			if msg.ExternalID == "" {
				msg.ExternalID = external
			}
			return ad.Send(context.Background(), ch, a.channelConfig(ch), msg)
		},
	}
	_, err = a.startOrInject(ch.BotID, c.ID, in.Text, nil, origin)
	if err != nil {
		return err
	}
	a.DB.Model(&c).Update("updated_at", time.Now())
	return nil
}

// findOrCreateConversation maps an external conversation to a Chat row.
func (a *App) findOrCreateConversation(ch *db.Channel, external, title string) (*db.Chat, error) {
	a.chatMu.Lock()
	defer a.chatMu.Unlock()
	var c db.Chat
	a.DB.Where("bot_id = ? AND channel_id = ? AND external_id = ?", ch.BotID, ch.ID, external).Limit(1).Find(&c)
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
	if err := a.DB.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// startOrInject serializes the decision to either steer a live run or start a
// new one. This is the single entry point for chats and channels.
func (a *App) startOrInject(botID, chatID, text string, atts []*v1.Attachment, origin *runOrigin) (string, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if runID := a.liveRunID(botID, chatID); runID != "" {
		if a.inject(botID, chatID, runID, text, atts) {
			return runID, nil
		}
	}
	return a.startRun(runRequest{botID: botID, chatID: chatID, text: text, atts: atts, origin: origin})
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

func (a *App) protoChannel(ch *db.Channel) *v1.Channel {
	adapterName := ch.Adapter
	if ad, ok := channels.Lookup(ch.Adapter); ok {
		adapterName = ad.Descriptor().Name
	}
	return &v1.Channel{
		Id: ch.ID, BotId: ch.BotID, Adapter: ch.Adapter, AdapterName: adapterName,
		Name: ch.Name, Enabled: ch.Enabled, Inbound: ch.Inbound, Prompt: ch.Prompt,
		Config: nonSecretConfig(ch), SecretsSet: a.channelSecretsSet(ch),
		Status: ch.Status, StatusDetail: ch.StatusDetail,
		State:      protoChannelState(a.channelState(ch.ID)),
		ExternalId: ch.ExternalID, TargetTitle: ch.TargetTitle,
	}
}

func (a *App) ListChannelAdapters(ctx context.Context, _ *connect.Request[v1.ListChannelAdaptersRequest]) (*connect.Response[v1.ListChannelAdaptersResponse], error) {
	if currentUser(ctx) == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	out := &v1.ListChannelAdaptersResponse{}
	for _, d := range channels.Descriptors() {
		out.Adapters = append(out.Adapters, protoChannelAdapter(d))
	}
	return connect.NewResponse(out), nil
}

func (a *App) ListBotChannels(ctx context.Context, req *connect.Request[v1.ListBotChannelsRequest]) (*connect.Response[v1.ListBotChannelsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	out := &v1.ListBotChannelsResponse{}
	for i, ch := range a.allChannels(req.Msg.GetBotId()) {
		_ = i
		c := ch
		out.Channels = append(out.Channels, a.protoChannel(&c))
	}
	return connect.NewResponse(out), nil
}

func (a *App) CreateChannel(ctx context.Context, req *connect.Request[v1.CreateChannelRequest]) (*connect.Response[v1.Channel], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
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
	name = a.uniqueChannelName(botID, name, "")
	ch := db.Channel{
		ID: ids.New(), BotID: botID, Adapter: ad.Descriptor().Slug, Name: name,
		Enabled: req.Msg.GetEnabled(), Inbound: req.Msg.GetInbound(),
		Prompt: strings.TrimSpace(req.Msg.GetPrompt()), Status: "stopped", CreatedAt: time.Now(),
		ExternalID: strings.TrimSpace(req.Msg.GetExternalId()), TargetTitle: strings.TrimSpace(req.Msg.GetTargetTitle()),
	}
	if err := a.saveChannelConfig(&ch, ad, req.Msg.GetConfig(), req.Msg.GetSecrets()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := a.DB.Create(&ch).Error; err != nil {
		return nil, err
	}
	if ch.Enabled {
		a.startChannel(&ch)
	}
	return connect.NewResponse(a.protoChannel(&ch)), nil
}

func (a *App) UpdateChannel(ctx context.Context, req *connect.Request[v1.UpdateChannelRequest]) (*connect.Response[v1.Channel], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := a.DB.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("adapter not available"))
	}
	if name := strings.TrimSpace(req.Msg.GetName()); name != "" && name != ch.Name {
		ch.Name = a.uniqueChannelName(ch.BotID, name, ch.ID)
	}
	ch.Enabled = req.Msg.GetEnabled()
	ch.Inbound = req.Msg.GetInbound()
	ch.Prompt = strings.TrimSpace(req.Msg.GetPrompt())
	if next := strings.TrimSpace(req.Msg.GetExternalId()); next != ch.ExternalID {
		// Re-picking the chat starts a fresh conversation.
		a.clearChannelChats(ch.ID)
		ch.ExternalID = next
	}
	ch.TargetTitle = strings.TrimSpace(req.Msg.GetTargetTitle())
	if err := a.saveChannelConfig(&ch, ad, req.Msg.GetConfig(), req.Msg.GetSecrets()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := a.DB.Save(&ch).Error; err != nil {
		return nil, err
	}
	a.startChannel(&ch)
	return connect.NewResponse(a.protoChannel(&ch)), nil
}

func (a *App) DeleteChannel(ctx context.Context, req *connect.Request[v1.DeleteChannelRequest]) (*connect.Response[v1.DeleteChannelResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := a.DB.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	a.deleteChannel(&ch)
	return connect.NewResponse(&v1.DeleteChannelResponse{}), nil
}

func (a *App) clearChannelChats(channelID string) {
	var chats []db.Chat
	a.DB.Where("channel_id = ?", channelID).Find(&chats)
	for _, c := range chats {
		var runs []db.Run
		a.DB.Where("chat_id = ?", c.ID).Find(&runs)
		for _, r := range runs {
			a.DB.Where("run_id = ?", r.ID).Delete(&db.RunEvent{})
		}
		a.DB.Where("chat_id = ?", c.ID).Delete(&db.Run{})
	}
	a.DB.Where("channel_id = ?", channelID).Delete(&db.Chat{})
}

func (a *App) deleteChannel(ch *db.Channel) {
	a.stopChannel(ch.ID)
	a.clearChannelChats(ch.ID)
	a.DB.Where("bot_id = ? AND name LIKE ?", ch.BotID, "channel."+ch.ID+".%").Delete(&db.Secret{})
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", ch.BotID, "channels", ch.ID).Delete(&db.Rule{})
	a.DB.Delete(ch)
	a.chanMu.Lock()
	delete(a.chanStates, ch.ID)
	a.chanMu.Unlock()
}

func (a *App) ChannelAction(ctx context.Context, req *connect.Request[v1.ChannelActionRequest]) (*connect.Response[v1.ChannelActionResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var ch db.Channel
	if err := a.DB.First(&ch, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
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
			a.clearChannelChats(ch.ID)
			ch.ExternalID = value
		}
		ch.TargetTitle = title
		if err := a.DB.Save(&ch).Error; err != nil {
			return nil, err
		}
		// Ask the adapter to republish its state so the peer access hash it
		// just used is persisted before the next restart.
		if rst, rerr := ad.Action(ctx, &ch, a.channelConfig(&ch), "refresh", nil); rerr == nil && rst.Kind != channels.StateError {
			a.publishChannelState(ch.ID, rst)
		}
		st := channels.State{Kind: channels.StateInfo, Message: "Chat: " + title}
		return connect.NewResponse(&v1.ChannelActionResponse{State: protoChannelState(st)}), nil
	}
	st, err := ad.Action(ctx, &ch, a.channelConfig(&ch), req.Msg.GetAction(), req.Msg.GetPayload())
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	a.publishChannelState(ch.ID, st)
	return connect.NewResponse(&v1.ChannelActionResponse{State: protoChannelState(st)}), nil
}

func (a *App) uniqueChannelName(botID, name, exceptID string) string {
	base := name
	for i := 2; ; i++ {
		var n int64
		q := a.DB.Model(&db.Channel{}).Where("bot_id = ? AND name = ?", botID, name)
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
