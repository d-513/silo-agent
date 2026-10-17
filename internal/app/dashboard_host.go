package app

import (
	"context"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/dashboard"
	"silo.agent/internal/ids"
)

var _ dashboard.Host = (*App)(nil)

// Online reports whether the Bot's worker is connected now.
func (a *App) Online(botID string) bool { return a.Hub.Connected(botID) }

// RunPython runs code in a throwaway interpreter on the Bot's machine, outside
// any run: what it prints (with a failing script's traceback) is the answer.
// Calls the code makes back into Silo are orphans, under the Bot's rules.
func (a *App) RunPython(ctx context.Context, botID, code string) (string, error) {
	cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_ExecPython{ExecPython: &v1.ExecPythonCmd{Code: code}}}
	return a.Hub.Exec(ctx, botID, cmd)
}

// RuleDecision is the Bot's call rule for conn.action now, without asking.
func (a *App) RuleDecision(botID, conn, action string) string {
	return a.ruleDecision(botID, conn, action, "")
}
