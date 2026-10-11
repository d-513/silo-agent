package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLive asks the real sites. It runs only with SILO_SEARCH_LIVE=1:
//
//	SILO_SEARCH_LIVE=1 go test ./internal/search -run TestLive -v
//
// By default it runs whole searches, as the Bot would. SILO_SEARCH_ONLY=brave,mojeek
// asks those sources on their own instead, which is how to see why one is
// blocked; SILO_SEARCH_QUERY="text|pl" replaces the queries; and
// SILO_SEARCH_DUMP=<dir> saves every page a source was answered with, which
// is how the fixtures in testdata are captured.
func TestLive(t *testing.T) {
	if os.Getenv("SILO_SEARCH_LIVE") == "" {
		t.Skip("set SILO_SEARCH_LIVE=1 to ask the real search sites")
	}
	queries := []Query{{Text: "golang context cancellation"}, {Text: "najlepsze pierogi w Krakowie", Language: "pl"}}
	if v := os.Getenv("SILO_SEARCH_QUERY"); v != "" {
		text, lang, _ := strings.Cut(v, "|")
		queries = []Query{{Text: text, Language: lang}}
	}
	if dir := os.Getenv("SILO_SEARCH_DUMP"); dir != "" {
		real := fetch
		var n atomic.Int32
		fetch = func(ctx context.Context, rq request) (page, error) {
			p, err := real(ctx, rq)
			u, _ := url.Parse(rq.url)
			name := filepath.Join(dir, fmt.Sprintf("%02d_%s_%d.txt", n.Add(1), u.Hostname(), p.status))
			_ = os.WriteFile(name, append([]byte(rq.url+"\n"), p.body...), 0o644)
			return p, err
		}
		defer func() { fetch = real }()
	}
	only := os.Getenv("SILO_SEARCH_ONLY")
	for _, q := range queries {
		q = cleanQuery(q)
		if only == "" {
			start := time.Now()
			res, err := newMeta(nil).Search(context.Background(), q)
			t.Logf("%q: %d ms, %d results, blocked %v, note %q, err %v", q.Text, time.Since(start).Milliseconds(), len(res.Results), res.Blocked, res.Note, err)
			for _, h := range res.Results {
				t.Logf("   [%d] %s  %s", h.Engines, h.Title, h.URL)
			}
			continue
		}
		for _, s := range sources {
			if !strings.Contains(only, s.name) || (s.skip != nil && s.skip(q)) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			start := time.Now()
			hits, err := s.fetch(ctx, q)
			cancel()
			first := ""
			if len(hits) > 0 {
				first = hits[0].Title + " | " + hits[0].URL + " | " + clip(hits[0].Snippet, 60)
			}
			t.Logf("%-12s %-28q %4d ms  %2d hits  err=%v  %s", s.name, q.Text, time.Since(start).Milliseconds(), len(hits), err, first)
		}
	}
}

// TestFingerprint prints what a fingerprint echo service sees of the browser
// client: the TLS hello (JA4), the HTTP/2 SETTINGS and the headers in order.
// A real Chrome 145 reads
//
//	ja4     t13d1516h2_8daaf6152771_d8a2da3f94cd
//	akamai  1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p
//
// with the headers of chromeOrder. Run it after touching http.go.
func TestFingerprint(t *testing.T) {
	if os.Getenv("SILO_SEARCH_LIVE") == "" {
		t.Skip("set SILO_SEARCH_LIVE=1 to ask the fingerprint echo service")
	}
	p, err := fetch(context.Background(), request{url: "https://tls.peet.ws/api/all"})
	if err != nil || p.status != 200 {
		t.Fatalf("status %d err %v", p.status, err)
	}
	var seen struct {
		TLS struct {
			JA4 string `json:"ja4"`
		} `json:"tls"`
		HTTP2 struct {
			Akamai string `json:"akamai_fingerprint"`
			Frames []struct {
				Type    string   `json:"frame_type"`
				Headers []string `json:"headers"`
			} `json:"sent_frames"`
		} `json:"http2"`
	}
	if err := json.Unmarshal(p.body, &seen); err != nil {
		t.Fatal(err)
	}
	t.Logf("ja4     %s", seen.TLS.JA4)
	t.Logf("akamai  %s", seen.HTTP2.Akamai)
	for _, f := range seen.HTTP2.Frames {
		for _, h := range f.Headers {
			t.Logf("  %s", h)
		}
	}
	if seen.TLS.JA4 != "t13d1516h2_8daaf6152771_d8a2da3f94cd" || seen.HTTP2.Akamai != "1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p" {
		t.Fatal("the client no longer looks like the Chrome it says it is")
	}
}
