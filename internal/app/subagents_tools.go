package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/security"
) // --- sleep ---

// sleepTool pauses a run until the time is up, a message arrives, or (for a
// lead) a subagent finishes. A reason already pending returns at once.
func (a *App) sleepTool(ctx context.Context, chatID, runID string, args map[string]any) (string, error) {
	secs := num(args, "seconds")
	if secs < 1 {
		secs = 1
	}
	if secs > sleepMax {
		secs = sleepMax
	}
	a.mu.Lock()
	lr := a.runs[runID]
	a.mu.Unlock()
	if lr == nil {
		return "", errors.New("no live run")
	}
	// Stale nudges were already visible in this turn's note; only new ones count.
drain:
	for {
		select {
		case <-lr.wake:
		default:
			break drain
		}
	}
	if len(lr.inbox) > 0 {
		return "woke at once: a new message is waiting", nil
	}
	var unread int64
	a.DB.Model(&db.Subagent{}).Where("parent_chat_id = ? AND reported = ? AND status <> ?", chatID, false, "running").Count(&unread)
	if unread > 0 {
		return "woke at once: a subagent already finished — check agent_status", nil
	}
	start := time.Now()
	t := time.NewTimer(time.Duration(secs) * time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.C:
		return fmt.Sprintf("slept %ds", secs), nil
	case why := <-lr.wake:
		return fmt.Sprintf("woke after %s: %s", shortDur(time.Since(start)), why), nil
	}
}

// --- agent tools ---

func (a *App) agentTool(ctx context.Context, bot *db.Bot, chatID, runID, name string, args map[string]any) (string, error) {
	str := func(k string) string {
		s, _ := args[k].(string)
		return strings.TrimSpace(s)
	}
	if a.subagentOfChat(chatID) != nil {
		return "", errors.New("subagents cannot manage agents; only the lead can")
	}
	if chatID == "" {
		return "", errors.New("no conversation")
	}
	slip := func(extra map[string]any) string {
		b, _ := json.Marshal(extra)
		return string(b)
	}
	switch name {
	case "spawn_agent":
		return a.spawnAgent(ctx, bot, chatID, runID, str("name"), str("goal"), str("context"), str("model"))
	case "agent_status":
		if _, err := a.AuthorizeAction(ctx, bot, runID, security.Agents, "status", slip(map[string]any{"name": str("name")}), ""); err != nil {
			return "", err
		}
		if str("name") == "" {
			rows := a.subagentsOf(chatID)
			if len(rows) == 0 {
				return "No subagents in this chat.", nil
			}
			var b strings.Builder
			b.WriteString("Subagents:\n")
			for i := range rows {
				b.WriteString(a.subagentLine(&rows[i]) + "\n")
			}
			return b.String(), nil
		}
		sa, err := a.findSubagent(chatID, str("name"))
		if err != nil {
			return "", err
		}
		return a.subagentDetail(sa, num(args, "events")), nil
	case "message_agent":
		sa, err := a.findSubagent(chatID, str("name"))
		if err != nil {
			return "", err
		}
		text := str("text")
		if text == "" {
			return "", errors.New("text required")
		}
		if _, err := a.AuthorizeAction(ctx, bot, runID, security.Agents, "message", slip(map[string]any{"name": sa.Name, "text": text}), ""); err != nil {
			return "", err
		}
		resumed, err := a.messageSubagent(sa, text)
		if err != nil {
			return "", err
		}
		if resumed {
			return "resumed " + sa.Name + " with your message; it works in the background again", nil
		}
		return "delivered to " + sa.Name + "'s live run", nil
	case "stop_agent":
		sa, err := a.findSubagent(chatID, str("name"))
		if err != nil {
			return "", err
		}
		if _, err := a.AuthorizeAction(ctx, bot, runID, security.Agents, "stop", slip(map[string]any{"name": sa.Name}), ""); err != nil {
			return "", err
		}
		if !a.subagentLive(sa) {
			return sa.Name + " is not running (" + sa.Status + ")", nil
		}
		a.quietSubagents("stopped", "id = ?", sa.ID)
		a.stopChat(bot.ID, sa.ChatID)
		a.pingLead(bot.ID, chatID, "subagents")
		return "stopped " + sa.Name, nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// subagentDefaultModel is the operator's subagent model when it is allowed,
// otherwise the lead's own model.
func (a *App) subagentDefaultModel(botID, leadChatID string) string {
	cfg := a.cfg()
	if m := strings.TrimSpace(cfg.ModelSubagent); m != "" && llm.Allowed(m, cfg.Models) {
		return m
	}
	return a.Models.Resolve(botID, leadChatID)
}

func subagentBrief(goal, ctxText string) string {
	var b strings.Builder
	b.WriteString("## Goal\n" + strings.TrimSpace(goal))
	if c := strings.TrimSpace(ctxText); c != "" {
		b.WriteString("\n\n## Context\n" + c)
	}
	return b.String()
}

func (a *App) spawnAgent(ctx context.Context, bot *db.Bot, leadChatID, runID, name, goal, ctxText, model string) (string, error) {
	if !subagentNameRE.MatchString(name) {
		return "", errors.New("name must be 1-24 letters, digits, - or _")
	}
	if goal == "" {
		return "", errors.New("goal required")
	}
	var clash db.Subagent
	a.DB.Where("parent_chat_id = ? AND lower(name) = lower(?)", leadChatID, name).Limit(1).Find(&clash)
	if clash.ID != "" {
		return "", fmt.Errorf("a subagent named %q already exists here — message_agent it, or pick another name", clash.Name)
	}
	live := 0
	for _, sa := range a.subagentsOf(leadChatID) {
		if a.subagentLive(&sa) {
			live++
		}
	}
	if live >= subagentMaxLive {
		return "", fmt.Errorf("at most %d subagents may run at once; wait for one to finish", subagentMaxLive)
	}
	if model != "" {
		if _, _, err := llm.Parse(model); err != nil {
			return "", err
		}
		if !llm.Allowed(model, a.cfg().Models) {
			return "", fmt.Errorf("model %q is not allowed; see list_models", model)
		}
	} else {
		model = a.subagentDefaultModel(bot.ID, leadChatID)
	}
	slip, _ := json.Marshal(map[string]any{"name": name, "model": model, "goal": goal})
	if _, err := a.AuthorizeAction(ctx, bot, runID, security.Agents, "spawn", string(slip), ""); err != nil {
		return "", err
	}
	now := time.Now()
	sa := db.Subagent{
		ID: ids.New(), BotID: bot.ID, ParentChatID: leadChatID, Name: name, Goal: goal, Context: ctxText,
		Model: model, Status: "running", Reported: true, CreatedAt: now,
	}
	c := db.Chat{ID: ids.New(), BotID: bot.ID, SubagentID: sa.ID, Title: name, Model: model, CreatedAt: now, UpdatedAt: now}
	if err := a.DB.Create(&c).Error; err != nil {
		return "", err
	}
	sa.ChatID = c.ID
	if err := a.DB.Create(&sa).Error; err != nil {
		return "", err
	}
	a.convMu.Lock()
	_, err := a.StartRun(run.Request{BotID: bot.ID, ChatID: c.ID, Text: subagentBrief(goal, ctxText), From: "lead", Origin: &run.Origin{Subagent: &sa}})
	a.convMu.Unlock()
	if err != nil {
		return "", err
	}
	a.pingLead(bot.ID, leadChatID, "subagents")
	return fmt.Sprintf("started subagent %s on %s. It works in the background. Unless you have your own work left, end your turn now: you are woken with its result when it finishes.", name, model), nil
}

// messageSubagent injects text into a live subagent run or resumes a finished
// one. It reports whether it resumed.
func (a *App) messageSubagent(sa *db.Subagent, text string) (bool, error) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if runID := a.LiveRunID(sa.BotID, sa.ChatID); runID != "" {
		if a.inject(sa.BotID, sa.ChatID, runID, text, nil, "lead") {
			return false, nil
		}
		return false, fmt.Errorf("%s has too many unread messages; wait for it to catch up", sa.Name)
	}
	a.DB.Model(&db.Subagent{}).Where("id = ?", sa.ID).Updates(map[string]any{"status": "running", "reported": true, "finished_at": nil})
	sa.Status, sa.Reported, sa.FinishedAt = "running", true, nil
	if _, err := a.StartRun(run.Request{BotID: sa.BotID, ChatID: sa.ChatID, Text: text, From: "lead", Origin: &run.Origin{Subagent: sa}}); err != nil {
		return false, err
	}
	a.pingLead(sa.BotID, sa.ParentChatID, "subagents")
	return true, nil
}

// subagentDetail is agent_status for one subagent. Reading it marks the
// result seen.
func (a *App) subagentDetail(sa *db.Subagent, n int) string {
	if n <= 0 {
		n = 12
	}
	if n > 40 {
		n = 40
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", sa.Name, sa.Model)
	b.WriteString(strings.TrimPrefix(a.subagentLine(sa), "- ") + "\n")
	fmt.Fprintf(&b, "Goal: %s\n", oneLine(sa.Goal, 300))
	var run db.Run
	a.DB.Where("chat_id = ?", sa.ChatID).Order("created_at desc").Limit(1).Find(&run)
	if run.ID != "" {
		var evs []db.RunEvent
		a.DB.Where("run_id = ? AND kind IN ?", run.ID, []string{"user", "tool", "tool_result", "assistant", "section", "section_live", "error"}).
			Order("seq desc").Limit(n).Find(&evs)
		slices.Reverse(evs)
		if len(evs) > 0 {
			b.WriteString("\nRecent steps (latest run):\n")
		}
		for _, ev := range evs {
			switch ev.Kind {
			case "user":
				fmt.Fprintf(&b, "- message: %s\n", oneLine(ev.Body, 200))
			case "tool":
				fmt.Fprintf(&b, "- %s %s\n", ev.Tool, oneLine(ev.Body, 160))
			case "tool_result":
				fmt.Fprintf(&b, "  → %s\n", oneLine(ev.Body, 200))
			case "error":
				fmt.Fprintf(&b, "- error: %s\n", oneLine(ev.Body, 300))
			default:
				fmt.Fprintf(&b, "- says: %s\n", oneLine(ev.Body, 400))
			}
		}
	}
	if !a.subagentLive(sa) {
		res := strings.TrimSpace(sa.Result)
		if res == "" {
			res = "(no final reply)"
		}
		b.WriteString("\nResult:\n" + res + "\n")
		a.DB.Model(&db.Subagent{}).Where("id = ?", sa.ID).Update("reported", true)
	}
	return b.String()
}
