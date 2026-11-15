package reader

import (
	"bytes"

	readability "codeberg.org/readeck/go-readability/v2"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// The thresholds below are ours to tune: nobody publishes theirs.
const (
	// thinText is the visible text under which a page counts as a shell.
	thinText = 250
	// mainMin is the least the main-content extractor must find to be
	// believed; mainShare the least share of a long page's text.
	mainMin   = 200
	mainShare = 0.15
	// wallText is the most text a page may have and still be a bot wall
	// or a sign-in form: a real article that mentions a captcha is longer.
	wallText = 1500
	// rescueMin is the least text worth taking from a page's embedded data.
	rescueMin = 500
	// textMax caps one page's text.
	textMax = 1500000
)

// signals is what the HTML says about how it should be read.
type signals struct {
	// blocked: a bot wall stands where the page should be.
	blocked bool
	// login: a sign-in form stands where the page should be.
	login bool
	// thin: almost no visible text.
	thin bool
	// scripts is how many script elements the page has: a thin page with
	// none is just a short page.
	scripts int
	// alternate is the page's own markdown version, when it links one.
	alternate string
	// embedded is article text found in the page's JSON-LD; nextData what
	// a Next.js page carries for its scripts to draw.
	embedded, nextData string
}

// junk never carries the page's words.
const junk = "script, style, noscript, template, svg, canvas, iframe, object, embed, link, meta, head, " +
	"input, select, textarea, button, dialog, [hidden]"

// shell is the page chrome a reader does not want: the list Firecrawl removes
// for its main-content mode, plus the ARIA landmarks that say the same thing.
const shell = "header, footer, nav, aside, " +
	".header, .top, .navbar, #header, .footer, .bottom, #footer, " +
	".sidebar, .side, .aside, #sidebar, .modal, .popup, #modal, .overlay, " +
	".ad, .ads, .advert, #ad, .lang-selector, .language, #language-selector, " +
	".social, .social-media, .social-links, #social, " +
	".menu, .navigation, #nav, .breadcrumbs, #breadcrumbs, " +
	".share, #share, .widget, #widget, .cookie, #cookie, " +
	"[role=navigation], [role=banner], [role=contentinfo], [role=complementary]"

// chrome is the part of the shell that is never an article's own.
const chrome = "nav, footer, aside, [role=navigation], [role=contentinfo], [role=complementary]"

// content is what a shell element must not hold to be removed.
const content = "main, article, #main, [role=main]"

var wallTitles = []string{
	"just a moment", "attention required", "access denied", "are you a robot", "verify you are human",
	"verifying you are human", "pardon our interruption", "security check", "captcha", "request blocked",
	"bot verification", "human verification", "access to this page has been denied",
}

var wallMarks = []string{
	"cf-chl-", "/cdn-cgi/challenge-platform", "px-captcha", "captcha-delivery.com", "_incapsula_resource",
	"g-recaptcha", "h-captcha",
}

// decode makes a page UTF-8 from what its header and its own markup say.
func decode(body []byte, contentType string) []byte {
	rd, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return bytes.ToValidUTF8(body, []byte("�"))
	}
	out, err := io.ReadAll(rd)
	if err != nil && len(out) == 0 {
		return bytes.ToValidUTF8(body, []byte("�"))
	}
	return bytes.ToValidUTF8(out, []byte("�"))
}

// fromHTML reads a page's markup: its text as markdown, what it says about
// itself, and the signals that decide whether it has to be rendered.
func fromHTML(at *url.URL, src []byte, rq Request) (*doc, signals) {
	d := &doc{url: at.String(), site: at.Hostname(), kind: "page"}
	var sig signals
	root, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		sig.thin = true
		return d, sig
	}
	page := goquery.NewDocumentFromNode(root)
	if href, ok := page.Find("base[href]").First().Attr("href"); ok {
		if b, err := at.Parse(href); err == nil {
			at = b
		}
	}
	describe(page, d)
	sig.scripts = page.Find("script").Length()
	page.Find(`link[rel~="alternate"][type="text/markdown"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if u, err := at.Parse(s.AttrOr("href", "")); err == nil && u.Hostname() != "" {
			sig.alternate = u.String()
		}
		return sig.alternate == ""
	})
	sig.embedded = jsonLD(page)
	sig.nextData = nextData(page, at)
	password := page.Find(`input[type="password"]`).Length() > 0

	// The main content, as a reader mode would show it. Readability keeps the
	// page's own elements, so a code block stays a block and a table a table.
	var main string
	if !rq.FullPage {
		if art, err := readability.FromReader(bytes.NewReader(src), at); err == nil && art.Node != nil {
			// On a page that is not an article it returns the body,
			// navigation and all.
			goquery.NewDocumentFromNode(art.Node).Find(chrome).Remove()
			main = markdown(art.Node, at, rq.Links)
			d.title = first(d.title, art.Title())
			d.author = first(d.author, art.Byline())
			d.language = first(d.language, art.Language())
			d.site = first(art.SiteName(), d.site)
			if t, err := art.PublishedTime(); d.date == "" && err == nil && !t.IsZero() {
				d.date = t.Format("2006-01-02")
			}
		}
	}

	// The whole page, without what never carries words.
	page.Find(junk).Remove()
	body := page.Find("body").First()
	if body.Length() == 0 {
		body = page.Selection
	}
	visible := runes(strings.Join(strings.Fields(body.Text()), " "))
	sig.thin = visible < thinText
	sig.login = password && visible < wallText
	sig.blocked = wall(d.title, src, visible)

	switch {
	case rq.FullPage:
		d.text = nodes(body, at, rq.Links)
	case runes(main) >= mainMin && float64(runes(main)) >= mainShare*float64(visible):
		d.text, d.kind = main, "article"
	default:
		// The extractor found too little of the page: a listing, a docs
		// index, a product page. Take the page without its chrome, and
		// the whole of it when the chrome was most of it.
		whole := nodes(body, at, rq.Links)
		body.Find(shell).FilterFunction(func(_ int, s *goquery.Selection) bool {
			return !s.Is(content) && s.Find(content).Length() == 0
		}).Remove()
		d.text = nodes(body, at, rq.Links)
		if runes(d.text) < mainMin {
			d.text = whole
		}
		if runes(main) > runes(d.text) {
			d.text, d.kind = main, "article"
		}
	}
	return d, sig
}

// describe reads what a page says about itself in its head.
func describe(page *goquery.Document, d *doc) {
	meta := func(sel string) string {
		return strings.TrimSpace(page.Find(sel).First().AttrOr("content", ""))
	}
	d.title = first(meta(`meta[property="og:title"]`), strings.Join(strings.Fields(page.Find("title").First().Text()), " "))
	d.site = first(meta(`meta[property="og:site_name"]`), d.site)
	d.author = meta(`meta[name="author"]`)
	d.language = strings.TrimSpace(page.Find("html").First().AttrOr("lang", ""))
	if t := meta(`meta[property="article:published_time"]`); len(t) >= 10 {
		d.date = t[:10]
	}
}

// wall reports whether a bot check stands where the page should be. A title
// alone is not believed on a page with real text.
func wall(title string, src []byte, visible int) bool {
	if visible > wallText {
		return false
	}
	title = strings.ToLower(title)
	for _, w := range wallTitles {
		if strings.Contains(title, w) {
			return true
		}
	}
	if visible >= thinText {
		return false
	}
	low := bytes.ToLower(src)
	for _, m := range wallMarks {
		if bytes.Contains(low, []byte(m)) {
			return true
		}
	}
	return false
}

var md = converter.NewConverter(converter.WithPlugins(
	base.NewBasePlugin(),
	commonmark.NewCommonmarkPlugin(),
	table.NewTablePlugin(
		// A cell with a line break in it must not cost the whole table,
		// and padding every cell to its column's width is tokens for
		// nothing.
		table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
		table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
		table.WithHeaderPromotion(true),
		table.WithSkipEmptyRows(true),
	),
	strikethrough.NewStrikethroughPlugin(),
))

// nodes is the markdown of a selection's elements.
func nodes(sel *goquery.Selection, at *url.URL, links bool) string {
	var b strings.Builder
	for _, n := range sel.Nodes {
		b.WriteString(markdown(n, at, links))
		b.WriteString("\n\n")
	}
	return tidy(b.String())
}

// markdown converts one element. Links are their words unless asked for, and
// an image is its description: both cost a model more than they tell it.
func markdown(n *html.Node, at *url.URL, links bool) string {
	sel := goquery.NewDocumentFromNode(n).Selection
	sel.Find(junk).Remove()
	sel.Find("picture source").Remove()
	sel.Find("img").Each(func(_ int, s *goquery.Selection) {
		alt := strings.Join(strings.Fields(s.AttrOr("alt", "")), " ")
		src := first(s.AttrOr("src", ""), s.AttrOr("data-src", ""))
		u, err := at.Parse(src)
		switch {
		case links && src != "" && err == nil && !strings.HasPrefix(src, "data:"):
			s.SetAttr("src", u.String())
			s.RemoveAttr("srcset")
		case len(alt) >= 3:
			s.ReplaceWithNodes(&html.Node{Type: html.TextNode, Data: "[image: " + alt + "]"})
		default:
			s.Remove()
		}
	})
	sel.Find("a").Each(func(_ int, s *goquery.Selection) {
		href := strings.TrimSpace(s.AttrOr("href", ""))
		u, err := at.Parse(href)
		low := strings.ToLower(href)
		if !links || href == "" || err != nil || strings.HasPrefix(href, "#") || strings.HasPrefix(low, "javascript:") {
			s.ReplaceWithSelection(s.Contents())
			return
		}
		s.SetAttr("href", u.String())
	})
	out, err := md.ConvertNode(n)
	if err != nil {
		return tidy(sel.Text())
	}
	return tidy(string(out))
}

var (
	blankRuns  = regexp.MustCompile(`\n{3,}`)
	lineSpaces = regexp.MustCompile(`(?m)[ \t]+$`)
	skipLinks  = regexp.MustCompile(`(?im)^\[?skip to (main )?content\]?(\([^)]*\))?\s*$`)
)

var (
	// breakRuns are the line breaks a table cell's blocks become; one says
	// as much as ten.
	breakRuns = regexp.MustCompile(`<br ?/?>(?:[ \t]*<br ?/?>)*`)
	cellEdges = regexp.MustCompile(`(?:<br>)*( ?\| ?)(?:<br>)*`)
	fences    = regexp.MustCompile("(?s)```[^\n]*\n.*?\n```")
)

// closeUp takes the blank lines out of a code block that has one after every
// line: a page that draws each line of code as its own element.
func closeUp(block string) string {
	lines := strings.Split(block, "\n")
	body := lines[1 : len(lines)-1]
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	if len(body) < 4 {
		return block
	}
	for i := 1; i < len(body); i += 2 {
		if strings.TrimSpace(body[i]) != "" {
			return block
		}
	}
	out := []string{lines[0]}
	for i := 0; i < len(body); i += 2 {
		out = append(out, body[i])
	}
	return strings.Join(append(out, lines[len(lines)-1]), "\n")
}

// tidy is the last pass over a page's markdown.
func tidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = lineSpaces.ReplaceAllString(s, "")
	s = skipLinks.ReplaceAllString(s, "")
	s = breakRuns.ReplaceAllString(s, "<br>")
	s = cellEdges.ReplaceAllString(s, "$1")
	s = fences.ReplaceAllStringFunc(s, closeUp)
	s = blankRuns.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)
	if len(s) > textMax {
		s = s[:textMax]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// jsonLD is the article text a page carries in its structured data, which a
// script-built page often has in full while its body is empty.
func jsonLD(page *goquery.Document) string {
	best := ""
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 6 {
			return
		}
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				walk(x, depth+1)
			}
		case map[string]any:
			if s, ok := t["articleBody"].(string); ok && len(s) > len(best) {
				best = s
			}
			for _, x := range t {
				walk(x, depth+1)
			}
		}
	}
	page.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var v any
		if json.Unmarshal([]byte(s.Text()), &v) == nil {
			walk(v, 0)
		}
	})
	best = strings.TrimSpace(html.UnescapeString(best))
	if runes(best) < rescueMin {
		return ""
	}
	return tidy(best)
}

// nextData is the prose in a Next.js page's props: the long strings that
// read as text. It is a last resort, for a page that could not be rendered.
func nextData(page *goquery.Document, at *url.URL) string {
	raw := page.Find("script#__NEXT_DATA__").First().Text()
	if raw == "" {
		return ""
	}
	var v struct {
		Props struct {
			PageProps any `json:"pageProps"`
		} `json:"props"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	var parts []string
	total := 0
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		if depth > 12 || len(parts) >= 8 {
			return
		}
		switch t := x.(type) {
		case []any:
			for _, y := range t {
				walk(y, depth+1)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k], depth+1)
			}
		case string:
			if !prose(t) {
				return
			}
			if strings.Contains(t, "</") {
				if n, err := html.Parse(strings.NewReader(t)); err == nil {
					t = markdown(n, at, false)
				}
			}
			parts = append(parts, strings.TrimSpace(t))
			total += runes(t)
		}
	}
	walk(v.Props.PageProps, 0)
	if total < rescueMin {
		return ""
	}
	return tidy(strings.Join(parts, "\n\n"))
}

// prose reports whether a string reads as text: long, and spaced like
// sentences, which a URL, a token or encoded data is not.
func prose(s string) bool {
	if len(s) < 400 {
		return false
	}
	return float64(strings.Count(s, " ")) >= 0.08*float64(len(s))
}

func runes(s string) int { return utf8.RuneCountInString(s) }

func first(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
