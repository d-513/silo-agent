package egress

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// Feed formats.
const (
	// FormatHosts is a hosts file: "127.0.0.1 host" or "0.0.0.0 host". Each
	// host is listed for itself only.
	FormatHosts = "hosts"
	// FormatDomains is one domain per line, covering everything below it.
	FormatDomains = "domains"
	// FormatIPs is an address or CIDR per line, anything after it ignored.
	FormatIPs = "ips"
	// FormatCIDRJSON is a line of JSON per range with a "cidr" field
	// (Spamhaus DROP).
	FormatCIDRJSON = "cidr_json"
)

const (
	feedRefresh = 12 * time.Hour
	// feedRetry is how soon a failed download is tried again.
	feedRetry = 30 * time.Minute
	// feedMaxBytes caps one download; a list past it is cut, not refused.
	feedMaxBytes = 64 << 20
)

// feedList is one downloaded feed.
type feedList struct {
	id    string
	hosts map[string]struct{}
	// domains cover everything below them.
	domains map[string]struct{}
	addrs   map[netip.Addr]struct{}
	nets    []netip.Prefix
}

func (l *feedList) size() int {
	return len(l.hosts) + len(l.domains) + len(l.addrs) + len(l.nets)
}

// parseFeed reads a feed in the given format. Lines it cannot read are
// skipped: one bad line must not cost the list.
func parseFeed(id, format string, r io.Reader) (*feedList, error) {
	l := &feedList{id: id, hosts: map[string]struct{}{}, domains: map[string]struct{}{}, addrs: map[netip.Addr]struct{}{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' || line[0] == '!' {
			continue
		}
		switch format {
		case FormatHosts:
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			for _, h := range f[1:] {
				if strings.HasPrefix(h, "#") {
					break
				}
				h = NormHost(h)
				if a, err := netip.ParseAddr(h); err == nil {
					l.addrs[a.Unmap()] = struct{}{}
				} else if h != "" && h != "localhost" && ValidPattern(h) {
					l.hosts[h] = struct{}{}
				}
			}
		case FormatDomains:
			h := NormHost(strings.Fields(line)[0])
			h = strings.TrimPrefix(h, "*.")
			if ValidPattern(h) {
				l.domains[h] = struct{}{}
			}
		case FormatIPs:
			l.addIP(strings.Fields(strings.SplitN(line, ";", 2)[0]))
		case FormatCIDRJSON:
			var row struct {
				CIDR string `json:"cidr"`
			}
			if json.Unmarshal([]byte(line), &row) == nil && row.CIDR != "" {
				l.addIP([]string{row.CIDR})
			}
		default:
			return nil, fmt.Errorf("feed %s: unknown format %q", id, format)
		}
	}
	if err := sc.Err(); err != nil {
		return l, err
	}
	return l, nil
}

func (l *feedList) addIP(fields []string) {
	if len(fields) == 0 {
		return
	}
	s := fields[0]
	if p, err := netip.ParsePrefix(s); err == nil {
		if p.Bits() == p.Addr().BitLen() {
			l.addrs[p.Addr().Unmap()] = struct{}{}
		} else {
			l.nets = append(l.nets, p.Masked())
		}
		return
	}
	if a, err := netip.ParseAddr(s); err == nil {
		l.addrs[a.Unmap()] = struct{}{}
	}
}

// Feeds holds every downloaded threat list and keeps them fresh.
type Feeds struct {
	mu    sync.RWMutex
	lists map[string]*feedList
	state map[string]*v1.EgressFeedState
	specs map[string]*v1.EgressFeed
	// fetch downloads a feed; a test swaps it.
	fetch func(ctx context.Context, url string) (io.ReadCloser, error)
	// next is when each feed is due again.
	next map[string]time.Time
}

func NewFeeds(client *http.Client) *Feeds {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Feeds{
		lists: map[string]*feedList{},
		state: map[string]*v1.EgressFeedState{},
		specs: map[string]*v1.EgressFeed{},
		next:  map[string]time.Time{},
		fetch: func(ctx context.Context, url string) (io.ReadCloser, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "silo-egress (threat list refresh)")
			res, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			if res.StatusCode/100 != 2 {
				res.Body.Close()
				return nil, fmt.Errorf("http %d", res.StatusCode)
			}
			return res.Body, nil
		},
	}
}

// SetSpecs replaces which feeds are wanted. A feed no longer wanted is
// dropped at once; a new one is due now.
func (f *Feeds) SetSpecs(specs []*v1.EgressFeed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]*v1.EgressFeed{}
	for _, s := range specs {
		if s.GetId() == "" || s.GetUrl() == "" {
			continue
		}
		want[s.GetId()] = s
		if old, ok := f.specs[s.GetId()]; !ok || old.GetUrl() != s.GetUrl() || old.GetFormat() != s.GetFormat() {
			f.next[s.GetId()] = time.Time{}
		}
	}
	for id := range f.specs {
		if _, ok := want[id]; !ok {
			delete(f.lists, id)
			delete(f.state, id)
			delete(f.next, id)
		}
	}
	f.specs = want
}

// Refresh downloads every feed that is due, one at a time.
func (f *Feeds) Refresh(ctx context.Context, now time.Time) {
	f.mu.RLock()
	var due []*v1.EgressFeed
	for id, s := range f.specs {
		if !now.Before(f.next[id]) {
			due = append(due, s)
		}
	}
	f.mu.RUnlock()
	for _, s := range due {
		if ctx.Err() != nil {
			return
		}
		f.load(ctx, s, now)
	}
}

func (f *Feeds) load(ctx context.Context, s *v1.EgressFeed, now time.Time) {
	list, err := func() (*feedList, error) {
		body, err := f.fetch(ctx, s.GetUrl())
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return parseFeed(s.GetId(), s.GetFormat(), io.LimitReader(body, feedMaxBytes))
	}()
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.specs[s.GetId()]; !ok {
		return
	}
	st := f.state[s.GetId()]
	if st == nil {
		st = &v1.EgressFeedState{Id: s.GetId()}
		f.state[s.GetId()] = st
	}
	// An empty list is a broken download, not a clean internet: keep the old.
	if err == nil && list.size() == 0 {
		err = fmt.Errorf("the list came back empty")
	}
	if err != nil {
		st.Error = err.Error()
		f.next[s.GetId()] = now.Add(feedRetry)
		return
	}
	f.lists[s.GetId()] = list
	st.Error = ""
	st.Entries = int32(list.size())
	st.UpdatedAt = now.Unix()
	f.next[s.GetId()] = now.Add(feedRefresh)
}

// States reports each feed's health.
func (f *Feeds) States() []*v1.EgressFeedState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*v1.EgressFeedState, 0, len(f.specs))
	for id := range f.specs {
		st := f.state[id]
		if st == nil {
			st = &v1.EgressFeedState{Id: id}
		}
		out = append(out, &v1.EgressFeedState{Id: st.Id, Entries: st.Entries, UpdatedAt: st.UpdatedAt, Error: st.Error})
	}
	return out
}

// Host reports which feed lists host, if any.
func (f *Feeds) Host(host string) (string, bool) {
	host = NormHost(host)
	if host == "" {
		return "", false
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return f.Addr(a)
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, l := range f.lists {
		if _, ok := l.hosts[host]; ok {
			return l.id, true
		}
		for h := host; ; {
			if _, ok := l.domains[h]; ok {
				return l.id, true
			}
			i := strings.IndexByte(h, '.')
			if i < 0 {
				break
			}
			h = h[i+1:]
		}
	}
	return "", false
}

// Addr reports which feed lists the address, if any.
func (f *Feeds) Addr(a netip.Addr) (string, bool) {
	a = a.Unmap()
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, l := range f.lists {
		if _, ok := l.addrs[a]; ok {
			return l.id, true
		}
		for _, p := range l.nets {
			if p.Contains(a) {
				return l.id, true
			}
		}
	}
	return "", false
}
