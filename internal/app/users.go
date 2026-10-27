package app

import (
	"context"
	"errors"
	"log"

	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
)

// What happens to Bots when a user goes is the App's to do; the account domain
// asks for it here (account.Host).

// DropUserBots deletes the Workspaces the user is alone in, with their Bots,
// and takes the user off everything else.
func (a *App) DropUserBots(ctx context.Context, userID string) {
	for _, sp := range a.Spaces.Alone(userID) {
		for _, b := range a.Spaces.Bots(sp.ID) {
			a.dropBot(ctx, &b)
		}
		if err := a.Spaces.Delete(sp.ID); err != nil {
			log.Printf("drop workspace %s: %v", sp.ID, err)
		}
	}
	a.Spaces.DropUser(ctx, userID)
	a.Spaces.Forget(userID)
}

// SuspendUser stops what the Bots left with no enabled manager are doing once
// the user is disabled: live runs, channel workers and the boxes themselves.
// Nothing is deleted, and StartRun refuses those Bots until a manager of
// theirs is enabled again.
func (a *App) SuspendUser(ctx context.Context, userID string) {
	for _, b := range a.Spaces.Managed(userID) {
		if !access.Suspended(a.DB, b.ID) {
			continue
		}
		a.Channels.Suspend(b.ID)
		a.haltBot(ctx, &b)
		a.DB.Model(&db.Bot{}).Where("id = ?", b.ID).Update("status", "stopped")
	}
	// What they lent is not lent while they cannot sign in: connector calls
	// refuse (LenderOK) and their drives come down.
	a.Drives.LenderChanged(userID)
}

// ResumeUser lets the Bots an enabled user manages work again. Their channels
// start listening; a box starts with the next message, as after any Stop.
func (a *App) ResumeUser(userID string) {
	for _, b := range a.Spaces.Managed(userID) {
		if !access.Suspended(a.DB, b.ID) {
			a.Channels.Resume(b.ID)
		}
	}
	a.Drives.LenderChanged(userID)
}

var errOwnerDisabled = errors.New("nobody who manages this Bot can sign in: its managers are disabled")

// ownerDisabled reports whether the Bot is suspended (no enabled manager):
// no chat reply, automation, channel message or subagent.
func (a *App) ownerDisabled(b *db.Bot) bool {
	return access.Suspended(a.DB, b.ID)
}
