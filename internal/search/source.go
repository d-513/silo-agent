package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// source is one site the Built-in Scraper asks. Sources that read the same
// index share a provider and count as one voice.
type source struct {
	name     string
	provider string
	// fetch answers the hits of the first page, best first. A page that truly
	// has no results is (nil, nil); a refusal is errBlocked.
	fetch func(ctx context.Context, q Query) ([]Hit, error)
	// skip, when set, leaves the source out of a query it cannot serve.
	skip func(q Query) bool
	// api marks a public API, asked on every search. The others are pages
	// scraped behind a browser's face: a search asks only a few of them
	// (fanout) and keeps the rest in reserve.
	api bool
	// limit is how often the site may be asked; zero is no limit.
	limit limit
}

// errBlocked is a source refusing to answer: a rate limit, a captcha, or a
// page with neither results nor the site's own "no results" notice.
var errBlocked = errors.New("blocked")

// errBusy is a source whose next turn is too far off to wait for. It says
// nothing about the site's health.
var errBusy = errors.New("busy")

// refused is a site turning a request away by its status. It is errBlocked.
type refused struct {
	status int
	// limited is the site saying "too many requests" (429, DuckDuckGo's 202,
	// a 503): asking again soon makes it worse, so the backoff starts longer.
	limited bool
	// after is the Retry-After the site sent, if any.
	after time.Duration
}

func (r *refused) Error() string        { return fmt.Sprintf("blocked: HTTP %d", r.status) }
func (r *refused) Is(target error) bool { return target == errBlocked }

// rateLimited reports whether err is a site saying "too many requests", and
// how long it asked to be left alone.
func rateLimited(err error) (after time.Duration, ok bool) {
	var r *refused
	if errors.As(err, &r) && r.limited {
		return r.after, true
	}
	return 0, false
}

// scraped is the budget of a scraped site. The sites publish no limit; this
// is a guess from what they tolerated: DuckDuckGo answered four requests in
// three seconds and then 202 to everything, Brave 429 after a dozen in a few
// minutes. Ten a minute with at most two together has to be earned back with
// patience, and a block costs far more than a skipped site.
var scraped = limit{every: 6 * time.Second, burst: 2}

// sources is every site asked, in no particular order.
var sources = []source{
	{name: "duckduckgo", provider: "duckduckgo", fetch: duckduckgo, limit: scraped},
	{name: "mojeek", provider: "mojeek", fetch: mojeek, limit: scraped},
	{name: "brave", provider: "brave", fetch: brave, limit: scraped},
	{name: "startpage", provider: "startpage", fetch: startpage, limit: scraped},
	{name: "wikipedia", provider: "wikipedia", fetch: wikipedia, api: true},
	// The public key's quota is everybody's: take little of it.
	{name: "marginalia", provider: "marginalia", fetch: marginalia, api: true, skip: notEnglish, limit: limit{every: 2 * time.Second, burst: 2}},
}

func sourceNames() []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.name
	}
	return out
}

func notEnglish(q Query) bool { return q.Language != "" && q.Language != "en" }

// get fetches a page and reads its status: a refusal is errBlocked, any other
// failure a plain error.
func get(ctx context.Context, q Query, rq request) ([]byte, error) {
	rq.country, rq.lang = region(q)
	p, err := fetch(ctx, rq)
	if err != nil {
		return nil, err
	}
	switch p.status {
	case http.StatusOK:
		return p.body, nil
	case http.StatusAccepted, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return nil, &refused{status: p.status, limited: true, after: p.retryAfter}
	case http.StatusForbidden:
		return nil, &refused{status: p.status}
	}
	return nil, fmt.Errorf("HTTP %d", p.status)
}

func parse(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

// text is a node's text on one line.
func text(s *goquery.Selection) string {
	return strings.Join(strings.Fields(s.Text()), " ")
}

// langCountry is the country a language is searched in when the query names
// none, where it is not spelled like the language.
var langCountry = map[string]string{
	"en": "us", "ja": "jp", "zh": "cn", "ko": "kr", "cs": "cz", "da": "dk", "sv": "se", "el": "gr",
	"uk": "ua", "he": "il", "hi": "in", "et": "ee", "sl": "si", "sr": "rs", "ca": "es", "vi": "vn",
	"fa": "ir", "ar": "sa", "nb": "no", "ms": "my", "sq": "al", "ka": "ge", "hy": "am", "be": "by",
}

// countryLang is the language of a country when the query names none.
var countryLang = map[string]string{
	"us": "en", "gb": "en", "uk": "en", "au": "en", "ca": "en", "ie": "en", "nz": "en", "in": "en",
	"at": "de", "ch": "de", "be": "nl", "br": "pt", "mx": "es", "ar": "es", "jp": "ja", "cn": "zh",
	"kr": "ko", "cz": "cs", "dk": "da", "se": "sv", "gr": "el", "ua": "uk", "il": "he",
}

// region fills in whichever of country and language a query left out. Both
// are empty when the query names neither: the search is then worldwide.
func region(q Query) (country, lang string) {
	country, lang = q.Country, q.Language
	if country == "" && lang == "" {
		return "", ""
	}
	if country == "" {
		if country = langCountry[lang]; country == "" && len(lang) == 2 {
			country = lang
		}
	}
	if lang == "" {
		if lang = countryLang[country]; lang == "" {
			lang = country
		}
	}
	return country, lang
}
