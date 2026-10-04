package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/textx"
) // Subagents are background agent loops a chat's lead starts with spawn_agent.
// Each one owns a hidden log Chat (chats.subagent_id) and runs through the same
// engine as a chat, from a fresh context: its history is only its own log. The
// lead and its subagents share the lead chat's Taskboard.
//
// Waking: when a subagent finishes and its lead has no live run, the lead gets a
// new run that opens with a subagent_report event. A lead that is running is
// expected to inspect on its own (agent_status, the per-turn note, sleep); when
// such a run ends with results it never looked at, the same wake fires once.

const (
	// subagentReportKind opens a lead's wake run. Body is the report; Tool is
	// "name:status,…" for the UI.
	subagentReportKind = "subagent_report"

	subagentMaxLive   = 8
	subagentResultMax = 20000
	subagentReportMax = 4000
	sleepMax          = 600
	taskboardMax      = 200
	taskTextMax       = 500
	// wakeDelay batches subagents that finish together into one wake run.
	wakeDelay = 750 * time.Millisecond
)

var subagentNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,24}$`)

// runContext is the run's deadline: the configured cap, or none when it is 0.
func runContext(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
}

// --- tool sets ---

// leadOnlyTools are dropped from a subagent's tool list: it cannot start or
// steer agents, wipe the shared board, talk on channels, switch models, or
// schedule automations.
var leadOnlyTools = map[string]bool{
	"spawn_agent": true, "agent_status": true, "message_agent": true, "stop_agent": true,
	"task_reset": true, "channel": true, "switch_model": true,
	"list_automations": true, "create_automation": true, "update_automation": true, "delete_automation": true,
}

func isAgentTool(name string) bool {
	switch name {
	case "spawn_agent", "agent_status", "message_agent", "stop_agent":
		return true
	}
	return false
}

// toolsFor is the tool list for a run: everything for a chat, minus the
// lead-only tools for a subagent and search_docs while nothing is indexed.
func (a *App) toolsFor(botID string, origin *run.Origin) []llm.Tool {
	all := a.runTools()
	if !a.Knowledge.Active(botID) {
		kept := make([]llm.Tool, 0, len(all))
		for _, t := range all {
			if t.Name != "search_docs" {
				kept = append(kept, t)
			}
		}
		all = kept
	}
	if origin == nil || origin.Subagent == nil {
		return all
	}
	out := make([]llm.Tool, 0, len(all))
	for _, t := range all {
		if !leadOnlyTools[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// --- lookups ---

// subagentOfChat returns the subagent whose log is chatID, if any.
func (a *App) subagentOfChat(chatID string) *db.Subagent {
	if chatID == "" || a.DB == nil {
		return nil
	}
	var sa db.Subagent
	a.DB.Where("chat_id = ?", chatID).Limit(1).Find(&sa)
	if sa.ID == "" {
		return nil
	}
	return &sa
}

// boardChat is the chat whose Taskboard a run uses: a subagent shares its
// lead's; anything else owns its own.
func (a *App) boardChat(chatID string) string {
	if sa := a.subagentOfChat(chatID); sa != nil {
		return sa.ParentChatID
	}
	return chatID
}

func (a *App) subagentsOf(leadChatID string) []db.Subagent {
	var rows []db.Subagent
	a.DB.Where("parent_chat_id = ?", leadChatID).Order("created_at").Find(&rows)
	return rows
}

func (a *App) findSubagent(leadChatID, name string) (*db.Subagent, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name required")
	}
	var sa db.Subagent
	a.DB.Where("parent_chat_id = ? AND lower(name) = lower(?)", leadChatID, name).Limit(1).Find(&sa)
	if sa.ID == "" {
		var names []string
		for _, s := range a.subagentsOf(leadChatID) {
			names = append(names, s.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no subagent named %q (this chat has none)", name)
		}
		return nil, fmt.Errorf("no subagent named %q (have: %s)", name, strings.Join(names, ", "))
	}
	return &sa, nil
}

func (a *App) subagentLive(sa *db.Subagent) bool {
	return a.LiveRunID(sa.BotID, sa.ChatID) != ""
}

// pingLead tells viewers of the lead chat that its subagents or board moved.
// It is transient: nothing is persisted and the client refetches.
func (a *App) pingLead(botID, leadChatID, kind string) {
	if leadChatID == "" {
		return
	}
	a.Bus.Publish(botID, &v1.RunEvent{ChatId: leadChatID, Kind: kind})
}

// quietSubagents marks running subagents stopped by something the lead already
// knows about (the lead's Stop, stop_agent, Stop Bot) so their finish does not
// wake the lead.
func (a *App) quietSubagents(status, where string, args ...any) {
	if a.DB == nil {
		return
	}
	now := time.Now()
	a.DB.Model(&db.Subagent{}).Where(where, args...).Where("status = ?", "running").
		Updates(map[string]any{"status": status, "reported": true, "finished_at": &now})
}

// stopSubagents stops every live subagent of a lead chat without waking it.
func (a *App) stopSubagents(botID, leadChatID string) {
	for _, sa := range a.subagentsOf(leadChatID) {
		if !a.subagentLive(&sa) {
			continue
		}
		a.quietSubagents("stopped", "id = ?", sa.ID)
		a.stopChat(botID, sa.ChatID)
	}
	a.pingLead(botID, leadChatID, "subagents")
}

// dropSubagents deletes a lead chat's subagents, their logs, and its board.
func (a *App) dropSubagents(botID, leadChatID string) {
	for _, sa := range a.subagentsOf(leadChatID) {
		a.quietSubagents("stopped", "id = ?", sa.ID)
		a.StopChatLive(botID, sa.ChatID)
		a.dropChat(sa.ChatID)
		a.DB.Delete(&db.Subagent{}, "id = ?", sa.ID)
	}
	a.DB.Where("chat_id = ?", leadChatID).Delete(&db.TaskItem{})
}

// --- finishing and waking ---

// afterRun is the finish hook: a subagent's run records its result and nudges
// its lead; a lead's run that ends with unseen results wakes it once.
func (a *App) afterRun(botID, chatID, runID, st string) {
	if a.DB == nil {
		return
	}
	if sa := a.subagentOfChat(chatID); sa != nil {
		a.subagentFinished(sa, runID, st)
		return
	}
	if st == "done" || st == "error" {
		a.scheduleWake(botID, chatID)
	}
}

func (a *App) subagentFinished(sa *db.Subagent, runID, st string) {
	defer a.pingLead(sa.BotID, sa.ParentChatID, "subagents")
	if st == "stopped" && sa.Status != "running" && sa.Reported {
		return // quieted by a stop the lead already knows about
	}
	status := st
	if status == "" {
		status = "done"
	}
	now := time.Now()
	result := textx.TruncateUTF8(a.runResult(runID), subagentResultMax)
	a.DB.Model(&db.Subagent{}).Where("id = ?", sa.ID).Updates(map[string]any{
		"status": status, "result": result, "reported": false, "finished_at": &now,
	})
	if lr := a.liveRunFor(sa.BotID, sa.ParentChatID); lr != nil {
		lr.signal("subagent " + sa.Name + " finished (" + status + ")")
	}
	a.scheduleWake(sa.BotID, sa.ParentChatID)
}

// runResult is a run's final reply: the text of its last model turn, or its
// last progress note or error when there is none.
func (a *App) runResult(runID string) string {
	var evs []db.RunEvent
	a.DB.Where("run_id = ?", runID).Order("seq").Find(&evs)
	var final []string
	fallback := ""
	for _, ev := range evs {
		switch ev.Kind {
		case "tool", "tool_result":
			final = nil
		case "assistant", "section":
			if strings.TrimSpace(ev.Body) != "" && ev.Body != "Stopped." {
				final = append(final, ev.Body)
			}
		case "section_live", "error":
			if strings.TrimSpace(ev.Body) != "" {
				fallback = ev.Body
			}
		}
	}
	if len(final) > 0 {
		return strings.Join(final, "\n\n")
	}
	return fallback
}
