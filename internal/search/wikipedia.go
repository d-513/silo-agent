package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// wikipediaURL takes the language as its subdomain.
var wikipediaURL = "https://%s.wikipedia.org/w/api.php"

// wikipediaHits is how many articles are taken: the source is there to put
// the encyclopedia's page for a subject among the results, not to fill them.
const wikipediaHits = 3

// wikipedia looks for articles of the query's language whose title matches
// the query, and answers them with their opening sentences, in one request.
// It matches titles, not text: a full-text search answers a long question
// with articles that merely share its words, and a source's first hit weighs
// as much as any other's.
func wikipedia(ctx context.Context, q Query) ([]Hit, error) {
	lang := q.Language
	if lang == "" {
		lang = "en"
	}
	base := fmt.Sprintf(wikipediaURL, lang)
	n := fmt.Sprint(wikipediaHits)
	body, err := get(ctx, q, request{api: true, url: base, query: url.Values{
		"action": {"query"}, "format": {"json"}, "utf8": {"1"}, "redirects": {"1"},
		"generator": {"prefixsearch"}, "gpssearch": {q.Text}, "gpslimit": {n}, "gpsprofile": {"fuzzy"},
		"prop": {"extracts|pageprops"}, "ppprop": {"disambiguation"},
		"exintro": {"1"}, "explaintext": {"1"}, "exsentences": {"2"}, "exlimit": {n},
	}})
	if err != nil {
		return nil, err
	}
	var res struct {
		// Present in every answer, with or without matches.
		BatchComplete *string `json:"batchcomplete"`
		Query         struct {
			Pages map[string]struct {
				Title     string         `json:"title"`
				Index     int            `json:"index"`
				Extract   string         `json:"extract"`
				PageProps map[string]any `json:"pageprops"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &res); err != nil || (res.BatchComplete == nil && res.Query.Pages == nil) {
		return nil, fmt.Errorf("%w: not a search answer", errBlocked)
	}
	site := strings.TrimSuffix(base, "/w/api.php")
	type indexed struct {
		Hit
		index int
	}
	var found []indexed
	for _, p := range res.Query.Pages {
		// A disambiguation page is a list of other pages, not an answer.
		if _, list := p.PageProps["disambiguation"]; list || p.Title == "" {
			continue
		}
		found = append(found, indexed{index: p.Index, Hit: Hit{
			Title:   p.Title + " - Wikipedia",
			URL:     site + "/wiki/" + url.PathEscape(strings.ReplaceAll(p.Title, " ", "_")),
			Snippet: strings.Join(strings.Fields(p.Extract), " "),
		}})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].index < found[j].index })
	out := make([]Hit, len(found))
	for i, f := range found {
		out[i] = f.Hit
	}
	return out, nil
}
