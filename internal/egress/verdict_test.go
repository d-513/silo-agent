package egress

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// noResolve is a resolver that answers nothing: tests must never touch DNS.
func noResolve(context.Context, string) ([]netip.Addr, error) {
	return nil, errors.New("no dns in tests")
}

func TestVerdictBlocks(t *testing.T) {
	s, err := NewState(testApply(t))
	if err != nil {
		t.Fatal(err)
	}
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	b2 := s.Client(netip.MustParseAddr("10.213.3.1"))

	feed := NewFeeds(nil)
	list, err := parseFeed("t", FormatDomains, strings.NewReader("badfeed.example\n"))
	if err != nil {
		t.Fatal(err)
	}
	feed.lists["t"] = list

	g := NewGuard(s, feed, nil)
	g.Resolve = noResolve
	ctx := context.Background()

	cases := []struct {
		name   string
		c      *Client
		req    Request
		reason string
	}{
		{"operator list", b2, Request{Host: "x.evil.example"}, "blocked_host"},
		{"bot list", b1, Request{Host: "ads.example"}, "blocked_host"},
		{"threat feed", b2, Request{Host: "sub.badfeed.example"}, "threat_list"},
		{"loopback", b2, Request{Host: "127.0.0.1"}, "private"},
		{"private range", b2, Request{Host: "10.1.2.3"}, "private"},
		{"metadata", b2, Request{Host: "169.254.169.254"}, "private"},
		{"cgnat metadata", b2, Request{Host: "100.100.100.200"}, "private"},
		{"surrogate leak", b1, Request{Host: "example.com", Payload: "Bearer ghp_SURROGATE0000000000"}, "surrogate_leak"},
	}
	for _, tc := range cases {
		req := tc.req
		req.Client = tc.c
		v := g.Check(ctx, req)
		if !v.Block || v.Reason != tc.reason {
			t.Errorf("%s: verdict = %+v, want block %s", tc.name, v, tc.reason)
		}
	}
}

func TestVerdictResolvedPrivate(t *testing.T) {
	s, _ := NewState(testApply(t))
	g := NewGuard(s, nil, nil)
	g.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.213.0.9")}, nil
	}
	v := g.Check(context.Background(), Request{Client: s.Client(netip.MustParseAddr("10.213.1.1")), Host: "internal.example"})
	if !v.Block || v.Reason != "private" {
		t.Fatalf("a name resolving inside must be blocked: %+v", v)
	}
}

func TestVerdictFlags(t *testing.T) {
	s, _ := NewState(testApply(t))
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	b2 := s.Client(netip.MustParseAddr("10.213.3.1"))

	now := time.Unix(1000, 0)
	rep := NewReporter(func() time.Time { return now })
	g := NewGuard(s, nil, rep)
	g.Resolve = noResolve
	ctx := context.Background()

	cases := []struct {
		name   string
		c      *Client
		req    Request
		reason string
	}{
		{"exfil sink host", b2, Request{Host: "webhook.site", Path: "/"}, "exfil_sink"},
		{"exfil sink path", b2, Request{Host: "discord.com", Path: "/api/webhooks/123"}, "exfil_sink"},
		{"secret shape", b2, Request{Host: "example.com", Payload: "key AKIAIOSFODNN7EXAMPLE"}, "secret_shape"},
		{"plain secret", b1, Request{Host: "example.com", Payload: "password=hunter2hunter2"}, "plain_secret"},
		{"query entropy", b2, Request{Host: "example.com", Path: "/?abcdefghijklmnopqrstuvwxyz0123456789ABCD"}, "query_entropy"},
	}
	s.queryMin = 10
	for _, tc := range cases {
		req := tc.req
		req.Client = tc.c
		v := g.Check(ctx, req)
		if !v.Flag || v.Reason != tc.reason {
			t.Errorf("%s: verdict = %+v, want flag %s", tc.name, v, tc.reason)
		}
	}
}

func TestVerdictTrustSilencesFlagsNotBlocks(t *testing.T) {
	s, _ := NewState(testApply(t))
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	ctx := context.Background()

	// A secret shape to a trusted host passes.
	if v := g.Check(ctx, Request{Client: b1, Host: "pastebin.com", Payload: "AKIAIOSFODNN7EXAMPLE"}); v.Flag || v.Block {
		t.Errorf("trust did not silence the flag: %+v", v)
	}
	// A block still wins over trust.
	if v := g.Check(ctx, Request{Client: b1, Host: "ads.example", Payload: "AKIAIOSFODNN7EXAMPLE"}); !v.Block {
		t.Errorf("a blocked host was silenced by trust: %+v", v)
	}
}

func TestVerdictBudgetAndBeacon(t *testing.T) {
	s, _ := NewState(testApply(t))
	s.budget = 10
	b1 := s.Client(netip.MustParseAddr("10.213.1.1"))

	now := time.Unix(1000, 0)
	rep := NewReporter(func() time.Time { return now })
	g := NewGuard(s, nil, rep)
	g.Resolve = noResolve
	ctx := context.Background()

	v := g.Check(ctx, Request{Client: b1, Host: "upload.example", Payload: strings.Repeat("x", 20)})
	if !v.Flag || v.Reason != "upload_budget" {
		t.Fatalf("upload budget: %+v", v)
	}

	// A steady clock to one host is a beacon by the ninth call.
	var beacon Verdict
	for i := 0; i < 9; i++ {
		now = now.Add(60 * time.Second)
		beacon = g.Check(ctx, Request{Client: b1, Host: "c2.example.com"})
	}
	if !beacon.Flag || beacon.Reason != "beaconing" {
		t.Fatalf("beaconing: %+v", beacon)
	}
}

func TestVerdictAllowsOrdinary(t *testing.T) {
	s, _ := NewState(testApply(t))
	g := NewGuard(s, nil, nil)
	g.Resolve = noResolve
	v := g.Check(context.Background(), Request{
		Client: s.Client(netip.MustParseAddr("10.213.1.1")),
		Host:   "example.com",
		Path:   "/index.html",
		Method: "GET",
	})
	if v.Flag || v.Block {
		t.Fatalf("an ordinary request was judged: %+v", v)
	}
	if v := (*Guard)(nil).Check(context.Background(), Request{Host: "evil.example"}); v.Block {
		t.Error("a nil guard must pass everything")
	}
}

func TestCheckResponseExecutable(t *testing.T) {
	s, _ := NewState(testApply(t))
	g := NewGuard(s, nil, nil)
	c := s.Client(netip.MustParseAddr("10.213.1.1"))
	if v := g.CheckResponse(c, "example.com", []byte("\x7fELF\x02\x01\x01"), "application/octet-stream"); !v.Flag || v.Reason != "executable" {
		t.Errorf("ELF: %+v", v)
	}
	if v := g.CheckResponse(c, "example.com", []byte("<html>"), "text/html"); v.Flag {
		t.Errorf("HTML was called an executable: %+v", v)
	}
}
