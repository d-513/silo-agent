// Package host names what the domain packages under internal/app need from the
// App that hosts them. Each domain declares the few it uses by embedding these
// in its own Host interface; *app.App implements them all, so a domain never
// imports package app and tests can pass a small fake.
package host

import (
	"context"

	"silo.agent/internal/db"
)

// Authorizer gates one connector.action for a Bot through its rules: nil means
// allowed, an error means denied (or the human declined). A Bot-rule action is
// conn "bot". fallback is the default decision when no rule row exists.
type Authorizer interface {
	AuthorizeAction(ctx context.Context, bot *db.Bot, runID, conn, action, argsJSON, fallback string) (string, error)
}

// Runs resolves a run to the conversation it belongs to ("" when unknown).
type Runs interface {
	ChatOfRun(runID string) string
}

// Emitter publishes one run event to the Bot's viewers and, when the run is
// known, persists it in the run's log (secrets masked, text made valid UTF-8).
type Emitter interface {
	Emit(botID, chatID, runID, kind, body, tool string)
}
