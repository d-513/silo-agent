package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"silo.agent/internal/db"
	"silo.agent/internal/llm"
	"silo.agent/internal/prompts"
)

// Compaction replaces a conversation that no longer fits the model's context
// window with a model-written summary. It is recorded as one persisted
// `compaction` event (body = summary, tool = reason, Meta.after = the last
// event the summary covers); the thread keeps every original event and replay
// starts from the summary. A `compacting` event marks the start so the UI can
// show progress; the `compaction` or an `error` closes it.
const (
	compactingKind = "compacting"
	compactionKind = "compaction"

	compactAuto   = "auto"
	compactManual = "manual"
)

// Token estimation is a conservative runes/3.5 so the first turn of a run, which
// has no provider usage yet, compacts early rather than overflowing.
const (
	imageTokens = 1500
	// compactReserve is kept free for the summary itself when the transcript
	// is trimmed to fit the summarizing call.
	compactReserve = 8000
	// transcriptToolCap / transcriptArgsCap bound each tool result and tool
	// call in the summarizing transcript; the summary names files, it does not
	// need their bodies.
	transcriptToolCap = 2000
	transcriptArgsCap = 600
)

type compactionMeta struct {
	After string `json:"after,omitempty"`
}

func runeTokens(s string) int {
	return (utf8.RuneCountInString(s)*2 + 6) / 7
}

// estimateMessages is the approximate token size of msgs.
func estimateMessages(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += 4 + runeTokens(m.Text) + imageTokens*len(m.Images)
		for _, tc := range m.ToolCalls {
			n += 4 + runeTokens(tc.Name) + runeTokens(tc.Arguments)
		}
	}
	return n
}

// estimateBase is the approximate token size of the system prompt and tools.
func estimateBase(system []llm.SystemBlock, tools []llm.Tool) int {
	n := 0
	for _, b := range system {
		n += runeTokens(b.Text)
	}
	for _, t := range tools {
		n += runeTokens(t.Name) + runeTokens(t.Description) + runeTokens(string(t.Parameters))
	}
	return n
}

// isContextOverflow reports whether a provider error says the request was too
// large for the model's context window. Providers word it differently.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, p := range []string{
		"context length", "context_length", "context window", "maximum context",
		"prompt is too long", "too many tokens", "input is too long", "reduce the length",
		"context_length_exceeded", "exceeds the context",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// contextWindow is the model's context size in tokens: an operator per-model
// override, then the provider's report (OpenRouter /models), then the operator
// fallback.
func (a *App) contextWindow(ctx context.Context, modelID string) int {
	cfg := a.cfg().Context
	if n := cfg.WindowFor(modelID); n > 0 {
		return n
	}
	if client, _, model, err := a.providerClient(modelID); err == nil {
		if cw, ok := client.(llm.ContextWindower); ok {
			if n, err := cw.ContextWindow(ctx, model); err == nil && n > 0 {
				return n
			}
		}
	}
	return cfg.FallbackWindow()
}

// needsCompaction decides whether the next turn should compact first. used is
// the expected request size; base is the part compaction cannot shrink (system
// prompt + tools). A history under a tenth of the window is left alone even
// when the base alone crosses the threshold, so a huge prompt does not compact
// every turn.
func needsCompaction(used, base, window int, threshold float64) bool {
	if window <= 0 {
		return false
	}
	return float64(used) >= threshold*float64(window) && used-base > window/10
}

// compactionText is the user turn a summary replays as.
func compactionText(summary string) string {
	return "[The earlier conversation was compacted to fit the context window. Summary of everything before this point:]\n\n" + strings.TrimSpace(summary)
}

// compactionContinue follows a summary made in the middle of a run so the model
// resumes the task instead of greeting.
const compactionContinue = "\n\n[Context was compacted mid-task. Continue from where you left off.]"

// lastEventID is the newest persisted event of a chat, the anchor a
// compaction summarizes through. Seq is a global insert order.
func (a *App) lastEventID(chatID string) string {
	var ev db.RunEvent
	a.DB.Joins("JOIN runs ON runs.id = run_events.run_id").
		Where("runs.chat_id = ?", chatID).
		Order("run_events.seq DESC").Limit(1).Find(&ev)
	return ev.ID
}

// transcript flattens msgs into one plain-text document for the summarizing
// call. A flat text never breaks tool-call pairing and trims from the front.
// A previous summary is always kept; the oldest entries are dropped until the
// transcript fits budget tokens.
func transcript(msgs []llm.Message, budget int) string {
	entries := make([]string, 0, len(msgs))
	keepFirst := false
	for i, m := range msgs {
		var b strings.Builder
		switch m.Role {
		case llm.RoleUser:
			if i == 0 && strings.HasPrefix(m.Text, "[The earlier conversation was compacted") {
				keepFirst = true
				b.WriteString("PREVIOUS SUMMARY:\n")
			} else {
				b.WriteString("USER:\n")
			}
			b.WriteString(m.Text)
			if len(m.Images) > 0 {
				fmt.Fprintf(&b, "\n[%d image(s)]", len(m.Images))
			}
		case llm.RoleAssistant:
			if strings.TrimSpace(m.Text) != "" {
				b.WriteString("ASSISTANT:\n")
				b.WriteString(m.Text)
			}
			for _, tc := range m.ToolCalls {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				args := tc.Arguments
				if utf8.RuneCountInString(args) > transcriptArgsCap {
					args = truncateUTF8(args, transcriptArgsCap) + "…"
				}
				fmt.Fprintf(&b, "TOOL CALL %s %s", tc.Name, args)
			}
		case llm.RoleTool:
			out := m.Text
			if utf8.RuneCountInString(out) > transcriptToolCap {
				out = truncateUTF8(out, transcriptToolCap) + "\n…truncated"
			}
			b.WriteString("TOOL RESULT:\n")
			b.WriteString(out)
		}
		if b.Len() > 0 {
			entries = append(entries, b.String())
		}
	}
	total := 0
	for _, e := range entries {
		total += runeTokens(e) + 1
	}
	start := 0
	if keepFirst {
		start = 1
	}
	dropped := 0
	for total > budget && start < len(entries)-1 {
		total -= runeTokens(entries[start]) + 1
		entries = append(entries[:start], entries[start+1:]...)
		dropped++
	}
	out := strings.Join(entries, "\n\n")
	if dropped > 0 {
		out = fmt.Sprintf("[%d older entries were dropped to fit.]\n\n", dropped) + out
	}
	return out
}

// compact summarizes msgs with the conversation's model and records the
// compaction. It returns the replacement history: one user turn holding the
// summary. The caller appends a continue note when a task is mid-flight.
func (a *App) compact(ctx context.Context, botID, chatID, runID, reason, modelID string, msgs []llm.Message, window int) ([]llm.Message, error) {
	if len(msgs) == 0 {
		return nil, errors.New("nothing to compact")
	}
	after := a.lastEventID(chatID)
	a.emit(botID, chatID, runID, compactingKind, "", reason)
	client, _, model, err := a.modelClient(modelID, botID, "compact")
	if err != nil {
		return nil, err
	}
	budget := window - compactReserve - runeTokens(prompts.Compact)
	if budget < window/4 {
		budget = window / 4
	}
	res, err := client.Complete(ctx, llm.Request{
		Model:    model,
		System:   []llm.SystemBlock{{Text: prompts.Compact}},
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "Conversation transcript to summarize:\n\n" + transcript(msgs, budget)}},
	})
	if err != nil {
		return nil, fmt.Errorf("compaction failed: %w", err)
	}
	summary := strings.TrimSpace(res.Text)
	if summary == "" {
		return nil, errors.New("compaction failed: the model returned an empty summary")
	}
	meta, _ := json.Marshal(compactionMeta{After: after})
	a.emitMeta(botID, chatID, runID, compactionKind, summary, reason, string(meta))
	out := []llm.Message{{Role: llm.RoleUser, Text: compactionText(summary)}}
	a.emitUsage(botID, chatID, runID, llm.Usage{InputTokens: estimateMessages(out)}, window)
	return out, nil
}

// cutAtCompaction returns where replay starts and the summary to open with.
// The last compaction wins; replay resumes after its anchor, so a message
// injected while the summary was being written is not lost.
func cutAtCompaction(evs []db.RunEvent) (start int, summary string, ok bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind != compactionKind {
			continue
		}
		var meta compactionMeta
		_ = json.Unmarshal([]byte(evs[i].Meta), &meta)
		start = i + 1
		if meta.After != "" {
			for j := 0; j < i; j++ {
				if evs[j].ID == meta.After {
					start = j + 1
					break
				}
			}
		}
		return start, evs[i].Body, true
	}
	return 0, "", false
}
