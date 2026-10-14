package egress

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

func TestParseFeedHosts(t *testing.T) {
	// The shape of URLhaus' and ThreatFox' host files.
	l, err := parseFeed("urlhaus", FormatHosts, strings.NewReader(`# URLhaus Host file
# Last updated
127.0.0.1	bad.example.com
0.0.0.0 evil.test # inline comment
127.0.0.1	localhost
127.0.0.1	203.0.113.9
malformed
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.hosts["bad.example.com"]; !ok {
		t.Error("bad.example.com missing")
	}
	if _, ok := l.hosts["evil.test"]; !ok {
		t.Error("evil.test missing")
	}
	if _, ok := l.hosts["localhost"]; ok {
		t.Error("localhost must never be listed")
	}
	if _, ok := l.addrs[netip.MustParseAddr("203.0.113.9")]; !ok {
		t.Error("an address in a hosts file must be listed as an address")
	}
	if l.size() != 3 {
		t.Errorf("size = %d, want 3", l.size())
	}
}

func TestParseFeedDomainsAndIPs(t *testing.T) {
	d, err := parseFeed("hagezi", FormatDomains, strings.NewReader("# HaGeZi\nbad.example\n*.worse.example\n! adblock comment\nnot a domain at all\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.domains["bad.example"]; !ok {
		t.Error("bad.example missing")
	}
	if _, ok := d.domains["worse.example"]; !ok {
		t.Error("a wildcard line must cover its domain")
	}
	if _, ok := d.domains["not"]; ok {
		t.Error("a line that is no domain must be skipped")
	}

	ips, err := parseFeed("et", FormatIPs, strings.NewReader("198.51.100.7\n192.0.2.0/24 ; SBL123\n203.0.113.5/32\nnonsense\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ips.addrs) != 2 || len(ips.nets) != 1 {
		t.Errorf("addrs %d nets %d, want 2 and 1", len(ips.addrs), len(ips.nets))
	}

	drop, err := parseFeed("drop", FormatCIDRJSON, strings.NewReader(`{"cidr":"100.64.10.0/24","sblid":"SBL1","rir":"arin"}
{"type":"metadata","timestamp":1,"size":1}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(drop.nets) != 1 || drop.nets[0].String() != "100.64.10.0/24" {
		t.Errorf("nets = %v", drop.nets)
	}

	if _, err := parseFeed("x", "nope", strings.NewReader("a\n")); err == nil {
		t.Error("an unknown format must be an error")
	}
}

// fakeFetch serves feed bodies by URL.
type fakeFetch struct {
	bodies map[string]string
	errs   map[string]error
	calls  int
}

func (f *fakeFetch) fetch(_ context.Context, url string) (io.ReadCloser, error) {
	f.calls++
	if err := f.errs[url]; err != nil {
		return nil, err
	}
	b, ok := f.bodies[url]
	if !ok {
		return nil, errors.New("http 404")
	}
	return io.NopCloser(strings.NewReader(b)), nil
}

func TestFeedsRefresh(t *testing.T) {
	ff := &fakeFetch{bodies: map[string]string{
		"https://hosts":   "127.0.0.1 bad.example.com\n",
		"https://domains": "evil.example\n",
		"https://ips":     "192.0.2.0/24\n198.51.100.7\n",
	}, errs: map[string]error{}}
	f := NewFeeds(nil)
	f.fetch = ff.fetch
	f.SetSpecs([]*v1.EgressFeed{
		{Id: "h", Url: "https://hosts", Format: FormatHosts},
		{Id: "d", Url: "https://domains", Format: FormatDomains},
		{Id: "i", Url: "https://ips", Format: FormatIPs},
		{Id: "", Url: "https://ignored", Format: FormatHosts},
	})
	now := time.Unix(1_800_000_000, 0)
	f.Refresh(context.Background(), now)
	if ff.calls != 3 {
		t.Fatalf("calls = %d, want 3", ff.calls)
	}

	for host, want := range map[string]string{
		"bad.example.com":     "h",
		"BAD.example.com.":    "h",
		"sub.bad.example.com": "", // a hosts file lists the host alone
		"evil.example":        "d",
		"a.b.evil.example":    "d",
		"192.0.2.44":          "i",
		"198.51.100.7:443":    "i",
		"[::ffff:192.0.2.1]":  "i",
		"good.example":        "",
	} {
		got, ok := f.Host(host)
		if got != want || ok != (want != "") {
			t.Errorf("Host(%q) = %q, %v; want %q", host, got, ok, want)
		}
	}

	// Not due yet: nothing is fetched again.
	f.Refresh(context.Background(), now.Add(time.Hour))
	if ff.calls != 3 {
		t.Errorf("calls = %d after an early refresh, want 3", ff.calls)
	}

	// A failed or empty download keeps the old list and records the error.
	ff.errs["https://hosts"] = errors.New("http 503")
	ff.bodies["https://domains"] = "# nothing today\n"
	f.Refresh(context.Background(), now.Add(feedRefresh))
	if _, ok := f.Host("bad.example.com"); !ok {
		t.Error("a failed download dropped the old list")
	}
	if _, ok := f.Host("evil.example"); !ok {
		t.Error("an empty download dropped the old list")
	}
	states := map[string]*v1.EgressFeedState{}
	for _, s := range f.States() {
		states[s.GetId()] = s
	}
	if states["h"].GetError() != "http 503" || states["h"].GetEntries() != 1 || states["h"].GetUpdatedAt() != now.Unix() {
		t.Errorf("state h = %v", states["h"])
	}
	if states["d"].GetError() == "" {
		t.Error("an empty download must read as an error")
	}
	if states["i"].GetError() != "" || states["i"].GetUpdatedAt() != now.Add(feedRefresh).Unix() {
		t.Errorf("state i = %v", states["i"])
	}

	// A failure is retried sooner than a success is refreshed.
	calls := ff.calls
	f.Refresh(context.Background(), now.Add(feedRefresh+feedRetry))
	if ff.calls != calls+2 {
		t.Errorf("retried %d feeds, want the 2 that failed", ff.calls-calls)
	}

	// A feed no longer wanted is gone at once; a changed URL is due now.
	f.SetSpecs([]*v1.EgressFeed{{Id: "i", Url: "https://ips2", Format: FormatIPs}})
	if _, ok := f.Host("bad.example.com"); ok {
		t.Error("a dropped feed still matches")
	}
	if len(f.States()) != 1 {
		t.Errorf("states = %d, want 1", len(f.States()))
	}
	calls = ff.calls
	f.Refresh(context.Background(), now.Add(feedRefresh+feedRetry))
	if ff.calls != calls+1 {
		t.Error("a feed whose URL changed was not fetched again")
	}
}

func TestFeedsRefreshStopsOnCancel(t *testing.T) {
	ff := &fakeFetch{bodies: map[string]string{"https://a": "1.2.3.4\n", "https://b": "1.2.3.5\n"}}
	f := NewFeeds(nil)
	f.fetch = ff.fetch
	f.SetSpecs([]*v1.EgressFeed{{Id: "a", Url: "https://a", Format: FormatIPs}, {Id: "b", Url: "https://b", Format: FormatIPs}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Refresh(ctx, time.Now())
	if ff.calls != 0 {
		t.Errorf("calls = %d after cancel, want 0", ff.calls)
	}
}
