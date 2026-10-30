package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/app/toolarg"
	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

// SendTool implements the `channel` chat tool and the Python
// `silo_runtime.send_channel` path. It defaults to the channel the run came
// from and gates on a per-channel rule (channels.<channelID>).
func (s *Service) SendTool(ctx context.Context, botID, runID string, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("text required")
	}
	name, _ := args["channel"].(string)

	var ch db.Channel
	if strings.TrimSpace(name) == "" {
		var run db.Run
		if s.db.First(&run, "id = ?", runID).Error != nil || run.ChannelID == "" {
			return "", fmt.Errorf("no channel context; pass channel")
		}
		if s.db.First(&ch, "id = ?", run.ChannelID).Error != nil {
			return "", fmt.Errorf("origin channel not found")
		}
	} else if s.db.First(&ch, "bot_id = ? AND name = ?", botID, strings.TrimSpace(name)).Error != nil {
		if s.db.First(&ch, "bot_id = ? AND id = ?", botID, strings.TrimSpace(name)).Error != nil {
			return "", fmt.Errorf("unknown channel %q", name)
		}
	}
	if strings.TrimSpace(ch.ExternalID) == "" {
		return "", fmt.Errorf("channel %q has no chat selected yet", ch.Name)
	}

	var bot db.Bot
	if err := s.db.First(&bot, "id = ?", botID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	argsJSON, _ := json.Marshal(map[string]string{"channel": ch.Name, "text": text})
	if _, err := s.host.AuthorizeAction(ctx, &bot, runID, security.Channels, ch.ID, string(argsJSON), ""); err != nil {
		return "", err
	}

	adapter, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return "", fmt.Errorf("adapter %q is not available", ch.Adapter)
	}
	cfg := s.channelConfig(&ch)
	if err := adapter.Send(ctx, &ch, cfg, channels.Outbound{ExternalID: ch.ExternalID, Text: text}); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent to %s", ch.Name), nil
}

// ChatsTool implements the `chats` chat tool and the Python
// `silo_runtime.read_chats` path: list this Bot's chats, or read one's history.
func (s *Service) ChatsTool(ctx context.Context, botID, runID string, args map[string]any) (string, error) {
	var bot db.Bot
	if err := s.db.First(&bot, "id = ?", botID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	if _, err := s.host.AuthorizeAction(ctx, &bot, runID, security.Chats, "read", "", ""); err != nil {
		return "", err
	}
	query, _ := args["chat"].(string)
	query = strings.TrimSpace(query)
	limit := toolarg.Int(args, "limit")
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	if query == "" {
		return s.listChats(botID), nil
	}
	c, ch, err := s.findConversation(botID, query)
	if err != nil {
		return "", err
	}
	if ch.ID == "" {
		return s.localHistoryTool(c, limit), nil
	}
	// A channel conversation belongs to the platform, so try the adapter's own
	// history first. Not every platform can give it (BOT_METHOD_INVALID on a
	// Telegram bot account, always on WhatsApp): fall back to what this Bot has
	// received, and say so, or a partial log reads as the whole conversation.
	out, err := s.channelHistoryTool(ctx, &ch, c, limit)
	if err == nil {
		return out, nil
	}
	if c.ID == "" {
		return fmt.Sprintf("Channel %q: its history cannot be read (%v) and this Bot has received nothing from it, so what was said there is unknown, not empty.\n", ch.Name, err), nil
	}
	return fmt.Sprintf("Only what this Bot received is below: the platform's history cannot be read (%v).\n", err) + s.localHistoryTool(c, limit), nil
}

// listChats names this Bot's chats, then its channels: a channel that has only
// ever sent has no chat row, and is read by its name.
func (s *Service) listChats(botID string) string {
	var convs []db.Chat
	s.db.Where("bot_id = ?", botID).Order("updated_at desc").Limit(50).Find(&convs)
	var b strings.Builder
	if len(convs) > 0 {
		b.WriteString("Chats:\n")
	}
	for _, c := range convs {
		kind := "web"
		if c.ChannelID != "" {
			kind = "channel"
		}
		if c.AutomationID != "" {
			kind = "automation log"
		}
		if c.SubagentID != "" {
			kind = "subagent log"
		}
		fmt.Fprintf(&b, "- %s  %s (%s)\n", c.ID, c.Title, kind)
	}
	named := false
	for _, ch := range s.Enabled(botID) {
		if strings.TrimSpace(ch.ExternalID) == "" {
			continue
		}
		if !named {
			b.WriteString("Channels, read by name:\n")
			named = true
		}
		fmt.Fprintf(&b, "- %s (adapter: %s)\n", ch.Name, ch.Adapter)
	}
	if b.Len() == 0 {
		return "no chats"
	}
	return b.String()
}

// findConversation resolves what read_chats was asked for: a chat by id, a
// channel by name or id, then a chat by title. A channel's name is unique on
// its Bot and a chat's title is not, so the name goes first. The chat is empty
// for a channel nothing has been received from; the channel is empty for a chat
// that is not a channel's.
func (s *Service) findConversation(botID, query string) (db.Chat, db.Channel, error) {
	var c db.Chat
	var ch db.Channel
	s.db.Where("bot_id = ? AND id = ?", botID, query).Limit(1).Find(&c)
	if c.ID == "" {
		s.db.Where("bot_id = ? AND (name = ? OR id = ?)", botID, query, query).Limit(1).Find(&ch)
	}
	if ch.ID != "" {
		if strings.TrimSpace(ch.ExternalID) == "" {
			return c, ch, fmt.Errorf("channel %q has no chat selected yet", ch.Name)
		}
		s.db.Where("bot_id = ? AND channel_id = ? AND external_id = ?", botID, ch.ID, ch.ExternalID).Limit(1).Find(&c)
		return c, ch, nil
	}
	if c.ID == "" {
		s.db.Where("bot_id = ? AND title = ?", botID, query).Limit(1).Find(&c)
	}
	if c.ID == "" {
		return c, ch, fmt.Errorf("unknown chat %q", query)
	}
	if c.ChannelID != "" {
		s.db.Where("id = ?", c.ChannelID).Limit(1).Find(&ch)
	}
	return c, ch, nil
}

// localHistoryTool reads the messages this Bot has recorded for a chat.
func (s *Service) localHistoryTool(c db.Chat, limit int) string {
	var runs []db.Run
	s.db.Where("chat_id = ?", c.ID).Order("created_at").Find(&runs)
	ids := make([]string, len(runs))
	for i := range runs {
		ids[i] = runs[i].ID
	}
	var evs []db.RunEvent
	if len(ids) > 0 {
		s.db.Where("run_id IN ? AND kind IN ?", ids, []string{"user", "assistant", "section"}).Order("seq").Find(&evs)
	}
	if len(evs) > limit {
		evs = evs[len(evs)-limit:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Chat %q (%s):\n", c.Title, c.ID)
	for _, ev := range evs {
		who := "Bot"
		if ev.Kind == "user" {
			who = "User"
		}
		fmt.Fprintf(&b, "%s: %s\n", who, ev.Body)
	}
	return b.String()
}

// channelHistoryTool reads a channel conversation's messages from the adapter
// (the platform), not the local run log. c is empty for a channel with no chat
// row yet, and the channel's own target is the conversation.
func (s *Service) channelHistoryTool(ctx context.Context, ch *db.Channel, c db.Chat, limit int) (string, error) {
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return "", fmt.Errorf("adapter %q is not available", ch.Adapter)
	}
	title, external := c.Title, c.ExternalID
	if c.ID == "" {
		title, external = ch.Name, ch.ExternalID
	}
	msgs, err := ad.History(ctx, ch, s.channelConfig(ch), external, limit)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Chat %q via %s, newest last:\n", title, ad.Descriptor().Name)
	if len(msgs) == 0 {
		b.WriteString("(no messages)\n")
	}
	for _, m := range msgs {
		who := m.Author
		if who == "" {
			if m.Out {
				who = "Bot"
			} else {
				who = "User"
			}
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			text = "[media]"
		}
		fmt.Fprintf(&b, "%s: %s\n", who, text)
	}
	return b.String(), nil
}
