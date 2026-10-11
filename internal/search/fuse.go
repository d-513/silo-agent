package search

import (
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// rrfK is the constant of reciprocal rank fusion: a hit at rank r in one
	// provider's list adds 1/(rrfK+r) to its score.
	rrfK = 60
	// snippetMax caps a snippet, in runes: results are paid for on every
	// later turn of the conversation that reads them.
	snippetMax = 320
)

// ranked is one source's answer, best hit first.
type ranked struct {
	provider string
	hits     []Hit
}

// fuse merges the sources' lists into one: hits are matched on their canonical
// URL and ordered by reciprocal rank fusion over providers. Two sources of one
// provider count once, at the better rank, so Hit.Engines is how many
// independent providers agree.
func fuse(lists []ranked) []Hit {
	type entry struct {
		hit   Hit
		ranks map[string]int
		score float64
		best  int
	}
	byURL := map[string]*entry{}
	var order []*entry
	for _, l := range lists {
		rank := 0
		for _, h := range l.hits {
			key := canonical(h.URL)
			if key == "" || strings.TrimSpace(h.Title) == "" {
				continue
			}
			rank++
			e := byURL[key]
			if e == nil {
				e = &entry{hit: h, ranks: map[string]int{}}
				byURL[key] = e
				order = append(order, e)
			}
			if len(h.Snippet) > len(e.hit.Snippet) {
				e.hit.Snippet = h.Snippet
			}
			if strings.HasPrefix(h.URL, "https://") && !strings.HasPrefix(e.hit.URL, "https://") {
				e.hit.URL = h.URL
			}
			if r, ok := e.ranks[l.provider]; !ok || rank < r {
				e.ranks[l.provider] = rank
			}
		}
	}
	for _, e := range order {
		for _, r := range e.ranks {
			e.score += 1 / float64(rrfK+r)
			if e.best == 0 || r < e.best {
				e.best = r
			}
		}
		e.hit.Engines = len(e.ranks)
		e.hit.Snippet = clip(e.hit.Snippet, snippetMax)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.hit.Engines != b.hit.Engines {
			return a.hit.Engines > b.hit.Engines
		}
		return a.best < b.best
	})
	out := make([]Hit, len(order))
	for i, e := range order {
		out[i] = e.hit
	}
	return out
}

// tracking are query parameters that name a campaign, not a page.
var tracking = map[string]bool{
	"fbclid": true, "gclid": true, "msclkid": true, "igshid": true, "mc_cid": true, "mc_eid": true,
	"ref": true, "ref_src": true, "srsltid": true,
}

// canonical is the identity of a page for deduplication: the URL without its
// scheme (http and https are one page), "www.", default port, fragment,
// trailing slash and tracking parameters. It is never shown.
func canonical(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		host += ":" + p
	}
	q := u.Query()
	for k := range q {
		if tracking[strings.ToLower(k)] || strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	key := host + strings.TrimRight(u.EscapedPath(), "/")
	if enc := q.Encode(); enc != "" {
		key += "?" + enc
	}
	return key
}

// clip cuts s to at most n runes, on a rune boundary.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
