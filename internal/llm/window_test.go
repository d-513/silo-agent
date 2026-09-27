package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenRouterContextWindow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5.6-luna","context_length":400000},{"id":"x/none"}]}`))
	}))
	defer srv.Close()

	c, err := New(OpenRouter, Settings{"api_key": "k", "base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cw, ok := c.(ContextWindower)
	if !ok {
		t.Fatal("openrouter client is not a ContextWindower")
	}
	n, err := cw.ContextWindow(context.Background(), "openai/gpt-5.6-luna")
	if err != nil || n != 400000 {
		t.Fatalf("window %d %v", n, err)
	}
	if _, err := cw.ContextWindow(context.Background(), "x/none"); err == nil {
		t.Fatal("a model without context_length should error")
	}
	if hits.Load() != 1 {
		t.Fatalf("model list fetched %d times, want cached once", hits.Load())
	}

	o, _ := New(OpenAI, Settings{"api_key": "k", "base_url": srv.URL})
	if _, err := o.(ContextWindower).ContextWindow(context.Background(), "gpt"); err == nil {
		t.Fatal("OpenAI should not report windows")
	}
}
