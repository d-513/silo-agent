package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
) // --- per-turn note ---

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
