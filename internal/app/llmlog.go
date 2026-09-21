package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
)

// llmlogLimit caps the debug table. The oldest rows are dropped past it so a
// long debug session cannot grow the database without bound.
const llmlogLimit = 500

// llmlogTextMax caps one stored request or response so a giant prompt cannot
// bloat a row. Raw is preserved up to this many runes.
const llmlogTextMax = 200_000

// recordLLM persists one observed model call when the operator turned debug on.
// It runs synchronously on the caller's goroutine; the insert is small and
// SQLite handles concurrent writers with the configured busy timeout.
func (a *App) recordLLM(botID, label string, rec llm.Record) {
	if a.DB == nil || !a.cfg().Debug {
		return
	}
	row := db.LLMLog{
		ID:         ids.New(),
		At:         time.Now(),
		BotID:      botID,
		Label:      label,
		Provider:   rec.Provider,
		Model:      rec.Model,
		Request:    capLogText(formatLLMRequest(rec)),
		Response:   capLogText(formatLLMResponse(rec)),
		InputTok:   rec.Usage.InputTokens,
		OutputTok:  rec.Usage.OutputTokens,
		DurationMs: rec.Duration.Milliseconds(),
	}
	if rec.Error != nil {
		row.Error = rec.Error.Error()
	}
	if err := a.DB.Create(&row).Error; err != nil {
		log.Printf("llm log: %v", err)
		return
	}
	a.pruneLLMLogs()
}

func (a *App) pruneLLMLogs() {
	var n int64
	a.DB.Model(&db.LLMLog{}).Count(&n)
	if n <= llmlogLimit {
		return
	}
	var oldest []db.LLMLog
	a.DB.Order("at").Limit(int(n - llmlogLimit)).Find(&oldest)
	idsToDrop := make([]string, 0, len(oldest))
	for _, r := range oldest {
		idsToDrop = append(idsToDrop, r.ID)
	}
	if len(idsToDrop) > 0 {
		a.DB.Where("id IN ?", idsToDrop).Delete(&db.LLMLog{})
	}
}

func capLogText(s string) string {
	if len(s) <= llmlogTextMax {
		return s
	}
	return truncateUTF8(s, llmlogTextMax) + "\n…truncated"
}

func (a *App) ListLLMLogs(ctx context.Context, req *connect.Request[v1.ListLLMLogsRequest]) (*connect.Response[v1.ListLLMLogsResponse], error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	out := &v1.ListLLMLogsResponse{Enabled: a.cfg().Debug}
	if !out.Enabled {
		return connect.NewResponse(out), nil
	}
	q := a.DB.Order("at desc").Limit(200)
	if botID := strings.TrimSpace(req.Msg.GetBotId()); botID != "" {
		q = q.Where("bot_id = ?", botID)
	}
	var rows []db.LLMLog
	q.Find(&rows)
	for _, r := range rows {
		out.Logs = append(out.Logs, &v1.LLMLog{
			Id: r.ID, At: r.At.Format(time.RFC3339), BotId: r.BotID, Label: r.Label,
			Provider: r.Provider, Model: r.Model, Request: r.Request, Response: r.Response,
			Error: r.Error, InputTokens: int32(r.InputTok), OutputTokens: int32(r.OutputTok),
			DurationMs: r.DurationMs,
		})
	}
	return connect.NewResponse(out), nil
}

// formatLLMRequest renders the full outbound request as plain text. The order
// mirrors the model-visible prompt: providers render tools first, then the
// system prompt, then the conversation (Anthropic and OpenAI both inject tool
// definitions ahead of the system/developer instructions, and a cache
// breakpoint on the system prompt caches tools together with it).
func formatLLMRequest(rec llm.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s/%s\n", rec.Provider, rec.Model)
	if len(rec.Tools) > 0 {
		fmt.Fprintf(&b, "\n## TOOLS (%d)\n", len(rec.Tools))
		for _, t := range rec.Tools {
			fmt.Fprintf(&b, "- %s: %s\n", t.Name, strings.TrimSpace(t.Description))
		}
	}
	if sys := joinSystemBlocks(rec.System); strings.TrimSpace(sys) != "" {
		b.WriteString("\n## SYSTEM\n")
		b.WriteString(sys)
		b.WriteString("\n")
	}
	if len(rec.Messages) > 0 {
		b.WriteString("\n## MESSAGES\n")
		for _, m := range rec.Messages {
			b.WriteString(formatMessage(m))
		}
	}
	return tidyText(b.String())
}

func joinSystemBlocks(blocks []llm.SystemBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
}

func formatMessage(m llm.Message) string {
	var b strings.Builder
	role := string(m.Role)
	if m.ToolCallID != "" {
		fmt.Fprintf(&b, "\n[%s %s]\n", role, m.ToolCallID)
	} else {
		fmt.Fprintf(&b, "\n[%s]\n", role)
	}
	if t := strings.TrimSpace(m.Text); t != "" {
		b.WriteString(t)
		b.WriteString("\n")
	}
	if n := len(m.Images); n > 0 {
		fmt.Fprintf(&b, "(%d image%s)\n", n, plural(n))
	}
	for _, tc := range m.ToolCalls {
		fmt.Fprintf(&b, "→ %s %s\n", tc.Name, strings.TrimSpace(tc.Arguments))
	}
	return b.String()
}

// formatLLMResponse renders the assembled response plus usage as plain text.
// The response is the assistant turn, so it carries the same role header the
// request uses — the model only ever sees structured roles, never this text.
func formatLLMResponse(rec llm.Record) string {
	var b strings.Builder
	if rec.Text != "" {
		b.WriteString("[assistant]\n")
		b.WriteString(rec.Text)
		b.WriteString("\n")
	}
	if rec.Reasoning != "" {
		b.WriteString("\n## REASONING\n")
		b.WriteString(rec.Reasoning)
		b.WriteString("\n")
	}
	if len(rec.ToolCalls) > 0 {
		b.WriteString("\n## TOOL CALLS\n")
		for _, tc := range rec.ToolCalls {
			fmt.Fprintf(&b, "→ %s %s\n", tc.Name, strings.TrimSpace(tc.Arguments))
		}
	}
	u := rec.Usage
	if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
		fmt.Fprintf(&b, "\n[tokens in=%d out=%d cache_read=%d cache_write=%d]\n",
			u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
	}
	if rec.Duration > 0 {
		fmt.Fprintf(&b, "[%dms]\n", rec.Duration.Milliseconds())
	}
	if s := tidyText(b.String()); s != "" {
		return s
	}
	return "(empty)"
}

// tidyText trims trailing spaces and collapses runs of blank lines so the raw
// view stays readable instead of a wall of newlines.
func tidyText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
