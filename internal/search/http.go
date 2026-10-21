package search

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/imroc/req/v3/http2"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/publicsuffix"
)

// bodyMax caps a page a source answers with.
const bodyMax = 2 << 20

// request is one page a source asks for.
type request struct {
	method  string
	url     string
	query   url.Values
	form    url.Values
	headers map[string]string
	cookies map[string]string
	// lang is the query's language, for Accept-Language.
	lang, country string
	// api marks a public API rather than a page: it is asked as what we are,
	// not as a browser (Wikipedia refuses a client that only looks like one).
	api bool
	// keyed marks a paid API asked with the operator's key: it gets the time
	// a real index needs, not the budget of a scraped page. It implies api.
	keyed bool
	// body is a raw request body (JSON for the keyed APIs).
	body []byte
}

type page struct {
	status  int
	body    []byte
	cookies []*http.Cookie
	// retryAfter is how long the site asked to be left alone, when it said.
	retryAfter time.Duration
}

// The browser the scraped sites see is Chrome 145 on macOS, in every layer a
// site can look at: the TLS hello (utls's Chrome 133 hello is still what 145
// sends), the HTTP/2 SETTINGS and window, the order of the pseudo-headers and
// headers, and the headers themselves. A client that is Chrome in one layer
// and something else in another is exactly what bot detection looks for:
// the first version of this said "Chrome 133" with the SETTINGS frame of
// Chrome 110, "accept-encoding: gzip" and the no-cache pair of a hard reload.
//
// `SILO_SEARCH_LIVE=1 go test ./internal/search -run TestFingerprint -v`
// prints what a fingerprint echo service sees, to compare with a real Chrome
// when this is moved to a newer version.
const (
	chromeUA      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	chromeBrands  = `"Not:A-Brand";v="99", "Google Chrome";v="145", "Chromium";v="145"`
	chromeAccept  = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	chromeEncoded = "gzip, deflate, br, zstd"
)

// chromeOrder is the order Chrome writes its headers in, a form post's
// extras included.
var chromeOrder = []string{
	"content-length", "cache-control",
	"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
	"origin", "content-type",
	"upgrade-insecure-requests", "user-agent", "accept",
	"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest",
	"referer", "accept-encoding", "accept-language", "cookie", "priority",
}

// browser keeps no cookies: a query's region cookies must not reach the next
// query.
var browser = req.C().
	SetTLSFingerprint(utls.HelloChrome_133).
	SetHTTP2SettingsFrame(
		http2.Setting{ID: http2.SettingHeaderTableSize, Val: 65536},
		http2.Setting{ID: http2.SettingEnablePush, Val: 0},
		http2.Setting{ID: http2.SettingInitialWindowSize, Val: 6291456},
		http2.Setting{ID: http2.SettingMaxHeaderListSize, Val: 262144},
	).
	SetHTTP2ConnectionFlow(15663105).
	SetHTTP2HeaderPriority(http2.PriorityParam{StreamDep: 0, Exclusive: true, Weight: 255}).
	SetCommonPseudoHeaderOder(":method", ":authority", ":scheme", ":path").
	SetCommonHeaderOrder(chromeOrder...).
	SetCommonHeaders(map[string]string{
		"sec-ch-ua":                 chromeBrands,
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"macOS"`,
		"upgrade-insecure-requests": "1",
		"user-agent":                chromeUA,
		"accept":                    chromeAccept,
		"sec-fetch-mode":            "navigate",
		"sec-fetch-user":            "?1",
		"sec-fetch-dest":            "document",
		"accept-encoding":           chromeEncoded,
		"priority":                  "u=0, i",
	}).
	EnableAutoDecompress().
	SetCookieJar(nil).
	SetTimeout(sourceTimeout).
	DisableAutoReadResponse()

// plain asks public APIs, under an honest name.
var plain = req.C().
	SetUserAgent("SiloAgent/1.0 (web search for an AI agent)").
	SetCookieJar(nil).
	SetTimeout(sourceTimeout).
	DisableAutoReadResponse()

// keyedTimeout is how long a paid API may take: Exa reads the pages it
// returns before it answers.
const keyedTimeout = 15 * time.Second

// paid asks the keyed APIs, under an honest name.
var paid = req.C().
	SetUserAgent("SiloAgent/1.0 (web search for an AI agent)").
	SetCookieJar(nil).
	SetTimeout(keyedTimeout).
	DisableAutoReadResponse()

// fetch performs one request. Tests replace it.
var fetch = func(ctx context.Context, rq request) (page, error) {
	client := browser
	switch {
	case rq.keyed:
		rq.api, client = true, paid
	case rq.api:
		client = plain
	}
	r := client.R().SetContext(ctx)
	if !rq.api {
		r.SetHeader("accept-language", acceptLanguage(rq.lang, rq.country))
		for k, v := range navigation(rq) {
			r.SetHeader(k, v)
		}
	}
	for k, v := range rq.headers {
		r.SetHeader(k, v)
	}
	for k, v := range rq.cookies {
		r.SetCookies(&http.Cookie{Name: k, Value: v})
	}
	if rq.query != nil {
		r.SetQueryParamsFromValues(rq.query)
	}
	if rq.form != nil {
		r.SetFormDataFromValues(rq.form)
	}
	if rq.body != nil {
		r.SetBodyBytes(rq.body)
	}
	method := rq.method
	if method == "" {
		method = http.MethodGet
	}
	resp, err := r.Send(method, rq.url)
	if err != nil {
		return page{}, err
	}
	if resp.Response == nil || resp.Body == nil {
		return page{}, fmt.Errorf("no response")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyMax))
	if err != nil {
		return page{}, err
	}
	return page{status: resp.StatusCode, body: body, cookies: resp.Cookies(), retryAfter: retryAfter(resp.Header.Get("Retry-After"))}, nil
}

// navigation is the headers that depend on how a browser got to the page:
// typed into the address bar (no referer, "none"), or a link or a form on the
// page named by the referer. A form post also names its origin and, in
// Chrome, carries "cache-control: max-age=0".
func navigation(rq request) map[string]string {
	ref := rq.headers["referer"]
	if ref == "" {
		return map[string]string{"sec-fetch-site": "none"}
	}
	out := map[string]string{"sec-fetch-site": "cross-site"}
	from, err1 := url.Parse(ref)
	to, err2 := url.Parse(rq.url)
	if err1 == nil && err2 == nil {
		a, _ := publicsuffix.EffectiveTLDPlusOne(from.Hostname())
		b, _ := publicsuffix.EffectiveTLDPlusOne(to.Hostname())
		switch {
		case from.Scheme == to.Scheme && from.Host == to.Host:
			out["sec-fetch-site"] = "same-origin"
		case a != "" && a == b:
			out["sec-fetch-site"] = "same-site"
		}
		if rq.method == http.MethodPost {
			out["origin"] = from.Scheme + "://" + from.Host
			out["cache-control"] = "max-age=0"
		}
	}
	return out
}

// retryAfter reads a Retry-After header given in seconds; a date or nothing
// is 0.
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func acceptLanguage(lang, country string) string {
	if lang == "" || lang == "en" {
		return "en-US,en;q=0.9"
	}
	if country == "" {
		country = lang
	}
	return lang + "-" + strings.ToUpper(country) + "," + lang + ";q=0.9,en;q=0.8"
}
