package app

import (
	"context"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/ids"
)

func (a *App) ensureChrome(ctx context.Context, botID, runID string) error {
	id := ids.New()
	a.mu.Lock()
	a.cmdRun[id] = runID
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cmdRun, id)
		a.mu.Unlock()
	}()
	out, err := a.Hub.Exec(ctx, botID, &v1.Cmd{
		Id: id, RunId: runID,
		Body: &v1.Cmd_EnsureChrome{EnsureChrome: &v1.EnsureChromeCmd{}},
	})
	if err != nil {
		return err
	}
	if out == "started" {
		a.emit(botID, a.ChatOfRun(runID), runID, "call", "Chromium", "chromium")
		a.emit(botID, a.ChatOfRun(runID), runID, "call_result", "opened on the desktop", "chromium")
	}
	return nil
}
