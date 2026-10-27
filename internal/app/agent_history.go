package app

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"silo.agent/internal/app/quote"
	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/zone"
)

// messageTimeStamp renders t as a compact marker the model can read, in the
// zone the Bot was in when the message was written (zoneName, kept on its
// event). The date, time and zone ride on the user message rather than the
// system prompt, so the cached prompt prefix is never invalidated and a Bot
// that moves zone does not rewrite what it already sent.
func messageTimeStamp(t time.Time, zoneName string) string {
	if zoneName == "" {
		// A message from before Bots had a zone: the bytes it always had.
		return "[" + t.Local().Format("Mon, 2006-01-02 15:04:05 MST") + "] "
	}
	return "[" + zone.Stamp(t, zone.Resolve(zoneName)) + "] "
}

// stampUserText prefixes a user message with the moment it was sent. History
// keeps its original timestamp and zone (stable across runs, so prompt caches
// hold) and the newest message carries the current time where the human is.
func stampUserText(text string, t time.Time, zoneName string) string {
	return messageTimeStamp(t, zoneName) + text
}

// stampAuthored is stampUserText with the writer's name on the stamp, for a
// message of a Bot with several people ("[bob@… · Sun, …] "). A message
// with no author (a solo Bot's) keeps the bytes it always had.
func stampAuthored(text string, t time.Time, zoneName, author string) string {
	if author == "" {
		return stampUserText(text, t, zoneName)
	}
	return "[" + author + " · " + strings.TrimPrefix(messageTimeStamp(t, zoneName), "[") + text
}

func (a *App) historyFromDB(chatID string) []llm.Message {
	return a.contextFromDB(chatID).msgs
}

// contextFromDB is the conversation as a run continues it.
func (a *App) contextFromDB(chatID string) replayed {
	var runs []db.Run
	a.DB.Where("chat_id = ?", chatID).Order("created_at").Find(&runs)
	return a.contextFromRuns(runs)
}

// historyFromRuns replays the given runs, in order, as model messages.
func (a *App) historyFromRuns(runs []db.Run) []llm.Message {
	return a.contextFromRuns(runs).msgs
}

// contextFromRuns replays runs exactly as the runs that wrote them saw the
// conversation, cleared tool results included.
func (a *App) contextFromRuns(runs []db.Run) replayed {
	all := a.replayRows(runs)
	r := replayEvents(all)
	clearResults(r.msgs, r.cleared)
	return r
}

// streamedKinds are the live-only events: a turn's text, reasoning and
// command output as they streamed, and its token counts. Replay reads none of
// them, and they are most of a long conversation's rows.
var streamedKinds = []string{"chunk", "thinking_chunk", "thinking", "tool_chunk", "usage"}

// replayRows loads the events replay reads for runs, in one query, ordered by
// run (as given) and then by seq.
func (a *App) replayRows(runs []db.Run) []db.RunEvent {
	if len(runs) == 0 {
		return nil
	}
	order := make(map[string]int, len(runs))
	runIDs := make([]string, len(runs))
	for i, r := range runs {
		order[r.ID] = i
		runIDs[i] = r.ID
	}
	var evs []db.RunEvent
	a.DB.Where("run_id IN ? AND kind NOT IN ?", runIDs, streamedKinds).Order("seq").Find(&evs)
	sort.SliceStable(evs, func(i, j int) bool { return order[evs[i].RunID] < order[evs[j].RunID] })
	a.reanchorCompactions(evs)
	return evs
}

// reanchorCompactions points a compaction whose anchor is a streamed event
// (not loaded) at the newest loaded event before it, so replay still resumes
// right after what the summary covers.
func (a *App) reanchorCompactions(evs []db.RunEvent) {
	have := make(map[string]bool, len(evs))
	for _, ev := range evs {
		have[ev.ID] = true
	}
	for i := range evs {
		if evs[i].Kind != compactionKind {
			continue
		}
		var meta compactionMeta
		if json.Unmarshal([]byte(evs[i].Meta), &meta) != nil || meta.After == "" || have[meta.After] {
			continue
		}
		var anchor db.RunEvent
		if a.DB.Select("seq").Where("id = ?", meta.After).Limit(1).Find(&anchor).RowsAffected == 0 {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if evs[j].Seq <= anchor.Seq {
				meta.After = evs[j].ID
				raw, _ := json.Marshal(meta)
				evs[i].Meta = string(raw)
				break
			}
		}
	}
}

// The run log holds a few events only replay reads. They are what makes a
// replayed conversation identical, byte for byte, to the requests the live run
// sent — the property a provider's prompt cache needs — and they never reach a
// viewer (StreamRun skips them).
const (
	// turnKind is one assistant turn that called tools: Body is what the
	// model said, Meta its calls with the provider's ids.
	turnKind = "turn"
	// seenKind stands for the pixels a tool result came with: Body is the
	// text the turn is left with once the image is no longer sent.
	seenKind = "seen"
	// noteKind is a status note (taskboard, subagents) the model was told.
	noteKind = "note"
	// clearedKind records that old tool results were cleared from the
	// context: Meta.results is how many, counted from the start of the
	// replayed conversation.
	clearedKind = "cleared"
)

var internalKinds = []string{turnKind, seenKind, noteKind, clearedKind}

type turnMeta struct {
	Calls []turnCall `json:"calls"`
}

type turnCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

type clearedMeta struct {
	Results int `json:"results"`
}

// replayed is a conversation rebuilt from its events.
type replayed struct {
	msgs []llm.Message
	// note is the last status note in it, so a run only tells a changed one.
	note string
	// cleared is how many of its tool results (oldest first) were cleared.
	cleared int
}

// historyFromEvents turns a conversation's events (runs in order, each run's
// events by seq) into model messages. After a compaction, replay opens with
// the summary and resumes after the compaction's anchor.
func historyFromEvents(evs []db.RunEvent) []llm.Message {
	return replayEvents(evs).msgs
}

// replayEvents rebuilds what the model was shown, in the order it was shown.
// Nothing here may depend on the time of the replay: the same events must
// always give the same messages.
func replayEvents(evs []db.RunEvent) replayed {
	var out replayed
	msgs := out.msgs
	// fold is the index of a framed user turn (a quoted Feed post, a
	// compaction summary, a subagent report) that the user message right
	// after it is merged into, so roles still alternate. Only that one
	// message: the run that reads a second one appends it as a turn of its
	// own, and replay must give the same messages.
	fold := -1
	// A /clear starts the context over: nothing before it is read, not even
	// a compaction's summary.
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == contextClearedKind {
			evs = evs[i+1:]
			break
		}
	}
	if cut, ok := cutAtCompaction(evs); ok {
		text := compactionText(cut.summary)
		if cut.midTask {
			text += compactionContinue
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
		fold = 0
		evs = evs[cut.start:]
	}
	// pending is one model turn's calls and said what it wrote beside them;
	// after is what followed, in order: a result per call (a turn may call
	// several tools at once) and the note left by any pixels a result had.
	var pending []llm.ToolCall
	var said string
	var after, seen []llm.Message
	answered := 0
	flushTools := func() {
		if len(pending) > 0 {
			for ; answered < len(pending); answered++ {
				id := pending[answered].ID
				if id == "" {
					id = "call_missing"
				}
				after = append(after, llm.Message{Role: llm.RoleTool, ToolCallID: id, Text: "error: interrupted"})
			}
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: said, ToolCalls: pending})
			msgs = append(msgs, after...)
			// Notes left by pixels follow the turn's last result, as the run
			// appended them: nothing may sit between two results.
			msgs = append(msgs, seen...)
		}
		pending, said, after, seen, answered = nil, "", nil, nil, 0
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
		case clearedKind:
			var meta clearedMeta
			_ = json.Unmarshal([]byte(ev.Meta), &meta)
			out.cleared = max(out.cleared, meta.Results)
		case quote.Kind:
			flushTools()
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: quote.Text(&ev)})
			fold = len(msgs) - 1
			continue
		case subagentReportKind:
			flushTools()
			text := stampUserText(subagentReportText(ev.Body), ev.CreatedAt, ev.Zone)
			if n := len(msgs); n > 0 && msgs[n-1].Role == llm.RoleUser {
				msgs[n-1].Text += "\n\n" + text
			} else {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
			}
			fold = len(msgs) - 1
			continue
		case "user":
			flushTools()
			text := stampAuthored(userTurnText(ev.Body, decodeUserMeta(ev.Meta)), ev.CreatedAt, ev.Zone, ev.Author)
			if n := len(msgs); n > 0 && fold == n-1 && msgs[n-1].Role == llm.RoleUser {
				msgs[n-1].Text += "\n\n" + text
				fold = -1
			} else {
				msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: text})
			}
		case noteKind:
			flushTools()
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Text: ev.Body})
			out.note = ev.Body
		case "assistant", "section":
			flushTools()
			if strings.TrimSpace(ev.Body) != "" {
				msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Text: ev.Body})
			}
		case "tool":
			// A call after results is the model's next turn.
			if answered > 0 || len(after) > 0 || len(seen) > 0 {
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
		case turnKind:
			// The turn as the run kept it replaces what the streamed call
			// events pieced together: it has the provider's ids and the text.
			var meta turnMeta
			if json.Unmarshal([]byte(ev.Meta), &meta) != nil || len(meta.Calls) == 0 {
				continue
			}
			if answered > 0 || len(after) > 0 || len(seen) > 0 {
				flushTools()
			}
			pending = make([]llm.ToolCall, 0, len(meta.Calls))
			for _, c := range meta.Calls {
				pending = append(pending, llm.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Args})
			}
			said = ev.Body
		case "tool_result":
			if answered >= len(pending) {
				continue // answers no call
			}
			after = append(after, llm.Message{Role: llm.RoleTool, ToolCallID: pending[answered].ID, Text: ev.Body})
			answered++
		case seenKind:
			m := llm.Message{Role: llm.RoleUser, Text: ev.Body}
			if len(pending) > 0 {
				seen = append(seen, m)
			} else {
				msgs = append(msgs, m)
			}
		}
	}
	flushTools()
	out.msgs = msgs
	return out
}
