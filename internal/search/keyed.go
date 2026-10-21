package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// The keyed engines (Brave Search, Exa) are one provider's own index behind
// the operator's API key. Unlike the Built-in Scraper there is no one to be
// polite to but the plan's quota, and a failure says what it is: a rejected
// key or an empty account is the operator's to fix, and nothing a retry or
// another source could cover.

// retryWait caps how long a 429 is waited out before the one retry.
const retryWait = 3 * time.Second

// keyedCall is one request to a keyed API and what it answers when it works.
type keyedCall struct {
	// name is the engine as a person reads it ("Brave Search").
	name string
	rq   request
}

// call performs c. A 429 is retried once after the wait the API asked for
// (Retry-After, else a second); everything else that is not a 200 is an error
// that names the engine and never carries the key.
func (c keyedCall) call(ctx context.Context) (page, error) {
	c.rq.keyed = true
	for attempt := 0; ; attempt++ {
		p, err := fetch(ctx, c.rq)
		if err != nil {
			return page{}, fmt.Errorf("%s: %w", c.name, scrub(err, c.rq))
		}
		switch p.status {
		case http.StatusOK:
			return p, nil
		case http.StatusTooManyRequests:
			if attempt == 0 {
				wait := p.retryAfter
				if wait <= 0 {
					wait = time.Second
				}
				if wait > retryWait {
					wait = retryWait
				}
				select {
				case <-time.After(wait):
					continue
				case <-ctx.Done():
					return page{}, ctx.Err()
				}
			}
			return page{}, fmt.Errorf("%s is rate limited: the plan's request rate or quota is used up", c.name)
		case http.StatusUnauthorized, http.StatusForbidden:
			return page{}, fmt.Errorf("%s rejected the API key (HTTP %d): check search.%s.api_key", c.name, p.status, c.rq.keyName())
		case http.StatusPaymentRequired:
			return page{}, fmt.Errorf("%s has no credit left on this account (HTTP 402)", c.name)
		}
		return page{}, &httpError{name: c.name, status: p.status, detail: apiMessage(p.body)}
	}
}

// httpError is a keyed API answering with a status that has no meaning of its
// own here (a 422 for a parameter it does not take, a 5xx).
type httpError struct {
	name   string
	status int
	detail string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("%s answered HTTP %d%s", e.name, e.status, e.detail)
}

// keyName is the engine id a request belongs to, for the hint in an error.
func (r request) keyName() string {
	switch {
	case strings.Contains(r.url, "brave.com"):
		return "brave"
	case strings.Contains(r.url, "exa.ai"):
		return "exa"
	}
	return "<engine>"
}

// apiMessage is the error text a JSON error body carries, as ": text", or
// nothing.
func apiMessage(body []byte) string {
	var e struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = e.Detail
	}
	if msg == "" && len(e.Error) > 0 {
		var s string
		if json.Unmarshal(e.Error, &s) == nil {
			msg = s
		} else {
			var o struct{ Message string }
			_ = json.Unmarshal(e.Error, &o)
			msg = o.Message
		}
	}
	if msg = strings.Join(strings.Fields(msg), " "); msg == "" {
		return ""
	}
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	return ": " + msg
}

// scrub removes the API key from a transport error (a URL in it could carry
// one; ours go in headers, but an error must never be the place to find out).
func scrub(err error, rq request) error {
	msg := err.Error()
	for _, v := range rq.headers {
		if len(v) >= 8 {
			msg = strings.ReplaceAll(msg, v, "…")
		}
	}
	return fmt.Errorf("%s", msg)
}

// keyed wraps a search function with what the keyed engines share: the
// cache (a paid answer is kept for cacheTTL and identical concurrent
// queries share one request) and the empty-query check.
type keyed struct {
	id   string
	do   func(ctx context.Context, q Query) ([]Hit, error)
	keep *cache
}

func newKeyed(id string, do func(ctx context.Context, q Query) ([]Hit, error)) *keyed {
	return &keyed{id: id, do: do, keep: sharedCache}
}

func (k *keyed) Search(ctx context.Context, q Query) (Result, error) {
	q = cleanQuery(q)
	if q.Text == "" {
		return Result{}, fmt.Errorf("query required")
	}
	// An answer is cut to the number asked for at the source, so the count
	// is part of the key; the engine keeps answers of different engines apart.
	key := fmt.Sprintf("%s\x00%d\x00%s", k.id, q.Max, cacheKey(q))
	var failure error
	f := k.keep.do(key, func() found {
		hits, err := k.do(context.WithoutCancel(ctx), q)
		if err != nil {
			failure = err
			// blocked with no hits is what keeps a failure out of the cache.
			return found{blocked: []string{k.id}}
		}
		return found{hits: hits}
	})
	if failure != nil {
		return Result{}, failure
	}
	if len(f.hits) == 0 && len(f.blocked) > 0 {
		// Another caller's request failed while this one waited on it.
		return Result{}, fmt.Errorf("search failed: try again")
	}
	if len(f.hits) > q.Max {
		f.hits = f.hits[:q.Max]
	}
	if f.hits == nil {
		f.hits = []Hit{}
	}
	return Result{Results: f.hits}, nil
}

// plainText is a snippet's text with the markup an API puts in it (Brave
// wraps matches in <strong>) removed and entities decoded.
func plainText(s string) string {
	if !strings.ContainsAny(s, "<&") {
		return strings.Join(strings.Fields(s), " ")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<body>" + s + "</body>"))
	if err != nil {
		return strings.Join(strings.Fields(s), " ")
	}
	return text(doc.Find("body"))
}
