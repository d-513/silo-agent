// Package chats holds the pieces of a conversation other domains also need: how
// a chat becomes its wire form, whether its title is still a placeholder, and
// where a run lives (a web chat, an automation log, a channel, a subagent).
package chats

import (
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
	"silo.agent/internal/zone"
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

// TitleMax caps a chat's title.
const TitleMax = 120

// NewTitle is a web chat's title until the title model names it.
const NewTitle = "New chat"

// New makes a web chat for the Bot. A blank title is NewTitle.
func New(gdb *gorm.DB, botID, title string) (*db.Chat, error) {
	title = textx.ClipRunes(strings.TrimSpace(strings.TrimLeft(title, "# ")), TitleMax)
	if title == "" {
		title = NewTitle
	}
	now := time.Now()
	c := db.Chat{ID: ids.New(), BotID: botID, Title: title, CreatedAt: now, UpdatedAt: now}
	if err := gdb.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// Web reports whether c is a conversation the owner reads in Silo: not an
// automation's or a subagent's log, not a channel conversation.
func Web(c *db.Chat) bool {
	return c != nil && c.ID != "" && c.AutomationID == "" && c.SubagentID == "" && c.ChannelID == ""
}

// WebScope keeps the rows Web would: the SQL form of the same test.
func WebScope(q *gorm.DB) *gorm.DB {
	return q.Where("(channel_id = '' OR channel_id IS NULL) AND (automation_id = '' OR automation_id IS NULL) AND (subagent_id = '' OR subagent_id IS NULL)")
}

// Put writes one event into a chat outside any live run: in a finished run
// of its own (origin names what wrote it), so replay reads it in order and
// the owner's next message there follows it. The event's zone is the Bot's
// now; at, when set, is its time. The chat's updated_at moves.
func Put(gdb *gorm.DB, ch *db.Chat, origin string, ev db.RunEvent) (*db.RunEvent, error) {
	now := time.Now()
	r := db.Run{ID: ids.New(), BotID: ch.BotID, ChatID: ch.ID, Origin: origin, Status: "done", CreatedAt: now}
	ev.ID, ev.RunID = ids.New(), r.ID
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = now
	}
	if ev.Zone == "" {
		ev.Zone = zone.OfBot(gdb, ch.BotID).String()
	}
	err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		if err := tx.Create(&ev).Error; err != nil {
			return err
		}
		return tx.Model(&db.Chat{}).Where("id = ?", ch.ID).Update("updated_at", now).Error
	})
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// DropRun deletes one run with its events: what Put wrote, taken back.
func DropRun(gdb *gorm.DB, runID string) {
	gdb.Where("run_id = ?", runID).Delete(&db.RunEvent{})
	gdb.Where("id = ?", runID).Delete(&db.Run{})
}

// Placeholders are the titles a chat has until it is named.
var Placeholders = []string{NewTitle, "Chat", ""}

// Untitled reports whether s is still a placeholder title.
func Untitled(s string) bool {
	return slices.Contains(Placeholders, strings.TrimSpace(s))
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
		title = NewTitle
	}
	return "chat", title
}

// SourceLabel is the human line for a run source (Source's kind and name),
// as a quote and a card name where they came from.
func SourceLabel(kind, name string) string {
	switch kind {
	case "automation":
		return "automation “" + name + "”"
	case "channel":
		return "channel “" + name + "”"
	case "chat":
		return "chat “" + name + "”"
	case "subagent":
		return "subagent “" + name + "”"
	}
	return ""
}

// Drop deletes a chat with its runs and events.
func Drop(gdb *gorm.DB, chatID string) {
	gdb.Where("run_id IN (?)", gdb.Model(&db.Run{}).Select("id").Where("chat_id = ?", chatID)).Delete(&db.RunEvent{})
	gdb.Where("chat_id = ?", chatID).Delete(&db.Run{})
	gdb.Where("id = ?", chatID).Delete(&db.Chat{})
}
