package search

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var yahooURL = "https://search.yahoo.com/search"

// yahoo serves Bing's index as a page that needs no script. The language is
// a parameter and a preference cookie there.
func yahoo(ctx context.Context, q Query) ([]Hit, error) {
	rq := request{url: yahooURL, query: url.Values{"p": {q.Text}}}
	if _, lang := region(q); lang != "" {
		rq.query.Set("vl", "lang_"+lang)
		rq.cookies = map[string]string{"sB": "v=1&vm=p&fl=1&vl=lang_" + lang + "&pn=10&rw=new&userset=1"}
	}
	body, err := get(ctx, q, rq)
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	rows := doc.Find("div.algo-sr")
	if rows.Length() == 0 {
		// The page with no results still has the results column, empty.
		if doc.Find("#results #web").Length() > 0 {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	rows.Each(func(_ int, row *goquery.Selection) {
		a := row.Find(".compTitle a").First()
		href, _ := a.Attr("href")
		ref := decodeYahooURL(href)
		title := text(a.Find("h3").First())
		if title == "" || ref == "" {
			return
		}
		out = append(out, Hit{Title: title, URL: ref, Snippet: text(row.Find(".compText").First())})
	})
	return out, nil
}

// decodeYahooURL unwraps Yahoo's redirect link (…/RU=<the page>/RK=…).
func decodeYahooURL(href string) string {
	href = strings.TrimSpace(href)
	if _, rest, ok := strings.Cut(href, "/RU="); ok {
		rest, _, _ = strings.Cut(rest, "/")
		if v, err := url.QueryUnescape(rest); err == nil {
			href = v
		}
	}
	if !strings.HasPrefix(href, "http") {
		return ""
	}
	return href
}
