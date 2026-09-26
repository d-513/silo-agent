package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

// sharedTools are the chat tools Python reaches too, through
// `silo_runtime` → CallTool(connector, action). Both paths run runShared, so the
// rule gate and behavior cannot drift. Chat-only tools (soul, core_memory,
// switch_model, the file and desktop tools with their own worker paths) are
// not here.
var sharedTools = map[string][2]string{
	"feed":              {security.Bot, "feed"},
	"remember":          {security.Bot, "remember"},
	"recall":            {security.Bot, "recall"},
	"forget":            {security.Bot, "forget"},
	"list_automations":  {security.Automations, "list"},
	"create_automation": {security.Automations, "create"},
	"update_automation": {security.Automations, "update"},
	"delete_automation": {security.Automations, "delete"},
	"list_models":       {security.Model, "list"},
}

// sharedToolName is the chat tool behind connector.action, if it is shared.
func sharedToolName(conn, action string) (string, bool) {
	for name, key := range sharedTools {
		if key[0] == conn && key[1] == action {
			return name, true
		}
	}
	return "", false
}

// runShared authorizes and runs one shared action. structured asks for a JSON
// result where the chat tool returns prose (Python wants data, not lines).
func (a *App) runShared(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any, structured bool) (string, error) {
	key, ok := sharedTools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	str := func(k string) string {
		s, _ := args[k].(string)
		return s
	}
	switch key[0] {
	case security.Automations:
		// automationTool gates itself: its slip names the automation.
		return a.automationTool(ctx, bot, runID, name, args)
	case security.Bot:
		if name == "feed" {
			return a.feedTool(ctx, bot, runID, args)
		}
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := a.authorizeAction(ctx, bot, runID, key[0], key[1], string(argsJSON), ""); err != nil {
		return "", err
	}
	switch name {
	case "remember":
		return a.remember(ctx, bot.ID, runID, str("content"))
	case "recall":
		if !structured {
			return a.recallTool(ctx, bot.ID, args)
		}
		rows, err := a.recall(ctx, bot.ID, str("query"), num(args, "limit"), 0)
		if err != nil {
			return "", err
		}
		type memory struct {
			ID        string  `json:"id"`
			Content   string  `json:"content"`
			CreatedAt string  `json:"created_at"`
			Distance  float64 `json:"distance"`
		}
		out := struct {
			Memories []memory `json:"memories"`
		}{Memories: []memory{}}
		for _, r := range rows {
			out.Memories = append(out.Memories, memory{r.ID, r.Content, r.CreatedAt.Format(time.RFC3339), r.Distance})
		}
		b, err := json.Marshal(out)
		return string(b), err
	case "forget":
		return a.forget(bot.ID, str("id"))
	case "list_models":
		return a.listModelsTool(bot.ID, a.chatOfRun(runID))
	}
	return "", fmt.Errorf("unknown tool %s", name)
}
