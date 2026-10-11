package search

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	startpageHome = "https://www.startpage.com/"
	startpageURL  = "https://www.startpage.com/sp/search"
)

// scTTL is how long Startpage's form token is reused: fetching it is a round
// trip of its own, so a search is normally one.
const scTTL = 10 * time.Minute

var startpageSC struct {
	sync.Mutex
	value string
	at    time.Time
}

// startpageToken is the "sc" value the home page's search form carries.
func startpageToken(ctx context.Context, q Query) (string, error) {
	startpageSC.Lock()
	defer startpageSC.Unlock()
	if startpageSC.value != "" && time.Since(startpageSC.at) < scTTL {
		return startpageSC.value, nil
	}
	body, err := get(ctx, q, request{url: startpageHome})
	if err != nil {
		return "", err
	}
	doc, err := parse(body)
	if err != nil {
		return "", err
	}
	sc, _ := doc.Find("form#search input[name='sc'], form input[name='sc']").First().Attr("value")
	if sc == "" {
		return "", fmt.Errorf("%w: no form token", errBlocked)
	}
	startpageSC.value, startpageSC.at = sc, time.Now()
	return sc, nil
}

// startpage serves Google's results without Google's script wall.
func startpage(ctx context.Context, q Query) ([]Hit, error) {
	sc, err := startpageToken(ctx, q)
	if err != nil {
		return nil, err
	}
	form := url.Values{
		"query": {q.Text}, "cat": {"web"}, "t": {"device"}, "sc": {sc},
		"lui": {"english"}, "language": {"english"},
		"abp": {"1"}, "abd": {"0"}, "abe": {"0"}, "qadf": {"moderate"}, "segment": {"organic"},
	}
	if country, lang := region(q); country != "" {
		form.Set("qsr", lang+"_"+strings.ToUpper(country))
	}
	body, err := get(ctx, q, request{method: http.MethodPost, url: startpageURL, form: form,
		headers: map[string]string{"referer": "https://www.startpage.com/"}})
	if err != nil {
		return nil, err
	}
	doc, err := parse(body)
	if err != nil {
		return nil, err
	}
	rows := doc.Find("div.result").FilterFunction(func(_ int, row *goquery.Selection) bool {
		return row.ChildrenFiltered("a").Length() > 0
	})
	if rows.Length() == 0 {
		if strings.Contains(doc.Text(), "did not match any") || doc.Find(".no-results").Length() > 0 {
			return nil, nil
		}
		// A token Startpage no longer takes answers a page without results.
		startpageSC.Lock()
		startpageSC.value = ""
		startpageSC.Unlock()
		return nil, errBlocked
	}
	var out []Hit
	rows.Each(func(_ int, row *goquery.Selection) {
		href, _ := row.ChildrenFiltered("a").First().Attr("href")
		title := text(row.Find("h2").First())
		if title == "" || !strings.HasPrefix(href, "http") {
			return
		}
		out = append(out, Hit{Title: title, URL: href, Snippet: text(row.Find("p").First())})
	})
	return out, nil
}
