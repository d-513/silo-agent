package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/coder/websocket"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm/dummy"
)

// seen is what the service in the box saw of one request.
type seen struct {
	Method, Path, Query, Host, Cookie string
	XForwardedHost, XForwardedFor     string
	BodyLen                           int
}

// upstream is a web service on the Bot's localhost: it reports every request,
// streams SSE, echoes WebSockets, and counts the TCP connections it accepted.
type upstream struct {
	srv   *httptest.Server
	port  int
	conns atomic.Int64
	mu    sync.Mutex
	reqs  []seen
}

func startUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "data: one\n\n")
		fl.Flush()
		select { // the second event waits for the client to have read the first
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
		fmt.Fprint(w, "data: two\n\n")
		fl.Flush()
	})
	mux.HandleFunc("/sse-gate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "data: one\n\n")
		fl.Flush()
		<-r.Context().Done() // stays open until the client leaves
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1 << 20)
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, append([]byte("echo:"), data...)); err != nil {
				return
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := seen{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Host: r.Host, Cookie: r.Header.Get("Cookie"),
			XForwardedHost: r.Header.Get("X-Forwarded-Host"), XForwardedFor: r.Header.Get("X-Forwarded-For"), BodyLen: len(body)}
		u.mu.Lock()
		u.reqs = append(u.reqs, s)
		u.mu.Unlock()
		w.Header().Set("Set-Cookie", "app=1; Path=/")
		json.NewEncoder(w).Encode(s)
	})
	u.srv = httptest.NewUnstartedServer(mux)
	u.srv.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			u.conns.Add(1)
		}
	}
	u.srv.Start()
	t.Cleanup(u.srv.Close)
	u.port = u.srv.Listener.Addr().(*net.TCPAddr).Port
	return u
}

func (u *upstream) requests() []seen {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]seen(nil), u.reqs...)
}

// proxyEnv is a CP with tunnels under tunnels.test, one Bot with a live worker,
// and the upstream service on the Bot's localhost.
type proxyEnv struct {
	h     *apptest.H
	botID string
	up    *upstream
	cp    string // host:port of the CP's listener
}

func newProxyEnv(t *testing.T, extraYAML string) *proxyEnv {
	t.Helper()
	dummy.Reset()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cp := ln.Addr().String()
	yaml := apptest.DefaultYAML(dir) + "public_url: http://" + cp + "\ntunnels:\n  host: " + tunnelTestHost + "\n" + extraYAML
	h := apptest.New(t, apptest.WithDataDir(dir), apptest.WithListener(ln), apptest.WithYAML(yaml))
	bot := h.CreateBot("Served").GetId()
	h.StartWorker(bot)
	return &proxyEnv{h: h, botID: bot, up: startUpstream(t), cp: cp}
}

func (e *proxyEnv) tunnel(t *testing.T, public bool) *v1.Tunnel {
	t.Helper()
	return createTunnel(t, e.h, e.botID, int32(e.up.port), public)
}

// browser is an HTTP client with its own cookie jar that reaches the CP for
// every hostname, so <name>.tunnels.test and the CP's own origin are separate
// cookie origins, as in a real browser.
type browser struct {
	cp string
	c  *http.Client
}

func (e *proxyEnv) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{cp: e.cp, c: &http.Client{Jar: jar, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, e.cp)
		},
	}}}
}

// signedIn returns a browser holding the harness admin's session.
func (e *proxyEnv) signedIn() *browser {
	b := e.browser()
	cpURL, _ := url.Parse(e.h.URL)
	b.c.Jar.SetCookies(cpURL, e.h.HTTP.Jar.Cookies(cpURL))
	return b
}

func (b *browser) noFollow() *browser {
	c := *b.c
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{cp: b.cp, c: &c}
}

func (b *browser) do(t *testing.T, method, rawURL string, hdr map[string]string, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := b.c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res, string(out)
}

var nav = map[string]string{"Accept": "text/html"}

func tunnelURL(tun *v1.Tunnel, path string) string {
	return "http://" + tun.GetName() + "." + tunnelTestHost + path
}

func decode(t *testing.T, body string) seen {
	t.Helper()
	var s seen
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("upstream body %q: %v", body, err)
	}
	return s
}

func TestProxyPublicTunnelServesRequests(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	b := e.browser()

	res, body := b.do(t, "GET", tunnelURL(tun, "/hello?x=1&y=two"), nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	s := decode(t, body)
	if s.Method != "GET" || s.Path != "/hello" || s.Query != "x=1&y=two" {
		t.Fatalf("upstream saw %+v", s)
	}
	// Dev servers check Host; it must be the one the browser used.
	if s.Host != tun.GetName()+"."+tunnelTestHost || s.XForwardedHost != s.Host || s.XForwardedFor == "" {
		t.Fatalf("host headers: %+v", s)
	}

	// A request body goes through whole.
	payload := strings.Repeat("0123456789", 100_000) // 1 MB
	_, body = b.do(t, "POST", tunnelURL(tun, "/upload"), nil, strings.NewReader(payload))
	if s := decode(t, body); s.Method != "POST" || s.BodyLen != len(payload) {
		t.Fatalf("POST: %+v", s)
	}
}

func TestProxyReusesOneConnectionForKeepAlive(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	b := e.browser()
	for range 5 {
		if res, _ := b.do(t, "GET", tunnelURL(tun, "/"), nil, nil); res.StatusCode != 200 {
			t.Fatalf("status %d", res.StatusCode)
		}
	}
	if n := e.up.conns.Load(); n != 1 {
		t.Fatalf("service accepted %d connections for 5 sequential requests; keep-alive should reuse one", n)
	}
}

func TestProxyStreamsServerSentEvents(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	req, _ := http.NewRequest("GET", tunnelURL(tun, "/sse-gate"), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := e.browser().c.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// The service never finishes this response, so seeing the first event at all
	// proves the proxy flushes as it goes instead of buffering.
	buf := make([]byte, len("data: one\n\n"))
	if _, err := io.ReadFull(res.Body, buf); err != nil || string(buf) != "data: one\n\n" {
		t.Fatalf("first event = %q, %v", buf, err)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
}

func TestProxyWebSocketEcho(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+tun.GetName()+"."+tunnelTestHost+"/ws", &websocket.DialOptions{HTTPClient: e.browser().c})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	for _, msg := range []string{"hello", strings.Repeat("x", 200_000)} {
		if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		_, got, err := c.Read(ctx)
		if err != nil || string(got) != "echo:"+msg {
			t.Fatalf("echo of %d bytes = %d bytes, %v", len(msg), len(got), err)
		}
	}
}

func TestProxyPrivateTunnelHandsOffTheOwnersSession(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	owner := e.signedIn()

	// A navigation without a grant bounces through the CP, which knows who is
	// signed in, and comes back with a cookie for the tunnel's own origin.
	res, body := owner.do(t, "GET", tunnelURL(tun, "/page?a=1"), nav, nil)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if s := decode(t, body); s.Path != "/page" || s.Query != "a=1" {
		t.Fatalf("landed on %+v, want the original path and query", s)
	}
	// The next request carries the cookie and needs no further hop.
	nf := owner.noFollow()
	res, _ = nf.do(t, "GET", tunnelURL(tun, "/again"), nav, nil)
	if res.StatusCode != 200 {
		t.Fatalf("second request status %d", res.StatusCode)
	}
	var grants int64
	e.h.DB.Model(&db.TunnelGrant{}).Where("tunnel_id = ?", tun.GetId()).Count(&grants)
	if grants != 1 {
		t.Fatalf("%d grants, want 1", grants)
	}
	// The grant cookie is for the proxy, not the app: the service never sees it.
	_, body = owner.do(t, "GET", tunnelURL(tun, "/cookies"), nil, nil)
	if c := decode(t, body).Cookie; strings.Contains(c, "silo_tunnel") {
		t.Fatalf("the service received the grant cookie: %q", c)
	}
	// ...and the cookies the app sets itself still work.
	_, body = owner.do(t, "GET", tunnelURL(tun, "/cookies"), nil, nil)
	if c := decode(t, body).Cookie; !strings.Contains(c, "app=1") {
		t.Fatalf("app cookie lost: %q", c)
	}
}

func TestProxyPrivateTunnelRefusesWithoutAGrant(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)

	// An API call (not a page load) is a plain 401, not a redirect to follow.
	res, _ := e.browser().noFollow().do(t, "GET", tunnelURL(tun, "/api"), map[string]string{"Accept": "application/json"}, nil)
	if res.StatusCode != 401 {
		t.Fatalf("API status %d", res.StatusCode)
	}
	// A page load is sent to the CP to sign in.
	res, _ = e.browser().noFollow().do(t, "GET", tunnelURL(tun, "/"), nav, nil)
	loc, _ := res.Location()
	if res.StatusCode != 302 || loc == nil || loc.Host != e.cp || loc.Path != "/tunnels/auth" || loc.Query().Get("name") != tun.GetName() {
		t.Fatalf("page load: %d -> %v", res.StatusCode, loc)
	}
	// Never reached the service.
	if n := len(e.up.requests()); n != 0 {
		t.Fatalf("the service saw %d requests from someone with no grant", n)
	}
	// Signed out at the control plane, the handoff goes to the sign-in page and
	// carries itself along as `next`, so signing in lands back on the tunnel.
	res, _ = e.browser().noFollow().do(t, "GET", loc.String(), nil, nil)
	to, err := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != 302 || err != nil || to.Path != "/signin" || to.Host != "" {
		t.Fatalf("signed out at the CP: %d -> %q", res.StatusCode, res.Header.Get("Location"))
	}
	next := to.Query().Get("next")
	if next != loc.RequestURI() {
		t.Fatalf("next = %q, want the handoff %q", next, loc.RequestURI())
	}
	// After signing in, following next ends on the tunnel, at the page asked for.
	res, body := e.signedIn().do(t, "GET", "http://"+e.cp+next, nav, nil)
	if res.StatusCode != 200 || decode(t, body).Path != "/" {
		t.Fatalf("after sign-in: %d %q", res.StatusCode, body)
	}
}

func TestProxyPrivateTunnelIsOwnerOnly(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)

	// Another signed-in user is not the owner: the CP will not hand them a ticket.
	_, eveHTTP := e.h.SignedInUser("eve@test.local")
	eve := e.browser()
	cpURL, _ := url.Parse(e.h.URL)
	eve.c.Jar.SetCookies(cpURL, eveHTTP.Jar.Cookies(cpURL))
	res, _ := eve.do(t, "GET", tunnelURL(tun, "/"), nav, nil)
	if res.StatusCode != 404 {
		t.Fatalf("non-owner status %d, want 404", res.StatusCode)
	}

	// A grant whose user is not the Bot's owner (the Bot changed hands) is dead.
	cookie := ids.New()
	e.h.DB.Create(&db.TunnelGrant{ID: ids.Hash(cookie), TunnelID: tun.GetId(), UserID: "someone-else", ExpiresAt: time.Now().Add(time.Hour)})
	forged := e.browser().noFollow()
	forged.c.Jar.SetCookies(&url.URL{Scheme: "http", Host: tun.GetName() + "." + tunnelTestHost}, []*http.Cookie{{Name: "silo_tunnel", Value: cookie, Path: "/"}})
	if res, _ := forged.do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil); res.StatusCode != 401 {
		t.Fatalf("grant for another user: %d", res.StatusCode)
	}
	if n := len(e.up.requests()); n != 0 {
		t.Fatalf("the service saw %d requests", n)
	}
}

func TestProxyGrantsExpireAndAreHashedAtRest(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	owner := e.signedIn()
	owner.do(t, "GET", tunnelURL(tun, "/"), nav, nil)

	var g db.TunnelGrant
	e.h.DB.First(&g, "tunnel_id = ?", tun.GetId())
	if len(g.ID) != 64 { // sha256 hex, never the cookie value itself
		t.Fatalf("grant id %q", g.ID)
	}
	var cookieVal string
	for _, c := range owner.c.Jar.Cookies(&url.URL{Scheme: "http", Host: tun.GetName() + "." + tunnelTestHost}) {
		if c.Name == "silo_tunnel" {
			cookieVal = c.Value
		}
	}
	if cookieVal == "" || cookieVal == g.ID || ids.Hash(cookieVal) != g.ID {
		t.Fatalf("cookie %q vs stored %q", cookieVal, g.ID)
	}
	if time.Until(g.ExpiresAt) < time.Hour || time.Until(g.ExpiresAt) > 13*time.Hour {
		t.Fatalf("grant expires %v", g.ExpiresAt)
	}

	e.h.DB.Model(&g).Update("expires_at", time.Now().Add(-time.Minute))
	res, _ := owner.noFollow().do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil)
	if res.StatusCode != 401 {
		t.Fatalf("expired grant: %d", res.StatusCode)
	}
}

func TestProxyTicketsAreOneShotAndPerTunnel(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	other := createTunnel(t, e.h, e.botID, 8123, false)
	owner := e.signedIn().noFollow()

	ticketFor := func(name, rd string) string {
		res, _ := owner.do(t, "GET", "http://"+e.cp+"/tunnels/auth?name="+name+"&rd="+url.QueryEscape(rd), nil, nil)
		loc, err := res.Location()
		if res.StatusCode != 302 || err != nil {
			t.Fatalf("auth: %d %v", res.StatusCode, err)
		}
		if loc.Host != name+"."+tunnelTestHost || loc.Path != "/.silo/auth" {
			t.Fatalf("redirect %v", loc)
		}
		return loc.Query().Get("ticket")
	}
	redeem := func(name, ticket string) *http.Response {
		res, _ := e.browser().noFollow().do(t, "GET", "http://"+name+"."+tunnelTestHost+"/.silo/auth?ticket="+ticket+"&rd=%2F", nil, nil)
		return res
	}

	tk := ticketFor(tun.GetName(), "/")
	if res := redeem(tun.GetName(), tk); res.StatusCode != 302 {
		t.Fatalf("first redeem: %d", res.StatusCode)
	}
	if res := redeem(tun.GetName(), tk); res.StatusCode != 403 {
		t.Fatalf("replayed ticket: %d", res.StatusCode)
	}
	// A ticket minted for one tunnel does not open another.
	tk = ticketFor(tun.GetName(), "/")
	if res := redeem(other.GetName(), tk); res.StatusCode != 403 {
		t.Fatalf("ticket on the wrong tunnel: %d", res.StatusCode)
	}
	if res := redeem(tun.GetName(), "nonsense"); res.StatusCode != 403 {
		t.Fatalf("bad ticket: %d", res.StatusCode)
	}
}

// The return path after sign-in is only ever a path on the tunnel itself.
func TestProxyAuthDoesNotRedirectOffsite(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	owner := e.signedIn().noFollow()
	for _, rd := range []string{"//evil.example", "https://evil.example/x", `/\evil.example`, "relative", ""} {
		res, _ := owner.do(t, "GET", "http://"+e.cp+"/tunnels/auth?name="+tun.GetName()+"&rd="+url.QueryEscape(rd), nil, nil)
		loc, _ := res.Location()
		if loc == nil || loc.Host != tun.GetName()+"."+tunnelTestHost || loc.Query().Get("rd") != "/" {
			t.Errorf("rd=%q -> %v", rd, loc)
		}
	}
	// And the redeem step ends on the tunnel, whatever rd it is given.
	tk := func() string {
		res, _ := owner.do(t, "GET", "http://"+e.cp+"/tunnels/auth?name="+tun.GetName(), nil, nil)
		loc, _ := res.Location()
		return loc.Query().Get("ticket")
	}()
	res, _ := e.browser().noFollow().do(t, "GET", tunnelURL(tun, "/.silo/auth?ticket="+tk+"&rd=%2F%2Fevil.example"), nil, nil)
	// Location is relative (a path on the tunnel itself), never another host.
	if loc := res.Header.Get("Location"); loc != "/" {
		t.Fatalf("redeem redirected to %q", loc)
	}
}

func TestProxyMakingATunnelPublicOpensIt(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	stranger := e.browser().noFollow()
	if res, _ := stranger.do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil); res.StatusCode != 401 {
		t.Fatalf("private: %d", res.StatusCode)
	}
	if _, err := e.h.Client.UpdateTunnel(e.h.Ctx(), connect.NewRequest(&v1.UpdateTunnelRequest{BotId: e.botID, Id: tun.GetId(), Public: true})); err != nil {
		t.Fatal(err)
	}
	if res, _ := stranger.do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil); res.StatusCode != 200 {
		t.Fatalf("public: %d", res.StatusCode)
	}
	// And private again closes it.
	e.h.Client.UpdateTunnel(e.h.Ctx(), connect.NewRequest(&v1.UpdateTunnelRequest{BotId: e.botID, Id: tun.GetId(), Public: false}))
	if res, _ := stranger.do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil); res.StatusCode != 401 {
		t.Fatalf("private again: %d", res.StatusCode)
	}
}

func TestProxyErrorPages(t *testing.T) {
	e := newProxyEnv(t, "")
	b := e.browser()

	// Nothing listens on the port: 502 that says so.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	closed := createTunnel(t, e.h, e.botID, int32(dead), true)
	res, body := b.do(t, "GET", tunnelURL(closed, "/"), nil, nil)
	if res.StatusCode != 502 || !strings.Contains(body, fmt.Sprint(dead)) {
		t.Fatalf("closed port: %d %q", res.StatusCode, body)
	}

	// A Bot whose box is down: 503, and the request never starts one.
	idle := e.h.CreateBot("Idle").GetId()
	off := createTunnel(t, e.h, idle, 8000, true)
	boxes := e.h.Fake.Creates.Load() + e.h.Fake.Starts.Load()
	res, body = b.do(t, "GET", tunnelURL(off, "/"), nil, nil)
	if res.StatusCode != 503 || !strings.Contains(strings.ToLower(body), "offline") {
		t.Fatalf("offline bot: %d %q", res.StatusCode, body)
	}
	if got := e.h.Fake.Creates.Load() + e.h.Fake.Starts.Load(); got != boxes {
		t.Fatalf("a tunnel request started a box (%d -> %d)", boxes, got)
	}

	// An unknown name is a 404 on the tunnel domain.
	res, _ = b.do(t, "GET", "http://no-such-name-here."+tunnelTestHost+"/", nil, nil)
	if res.StatusCode != 404 {
		t.Fatalf("unknown tunnel: %d", res.StatusCode)
	}
}

func TestProxyOnlyAnswersTheTunnelDomain(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	b := e.browser()
	// Other hosts reach the normal routes (the control plane itself).
	for _, host := range []string{e.cp, "other.example.com", tunnelTestHost, "a.b." + tunnelTestHost, "x" + tunnelTestHost} {
		res, body := b.do(t, "GET", "http://"+host+"/healthz", nil, nil)
		if res.StatusCode != 200 || body != "ok" {
			t.Errorf("%s/healthz: %d %q", host, res.StatusCode, body)
		}
	}
	// A tunnel host is proxied, not routed: /healthz belongs to the service.
	_, body := b.do(t, "GET", tunnelURL(tun, "/healthz"), nil, nil)
	if decode(t, body).Path != "/healthz" {
		t.Fatalf("tunnel host /healthz = %q", body)
	}
}

func TestProxyOffLeavesTheDomainToTheControlPlane(t *testing.T) {
	e := newProxyEnv(t, "  enabled: false\n")
	e.h.DB.Create(&db.Tunnel{ID: "t1", BotID: e.botID, Port: e.up.port, Name: "quiet-amber-heron", Public: true, CreatedAt: time.Now()})
	res, body := e.browser().do(t, "GET", "http://quiet-amber-heron."+tunnelTestHost+"/healthz", nil, nil)
	if res.StatusCode != 200 || body != "ok" {
		t.Fatalf("%d %q: a disabled tunnel domain must not proxy", res.StatusCode, body)
	}
	if n := len(e.up.requests()); n != 0 {
		t.Fatalf("service saw %d requests", n)
	}
}

func TestProxyRecordsLastUse(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, true)
	e.browser().do(t, "GET", tunnelURL(tun, "/"), nil, nil)
	var row db.Tunnel
	e.h.DB.First(&row, "id = ?", tun.GetId())
	if row.LastUsedAt == nil || time.Since(*row.LastUsedAt) > time.Minute {
		t.Fatalf("last_used_at = %v", row.LastUsedAt)
	}
}

// A private tunnel is only as private as the Silo session behind it: signing
// out must end access at once, not when a 12-hour cookie runs out.
func TestProxySigningOutEndsPrivateTunnelAccess(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	owner := e.signedIn()
	api := map[string]string{"Accept": "application/json"}

	if res, _ := owner.do(t, "GET", tunnelURL(tun, "/"), nav, nil); res.StatusCode != 200 {
		t.Fatalf("before sign-out: %d", res.StatusCode)
	}
	if res, _ := owner.noFollow().do(t, "GET", tunnelURL(tun, "/again"), api, nil); res.StatusCode != 200 {
		t.Fatalf("grant not in place: %d", res.StatusCode)
	}
	hits := len(e.up.requests())

	if _, err := e.h.Client.SignOut(e.h.Ctx(), connect.NewRequest(&v1.SignOutRequest{})); err != nil {
		t.Fatal(err)
	}

	// The grant cookie is still in the browser, and still dead.
	nf := owner.noFollow()
	if res, _ := nf.do(t, "GET", tunnelURL(tun, "/after"), api, nil); res.StatusCode != 401 {
		t.Fatalf("API after sign-out: %d, want 401", res.StatusCode)
	}
	res, _ := nf.do(t, "GET", tunnelURL(tun, "/after"), nav, nil)
	if loc, _ := res.Location(); res.StatusCode != 302 || loc == nil || loc.Path != "/tunnels/auth" {
		t.Fatalf("page after sign-out: %d -> %v", res.StatusCode, res.Header.Get("Location"))
	}
	if n := len(e.up.requests()); n != hits {
		t.Fatalf("the service saw %d requests after sign-out", n-hits)
	}

	// The captured Silo session cookie is dead on the server too: the handoff
	// sends it to sign in instead of handing out a ticket.
	res, _ = owner.noFollow().do(t, "GET", "http://"+e.cp+"/tunnels/auth?name="+tun.GetName(), nil, nil)
	if loc := res.Header.Get("Location"); res.StatusCode != 302 || !strings.HasPrefix(loc, "/signin?next=") {
		t.Fatalf("handoff with a signed-out session: %d -> %q", res.StatusCode, loc)
	}
}

// Signing back in is a new session: the old grant stays dead, and a fresh
// handoff works.
func TestProxyNewSignInDoesNotReviveAnOldGrant(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	old := e.signedIn()
	api := map[string]string{"Accept": "application/json"}
	old.do(t, "GET", tunnelURL(tun, "/"), nav, nil)

	e.h.Client.SignOut(e.h.Ctx(), connect.NewRequest(&v1.SignOutRequest{}))
	if _, err := e.h.Client.SignIn(e.h.Ctx(), connect.NewRequest(&v1.SignInRequest{Email: e.h.Email, Password: e.h.Password})); err != nil {
		t.Fatal(err)
	}
	if res, _ := old.noFollow().do(t, "GET", tunnelURL(tun, "/"), api, nil); res.StatusCode != 401 {
		t.Fatalf("old grant after a new sign-in: %d", res.StatusCode)
	}
	if res, body := e.signedIn().do(t, "GET", tunnelURL(tun, "/fresh"), nav, nil); res.StatusCode != 200 || decode(t, body).Path != "/fresh" {
		t.Fatalf("fresh handoff: %d %q", res.StatusCode, body)
	}
}

// A grant cannot outlive the session it came from, even without a sign-out:
// when the session expires, so does access.
func TestProxyGrantDiesWithItsSession(t *testing.T) {
	e := newProxyEnv(t, "")
	tun := e.tunnel(t, false)
	owner := e.signedIn()
	owner.do(t, "GET", tunnelURL(tun, "/"), nav, nil)

	var g db.TunnelGrant
	e.h.DB.First(&g, "tunnel_id = ?", tun.GetId())
	if g.SessionID == "" {
		t.Fatal("grant is not bound to a session")
	}
	e.h.DB.Model(&db.Session{}).Where("id = ?", g.SessionID).Update("expires_at", time.Now().Add(-time.Minute))
	res, _ := owner.noFollow().do(t, "GET", tunnelURL(tun, "/"), map[string]string{"Accept": "application/json"}, nil)
	if res.StatusCode != 401 {
		t.Fatalf("grant of an expired session: %d", res.StatusCode)
	}
}
