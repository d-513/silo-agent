package app

import (
	"context"

	"silo.agent/internal/app/access"

	"silo.agent/internal/db"
)

// spaceHost is the App as the Workspaces domain sees it.
type spaceHost struct{ a *App }

func (h spaceHost) DropBot(ctx context.Context, botID string) {
	var b db.Bot
	if h.a.DB.Where("id = ?", botID).Limit(1).Find(&b); b.ID != "" {
		h.a.dropBot(ctx, &b)
	}
}

func (h spaceHost) AccessEnded(botID, userID string) { h.a.Gone.End(botID, userID) }

func (h spaceHost) SignUp(ctx context.Context, email, password string) (*db.User, error) {
	return h.a.Accounts.SignUp(ctx, email, password)
}

// chatAuthor is who writes in a chat: "" for the Bot's own.
func (a *App) chatAuthor(chatID string) string {
	if chatID == "" {
		return ""
	}
	var c db.Chat
	a.DB.Select("user_id").Where("id = ?", chatID).Limit(1).Find(&c)
	return c.UserID
}

// shared reports whether the Bot has more than one person.
func (a *App) shared(botID string) bool {
	return len(access.People(a.DB, botID)) > 1
}

// chatReader answers, for one person on a Bot, whether they read a chat,
// remembering each answer: the Bot-wide stream asks it for every event.
func (a *App) chatReader(botID, userID string) func(chatID string) bool {
	manager := access.BotRole(a.DB, botID, userID) == access.RoleManage
	seen := map[string]bool{}
	return func(chatID string) bool {
		if manager {
			return true
		}
		if ok, hit := seen[chatID]; hit {
			return ok
		}
		var c db.Chat
		a.DB.Select("id", "user_id", "automation_id").Where("id = ?", chatID).Limit(1).Find(&c)
		ok := c.ID != "" && access.CanRead(&c, userID, false)
		seen[chatID] = ok
		return ok
	}
}
