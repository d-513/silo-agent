package models

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"silo.agent/internal/llm"
)

// Call is one model call as it is kept for the Bot's managers: what it was
// for and what it cost, never what was said. A call made for a run is kept in
// that run's log; any other is a line in memory (see package runlog).
type Call struct {
	BotID string
	// RunID is the run the call was made for (InRun), "" for none.
	RunID string
	// Turn marks a run's own model turn (AsTurn), which the run log already
	// holds as its events; everything else is a call beside the turns.
	Turn bool
	// Label is what the call was for: title, compact, approval, filter,
	// decide, memory, voice-talk, embed, speech, dictation, …
	Label    string
	Provider string
	Model    string
	In, Out  int
	Duration time.Duration
	// Verdict is a judge's one-word answer; "" for every other call.
	Verdict string
	Err     string
}

type (
	runKey  struct{}
	turnKey struct{}
)

// InRun marks ctx as work done for a run, so the model calls made under it
// are kept with that run. An empty id leaves ctx as it is.
func InRun(ctx context.Context, runID string) context.Context {
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, runKey{}, runID)
}

// RunOf is the run ctx was marked with, or "".
func RunOf(ctx context.Context) string {
	id, _ := ctx.Value(runKey{}).(string)
	return id
}

// AsTurn marks ctx as a run's own model turn.
func AsTurn(ctx context.Context) context.Context {
	return context.WithValue(ctx, turnKey{}, true)
}

// judges are the labels whose answer is a verdict worth keeping.
var judges = map[string]bool{"approval": true, "filter": true}

// see tells Seen of a recorded call.
func (s *Service) see(ctx context.Context, botID, label string, rec llm.Record) {
	if s.Seen == nil || botID == "" {
		return
	}
	c := Call{
		BotID: botID, RunID: RunOf(ctx), Label: label, Provider: rec.Provider, Model: rec.Model,
		In: rec.Usage.InputTokens, Out: rec.Usage.OutputTokens, Duration: rec.Duration,
	}
	c.Turn, _ = ctx.Value(turnKey{}).(bool)
	if rec.Error != nil {
		c.Err = rec.Error.Error()
	}
	if judges[label] {
		c.Verdict = verdict(rec.Text)
	}
	s.Seen(c)
}

// verdict is a judge's answer when it is one word; a decision model's typed
// answers are not.
func verdict(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" || utf8.RuneCountInString(text) > 24 || strings.ContainsAny(text, " \n\t{") {
		return ""
	}
	return text
}

// Note tells Seen of a call that has no llm.Record: an embedding.
func (s *Service) Note(ctx context.Context, botID, label, provider, model string, took time.Duration, err error) {
	rec := llm.Record{Provider: provider, Model: model, Duration: took, Error: err}
	s.see(ctx, botID, label, rec)
}
