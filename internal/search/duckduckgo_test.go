package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDecodeDDGURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"https://example.com/x", "https://example.com/x"},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Falpha&rut=abc", "https://example.com/alpha"},
		{"https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fbeta&rut=zz", "https://example.com/beta"},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fno-rut", "https://example.com/no-rut"},
	}
	for _, c := range cases {
		if got := decodeDDGURL(c.in); got != c.want {
			t.Fatalf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestDuckDuckGoSearch(t *testing.T) {
	raw, err := os.ReadFile("testdata/ddg_results.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Error("missing q")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	swapDDG(t, srv.URL+"/")

	got, err := NewDuckDuckGo().Search(context.Background(), "alpha", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("len %d: %+v", len(got.Results), got.Results)
	}
	if got.Results[0].Title != "Alpha Example" || got.Results[0].URL != "https://example.com/alpha" || got.Results[0].Snippet != "The alpha snippet." {
		t.Fatalf("first %+v", got.Results[0])
	}
	if got.Results[1].Title != "Beta Direct" || got.Results[1].URL != "https://example.com/beta" {
		t.Fatalf("second %+v", got.Results[1])
	}

	all, err := NewDuckDuckGo().Search(context.Background(), "alpha", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Results) != 3 {
		t.Fatalf("skip empty href: %d %+v", len(all.Results), all.Results)
	}
	if all.Results[2].Title != "Gamma Last" || all.Results[2].URL != "https://example.com/gamma" {
		t.Fatalf("third %+v", all.Results[2])
	}
}

func TestDuckDuckGoHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("no"))
	}))
	t.Cleanup(srv.Close)
	swapDDG(t, srv.URL+"/")
	_, err := NewDuckDuckGo().Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("%v", err)
	}
}

func TestDuckDuckGoLayoutChange(t *testing.T) {
	raw, err := os.ReadFile("testdata/ddg_empty.html")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	swapDDG(t, srv.URL+"/")
	_, err = NewDuckDuckGo().Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "layout changed") {
		t.Fatalf("%v", err)
	}
}

func TestDuckDuckGoTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", duckDuckGoMaxBody+2)))
	}))
	t.Cleanup(srv.Close)
	swapDDG(t, srv.URL+"/")
	_, err := NewDuckDuckGo().Search(context.Background(), "q", 5)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("%v", err)
	}
}

func TestDuckDuckGoCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	swapDDG(t, srv.URL+"/")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := NewDuckDuckGo().Search(ctx, "q", 5)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestDuckDuckGoQueryRequired(t *testing.T) {
	_, err := NewDuckDuckGo().Search(context.Background(), "  ", 5)
	if err == nil || !strings.Contains(err.Error(), "query required") {
		t.Fatalf("%v", err)
	}
}

func TestRegistry(t *testing.T) {
	if !Known(DuckDuckGoScraper) || Known("nope") {
		t.Fatal("known")
	}
	d, ok := Lookup(DuckDuckGoScraper)
	if !ok || d.Name != "DuckDuckGo Scraper" || len(d.Settings) != 0 {
		t.Fatalf("%+v", d)
	}
	eng, err := New(DuckDuckGoScraper, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*DuckDuckGo); !ok {
		t.Fatalf("%T", eng)
	}
	if _, err := New("nope", nil); err == nil {
		t.Fatal("unknown")
	}
	if ClampMax(0) != DefaultMaxResults || ClampMax(100) != MaxResultsCap || ClampMax(3) != 3 {
		t.Fatal("clamp")
	}
}

func TestDuckDuckGoTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	old := HTTPClient
	HTTPClient = &http.Client{Timeout: 20 * time.Millisecond}
	t.Cleanup(func() { HTTPClient = old })
	swapDDG(t, srv.URL+"/")
	_, err := NewDuckDuckGo().Search(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func swapDDG(t *testing.T, u string) {
	t.Helper()
	old := DuckDuckGoURL
	DuckDuckGoURL = u
	t.Cleanup(func() { DuckDuckGoURL = old })
}
