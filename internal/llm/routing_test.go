package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// OpenRouter's upstream pick decides whether tool arguments stream: some
// hosts (DeepInfra) send the whole call in one chunk at the end, so the thread
// cannot show exec_python/terminal/write as the model types them.
func TestOpenRouterIgnoresBufferingUpstreams(t *testing.T) {
	cases := []struct {
		name   string
		ignore *string
		want   any
	}{
		{"default", nil, []any{"DeepInfra"}},
		{"custom", strPtr(" Foo , Bar "), []any{"Foo", "Bar"}},
		{"none", strPtr("none"), nil},
		{"empty", strPtr(""), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			s := Settings{"api_key": "k", "base_url": srv.URL}
			if tc.ignore != nil {
				s["ignore"] = *tc.ignore
			}
			c, err := New(OpenRouter, s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
				t.Fatal(err)
			}
			var ignore any
			if p, ok := got["provider"].(map[string]any); ok {
				ignore = p["ignore"]
			}
			if !reflect.DeepEqual(ignore, tc.want) {
				t.Fatalf("provider.ignore = %#v, want %#v", ignore, tc.want)
			}
		})
	}
}

// OpenAI proper has no upstream routing; the field must not leak there.
func TestOpenAISendsNoRouting(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[]}`))
	}))
	defer srv.Close()
	c, err := New(OpenAI, Settings{"api_key": "k", "base_url": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["provider"]; ok {
		t.Fatalf("openai request carries provider: %v", got["provider"])
	}
}

func strPtr(s string) *string { return &s }
