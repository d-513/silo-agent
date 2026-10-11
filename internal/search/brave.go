package search

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var braveURL = "https://search.brave.com/search"

// brave is an independent index. The country is a cookie there.
func brave(ctx context.Context, q Query) ([]Hit, error) {
	rq := request{url: braveURL, query: url.Values{"q": {q.Text}, "source": {"web"}}}
	if country, _ := region(q); country != "" {
		rq.cookies = map[string]string{"country": country, "useLocation": "0"}
	}
	body, err := get(ctx, q, rq)
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	rows := doc.Find("div[data-type='web']")
	if rows.Length() == 0 {
		if doc.Find("#results").Length() > 0 && !strings.Contains(strings.ToLower(doc.Text()), "captcha") {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	rows.Each(func(_ int, row *goquery.Selection) {
		a := row.Find("a").FilterFunction(func(_ int, a *goquery.Selection) bool {
			href, _ := a.Attr("href")
			return strings.HasPrefix(href, "http") && a.Find(".title").Length() > 0
		}).First()
		href, _ := a.Attr("href")
		title := text(a.Find(".title").First())
		if title == "" || href == "" {
			return
		}
		out = append(out, Hit{Title: title, URL: href, Snippet: text(row.Find(".snippet .content, .snippet-content").First())})
	})
	return out, nil
}
