package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
)

// Subagents are background agent loops a chat's lead starts with spawn_agent.
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
func (a *App) toolsFor(botID string, origin *runOrigin) []llm.Tool {
	all := a.runTools()
	if !a.knowledgeActive(botID) {
		kept := make([]llm.Tool, 0, len(all))
		for _, t := range all {
			if t.Name != "search_docs" {
				kept = append(kept, t)
			}
		}
		all = kept
	}
	if origin == nil || origin.subagent == nil {
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
	return a.liveRunID(sa.BotID, sa.ChatID) != ""
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
		a.stopChatLive(botID, sa.ChatID)
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
	result := truncateUTF8(a.runResult(runID), subagentResultMax)
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

// scheduleWake debounces a wake of the lead chat so subagents finishing
// together land in one report.
func (a *App) scheduleWake(botID, leadChatID string) {
	var n int64
	a.DB.Model(&db.Subagent{}).Where("parent_chat_id = ? AND reported = ? AND status <> ?", leadChatID, false, "running").Count(&n)
	if n == 0 {
		return
	}
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	if a.wakeTimers == nil {
		a.wakeTimers = map[string]*time.Timer{}
	}
	if t := a.wakeTimers[leadChatID]; t != nil {
		t.Stop()
	}
	a.wakeTimers[leadChatID] = time.AfterFunc(wakeDelay, func() {
		a.wakeMu.Lock()
		delete(a.wakeTimers, leadChatID)
		a.wakeMu.Unlock()
		a.wakeLead(botID, leadChatID)
	})
}

// subagentReport is what a lead's wake run opens with.
type subagentReport struct {
	body  string
	label string
}

// wakeLead starts a lead run with every finished, unseen subagent result. A
// lead that is running inspects on its own; an automation log is never woken.
func (a *App) wakeLead(botID, leadChatID string) {
	a.convMu.Lock()
	defer a.convMu.Unlock()
	if a.liveRunID(botID, leadChatID) != "" {
		return
	}
	var chat db.Chat
	a.DB.Where("id = ?", leadChatID).Limit(1).Find(&chat)
	if chat.ID == "" || chat.AutomationID != "" || chat.SubagentID != "" {
		return
	}
	var done []db.Subagent
	a.DB.Where("parent_chat_id = ? AND reported = ? AND status <> ?", leadChatID, false, "running").Order("finished_at").Find(&done)
	if len(done) == 0 {
		return
	}
	ids := make([]string, 0, len(done))
	for _, sa := range done {
		ids = append(ids, sa.ID)
	}
	a.DB.Model(&db.Subagent{}).Where("id IN ?", ids).Update("reported", true)
	rep := a.buildReport(leadChatID, done)
	var origin *runOrigin
	if chat.ChannelID != "" {
		var ch db.Channel
		if a.DB.Where("id = ?", chat.ChannelID).Limit(1).Find(&ch); ch.ID != "" {
			origin = a.channelOrigin(&ch, chat.ExternalID)
		}
	}
	if _, err := a.startRun(runRequest{botID: botID, chatID: leadChatID, origin: origin, report: rep}); err != nil {
		a.DB.Model(&db.Subagent{}).Where("id IN ?", ids).Update("reported", false)
	}
}

func (a *App) buildReport(leadChatID string, done []db.Subagent) *subagentReport {
	var b strings.Builder
	labels := make([]string, 0, len(done))
	for _, sa := range done {
		labels = append(labels, sa.Name+":"+sa.Status)
		fmt.Fprintf(&b, "### %s — %s\n", sa.Name, sa.Status)
		res := strings.TrimSpace(sa.Result)
		if res == "" {
			res = "(no final reply)"
		}
		if len(res) > subagentReportMax {
			res = truncateUTF8(res, subagentReportMax) + "\n…truncated — agent_status " + sa.Name + " for the rest"
		}
		b.WriteString(res + "\n\n")
	}
	var running []string
	for _, sa := range a.subagentsOf(leadChatID) {
		if a.subagentLive(&sa) {
			running = append(running, sa.Name)
		}
	}
	if len(running) > 0 {
		fmt.Fprintf(&b, "Still running: %s.\n", strings.Join(running, ", "))
	} else {
		b.WriteString("No subagents are running.\n")
	}
	return &subagentReport{body: strings.TrimSpace(b.String()), label: strings.Join(labels, ",")}
}

func (a *App) emitReport(botID, chatID, runID string, rep *subagentReport) {
	body := validUTF8(a.Mask(botID).Apply(rep.body))
	id := ids.New()
	now := time.Now()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: subagentReportKind, Body: body, Tool: rep.label, CreatedAt: now})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: subagentReportKind, Body: body, Tool: rep.label, CreatedAt: now.Format(time.RFC3339)})
}

// subagentReportText frames a report as the user turn the lead replays.
func subagentReportText(body string) string {
	return "[Automatic report — your subagents finished. This is not a message from the human.]\n\n" + body +
		"\n\nRead the results, update the taskboard, and continue: start follow-up work, or report to the human if the job is done."
}

// --- per-turn note ---

// withTurnNote returns msgs with note appended to the last message, for one
// request only. msgs itself is never changed.
func withTurnNote(msgs []llm.Message, note string) []llm.Message {
	if note == "" || len(msgs) == 0 {
		return msgs
	}
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser && last.Role != llm.RoleTool {
		return msgs
	}
	out := slices.Clone(msgs)
	last.Text += "\n\n" + note
	out[len(out)-1] = last
	return out
}

// turnNote is the live taskboard (and, for a lead, its subagents) as of this
// turn. Empty when there is nothing to show.
func (a *App) turnNote(chatID string, origin *runOrigin) string {
	if a.DB == nil || chatID == "" {
		return ""
	}
	sub := origin != nil && origin.subagent != nil
	board := chatID
	if sub {
		board = origin.subagent.ParentChatID
	}
	var parts []string
	if items := a.boardItems(board); len(items) > 0 {
		parts = append(parts, "<taskboard>\n"+formatBoard(items)+"</taskboard>")
	}
	if !sub {
		if rows := a.subagentsOf(chatID); len(rows) > 0 {
			var b strings.Builder
			b.WriteString("<subagents>\n")
			for i := range rows {
				b.WriteString(a.subagentLine(&rows[i]) + "\n")
			}
			b.WriteString("</subagents>")
			parts = append(parts, b.String())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "[Live status, refreshed every turn — not from the human]\n" + strings.Join(parts, "\n")
}

func shortDur(d time.Duration) string {
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// subagentLine is one subagent's status for the lead.
func (a *App) subagentLine(sa *db.Subagent) string {
	if a.subagentLive(sa) {
		line := fmt.Sprintf("- %s: running %s", sa.Name, shortDur(time.Since(a.subagentStarted(sa))))
		if act := a.subagentActivity(sa); act != "" {
			line += " · " + act
		}
		return line
	}
	line := "- " + sa.Name + ": " + sa.Status
	if sa.FinishedAt != nil {
		line += " " + shortDur(time.Since(*sa.FinishedAt)) + " ago"
	}
	if !sa.Reported {
		line += " — result unread (agent_status " + sa.Name + ")"
	}
	return line
}

// subagentStarted is when its latest run began.
func (a *App) subagentStarted(sa *db.Subagent) time.Time {
	var run db.Run
	a.DB.Where("chat_id = ?", sa.ChatID).Order("created_at desc").Limit(1).Find(&run)
	if run.ID == "" {
		return sa.CreatedAt
	}
	return run.CreatedAt
}

// subagentActivity is the latest step of its latest run, one line.
func (a *App) subagentActivity(sa *db.Subagent) string {
	var run db.Run
	a.DB.Where("chat_id = ?", sa.ChatID).Order("created_at desc").Limit(1).Find(&run)
	if run.ID == "" {
		return ""
	}
	var ev db.RunEvent
	a.DB.Where("run_id = ? AND kind IN ?", run.ID, []string{"tool", "assistant", "section", "section_live", "thinking"}).
		Order("seq desc").Limit(1).Find(&ev)
	switch ev.Kind {
	case "tool":
		return "using " + ev.Tool
	case "thinking":
		return "thinking"
	case "":
		return ""
	}
	return oneLine(ev.Body, 80)
}

func oneLine(s string, n int) string {
	return capRunes(strings.Join(strings.Fields(s), " "), n)
}

// --- sleep ---

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
		if _, err := a.authorizeAction(ctx, bot, runID, security.Agents, "status", slip(map[string]any{"name": str("name")}), ""); err != nil {
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
		if _, err := a.authorizeAction(ctx, bot, runID, security.Agents, "message", slip(map[string]any{"name": sa.Name, "text": text}), ""); err != nil {
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
		if _, err := a.authorizeAction(ctx, bot, runID, security.Agents, "stop", slip(map[string]any{"name": sa.Name}), ""); err != nil {
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
	return a.resolveModel(botID, leadChatID)
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
	if _, err := a.authorizeAction(ctx, bot, runID, security.Agents, "spawn", string(slip), ""); err != nil {
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
	_, err := a.startRun(runRequest{botID: bot.ID, chatID: c.ID, text: subagentBrief(goal, ctxText), from: "lead", origin: &runOrigin{subagent: &sa}})
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
	if runID := a.liveRunID(sa.BotID, sa.ChatID); runID != "" {
		if a.inject(sa.BotID, sa.ChatID, runID, text, nil, "lead") {
			return false, nil
		}
		return false, fmt.Errorf("%s has too many unread messages; wait for it to catch up", sa.Name)
	}
	a.DB.Model(&db.Subagent{}).Where("id = ?", sa.ID).Updates(map[string]any{"status": "running", "reported": true, "finished_at": nil})
	sa.Status, sa.Reported, sa.FinishedAt = "running", true, nil
	if _, err := a.startRun(runRequest{botID: sa.BotID, chatID: sa.ChatID, text: text, from: "lead", origin: &runOrigin{subagent: sa}}); err != nil {
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

// --- taskboard ---

var assigneeRE = regexp.MustCompile(`^\s*\[([A-Za-z0-9_-]{1,24})\]\s*`)

func (a *App) boardItems(chatID string) []db.TaskItem {
	var items []db.TaskItem
	a.DB.Where("chat_id = ?", chatID).Order("n").Find(&items)
	return items
}

func formatBoard(items []db.TaskItem) string {
	var b strings.Builder
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	fmt.Fprintf(&b, "Taskboard (%d/%d done):\n", done, len(items))
	for _, it := range items {
		mark := "[ ]"
		if it.Done {
			mark = "[x]"
		}
		fmt.Fprintf(&b, "#%d %s ", it.N, mark)
		if it.Assignee != "" {
			fmt.Fprintf(&b, "[%s] ", it.Assignee)
		}
		b.WriteString(it.Text)
		if it.Done {
			b.WriteString(" — done")
			if it.DoneBy != "" {
				b.WriteString(" by " + it.DoneBy)
			}
			if it.Note != "" {
				b.WriteString(": " + it.Note)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// taskActor names who is writing to the board from a run: the subagent's name
// or "lead".
func (a *App) taskActor(runID string) (actor string, sub *db.Subagent, chatID string) {
	chatID = a.chatOfRun(runID)
	if sa := a.subagentOfChat(chatID); sa != nil {
		return sa.Name, sa, chatID
	}
	return "lead", nil, chatID
}

// taskTool runs the four taskboard actions for chat and Python alike.
func (a *App) taskTool(ctx context.Context, bot *db.Bot, runID, name string, args map[string]any, structured bool) (string, error) {
	action := map[string]string{"task_add": "add", "task_list": "read", "task_done": "done", "task_reset": "reset"}[name]
	actor, sub, chatID := a.taskActor(runID)
	if chatID == "" {
		return "", errors.New("the taskboard needs a conversation")
	}
	board := chatID
	if sub != nil {
		board = sub.ParentChatID
	}
	if action == "reset" && sub != nil {
		return "", errors.New("only the lead can reset the taskboard")
	}
	argsJSON, _ := json.Marshal(args)
	if _, err := a.authorizeAction(ctx, bot, runID, security.Tasks, action, string(argsJSON), ""); err != nil {
		return "", err
	}
	changed := false
	switch action {
	case "add":
		var texts []string
		switch v := args["tasks"].(type) {
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					texts = append(texts, s)
				}
			}
		case string:
			texts = strings.Split(v, "\n")
		}
		if s, ok := args["task"].(string); ok {
			texts = append(texts, s)
		}
		var clean []string
		for _, t := range texts {
			if t = strings.TrimSpace(t); t != "" {
				clean = append(clean, t)
			}
		}
		if len(clean) == 0 {
			return "", errors.New("tasks required")
		}
		items := a.boardItems(board)
		if len(items)+len(clean) > taskboardMax {
			return "", fmt.Errorf("the taskboard holds at most %d items; mark or reset first", taskboardMax)
		}
		next := 1
		if len(items) > 0 {
			next = items[len(items)-1].N + 1
		}
		for _, t := range clean {
			assignee := ""
			if m := assigneeRE.FindStringSubmatch(t); m != nil {
				assignee = m[1]
				t = strings.TrimSpace(t[len(m[0]):])
			}
			it := db.TaskItem{ID: ids.New(), ChatID: board, N: next, Text: clipRunes(t, taskTextMax), Assignee: assignee, CreatedBy: actor, CreatedAt: time.Now()}
			if err := a.DB.Create(&it).Error; err != nil {
				return "", err
			}
			next++
		}
		changed = true
	case "done":
		var ns []int
		switch v := args["ids"].(type) {
		case []any:
			for _, x := range v {
				switch n := x.(type) {
				case float64:
					ns = append(ns, int(n))
				case string:
					if i, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(n), "#")); err == nil {
						ns = append(ns, i)
					}
				}
			}
		}
		if n := num(args, "id"); n > 0 {
			ns = append(ns, n)
		}
		if len(ns) == 0 {
			return "", errors.New("ids required")
		}
		now := time.Now()
		note := clipRunes(stringArg(args, "note"), taskTextMax)
		res := a.DB.Model(&db.TaskItem{}).Where("chat_id = ? AND n IN ?", board, ns).
			Updates(map[string]any{"done": true, "done_by": actor, "note": note, "done_at": &now})
		if res.RowsAffected == 0 {
			return "", fmt.Errorf("no task numbered %v", ns)
		}
		changed = true
	case "reset":
		a.DB.Where("chat_id = ?", board).Delete(&db.TaskItem{})
		changed = true
	}
	if changed {
		a.pingLead(bot.ID, board, "board")
	}
	items := a.boardItems(board)
	if structured {
		type item struct {
			N        int    `json:"n"`
			Text     string `json:"text"`
			Assignee string `json:"assignee"`
			Done     bool   `json:"done"`
			DoneBy   string `json:"done_by"`
			Note     string `json:"note"`
		}
		out := struct {
			Items []item `json:"items"`
		}{Items: []item{}}
		for _, it := range items {
			out.Items = append(out.Items, item{it.N, it.Text, it.Assignee, it.Done, it.DoneBy, it.Note})
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	if len(items) == 0 {
		return "The taskboard is empty.", nil
	}
	return formatBoard(items), nil
}

func stringArg(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}

// --- prompt ---

// subagentSections is the per-run note that tells a subagent who it is.
func (a *App) subagentSections(pc promptContext) []promptSection {
	if pc.subagent == nil {
		return nil
	}
	body := strings.ReplaceAll(strings.TrimSpace(prompts.Subagent), "{{NAME}}", pc.subagent.Name)
	return []promptSection{{title: "This run", body: body, trailing: true}}
}

// --- RPC ---

func (a *App) protoSubagent(sa *db.Subagent) *v1.Subagent {
	out := &v1.Subagent{
		Id: sa.ID, BotId: sa.BotID, ParentChatId: sa.ParentChatID, ChatId: sa.ChatID, Name: sa.Name,
		Goal: sa.Goal, Context: sa.Context, Model: sa.Model, Status: sa.Status, Result: sa.Result,
		Running: a.subagentLive(sa), CreatedAt: sa.CreatedAt.Format(time.RFC3339), FinishedAt: rfc3339(sa.FinishedAt),
	}
	if out.Running {
		out.Status = "running"
		out.Activity = a.subagentActivity(sa)
	}
	var run db.Run
	a.DB.Select("id").Where("chat_id = ?", sa.ChatID).Order("created_at desc").Limit(1).Find(&run)
	out.RunId = run.ID
	return out
}

func (a *App) ownSubagent(ctx context.Context, botID, id string) (*db.Subagent, error) {
	return ownBotRow[db.Subagent](ctx, a, botID, id, "subagent")
}

func (a *App) ListSubagents(ctx context.Context, req *connect.Request[v1.ListSubagentsRequest]) (*connect.Response[v1.ListSubagentsResponse], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	out := &v1.ListSubagentsResponse{}
	rows := a.subagentsOf(req.Msg.GetChatId())
	for i := range rows {
		out.Subagents = append(out.Subagents, a.protoSubagent(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (a *App) GetSubagent(ctx context.Context, req *connect.Request[v1.GetSubagentRequest]) (*connect.Response[v1.Subagent], error) {
	sa, err := a.ownSubagent(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(a.protoSubagent(sa)), nil
}

// StopSubagent is the human stopping one subagent. The lead is told (woken if
// idle) so it can adjust its plan.
func (a *App) StopSubagent(ctx context.Context, req *connect.Request[v1.StopSubagentRequest]) (*connect.Response[v1.StopSubagentResponse], error) {
	sa, err := a.ownSubagent(ctx, req.Msg.GetBotId(), req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	a.stopChat(sa.BotID, sa.ChatID)
	return connect.NewResponse(&v1.StopSubagentResponse{}), nil
}

func (a *App) protoBoard(chatID string) *v1.Taskboard {
	out := &v1.Taskboard{ChatId: chatID}
	for _, it := range a.boardItems(chatID) {
		out.Items = append(out.Items, &v1.TaskItem{
			N: int32(it.N), Text: it.Text, Assignee: it.Assignee, Done: it.Done, DoneBy: it.DoneBy, Note: it.Note, CreatedBy: it.CreatedBy,
		})
	}
	return out
}

func (a *App) GetTaskboard(ctx context.Context, req *connect.Request[v1.GetTaskboardRequest]) (*connect.Response[v1.Taskboard], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(a.protoBoard(a.boardChat(req.Msg.GetChatId()))), nil
}

func (a *App) ClearTaskboard(ctx context.Context, req *connect.Request[v1.ClearTaskboardRequest]) (*connect.Response[v1.Taskboard], error) {
	if _, err := a.ownChat(ctx, req.Msg.GetBotId(), req.Msg.GetChatId()); err != nil {
		return nil, err
	}
	board := a.boardChat(req.Msg.GetChatId())
	a.DB.Where("chat_id = ?", board).Delete(&db.TaskItem{})
	a.pingLead(req.Msg.GetBotId(), board, "board")
	return connect.NewResponse(a.protoBoard(board)), nil
}
