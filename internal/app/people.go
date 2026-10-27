package app

import (
	"context"

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
