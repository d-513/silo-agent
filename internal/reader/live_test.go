package reader

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/netguard"
)

// TestLive reads a handful of real pages and prints what came back:
//
//	SILO_READER_LIVE=1 go test ./internal/reader -run 'TestLive$' -v
//
// SILO_READER_URLS (comma-separated) reads other addresses instead. It asks
// real sites from this address, so run it by hand and not in a loop.
func TestLive(t *testing.T) {
	if os.Getenv("SILO_READER_LIVE") == "" {
		t.Skip("set SILO_READER_LIVE=1 to read real pages")
	}
	urls := []string{
		"https://en.wikipedia.org/wiki/Gdynia",
		"https://go.dev/blog/go1.22",
		"https://github.com/golang/go",
		"https://www.youtube.com/watch?v=jNQXAC9IVRw",
		"https://arxiv.org/abs/1706.03762",
		"https://news.ycombinator.com/",
	}
	if v := os.Getenv("SILO_READER_URLS"); v != "" {
		urls = strings.Split(v, ",")
	}
	r := New(Options{Policy: func() netguard.Policy { return netguard.Policy{Block: true} }})
	for _, u := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		start := time.Now()
		p, err := r.Read(ctx, Request{URL: strings.TrimSpace(u), MaxChars: 1200, Query: os.Getenv("SILO_READER_QUERY")}, nil)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		t.Logf("\n=== %s  (%s)\nkind=%s via=%s status=%d total=%d next=%d outline=%d problem=%q\ntitle=%q author=%q date=%q\nnote=%q data=%v\n--- text\n%s\n",
			u, time.Since(start).Round(time.Millisecond), p.Kind, p.Via, p.Status, p.TotalChars, p.NextOffset, len(p.Outline), p.Problem, p.Title, p.Author, p.Date, p.Note, p.Data, p.Text)
	}
}
