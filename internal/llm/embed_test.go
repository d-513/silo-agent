package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatEmbed(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		vec := make([]float64, EmbedDims)
		vec[0] = 1
		// Out of order on purpose: the client must place by index.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 1, "embedding": vec},
				{"object": "embedding", "index": 0, "embedding": make([]float64, EmbedDims)},
			},
		})
	}))
	defer srv.Close()
	c, err := New("openai", Settings{"api_key": "k", "base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := c.(Embedder)
	if !ok {
		t.Fatal("openai client is not an Embedder")
	}
	out, err := e.Embed(context.Background(), "text-embedding-3-small", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1][0] != 1 || out[0][0] != 0 {
		t.Fatalf("vectors misplaced: %v", [][]float32{out[0][:1], out[1][:1]})
	}
	if got["model"] != "text-embedding-3-small" || got["dimensions"] != float64(EmbedDims) {
		t.Fatalf("request %v", got)
	}
}

func TestOpenAICompatEmbedRejectsWrongWidth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"object": "embedding", "index": 0, "embedding": []float64{1, 2, 3}},
		}})
	}))
	defer srv.Close()
	c, _ := New("openai", Settings{"api_key": "k", "base_url": srv.URL})
	if _, err := c.(Embedder).Embed(context.Background(), "m", []string{"a"}); err == nil {
		t.Fatal("a 3-wide vector must fail")
	}
}
