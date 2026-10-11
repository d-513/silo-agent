package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capture runs one streamed completion against a fake gateway and returns the
// request body it received.
func capture(t *testing.T, provider string, req Request, reply string) (map[string]any, Usage) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	c, err := New(provider, Settings{"api_key": "k", "base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var usage Usage
	for st.Next() {
		if ev := st.Event(); ev.Kind == EventUsage {
			usage = ev.Usage
		}
	}
	if err := st.Err(); err != nil {
		t.Fatal(err)
	}
	return got, usage
}

const sseDone = "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
	"data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":3,\"prompt_tokens_details\":{\"cached_tokens\":100,\"cache_write_tokens\":15}}}\n\n" +
	"data: [DONE]\n\n"

func cacheRequest(model string) Request {
	return Request{
		Model: model,
		System: []SystemBlock{
			{Text: "BASE", CacheAfter: true},
			{Text: "SESSION", CacheAfter: true},
			{Text: "TAIL"},
		},
		Messages: []Message{{Role: RoleUser, Text: "hi"}},
		Cache:    CachePolicy{Enabled: true, TTL: "1h", Key: "silo-bot-b1", Messages: true},
	}
}

// Claude through OpenRouter is only cached when the request carries
// cache_control: a breakpoint after each stable system tier and the automatic
// one that follows the end of the conversation.
func TestOpenRouterMarksClaudeCache(t *testing.T) {
	got, usage := capture(t, OpenRouter, cacheRequest("anthropic/claude-haiku-5.5"), sseDone)

	top, _ := got["cache_control"].(map[string]any)
	if top["type"] != "ephemeral" || top["ttl"] != "1h" {
		t.Fatalf("top-level cache_control = %v", got["cache_control"])
	}
	if got["prompt_cache_key"] != "silo-bot-b1" {
		t.Fatalf("sticky routing key = %v", got["prompt_cache_key"])
	}
	msgs, _ := got["messages"].([]any)
	sys, _ := msgs[0].(map[string]any)
	parts, ok := sys["content"].([]any)
	if sys["role"] != "system" || !ok || len(parts) != 3 {
		t.Fatalf("system message = %v", msgs[0])
	}
	for i, wantMark := range []bool{true, true, false} {
		p, _ := parts[i].(map[string]any)
		_, marked := p["cache_control"]
		if marked != wantMark || p["type"] != "text" {
			t.Fatalf("system part %d = %v, marked want %v", i, p, wantMark)
		}
	}
	if first, _ := parts[0].(map[string]any); first["text"] != "BASE" {
		t.Fatalf("system part 0 = %v", parts[0])
	}
	if usage.InputTokens != 120 || usage.CacheReadTokens != 100 || usage.CacheWriteTokens != 15 {
		t.Fatalf("usage = %+v", usage)
	}
}

// Upstreams that cache a stable prefix on their own get no markers, only the
// routing key that keeps a conversation on one of them.
func TestOpenRouterLeavesAutomaticCachesAlone(t *testing.T) {
	got, _ := capture(t, OpenRouter, cacheRequest("openai/gpt-5.6-luna"), sseDone)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "cache_control") {
		t.Fatalf("cache_control sent to an automatic cache: %s", raw)
	}
	msgs, _ := got["messages"].([]any)
	if sys, _ := msgs[0].(map[string]any); sys["content"] != "BASESESSIONTAIL" {
		t.Fatalf("system = %v", msgs[0])
	}
	if got["prompt_cache_key"] != "silo-bot-b1" {
		t.Fatalf("sticky routing key = %v", got["prompt_cache_key"])
	}
}

func TestOpenRouterCacheOffSendsNothing(t *testing.T) {
	req := cacheRequest("anthropic/claude-haiku-5.5")
	req.Cache = CachePolicy{}
	got, _ := capture(t, OpenRouter, req, sseDone)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "cache_control") || strings.Contains(string(raw), "prompt_cache_key") {
		t.Fatalf("cache fields sent while off: %s", raw)
	}
}

// Anthropic reports the uncached remainder as input_tokens; the engine's
// InputTokens is the whole prompt, as the OpenAI-compatible providers report.
func TestAnthropicInputCountsCachedTokens(t *testing.T) {
	var u anthropicUsage
	u.set(40, 900, 60)
	if got := u.usage(7); got.InputTokens != 1000 || got.CacheReadTokens != 900 || got.CacheWriteTokens != 60 || got.OutputTokens != 7 {
		t.Fatalf("usage = %+v", got)
	}
}
