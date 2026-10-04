package app

import (
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/feed"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
)

// messageTimeStamp renders t in the machine's local timezone as a compact
// marker the model can read. The date and time ride on the user message rather
// than the system prompt so the cached prompt prefix is never invalidated.
func messageTimeStamp(t time.Time) string {
	return "[" + t.Local().Format("Mon, 2006-01-02 15:04:05 MST") + "] "
}

// stampUserText prefixes a user message with the moment it was sent. History
// keeps its original timestamp (stable across runs, so prompt caches hold) and
// the newest message carries the current local time.
func stampUserText(text string, t time.Time) string {
	return messageTimeStamp(t) + text
}

func userTextWithAttachments(text string, atts []*v1.Attachment) string {
	if len(atts) == 0 {
		return text
	}
	paths := make([]string, 0, len(atts))
	for _, at := range atts {
		paths = append(paths, at.GetPath())
	}
	return text + "\n\n[Attached files in the workspace: " + strings.Join(paths, ", ") + "]"
}

func (a *App) historyFromDB(chatID string) []llm.Message {
	var runs []db.Run
	a.DB.Where("chat_id = ?", chatID).Order("created_at").Find(&runs)
	return a.historyFromRuns(runs)
}

// historyFromRuns replays the given runs, in order, as model messages.
func (a *App) historyFromRuns(runs []db.Run) []llm.Message {
	var all []db.RunEvent
	for _, run := range runs {
		var evs []db.RunEvent
		a.DB.Where("run_id = ?", run.ID).Order("seq").Find(&evs)
		all = append(all, evs...)
	}
	return historyFromEvents(all)
}

// historyFromEvents turns a conversation's events (runs in order, each run's
// events by seq) into model messages. After a compaction, replay opens with
// the summary and resumes after the compaction's anchor.
func historyFromEvents(evs []db.RunEvent) []llm.Message {
	var msgs []llm.Message
	// fold merges the next user message into a preceding framed user turn (a
	// quoted Feed post or a compaction summary) so roles still alternate.
	fold := false
	if start, summary, ok := cutAtCompaction(evs); ok {
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: compactionText(summary)})
		fold = true
		evs = evs[start:]
	}
	// pending is one model turn's calls; results answer them in call order,
	// which is the order the loop runs and records them (a turn may call
	// several tools at once).
	var pending []llm.ToolCall
	var results []llm.Message
	flushTools := func() {
		if len(pending) == 0 {
			results = nil
			return
		}
		for len(results) < len(pending) {
			id := pending[len(results)].ID
			if id == "" {
				id = "call_missing"
			}
			results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: "error: interrupted"})
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, ToolCalls: pending})
		msgs = append(msgs, results...)
		pending = nil
		results = nil
	}
	runID := ""
	for _, ev := range evs {
		if ev.RunID != runID {
			// Tool calls never pair across runs.
			flushTools()
			runID = ev.RunID
		}
		switch ev.Kind {
		case compactionKind, compactingKind:
			continue
		case feed.QuoteKind:
			flushTools()
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: feed.QuoteText(ev.Tool, ev.Body, ev.CreatedAt)})
			fold = true
			continue
		case subagentReportKind:
			flushTools()
			text := stampUserText(subagentReportText(ev.Body), ev.CreatedAt)
			if n := len(msgs); n > 0 && msgs[n-1].Role == llm.RoleUser {
				msgs[n-1].Text += "\n\n" + text
			} else {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
			}
			fold = true
			continue
		case "user":
			flushTools()
			text := ev.Body
			if atts := attachmentsFromMeta(ev.Meta); len(atts) > 0 {
				paths := make([]string, 0, len(atts))
				for _, at := range atts {
					paths = append(paths, at.Path)
				}
				text += "\n\n[Attached files in the workspace: " + strings.Join(paths, ", ") + "]"
			}
			text = stampUserText(text, ev.CreatedAt)
			if n := len(msgs); fold && n > 0 && msgs[n-1].Role == llm.RoleUser {
				msgs[n-1].Text += "\n\n" + text
			} else {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
			}
		case "assistant", "section":
			flushTools()
			if strings.TrimSpace(ev.Body) != "" {
				msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: ev.Body})
			}
		case "tool":
			// A call after results is the model's next turn.
			if len(results) > 0 {
				flushTools()
			}
			// Older runs could record a call twice, the second time with its
			// full arguments. A live call is always recorded with an empty
			// body, so an empty one is a new (possibly parallel) call.
			if n := len(pending); n > 0 && ev.Body != "" {
				last := &pending[n-1]
				same := last.Name == ev.Tool || last.Name == "" || ev.Tool == ""
				incomplete := last.Arguments == "" || !jsonLooksComplete(last.Arguments)
				if same && (incomplete || ev.Body == last.Arguments) {
					last.Arguments = ev.Body
					if ev.Tool != "" {
						last.Name = ev.Tool
					}
					continue
				}
			}
			pending = append(pending, llm.ToolCall{ID: "call_" + ev.ID, Name: ev.Tool, Arguments: ev.Body})
		case "tool_args_chunk":
			if n := len(pending); n > 0 {
				pending[n-1].Arguments += ev.Body
			}
		case "tool_result":
			if len(results) >= len(pending) {
				continue // answers no call
			}
			results = append(results, llm.Message{Role: llm.RoleTool, ToolCallID: pending[len(results)].ID, Text: ev.Body})
		}
	}
	flushTools()
	return msgs
}
