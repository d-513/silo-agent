package app

import (
	"context"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/ids"
)

func wantsChrome(code string) bool {
	s := strings.ToLower(code)
	return strings.Contains(s, "chrome_page") ||
		strings.Contains(s, "playwright") ||
		strings.Contains(s, "connect_over_cdp")
}

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
		a.emit(botID, a.chatOfRun(runID), runID, "call", "Chromium", "chromium")
		a.emit(botID, a.chatOfRun(runID), runID, "call_result", "opened on the desktop", "chromium")
	}
	return nil
}
