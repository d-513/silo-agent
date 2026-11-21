package search

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var seznamURL = "https://search.seznam.cz/"

// seznam is an independent index (the Czech engine, which crawls the whole
// web). Its class names are generated, so the page is read by what its
// elements are for: a result's heading link, and the block that follows it.
func seznam(ctx context.Context, q Query) ([]Hit, error) {
	body, err := get(ctx, q, request{url: seznamURL, query: url.Values{"q": {q.Text}}})
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	links := doc.Find("h3 a[data-e-a='heading']")
	if links.Length() == 0 {
		// "Unfortunately I found nothing."
		if strings.Contains(doc.Text(), "nic nenašel") {
			return nil, nil
		}
		return nil, errBlocked
	}
	var out []Hit
	links.Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		title := text(a)
		if title == "" || !strings.HasPrefix(href, "http") {
			return
		}
		// The snippet is the first text block after the heading and the
		// address line, inside the result.
		snippet := text(a.Closest("h3").Parent().ChildrenFiltered("div[data-e-b-n]").First().Find("span").First())
		out = append(out, Hit{Title: title, URL: href, Snippet: snippet})
	})
	return out, nil
}
