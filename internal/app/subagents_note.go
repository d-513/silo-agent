package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"silo.agent/internal/app/run"
	"silo.agent/internal/db"
	"silo.agent/internal/textx"
) // --- per-turn note ---

// turnNote is the taskboard (and, for a lead, the state of its subagents) as
// the model should now know it. Empty when there is nothing to show. The run
// tells it only when it differs from the last one told, and it then stays in
// the history, so it must not change with the clock: no durations, no "latest
// step" — a lead that wants those calls agent_status.
func (a *App) turnNote(chatID string, origin *run.Origin) string {
	if a.DB == nil || chatID == "" {
		return ""
	}
	sub := origin != nil && origin.Subagent != nil
	board := chatID
	if sub {
		board = origin.Subagent.ParentChatID
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
				b.WriteString(a.subagentState(&rows[i]) + "\n")
			}
			b.WriteString("</subagents>")
			parts = append(parts, b.String())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "[Status update — not from the human]\n" + strings.Join(parts, "\n")
}

// subagentState is one subagent's state for the status note: only what
// changes when something happened to it.
func (a *App) subagentState(sa *db.Subagent) string {
	if a.subagentLive(sa) {
		return "- " + sa.Name + ": running"
	}
	line := "- " + sa.Name + ": " + sa.Status
	if !sa.Reported {
		line += " — result unread (agent_status " + sa.Name + ")"
	}
	return line
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
	return textx.CapRunes(strings.Join(strings.Fields(s), " "), n)
}
