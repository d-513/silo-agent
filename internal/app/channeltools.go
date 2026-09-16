package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/channels"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

// channelSendTool implements the `channel` chat tool and the Python
// `silo_runtime.send_channel` path. It defaults to the channel the run came
// from and gates on a per-channel rule (channels.<channelID>).
func (a *App) channelSendTool(ctx context.Context, botID, runID string, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("text required")
	}
	name, _ := args["channel"].(string)

	var ch db.Channel
	if strings.TrimSpace(name) == "" {
		var run db.Run
		if a.DB.First(&run, "id = ?", runID).Error != nil || run.ChannelID == "" {
			return "", fmt.Errorf("no channel context; pass channel")
		}
		if a.DB.First(&ch, "id = ?", run.ChannelID).Error != nil {
			return "", fmt.Errorf("origin channel not found")
		}
	} else if a.DB.First(&ch, "bot_id = ? AND name = ?", botID, strings.TrimSpace(name)).Error != nil {
		if a.DB.First(&ch, "bot_id = ? AND id = ?", botID, strings.TrimSpace(name)).Error != nil {
			return "", fmt.Errorf("unknown channel %q", name)
		}
	}
	if strings.TrimSpace(ch.ExternalID) == "" {
		return "", fmt.Errorf("channel %q has no chat selected yet", ch.Name)
	}

	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	argsJSON, _ := json.Marshal(map[string]string{"channel": ch.Name, "text": text})
	if _, err := a.authorizeAction(ctx, &bot, runID, security.Channels, ch.ID, string(argsJSON), ""); err != nil {
		return "", err
	}

	adapter, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return "", fmt.Errorf("adapter %q is not available", ch.Adapter)
	}
	cfg := a.channelConfig(&ch)
	if err := adapter.Send(ctx, &ch, cfg, channels.Outbound{ExternalID: ch.ExternalID, Text: text}); err != nil {
		return "", err
	}
	return fmt.Sprintf("sent to %s", ch.Name), nil
}

// chatsReadTool implements the `chats` chat tool and the Python
// `silo_runtime.read_chats` path: list this Bot's chats, or read one's history.
func (a *App) chatsReadTool(ctx context.Context, botID, runID string, args map[string]any) (string, error) {
	var bot db.Bot
	if err := a.DB.First(&bot, "id = ?", botID).Error; err != nil {
		return "", fmt.Errorf("unknown bot")
	}
	if _, err := a.authorizeAction(ctx, &bot, runID, security.Chats, "read", "", ""); err != nil {
		return "", err
	}
	chatQuery, _ := args["chat"].(string)
	limit := num(args, "limit")
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	if strings.TrimSpace(chatQuery) == "" {
		var chats []db.Chat
		a.DB.Where("bot_id = ?", botID).Order("updated_at desc").Limit(50).Find(&chats)
		if len(chats) == 0 {
			return "no chats", nil
		}
		var b strings.Builder
		b.WriteString("Chats:\n")
		for _, c := range chats {
			kind := "web"
			if c.ChannelID != "" {
				kind = "channel"
			}
			fmt.Fprintf(&b, "- %s  %s (%s)\n", c.ID, c.Title, kind)
		}
		return b.String(), nil
	}

	var c db.Chat
	if a.DB.First(&c, "bot_id = ? AND id = ?", botID, strings.TrimSpace(chatQuery)).Error != nil {
		if err := a.DB.Where("bot_id = ? AND title = ?", botID, strings.TrimSpace(chatQuery)).Limit(1).Find(&c).Error; err != nil || c.ID == "" {
			return "", fmt.Errorf("unknown chat %q", chatQuery)
		}
	}
	// A channel conversation belongs to the platform, so try the adapter's own
	// history first. Bot accounts cannot read it (BOT_METHOD_INVALID), in which
	// case fall back to what this Bot has received.
	if c.ChannelID != "" {
		if out, err := a.channelHistoryTool(ctx, c, limit); err == nil {
			return out, nil
		}
	}
	return a.localHistoryTool(c, limit), nil
}

// localHistoryTool reads the messages this Bot has recorded for a chat.
func (a *App) localHistoryTool(c db.Chat, limit int) string {
	var runs []db.Run
	a.DB.Where("chat_id = ?", c.ID).Order("created_at").Find(&runs)
	ids := make([]string, len(runs))
	for i := range runs {
		ids[i] = runs[i].ID
	}
	var evs []db.RunEvent
	if len(ids) > 0 {
		a.DB.Where("run_id IN ? AND kind IN ?", ids, []string{"user", "assistant", "section"}).Order("created_at").Find(&evs)
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
// (the platform), not the local run log.
func (a *App) channelHistoryTool(ctx context.Context, c db.Chat, limit int) (string, error) {
	var ch db.Channel
	if a.DB.First(&ch, "id = ?", c.ChannelID).Error != nil {
		return "", fmt.Errorf("channel for this chat no longer exists")
	}
	ad, ok := channels.Lookup(ch.Adapter)
	if !ok {
		return "", fmt.Errorf("adapter %q is not available", ch.Adapter)
	}
	msgs, err := ad.History(ctx, &ch, a.channelConfig(&ch), c.ExternalID, limit)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Chat %q via %s, newest last:\n", c.Title, ad.Descriptor().Name)
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
