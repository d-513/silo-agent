// Package reader is the Control Plane's page reader: a URL in, the page's
// text as markdown out, cut to what a model can afford to read.
//
// A read tries the cheap ways first: a handler for a site with an API
// (sites.go), then one static request made as Chrome and the page's main
// content taken out of the HTML (html.go). Only a page that comes back as an
// empty shell or a bot wall is rendered, by the headless browser in the
// caller's own box (Box.Render); the reader never runs a page's scripts
// itself.
package reader

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"silo.agent/internal/netguard"
)

const (
	// DefaultMaxChars is how much of a page one read returns.
	DefaultMaxChars = 8000
	// MaxCharsCap is the most one read may ask for.
	MaxCharsCap = 100000
	// MaxURLs is how many pages one batch may carry.
	MaxURLs = 10
	// manyParallel is how many pages of a batch are read at once.
	manyParallel = 4
)

// Render modes.
const (
	RenderAuto   = "auto"
	RenderAlways = "always"
	RenderNever  = "never"
)

// What went wrong with a page that was reached. A read with a Problem is an
// answer, not an error: the note says what to do next.
const (
	// ProblemBlocked: the site answered with a bot wall or a challenge.
	ProblemBlocked = "blocked"
	// ProblemLogin: the page is behind a sign-in.
	ProblemLogin = "login_wall"
	// ProblemNeedsRender: the page builds itself with scripts and could not
	// be rendered.
	ProblemNeedsRender = "needs_render"
	// ProblemUnsupported: not something that reads as text.
	ProblemUnsupported = "unsupported"
	// ProblemHTTP: the site answered an error status.
	ProblemHTTP = "http_error"
)

// Request is one page to read.
type Request struct {
	URL string
	// Query, when set, returns only the passages that answer it.
	Query string
	// Offset is where to go on reading, in characters (a NextOffset).
	Offset int
	// MaxChars is how much text to return (DefaultMaxChars when 0).
	MaxChars int
	// FullPage keeps the whole page instead of its main content.
	FullPage bool
	// Links keeps link targets; off, a link is its words.
	Links bool
	// Render is RenderAuto (the default), RenderAlways or RenderNever.
	Render string
	// Fresh reads the page again even when a copy is kept.
	Fresh bool
	// Language is the ISO 639-1 language wanted, for Accept-Language and
	// for picking captions.
	Language string
}

// Heading is one line of a long page's outline.
type Heading struct {
	Heading string `json:"heading"`
	Offset  int    `json:"offset"`
}

// Page is what a read answers.
type Page struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Site     string `json:"site,omitempty"`
	Author   string `json:"author,omitempty"`
	Date     string `json:"date,omitempty"`
	Language string `json:"language,omitempty"`
	// Kind is what was read: article, page, markdown, text, json, feed,
	// pdf, document, or a site handler's name.
	Kind string `json:"kind,omitempty"`
	// Via is how: static, rendered or api.
	Via    string `json:"via,omitempty"`
	Status int    `json:"status,omitempty"`
	Text   string `json:"text"`
	// TotalChars is the length of the whole text; NextOffset is set when
	// there is more after what was returned.
	TotalChars int `json:"total_chars"`
	Offset     int `json:"offset,omitempty"`
	NextOffset int `json:"next_offset,omitempty"`
	// Outline lists a long page's headings with the offset of each.
	Outline []Heading `json:"outline,omitempty"`
	// Passages is how many passages matched a query, of how many.
	Passages string `json:"passages,omitempty"`
	Problem  string `json:"problem,omitempty"`
	Note     string `json:"note,omitempty"`
	// Data is what a site handler found beside the text (an audio URL).
	Data map[string]any `json:"data,omitempty"`
}

// Rendered is a page after its scripts ran.
type Rendered struct {
	HTML   string
	Status int
	URL    string
}

// ErrNoBox is what a Box answers when there is no machine to do the work.
var ErrNoBox = errors.New("no machine")

// Box is the caller's own machine, for the two things the reader will not do
// on the Control Plane: run a page's scripts, and open a document file.
type Box interface {
	// Render loads the page in the box's headless browser.
	Render(ctx context.Context, url string) (Rendered, error)
	// Document reads a PDF or office file's text.
	Document(ctx context.Context, name string, data []byte) (string, error)
}

// Options are the operator's settings, read each time they are needed.
type Options struct {
	// Policy is what the reader may connect to.
	Policy func() netguard.Policy
	// Refused names why a host may not be read ("" when it may): the
	// operator's block list.
	Refused func(host string) string
	// GitHubToken lifts GitHub's limit for unauthenticated API calls.
	GitHubToken func() string
	// RenderOn is false when the operator turned rendering off.
	RenderOn func() bool
}

// Reader reads pages. One is shared by every Bot: the cache and the per-site
// manners are the server's.
type Reader struct {
	opts  Options
	cache *cache
	hosts *hostGate
	http  clients
	// send performs one request. Tests replace it.
	send func(ctx context.Context, rq *fetchReq) (*response, error)
}

func New(opts Options) *Reader {
	r := &Reader{opts: opts, cache: newCache(time.Now), hosts: newHostGate()}
	r.send = r.fetch
	return r
}

// doc is a whole page as read, before it is cut for one answer.
type doc struct {
	url, title, site, author, date, language string
	kind, via                                string
	status                                   int
	text                                     string
	problem, note                            string
	data                                     map[string]any
}

// keep reports whether d may be served again: a wall or a shell is not the
// page.
func (d *doc) keep() bool {
	return d.problem == "" && strings.TrimSpace(d.text) != ""
}

// Read answers one request.
func (r *Reader) Read(ctx context.Context, rq Request, box Box) (Page, error) {
	u, err := parseURL(rq.URL)
	if err != nil {
		return Page{}, err
	}
	switch rq.Render {
	case "", RenderAuto:
		rq.Render = RenderAuto
	case RenderAlways, RenderNever:
	default:
		return Page{}, fmt.Errorf("render is %q, %q or %q", RenderAuto, RenderAlways, RenderNever)
	}
	if rq.Offset < 0 {
		rq.Offset = 0
	}
	key := strings.Join([]string{u.String(), fmt.Sprint(rq.FullPage, rq.Links), rq.Render, rq.Language}, "\x00")
	d, err := r.cache.do(key, rq.Fresh, func() (*doc, error) { return r.read(ctx, u, rq, box) })
	if err != nil {
		return Page{}, err
	}
	return view(d, rq), nil
}

// Many reads several pages side by side, answering in the order asked. A
// page that fails is an answer with its error as the note.
func (r *Reader) Many(ctx context.Context, rqs []Request, box Box) []Page {
	out := make([]Page, len(rqs))
	sem := make(chan struct{}, manyParallel)
	done := make(chan struct{})
	for i := range rqs {
		go func(i int) {
			sem <- struct{}{}
			defer func() { <-sem; done <- struct{}{} }()
			p, err := r.Read(ctx, rqs[i], box)
			if err != nil {
				p = Page{URL: rqs[i].URL, Problem: "error", Note: err.Error()}
			}
			out[i] = p
		}(i)
	}
	for range rqs {
		<-done
	}
	return out
}

func (r *Reader) read(ctx context.Context, u *url.URL, rq Request, box Box) (*doc, error) {
	if why := r.refused(u.Hostname()); why != "" {
		return nil, errors.New(why)
	}
	for _, s := range sites {
		if !s.match(u) {
			continue
		}
		d, err := s.read(ctx, r, u, rq, box)
		if err == nil && d != nil {
			if d.url == "" {
				d.url = u.String()
			}
			if d.kind == "" {
				d.kind = s.name
			}
			if d.via == "" {
				d.via = "api"
			}
			return d, nil
		}
		// A handler that cannot answer (its API is down, the limit is
		// spent, the thing is private) leaves the page to be read as one.
		break
	}
	return r.generic(ctx, u, rq, box)
}

func (r *Reader) refused(host string) string {
	if r.opts.Refused == nil {
		return ""
	}
	return r.opts.Refused(host)
}

func (r *Reader) renderOn() bool {
	return r.opts.RenderOn == nil || r.opts.RenderOn()
}

func parseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("url required")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("%q is not a URL", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https pages can be read, not %s:", u.Scheme)
	}
	u.Fragment = ""
	return u, nil
}
