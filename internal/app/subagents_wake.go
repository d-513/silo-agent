package app

import (
	"fmt"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/textx"
) // scheduleWake debounces a wake of the lead chat so subagents finishing
// together, and Bots replying together, land in one report.
func (a *App) scheduleWake(botID, leadChatID string) {
	if a.unreadResults(leadChatID) == 0 {
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

// unreadResults counts what a chat has handed off and not read: finished
// subagents and Bots' replies.
func (a *App) unreadResults(chatID string) int64 {
	var n, m int64
	a.DB.Model(&db.Subagent{}).Where("parent_chat_id = ? AND reported = ? AND status <> ?", chatID, false, "running").Count(&n)
	a.DB.Model(&db.BotMessage{}).Where("from_chat_id = ? AND reported = ? AND status <> ?", chatID, false, "running").Count(&m)
	return n + m
}

// wakeLead starts a lead run with every finished, unseen subagent result and
// Bot reply. A lead that is running inspects on its own; an automation log is
// never woken.
func (a *App) wakeLead(botID, leadChatID string) {
	defer a.lockConv(leadChatID)()
	if a.LiveRunID(botID, leadChatID) != "" {
		return
	}
	var chat db.Chat
	a.DB.Where("id = ?", leadChatID).Limit(1).Find(&chat)
	if chat.ID == "" || chat.AutomationID != "" || chat.SubagentID != "" {
		return
	}
	var done []db.Subagent
	a.DB.Where("parent_chat_id = ? AND reported = ? AND status <> ?", leadChatID, false, "running").Order("finished_at").Find(&done)
	replies := a.unreadReplies(leadChatID)
	if len(done) == 0 && len(replies) == 0 {
		return
	}
	ids := make([]string, 0, len(done))
	for _, sa := range done {
		ids = append(ids, sa.ID)
	}
	msgIDs := make([]string, 0, len(replies))
	for _, m := range replies {
		msgIDs = append(msgIDs, m.ID)
	}
	a.DB.Model(&db.Subagent{}).Where("id IN ?", ids).Update("reported", true)
	a.DB.Model(&db.BotMessage{}).Where("id IN ?", msgIDs).Update("reported", true)
	rep := a.buildReport(leadChatID, done, replies)
	var origin *run.Origin
	switch {
	case chat.ChannelID != "":
		var ch db.Channel
		if a.DB.Where("id = ?", chat.ChannelID).Limit(1).Find(&ch); ch.ID != "" {
			origin = a.Channels.Origin(&ch, chat.ExternalID)
		}
	case chat.PeerBotID != "":
		// A conversation answering another Bot goes on answering it.
		origin = &run.Origin{Peer: a.incomingMessage(leadChatID)}
	case chat.RoomID != "":
		// A Bot woken in its seat answers the room.
		origin = a.Rooms.Origin(leadChatID)
	}
	if _, err := a.StartRun(run.Request{BotID: botID, ChatID: leadChatID, Origin: origin, Report: rep}); err != nil {
		a.DB.Model(&db.Subagent{}).Where("id IN ?", ids).Update("reported", false)
		a.DB.Model(&db.BotMessage{}).Where("id IN ?", msgIDs).Update("reported", false)
	}
}

func (a *App) buildReport(leadChatID string, done []db.Subagent, replies []db.BotMessage) *run.Report {
	var b strings.Builder
	labels := make([]string, 0, len(done)+len(replies))
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
	for i := range replies {
		// A Bot's entry in the label is marked, so the report reads as
		// work handed off rather than subagents (botsReported).
		labels = append(labels, "@"+a.botName(replies[i].ToBotID)+":"+replies[i].Status)
		b.WriteString(a.botReplySection(&replies[i]) + "\n")
	}
	var running []string
	for _, sa := range a.subagentsOf(leadChatID) {
		if a.subagentLive(&sa) {
			running = append(running, sa.Name)
		}
	}
	switch {
	case len(running) > 0:
		fmt.Fprintf(&b, "Still running: %s.\n", strings.Join(running, ", "))
	case len(done) > 0:
		b.WriteString("No subagents are running.\n")
	}
	var waiting []db.BotMessage
	a.DB.Where("from_chat_id = ? AND status = ?", leadChatID, "running").Find(&waiting)
	if len(waiting) > 0 {
		var names []string
		for _, m := range waiting {
			names = append(names, a.botName(m.ToBotID))
		}
		fmt.Fprintf(&b, "Still waiting on: %s.\n", strings.Join(names, ", "))
	}
	return &run.Report{Body: strings.TrimSpace(b.String()), Label: strings.Join(labels, ",")}
}

func (a *App) emitReport(botID, chatID, runID string, rep *run.Report) {
	body := textx.ValidUTF8(a.Mask(botID).Apply(rep.Body))
	id := ids.New()
	now := time.Now()
	a.DB.Create(&db.RunEvent{ID: id, RunID: runID, Kind: subagentReportKind, Body: body, Tool: rep.Label, Zone: a.botZone(botID), CreatedAt: now})
	a.Bus.Publish(botID, &v1.RunEvent{Id: id, RunId: runID, ChatId: chatID, Kind: subagentReportKind, Body: body, Tool: rep.Label, CreatedAt: now.Format(time.RFC3339)})
}

// subagentReportText frames a report as the user turn the lead replays. label
// is the event's Tool: a report that carries a Bot's reply says so, and one
// with only subagents keeps the words it was first read with.
func subagentReportText(body, label string) string {
	if botsReported(label) {
		return "[Automatic report — work you handed off came back. This is not a message from the human.]\n\n" + body +
			"\n\nRead the results; a Bot's reply is its own words, so check what it claims. Then continue: hand off follow-up work, or report to the human if the job is done."
	}
	return "[Automatic report — your subagents finished. This is not a message from the human.]\n\n" + body +
		"\n\nRead the results, update the taskboard, and continue: start follow-up work, or report to the human if the job is done."
}

// botsReported reports whether a report's label names a Bot's reply.
func botsReported(label string) bool {
	return strings.HasPrefix(label, "@") || strings.Contains(label, ",@")
}

// reportTitle is what a wake run is listed by.
func reportTitle(rep *run.Report) string {
	if botsReported(rep.Label) {
		return "Handed-off work came back"
	}
	return "Subagents reported"
}
