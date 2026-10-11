package search

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var mojeekURL = "https://www.mojeek.com/search"

// mojeek is an independent index. Region and language are cookies there.
func mojeek(ctx context.Context, q Query) ([]Hit, error) {
	rq := request{url: mojeekURL, query: url.Values{"q": {q.Text}}}
	if country, lang := region(q); country != "" {
		rq.cookies = map[string]string{"arc": country, "lb": lang}
	}
	body, err := get(ctx, q, rq)
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	rows := doc.Find("ul.results-standard > li")
	if rows.Length() == 0 {
		if strings.Contains(doc.Find(".results, .result-col").Text(), "No pages found") {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	rows.Each(func(_ int, row *goquery.Selection) {
		a := row.Find("h2 a").First()
		href, _ := a.Attr("href")
		if title := text(a); title != "" && href != "" {
			out = append(out, Hit{Title: title, URL: href, Snippet: text(row.Find("p.s").First())})
		}
	})
	return out, nil
}
