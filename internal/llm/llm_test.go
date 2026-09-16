package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitAndParse(t *testing.T) {
	p, m, ok := Split("openrouter/openai/gpt-5.6-luna")
	if !ok || p != "openrouter" || m != "openai/gpt-5.6-luna" {
		t.Fatalf("split: %q %q %v", p, m, ok)
	}
	if _, _, ok := Split("gpt-5.6-luna"); ok {
		t.Fatal("bare model should not parse")
	}
	if _, _, ok := Split("openrouter/"); ok {
		t.Fatal("empty model should not parse")
	}
	prov, model, err := Parse("anthropic/claude-opus-5")
	if err != nil || prov != "anthropic" || model != "claude-opus-5" {
		t.Fatalf("parse: %q %q %v", prov, model, err)
	}
	if _, _, err := Parse("nope/thing"); err == nil {
		t.Fatal("unknown provider should fail")
	}
	if !Allowed("openai/gpt-5", []string{" openai/gpt-5 ", "x"}) {
		t.Fatal("allowlist should trim")
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(OpenRouter, Settings{}); err == nil {
		t.Fatal("missing key should error")
	}
	if _, err := New("nope", Settings{"api_key": "x"}); err == nil {
		t.Fatal("unknown provider should error")
	}
}

func TestOpenAICompatParamsCacheAndSystem(t *testing.T) {
	c := &openAICompatClient{sendCacheKey: true}
	p := c.params(Request{
		Model: "gpt-5.6-luna",
		System: []SystemBlock{
			{Text: "BASE", CacheAfter: true},
			{Text: "MEM", CacheAfter: true},
		},
		Messages: []Message{{Role: RoleUser, Text: "hi"}},
		Tools: []Tool{{
			Name:        "read",
			Description: "read a file",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
		Cache: CachePolicy{Enabled: true, Key: "silo-bot-b1"},
	})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"prompt_cache_key":"silo-bot-b1"`) {
		t.Fatalf("missing prompt_cache_key: %s", s)
	}
	if !strings.Contains(s, "BASEMEM") {
		t.Fatalf("system blocks should concatenate: %s", s)
	}
	if !strings.Contains(s, `"name":"read"`) {
		t.Fatalf("tool missing: %s", s)
	}
	if !strings.Contains(s, `"stream_options":{"include_usage":true}`) {
		t.Fatalf("usage not requested: %s", s)
	}
}

func TestOpenAICompatCacheOff(t *testing.T) {
	c := &openAICompatClient{}
	raw, _ := json.Marshal(c.params(Request{Model: "m", Cache: CachePolicy{Key: "k"}}))
	if strings.Contains(string(raw), "prompt_cache_key") {
		t.Fatalf("cache key sent while disabled: %s", raw)
	}
}

func TestAnthropicParamsCacheBreakpoints(t *testing.T) {
	c := &anthropicClient{maxTokens: 4096}
	p := c.params(Request{
		Model: "claude-opus-5",
		System: []SystemBlock{
			{Text: "BASE", CacheAfter: true},
			{Text: "MEMORY"},
		},
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "read", Arguments: `{"path":"x"}`}}},
			{Role: RoleTool, ToolCallID: "t1", Text: "ok"},
			{Role: RoleUser, Text: "again"},
		},
		Tools: []Tool{{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Cache: CachePolicy{Enabled: true, TTL: "1h", Messages: true},
	})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Count(s, `"cache_control"`) < 2 {
		t.Fatalf("expected system + message breakpoints: %s", s)
	}
	if !strings.Contains(s, `"ttl":"1h"`) {
		t.Fatalf("ttl missing: %s", s)
	}
	if !strings.Contains(s, `"tool_result"`) {
		t.Fatalf("tool result missing: %s", s)
	}
	if !strings.Contains(s, `"input_schema":{"type":"object"}`) {
		t.Fatalf("tool schema missing: %s", s)
	}
}

func TestAnthropicCacheOff(t *testing.T) {
	c := &anthropicClient{maxTokens: 4096}
	raw, _ := json.Marshal(c.params(Request{
		Model:  "claude-opus-5",
		System: []SystemBlock{{Text: "BASE", CacheAfter: true}},
		Cache:  CachePolicy{Messages: true},
	}))
	if strings.Contains(string(raw), "cache_control") {
		t.Fatalf("cache_control sent while disabled: %s", raw)
	}
}
