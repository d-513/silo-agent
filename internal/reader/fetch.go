package reader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"

	"silo.agent/internal/netguard"
	"silo.agent/internal/search"
)

const (
	// pageMax caps an HTML or text page; docMax a document file.
	pageMax = 5 << 20
	docMax  = 32 << 20
	// fetchTimeout is one request, redirects included.
	fetchTimeout = 25 * time.Second
	// hostParallel is how many requests one site gets at once: the server
	// is one address to it however many Bots are reading.
	hostParallel = 3
	honestAgent  = "SiloAgent/1.0 (page reader for an AI agent)"
)

// fetchReq is one request the reader makes.
type fetchReq struct {
	method  string
	url     string
	headers map[string]string
	body    []byte
	// api asks under our own name, as a public API expects, instead of as
	// a browser.
	api bool
	// lang is the language wanted, for Accept-Language.
	lang string
}

type response struct {
	status int
	// url is where the request ended up after redirects.
	url    *url.URL
	header http.Header
	body   []byte
	// truncated is set when the body was longer than the cap.
	truncated bool
}

func (p *response) contentType() string {
	ct := strings.ToLower(p.header.Get("Content-Type"))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.TrimSpace(ct)
}

// clients are the Reader's own two: one that asks as Chrome, one that asks
// under our name. Each has a connection pool, so a Reader is made once.
type clients struct {
	once   sync.Once
	chrome *req.Client
	plain  *req.Client
}

func (r *Reader) clients() (*req.Client, *req.Client) {
	r.http.once.Do(func() {
		setup := func(c *req.Client) *req.Client {
			return c.SetTimeout(fetchTimeout).
				DisableAutoDecode().
				SetDial(r.dial).
				SetRedirectPolicy(r.redirects)
		}
		r.http.chrome = setup(search.Chrome())
		r.http.plain = setup(req.C().SetUserAgent(honestAgent).SetCookieJar(nil).DisableAutoReadResponse())
	})
	return r.http.chrome, r.http.plain
}

// dial connects under the operator's policy of the moment. The check is on
// the address being connected to, after DNS, so a name that resolves inside
// and a redirect that leads inside are refused alike. A Reader given no
// policy reaches nothing internal.
func (r *Reader) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	policy := netguard.Policy{Block: true}
	if r.opts.Policy != nil {
		policy = r.opts.Policy()
	}
	return policy.Dialer(10*time.Second).DialContext(ctx, network, addr)
}

// redirects follows at most ten, none to a host the operator blocked.
func (r *Reader) redirects(rq *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if rq.URL.Scheme != "http" && rq.URL.Scheme != "https" {
		return fmt.Errorf("redirected to a %s: address", rq.URL.Scheme)
	}
	if why := r.refused(rq.URL.Hostname()); why != "" {
		return errors.New(why)
	}
	return nil
}

// fetch performs one request.
func (r *Reader) fetch(ctx context.Context, rq *fetchReq) (*response, error) {
	u, err := url.Parse(rq.url)
	if err != nil {
		return nil, err
	}
	release, err := r.hosts.enter(ctx, u.Hostname())
	if err != nil {
		return nil, err
	}
	defer release()

	chrome, plain := r.clients()
	client := chrome
	if rq.api {
		client = plain
	}
	q := client.R().SetContext(ctx)
	if !rq.api {
		q.SetHeader("accept-language", acceptLanguage(rq.lang))
		q.SetHeader("sec-fetch-site", "none")
	}
	for k, v := range rq.headers {
		q.SetHeader(k, v)
	}
	if rq.body != nil {
		q.SetBodyBytes(rq.body)
	}
	method := rq.method
	if method == "" {
		method = http.MethodGet
	}
	resp, err := q.Send(method, rq.url)
	if err != nil {
		return nil, explain(u, err)
	}
	if resp.Response == nil || resp.Body == nil {
		return nil, fmt.Errorf("%s did not answer", u.Hostname())
	}
	defer resp.Body.Close()
	out := &response{status: resp.StatusCode, header: resp.Header, url: u}
	if resp.Request != nil && resp.Request.URL != nil {
		out.url = resp.Request.URL
	}
	limit := int64(pageMax)
	if isDocument(out.contentType()) || out.contentType() == "application/octet-stream" {
		limit = docMax
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil && len(body) == 0 {
		return nil, explain(u, err)
	}
	if int64(len(body)) > limit {
		body, out.truncated = body[:limit], true
	}
	out.body = body
	return out, nil
}

// explain words a failed request for the model that reads it: what happened,
// and that it is this site, not the network.
func explain(u *url.URL, err error) error {
	host := u.Hostname()
	var dns *net.DNSError
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return fmt.Errorf("%s is an internal address, which this Silo's page reader does not reach", host)
	case errors.As(err, &dns) && dns.IsNotFound:
		return fmt.Errorf("there is no site named %s", host)
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return fmt.Errorf("%s did not answer in time", host)
	case errors.Is(err, context.Canceled):
		return err
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		msg = msg[i+2:]
	}
	return fmt.Errorf("%s could not be reached: %s", host, msg)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func acceptLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) != 2 || lang == "en" {
		return "en-US,en;q=0.9"
	}
	return lang + ",en;q=0.8"
}

// hostGate lets hostParallel requests at a time through to one host.
type hostGate struct {
	mu    sync.Mutex
	slots map[string]chan struct{}
}

func newHostGate() *hostGate { return &hostGate{slots: map[string]chan struct{}{}} }

func (g *hostGate) enter(ctx context.Context, host string) (func(), error) {
	g.mu.Lock()
	ch := g.slots[host]
	if ch == nil {
		if len(g.slots) > 2000 {
			g.slots = map[string]chan struct{}{}
		}
		ch = make(chan struct{}, hostParallel)
		g.slots[host] = ch
	}
	g.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
