package search

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// DuckDuckGo's two script-free pages.
var (
	ddgHTMLURL = "https://html.duckduckgo.com/html/"
	ddgLiteURL = "https://lite.duckduckgo.com/lite/"
)

// ddgPages is the health of each page on its own, under the source's.
var ddgPages = newBreaker(time.Now)

// duckduckgo asks the html page and, when that fails, the lite one. They read
// one index and share one rate limit (a few requests in a row from an address
// and both answer 202), so asking both at once would only spend it twice as
// fast, and asking the second after the first was rate-limited is one more
// request against a limit already hit. The lite page is there for when the
// html page's markup or host is the problem.
func duckduckgo(ctx context.Context, q Query) ([]Hit, error) {
	if !ddgPages.open("html") {
		hits, err := ddgHTML(ctx, q)
		if err == nil {
			ddgPages.ok("html")
			return hits, nil
		}
		if _, limited := rateLimited(err); limited || ctx.Err() != nil {
			return nil, err
		}
		ddgPages.fail("html", 0)
	}
	return ddgLite(ctx, q)
}

func ddgForm(q Query) url.Values {
	kl := "wt-wt"
	if country, lang := region(q); country != "" {
		kl = country + "-" + lang
	}
	return url.Values{"q": {q.Text}, "b": {""}, "kl": {kl}}
}

func ddgHTML(ctx context.Context, q Query) ([]Hit, error) {
	body, err := get(ctx, q, request{method: http.MethodPost, url: ddgHTMLURL, form: ddgForm(q),
		headers: map[string]string{"referer": "https://html.duckduckgo.com/"}})
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	rows := doc.Find(".result").Not(".result--ad, .result--more, .result--no-result")
	if rows.Length() == 0 {
		if doc.Find(".no-results, .result--no-result").Length() > 0 {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	rows.Each(func(_ int, row *goquery.Selection) {
		a := row.Find(".result__a").First()
		href, _ := a.Attr("href")
		if h, ok := ddgHit(text(a), href, text(row.Find(".result__snippet").First())); ok {
			out = append(out, h)
		}
	})
	return out, nil
}

func ddgLite(ctx context.Context, q Query) ([]Hit, error) {
	body, err := get(ctx, q, request{method: http.MethodPost, url: ddgLiteURL, form: ddgForm(q),
		headers: map[string]string{"referer": "https://lite.duckduckgo.com/"}})
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	links := doc.Find("a.result-link")
	if links.Length() == 0 {
		// The page with no results still carries the search form and says so.
		if doc.Find("form[action='/lite/']").Length() > 0 && strings.Contains(doc.Text(), "No results") {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	links.Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		// A result is a table row with the link, and the next row its snippet.
		snippet := text(a.Closest("tr").Next().Find(".result-snippet").First())
		if h, ok := ddgHit(text(a), href, snippet); ok {
			out = append(out, h)
		}
	})
	return out, nil
}

// ddgHit builds a hit from a DuckDuckGo link, leaving out its ads.
func ddgHit(title, href, snippet string) (Hit, bool) {
	ref := decodeDDGURL(href)
	if title == "" || ref == "" || strings.Contains(ref, "duckduckgo.com/y.js") {
		return Hit{}, false
	}
	return Hit{Title: title, URL: ref, Snippet: snippet}, true
}

// decodeDDGURL unwraps DuckDuckGo's redirect link (/l/?uddg=<the page>).
func decodeDDGURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	raw := href
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return href
	}
	if v := u.Query().Get("uddg"); v != "" {
		return v
	}
	return raw
}
