//go:build live

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveOpenRouterCachesClaude checks against the real gateway what the unit
// tests can only assume: that the cache markers this engine sends make Claude's
// prompt cache hit on the second request of a conversation. It spends a few
// thousand Haiku tokens. Run it after changing how requests are built or
// marked:
//
//	SILO_LIVE_OPENROUTER_KEY=sk-or-... go test -tags live ./internal/llm -run Live -v
func TestLiveOpenRouterCachesClaude(t *testing.T) {
	key := os.Getenv("SILO_LIVE_OPENROUTER_KEY")
	if key == "" {
		t.Skip("SILO_LIVE_OPENROUTER_KEY is not set")
	}
	model := os.Getenv("SILO_LIVE_MODEL")
	if model == "" {
		model = "anthropic/claude-haiku-4.5"
	}
	c, err := New(OpenRouter, Settings{"api_key": key})
	if err != nil {
		t.Fatal(err)
	}
	// A prefix nobody has cached: the nonce leads the first tier.
	nonce := fmt.Sprintf("run-%d", time.Now().UnixNano())
	filler := func(topic string, n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "Rule %d about %s: when the %s gauge reads %d, log it and carry on. ", i, topic, topic, i*7)
		}
		return b.String()
	}
	req := Request{
		Model: model,
		System: []SystemBlock{
			{Text: "You are a terse test assistant (" + nonce + "). " + filler("base", 260), CacheAfter: true},
			{Text: "\n\nSession notes. " + filler("session", 140), CacheAfter: true},
			{Text: "\n\nSOUL: answer in one short sentence.", CacheAfter: true},
			{Text: "\n\nA trailing note with no breakpoint of its own."},
		},
		Tools: []Tool{{
			Name: "lookup", Description: "Look a word up.",
			Parameters: json.RawMessage(`{"type":"object","properties":{"word":{"type":"string"}},"required":["word"]}`),
		}},
		Messages: []Message{{Role: RoleUser, Text: "Call lookup for the word gauge, then tell me what it said."}},
		Cache:    CachePolicy{Enabled: true, Key: "silo-live-" + nonce, Messages: true},
	}
	turn := func(req Request) (Usage, Message) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		st, err := c.Stream(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		var usage Usage
		msg := Message{Role: RoleAssistant}
		calls := map[int]*ToolCall{}
		var order []int
		for st.Next() {
			ev := st.Event()
			switch ev.Kind {
			case EventText:
				msg.Text += ev.Text
			case EventToolCallStart:
				if calls[ev.Index] == nil {
					calls[ev.Index] = &ToolCall{}
					order = append(order, ev.Index)
				}
				if ev.ToolCallID != "" {
					calls[ev.Index].ID = ev.ToolCallID
				}
				if ev.ToolName != "" {
					calls[ev.Index].Name = ev.ToolName
				}
			case EventToolCallDelta:
				if calls[ev.Index] != nil {
					calls[ev.Index].Arguments += ev.Text
				}
			case EventUsage:
				usage = ev.Usage
			}
		}
		if err := st.Err(); err != nil {
			t.Fatalf("stream: %v", err)
		}
		for _, i := range order {
			msg.ToolCalls = append(msg.ToolCalls, *calls[i])
		}
		return usage, msg
	}

	u1, first := turn(req)
	t.Logf("turn 1: input=%d cache_read=%d cache_write=%d calls=%d", u1.InputTokens, u1.CacheReadTokens, u1.CacheWriteTokens, len(first.ToolCalls))
	if u1.CacheWriteTokens == 0 {
		t.Fatalf("the first request wrote nothing to the cache: %+v", u1)
	}
	if len(first.ToolCalls) == 0 {
		t.Fatalf("the model made no tool call: %q", first.Text)
	}
	req.Messages = append(req.Messages, first)
	for _, tc := range first.ToolCalls {
		req.Messages = append(req.Messages, Message{Role: RoleTool, ToolCallID: tc.ID, Text: "gauge: an instrument that measures. " + filler("result", 30)})
	}
	u2, second := turn(req)
	t.Logf("turn 2: input=%d cache_read=%d cache_write=%d", u2.InputTokens, u2.CacheReadTokens, u2.CacheWriteTokens)
	// The whole first request (tools, system, the user message) must come
	// back from the cache; only the new turn is written.
	if u2.CacheReadTokens < u1.CacheWriteTokens*9/10 {
		t.Fatalf("the second request read %d cached tokens; the first wrote %d", u2.CacheReadTokens, u1.CacheWriteTokens)
	}

	// A third turn must read what the second wrote: the conversation
	// breakpoint moved forward past the tool result.
	req.Messages = append(req.Messages, second, Message{Role: RoleUser, Text: "Thanks. Say it once more, shorter."})
	u3, _ := turn(req)
	t.Logf("turn 3: input=%d cache_read=%d cache_write=%d", u3.InputTokens, u3.CacheReadTokens, u3.CacheWriteTokens)
	if u3.CacheReadTokens <= u2.CacheReadTokens {
		t.Fatalf("the third request read %d cached tokens, no more than the second (%d): the conversation is not being cached", u3.CacheReadTokens, u2.CacheReadTokens)
	}
}
