package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func ids(list []ModelInfo) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.ID)
	}
	return out
}

// Every OpenAI-compatible provider lists through the same GET {base}/models:
// OpenRouter fills name and context_length, OpenAI and self-hosted servers
// only the id.
func TestOpenAICompatListModels(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		auth = append(auth, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[
			{"id":"openai/gpt-old","name":"OpenAI: GPT Old","created":100,"context_length":8000},
			{"id":"openai/gpt-5.6-luna","name":"OpenAI: GPT-5.6 Luna","created":300,"context_length":400000},
			{"id":"b/undated"},
			{"id":"a/undated"},
			{"id":""}
		]}`))
	}))
	defer srv.Close()

	for _, provider := range []string{OpenRouter, OpenAI, Local} {
		c, err := New(provider, Settings{"api_key": "k", "base_url": srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		l, ok := c.(ModelLister)
		if !ok {
			t.Fatalf("%s client is not a ModelLister", provider)
		}
		got, err := l.ListModels(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		// Newest first, then by id; an entry with no id is dropped.
		want := []string{"openai/gpt-5.6-luna", "openai/gpt-old", "a/undated", "b/undated"}
		if !reflect.DeepEqual(ids(got), want) {
			t.Fatalf("%s order %v, want %v", provider, ids(got), want)
		}
		if got[0].Name != "OpenAI: GPT-5.6 Luna" || got[0].ContextWindow != 400000 {
			t.Fatalf("%s first %+v", provider, got[0])
		}
	}
	for _, a := range auth {
		if a != "Bearer k" {
			t.Fatalf("auth header %q", a)
		}
	}
}

func TestOpenAICompatListModelsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"Incorrect API key provided: sk-secret"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := New(OpenAI, Settings{"api_key": "sk-secret", "base_url": srv.URL})
	_, err := c.(ModelLister).ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want an http 401 error, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaks the key: %v", err)
	}
}

// Anthropic lists through its own Models API (GET /v1/models, paged).
func TestAnthropicListModels(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Api-Key") != "k" {
			t.Errorf("x-api-key %q", r.Header.Get("X-Api-Key"))
		}
		pages = append(pages, r.URL.Query().Get("after_id"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[
				{"type":"model","id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5","created_at":"2025-10-01T00:00:00Z","max_input_tokens":200000,"max_tokens":64000}
			],"has_more":true,"first_id":"claude-haiku-4-5","last_id":"claude-haiku-4-5"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-05-01T00:00:00Z","max_input_tokens":1000000,"max_tokens":128000}
		],"has_more":false,"first_id":"claude-opus-5-5","last_id":"claude-opus-5-5"}`))
	}))
	defer srv.Close()

	c, err := New(Anthropic, Settings{"api_key": "k", "base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	l, ok := c.(ModelLister)
	if !ok {
		t.Fatal("anthropic client is not a ModelLister")
	}
	got, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude-opus-5-5", "claude-haiku-4-5"}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("order %v, want %v", ids(got), want)
	}
	if got[0].Name != "Claude Opus 5.5" || got[0].ContextWindow != 1000000 {
		t.Fatalf("first %+v", got[0])
	}
	if !reflect.DeepEqual(pages, []string{"", "claude-haiku-4-5"}) {
		t.Fatalf("pages asked %v", pages)
	}
}
