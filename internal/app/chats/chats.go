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
)

// Proto is the wire form of a chat.
func Proto(c *db.Chat) *v1.Chat {
	return &v1.Chat{
		Id: c.ID, BotId: c.BotID, Title: c.Title, Model: c.Model, Thinking: c.Thinking,
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
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
