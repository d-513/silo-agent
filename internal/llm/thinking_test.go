package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestNearestThinking(t *testing.T) {
	for _, tc := range []struct {
		level  string
		levels []string
		want   string
	}{
		{"high", []string{"low", "medium", "high"}, "high"},
		{"xhigh", []string{"low", "medium", "high"}, "high"},
		{"minimal", []string{"low", "medium", "high"}, "low"},
		{"off", []string{"low", "medium", "high"}, "low"},
		// Equidistant: the cheaper level wins.
		{"medium", []string{"low", "high"}, "low"},
		{"max", []string{"off", "low", "medium", "high", "xhigh"}, "xhigh"},
		{"high", nil, ""},
		{"", []string{"low"}, ""},
		{"bogus", []string{"low"}, ""},
	} {
		if got := NearestThinking(tc.level, tc.levels); got != tc.want {
			t.Errorf("NearestThinking(%q, %v) = %q, want %q", tc.level, tc.levels, got, tc.want)
		}
	}
	if got := SortThinking([]string{"max", "low", "bogus", "low", "off"}); !reflect.DeepEqual(got, []string{"off", "low", "max"}) {
		t.Fatalf("SortThinking = %v", got)
	}
	if !ValidThinking("") || !ValidThinking("xhigh") || ValidThinking("ultra") {
		t.Fatal("ValidThinking")
	}
}

// bodies records every JSON request body a fake provider receives, by path.
type bodies struct {
	sync.Mutex
	got map[string][]map[string]any
}

func (b *bodies) add(path string, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	b.Lock()
	defer b.Unlock()
	if b.got == nil {
		b.got = map[string][]map[string]any{}
	}
	b.got[path] = append(b.got[path], m)
}

func (b *bodies) last(path string) map[string]any {
	b.Lock()
	defer b.Unlock()
	xs := b.got[path]
	if len(xs) == 0 {
		return nil
	}
	return xs[len(xs)-1]
}

const openAIReply = `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`

func TestOpenRouterThinking(t *testing.T) {
	var b bodies
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"openai/gpt-5.6-luna","context_length":400000,"supported_parameters":["tools","reasoning","include_reasoning"]},
				{"id":"meta/llama","context_length":8000,"supported_parameters":["tools"]}]}`))
		case "/chat/completions":
			b.add(r.URL.Path, r)
			_, _ = w.Write([]byte(openAIReply))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := New(OpenRouter, Settings{"api_key": "k", "base_url": srv.URL, "ignore": "none"})
	if err != nil {
		t.Fatal(err)
	}
	th := c.(Thinker)
	ctx := context.Background()
	levels, err := th.ThinkingLevels(ctx, "openai/gpt-5.6-luna")
	if err != nil || !reflect.DeepEqual(levels, openRouterThinking) {
		t.Fatalf("reasoning model levels %v %v", levels, err)
	}
	if levels, err := th.ThinkingLevels(ctx, "meta/llama"); err != nil || len(levels) != 0 {
		t.Fatalf("plain model levels %v %v", levels, err)
	}
	if _, err := th.ThinkingLevels(ctx, "nobody/unlisted"); err == nil {
		t.Fatal("an unlisted model should error so operator config decides")
	}

	for level, want := range map[string]string{"high": "high", "off": "none"} {
		if _, err := c.Complete(ctx, Request{Model: "openai/gpt-5.6-luna", Thinking: level, Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		body := b.last("/chat/completions")
		r, _ := body["reasoning"].(map[string]any)
		if r["effort"] != want {
			t.Fatalf("%s: reasoning %v", level, body["reasoning"])
		}
		if _, ok := body["reasoning_effort"]; ok {
			t.Fatalf("OpenRouter got reasoning_effort too: %v", body)
		}
	}
	if _, err := c.Complete(ctx, Request{Model: "openai/gpt-5.6-luna", Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.last("/chat/completions")["reasoning"]; ok {
		t.Fatal("no level must send no reasoning object")
	}
}

func TestOpenAIThinking(t *testing.T) {
	var b bodies
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b.add(r.URL.Path, r)
		_, _ = w.Write([]byte(openAIReply))
	}))
	defer srv.Close()

	if got := openAIThinking("gpt-5.6-luna"); !reflect.DeepEqual(got, []string{"low", "medium", "high"}) {
		t.Fatalf("gpt-5 levels %v", got)
	}
	if got := openAIThinking("o3-mini"); len(got) != 3 {
		t.Fatalf("o3 levels %v", got)
	}
	if got := openAIThinking("gpt-4.1"); got != nil {
		t.Fatalf("gpt-4.1 levels %v", got)
	}

	c, _ := New(OpenAI, Settings{"api_key": "k", "base_url": srv.URL})
	if _, err := c.(Thinker).ThinkingLevels(context.Background(), "gpt-5.6-luna"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), Request{Model: "gpt-5.6-luna", Thinking: "low", Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if got := b.last("/chat/completions")["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort %v", got)
	}

	local, _ := New(Local, Settings{"base_url": srv.URL})
	if _, err := local.(Thinker).ThinkingLevels(context.Background(), "qwen"); err == nil {
		t.Fatal("a local server cannot tell; operator config decides")
	}
}

func anthropicModel(adaptive bool) string {
	if adaptive {
		return `{"id":"claude-opus-5-5","type":"model","display_name":"Opus","created_at":"2026-01-01T00:00:00Z","capabilities":{
			"thinking":{"supported":true,"types":{"adaptive":{"supported":true},"enabled":{"supported":false}}},
			"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":true},"max":{"supported":true}}}}`
	}
	return `{"id":"claude-haiku-4-5","type":"model","display_name":"Haiku","created_at":"2026-01-01T00:00:00Z","capabilities":{
		"thinking":{"supported":true,"types":{"adaptive":{"supported":false},"enabled":{"supported":true}}},
		"effort":{"supported":false,"low":{"supported":false},"medium":{"supported":false},"high":{"supported":false},"xhigh":{"supported":false},"max":{"supported":false}}}}`
}

func TestAnthropicThinking(t *testing.T) {
	var b bodies
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models/claude-opus-5-5":
			_, _ = w.Write([]byte(anthropicModel(true)))
		case "/v1/models/claude-haiku-4-5":
			_, _ = w.Write([]byte(anthropicModel(false)))
		case "/v1/messages":
			b.add(r.URL.Path, r)
			_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"x","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := New(Anthropic, Settings{"api_key": "k", "base_url": srv.URL, "max_tokens": "1000"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	th := c.(Thinker)
	if levels, err := th.ThinkingLevels(ctx, "claude-opus-5-5"); err != nil || !reflect.DeepEqual(levels, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("adaptive levels %v %v", levels, err)
	}
	if levels, err := th.ThinkingLevels(ctx, "claude-haiku-4-5"); err != nil || !reflect.DeepEqual(levels, []string{"off", "low", "medium", "high"}) {
		t.Fatalf("budget levels %v %v", levels, err)
	}

	history := []Message{
		{Role: RoleUser, Text: "hi"},
		{Role: RoleAssistant, ThinkingModel: "claude-opus-5-5", Thinking: []ThinkingBlock{{Text: "hmm", Signature: "sig"}, {Redacted: "opaque"}},
			ToolCalls: []ToolCall{{ID: "t1", Name: "read", Arguments: `{}`}}},
		{Role: RoleTool, ToolCallID: "t1", Text: "ok"},
	}
	if _, err := c.Complete(ctx, Request{Model: "claude-opus-5-5", Thinking: "xhigh", Messages: history}); err != nil {
		t.Fatal(err)
	}
	body := b.last("/v1/messages")
	if th, _ := body["thinking"].(map[string]any); th["type"] != "adaptive" || th["display"] != "summarized" {
		t.Fatalf("thinking %v", body["thinking"])
	}
	if oc, _ := body["output_config"].(map[string]any); oc["effort"] != "xhigh" {
		t.Fatalf("output_config %v", body["output_config"])
	}
	raw, _ := json.Marshal(body["messages"])
	if !strings.Contains(string(raw), `"signature":"sig"`) || !strings.Contains(string(raw), `"redacted_thinking"`) {
		t.Fatalf("signed thinking not replayed to its own model: %s", raw)
	}
	// Thinking blocks come first in the assistant turn.
	msgs, _ := body["messages"].([]any)
	first := msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if first["type"] != "thinking" {
		t.Fatalf("assistant turn starts with %v", first["type"])
	}

	if _, err := c.Complete(ctx, Request{Model: "claude-haiku-4-5", Thinking: "medium", Messages: history}); err != nil {
		t.Fatal(err)
	}
	body = b.last("/v1/messages")
	th2, _ := body["thinking"].(map[string]any)
	if th2["type"] != "enabled" || th2["budget_tokens"] != float64(8192) {
		t.Fatalf("budget thinking %v", body["thinking"])
	}
	if body["max_tokens"] != float64(8192+1000) {
		t.Fatalf("max_tokens %v must leave room past the budget", body["max_tokens"])
	}
	if _, ok := body["output_config"]; ok {
		t.Fatalf("budget model got effort: %v", body["output_config"])
	}
	raw, _ = json.Marshal(body["messages"])
	if strings.Contains(string(raw), "thinking") {
		t.Fatalf("another model's thinking was replayed: %s", raw)
	}

	if _, err := c.Complete(ctx, Request{Model: "claude-opus-5-5", Messages: history[:1]}); err != nil {
		t.Fatal(err)
	}
	body = b.last("/v1/messages")
	if _, ok := body["thinking"]; ok {
		t.Fatalf("no level must leave thinking to the model default: %v", body["thinking"])
	}
}

func TestAnthropicStreamThinkingBlock(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"x","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"see"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/models/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(anthropicModel(true)))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			var head struct{ Type string }
			_ = json.Unmarshal([]byte(e), &head)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", head.Type, e)
		}
	}))
	defer srv.Close()

	c, _ := New(Anthropic, Settings{"api_key": "k", "base_url": srv.URL})
	st, err := c.Stream(context.Background(), Request{Model: "claude-opus-5-5", Thinking: "high", Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var blocks []ThinkingBlock
	var reasoning string
	for st.Next() {
		ev := st.Event()
		switch ev.Kind {
		case EventThinkingBlock:
			blocks = append(blocks, *ev.Block)
		case EventReasoning:
			reasoning += ev.Text
		}
	}
	if err := st.Err(); err != nil {
		t.Fatal(err)
	}
	if reasoning != "let me see" {
		t.Fatalf("reasoning %q", reasoning)
	}
	if len(blocks) != 1 || blocks[0].Text != "let me see" || blocks[0].Signature != "SIG" {
		t.Fatalf("blocks %+v", blocks)
	}
}
