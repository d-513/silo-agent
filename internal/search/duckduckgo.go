package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	duckDuckGoUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	duckDuckGoMaxBody = 1 << 20
)

var (
	HTTPClient    = &http.Client{Timeout: 15 * time.Second}
	DuckDuckGoURL = "https://html.duckduckgo.com/html/"
)

type DuckDuckGo struct{}

func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{}
}

func (d *DuckDuckGo) Search(ctx context.Context, query string, maxResults int) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, fmt.Errorf("query required")
	}
	maxResults = ClampMax(maxResults)
	u, err := url.Parse(DuckDuckGoURL)
	if err != nil {
		return Result{}, fmt.Errorf("duckduckgo: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("duckduckgo: %w", err)
	}
	req.Header.Set("User-Agent", duckDuckGoUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	res, err := HTTPClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("duckduckgo: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Result{}, fmt.Errorf("duckduckgo: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, duckDuckGoMaxBody+1))
	if err != nil {
		return Result{}, fmt.Errorf("duckduckgo: %w", err)
	}
	if len(body) > duckDuckGoMaxBody {
		return Result{}, fmt.Errorf("duckduckgo: response too large")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return Result{}, fmt.Errorf("duckduckgo: parse: %w", err)
	}
	sel := doc.Find(".web-result")
	if sel.Length() == 0 {
		sel = doc.Find(".result").Not(".result--ad, .result--more")
	}
	if sel.Length() == 0 {
		return Result{}, fmt.Errorf("duckduckgo: no results markup (blocked or layout changed)")
	}

	out := Result{Results: make([]Hit, 0, maxResults)}
	sel.EachWithBreak(func(_ int, node *goquery.Selection) bool {
		if len(out.Results) >= maxResults {
			return false
		}
		titleNode := node.Find(".result__a").First()
		title := strings.TrimSpace(titleNode.Text())
		href, _ := titleNode.Attr("href")
		ref := decodeDDGURL(href)
		if title == "" || ref == "" {
			return true
		}
		snippet := strings.TrimSpace(node.Find(".result__snippet").First().Text())
		out.Results = append(out.Results, Hit{Title: title, URL: ref, Snippet: snippet})
		return true
	})
	return out, nil
}

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
	return href
}
