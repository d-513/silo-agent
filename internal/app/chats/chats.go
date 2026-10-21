// Package chats holds the pieces of a conversation other domains also need: how
// a chat becomes its wire form, whether its title is still a placeholder, and
// where a run lives (a web chat, an automation log, a channel, a subagent).
package chats

import (
	"strings"
	"time"

	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Proto is the wire form of a chat.
func Proto(c *db.Chat) *v1.Chat {
	return &v1.Chat{
		Id: c.ID, BotId: c.BotID, Title: c.Title, Model: c.Model, Thinking: c.Thinking,
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339), Main: c.Main,
	}
}

// MainTitle is the main chat's title; it is not renamed.
const MainTitle = "Main"

// Main is the Bot's main chat, made the first time it is asked for. The
// caller has checked the Bot is the user's.
func Main(gdb *gorm.DB, botID string) (*db.Chat, error) {
	var c db.Chat
	if gdb.Where("bot_id = ? AND main", botID).Limit(1).Find(&c); c.ID != "" {
		return &c, nil
	}
	now := time.Now()
	c = db.Chat{ID: ids.New(), BotID: botID, Main: true, Title: MainTitle, CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&c).Error; err != nil {
		// Made at the same moment by another caller (chats_main).
		var won db.Chat
		if gdb.Where("bot_id = ? AND main", botID).Limit(1).Find(&won); won.ID != "" {
			return &won, nil
		}
		return nil, err
	}
	return &c, nil
}

// MainIdle is the Bot's main chat when nothing is running in it: what a
// reply to something the Bot made opens in. With a reply running there, the
// caller opens a chat of its own instead, so the run's history and the
// thread stay in the same order. live says whether a conversation has a
// live run.
func MainIdle(gdb *gorm.DB, botID string, live func(botID, chatID string) string) (*db.Chat, bool) {
	c, err := Main(gdb, botID)
	if err != nil || live(botID, c.ID) != "" {
		return nil, false
	}
	return c, true
}

// Untitled reports whether s is still a placeholder title.
func Untitled(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "New chat", "Chat":
		return true
	}
	return false
}

// Source names where a conversation lives: a web chat, an automation's log, a
// channel conversation, or a subagent's log. An unknown chat is ("", "").
func Source(gdb *gorm.DB, chatID string) (kind, name string) {
	var c db.Chat
	gdb.Where("id = ?", chatID).Limit(1).Find(&c)
	switch {
	case c.ID == "":
		return "", ""
	case c.AutomationID != "":
		var au db.Automation
		gdb.Select("name").Where("id = ?", c.AutomationID).Limit(1).Find(&au)
		return "automation", au.Name
	case c.ChannelID != "":
		var ch db.Channel
		gdb.Select("name").Where("id = ?", c.ChannelID).Limit(1).Find(&ch)
		return "channel", ch.Name
	case c.SubagentID != "":
		return "subagent", c.Title
	}
	title := c.Title
	if Untitled(title) {
		title = "New chat"
	}
	return "chat", title
}

// Drop deletes a chat with its runs and events.
func Drop(gdb *gorm.DB, chatID string) {
	gdb.Where("run_id IN (?)", gdb.Model(&db.Run{}).Select("id").Where("chat_id = ?", chatID)).Delete(&db.RunEvent{})
	gdb.Where("chat_id = ?", chatID).Delete(&db.Run{})
	gdb.Where("id = ?", chatID).Delete(&db.Chat{})
}
