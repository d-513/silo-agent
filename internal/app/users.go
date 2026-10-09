package app

import (
	"context"
	"errors"

	"silo.agent/internal/db"
)

// A user's Bots are the App's to stop and delete; the account domain asks for
// it here (account.Host).

// DropUserBots deletes every Bot the user owns.
func (a *App) DropUserBots(ctx context.Context, userID string) {
	for _, b := range a.botsOf(userID) {
		a.dropBot(ctx, &b)
	}
}

// SuspendUser stops what a disabled user's Bots are doing: live runs, channel
// workers and the boxes themselves. Nothing is deleted, and StartRun refuses
// those Bots until the user is enabled again.
func (a *App) SuspendUser(ctx context.Context, userID string) {
	for _, b := range a.botsOf(userID) {
		a.Channels.Suspend(b.ID)
		a.haltBot(ctx, &b)
		a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Update("status", "stopped")
	}
}

// ResumeUser lets an enabled user's Bots work again. Their channels start
// listening; a box starts with the next message, as after any Stop.
func (a *App) ResumeUser(userID string) {
	for _, b := range a.botsOf(userID) {
		a.Channels.Resume(b.ID)
	}
}

func (a *App) botsOf(userID string) []db.Bot {
	var bots []db.Bot
	a.DB.Where("user_id = ?", userID).Order("created_at").Find(&bots)
	return bots
}

var errOwnerDisabled = errors.New("this Bot's owner is disabled")

// ownerDisabled reports whether the Bot belongs to a disabled user, whose Bots
// do not run: no chat reply, automation, channel message or subagent.
func (a *App) ownerDisabled(b *db.Bot) bool {
	var n int64
	a.DB.Model(&db.User{}).Where("id = ? AND disabled = ?", b.UserID, true).Count(&n)
	return n > 0
}
