package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"silo.agent/internal/app/automation"
	"silo.agent/internal/app/tool"
	"silo.agent/internal/db"
)

// setProactivity moves a Bot to a proactivity level: the column (which
// decides whether a background run may write into the main chat) and the
// Heartbeat's schedule (how often it checks in unasked).
func (a *App) setProactivity(botID, level string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	if _, ok := automation.HeartbeatSchedules[level]; !ok {
		return fmt.Errorf("proactivity is off, quiet or active, not %q", level)
	}
	if err := a.DB.Model(&db.Bot{}).Where("id = ?", botID).Update("proactivity", level).Error; err != nil {
		return err
	}
	return a.Automations.SetHeartbeat(botID, level)
}

// proactivityTool is set_proactivity: the Bot moves itself to the level its
// owner chose (in the interview, or when asked in a chat).
func (a *App) proactivityTool(ctx context.Context, c *tool.Call) (string, error) {
	level := c.Str("level")
	if err := a.setProactivity(c.Bot.ID, level); err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]any{"proactivity": strings.ToLower(strings.TrimSpace(level)), "heartbeat": automation.HeartbeatSchedules[strings.ToLower(strings.TrimSpace(level))]})
	return string(out), nil
}
