package app

import (
	"fmt"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
) // scheduleWake debounces a wake of the lead chat so subagents finishing
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
			res = textx.TruncateUTF8(res, subagentReportMax) + "\n…truncated — agent_status " + sa.Name + " for the rest"
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
	body := textx.ValidUTF8(a.Mask(botID).Apply(rep.body))
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
