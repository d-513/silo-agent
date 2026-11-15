package reader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"silo.agent/internal/netguard"
)

// open is a Reader that may reach the test servers on loopback.
func open() *Reader {
	return New(Options{Policy: func() netguard.Policy { return netguard.Policy{} }})
}

// fakeBox renders and opens documents from what the test gives it.
type fakeBox struct {
	html     string
	status   int
	err      error
	renders  int
	docText  string
	docNames []string
}

func (b *fakeBox) Render(context.Context, string) (Rendered, error) {
	b.renders++
	if b.err != nil {
		return Rendered{}, b.err
	}
	return Rendered{HTML: b.html, Status: b.status}, nil
}

func (b *fakeBox) Document(_ context.Context, name string, _ []byte) (string, error) {
	b.docNames = append(b.docNames, name)
	if b.err != nil {
		return "", b.err
	}
	return b.docText, nil
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func page(ct, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write([]byte(body))
	}
}

var para = strings.Repeat("The harbour at Gdynia was built in the twenties on a stretch of empty coast, and within a decade it moved more cargo than any other port on the Baltic. ", 4)

const articleHTML = `<!doctype html><html lang="en"><head><title>The Port of Gdynia</title>
<meta name="author" content="Ada Nowak"><meta property="article:published_time" content="2026-03-04T10:00:00Z">
<script src="/app.js"></script></head><body>
<nav><a href="/">Home</a> <a href="/about">About us</a> <a href="/shop">Subscribe today</a></nav>
<article><h1>The Port of Gdynia</h1>
<p>%[1]s</p>
<h2>The cranes</h2>
<p>%[1]s See <a href="/cranes?id=7">the crane register</a> for more.</p>
<p><img src="/crane.jpg" alt="A gantry crane at dusk"> %[1]s</p>
</article>
<footer>Copyright Harbour Gazette. All rights reserved. Cookie settings.</footer>
</body></html>`

func TestReadsAnArticleWithoutItsChrome(t *testing.T) {
	s := serve(t, page("text/html; charset=utf-8", fmt.Sprintf(articleHTML, para)))
	r := open()
	p, err := r.Read(context.Background(), Request{URL: s.URL + "/port", MaxChars: 20000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "article" || p.Via != "static" || p.Status != 200 || p.Problem != "" {
		t.Fatalf("page %+v", p)
	}
	if p.Title != "The Port of Gdynia" || p.Author != "Ada Nowak" || p.Date != "2026-03-04" || p.Language != "en" {
		t.Fatalf("metadata %+v", p)
	}
	for _, want := range []string{"built in the twenties", "## The cranes", "the crane register", "A gantry crane at dusk"} {
		if !strings.Contains(p.Text, want) {
			t.Fatalf("text lacks %q:\n%s", want, p.Text)
		}
	}
	for _, not := range []string{"Subscribe today", "All rights reserved", "/cranes?id=7", "crane.jpg"} {
		if strings.Contains(p.Text, not) {
			t.Fatalf("text has %q:\n%s", not, p.Text)
		}
	}

	// Links are kept, and made absolute, only when asked for.
	p, err = r.Read(context.Background(), Request{URL: s.URL + "/port", MaxChars: 20000, Links: true}, nil)
	if err != nil || !strings.Contains(p.Text, "[the crane register]("+s.URL+"/cranes?id=7)") {
		t.Fatalf("links: %v\n%s", err, p.Text)
	}

	// The whole page keeps what the main content left out.
	p, err = r.Read(context.Background(), Request{URL: s.URL + "/port", MaxChars: 20000, FullPage: true}, nil)
	if err != nil || !strings.Contains(p.Text, "Subscribe today") || !strings.Contains(p.Text, "All rights reserved") {
		t.Fatalf("full page: %v\n%s", err, p.Text)
	}
}

// A page that is not an article (a listing the extractor finds little in) is
// read whole, minus its navigation.
func TestListingIsReadWithoutNavigation(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<html><head><title>Ferries</title></head><body><nav><a href="/">Home</a> <a href="/x">Timetables for every line</a></nav><main><table><tr><th>Line</th><th>Departs</th></tr>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "<tr><td>Ferry %d to Hel</td><td>%02d:15</td></tr>", i, i%24)
	}
	b.WriteString(`</table></main><footer>Port authority of the bay</footer></body></html>`)
	s := serve(t, page("text/html", b.String()))
	p, err := open().Read(context.Background(), Request{URL: s.URL, MaxChars: 50000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, "Ferry 39 to Hel") || !strings.Contains(p.Text, "| Line") {
		t.Fatalf("table lost:\n%s", p.Text)
	}
	if strings.Contains(p.Text, "Timetables for every line") || strings.Contains(p.Text, "Port authority") {
		t.Fatalf("chrome kept:\n%s", p.Text)
	}
}

func TestInternalAddressIsRefusedUnlessAllowed(t *testing.T) {
	s := serve(t, page("text/html", fmt.Sprintf(articleHTML, para)))
	block := true
	r := New(Options{Policy: func() netguard.Policy { return netguard.Policy{Block: block} }})
	_, err := r.Read(context.Background(), Request{URL: s.URL}, nil)
	if err == nil || !strings.Contains(err.Error(), "internal address") {
		t.Fatalf("loopback read: %v", err)
	}
	// The operator's setting is read at each dial.
	block = false
	if _, err := r.Read(context.Background(), Request{URL: s.URL}, nil); err != nil {
		t.Fatalf("after unblocking: %v", err)
	}
	// With no policy at all the reader refuses rather than reaches inside.
	if _, err := New(Options{}).Read(context.Background(), Request{URL: s.URL}, nil); err == nil {
		t.Fatal("a reader with no policy read loopback")
	}
}

// A redirect to an internal address is refused at its own dial.
func TestRedirectInsideIsRefused(t *testing.T) {
	inside := serve(t, page("text/html", fmt.Sprintf(articleHTML, para)))
	hop := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, inside.URL, http.StatusFound) })
	// Only the first server's port is opened.
	allow, err := netguard.ParseAllow(strings.TrimPrefix(hop.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{Policy: func() netguard.Policy { return netguard.Policy{Block: true, Allow: allow} }})
	if _, err := r.Read(context.Background(), Request{URL: hop.URL}, nil); err == nil || !strings.Contains(err.Error(), "internal address") {
		t.Fatalf("redirect inside: %v", err)
	}
}

func TestOperatorBlockListIsHonoured(t *testing.T) {
	inside := serve(t, page("text/html", fmt.Sprintf(articleHTML, para)))
	hop := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(inside.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	})
	r := New(Options{
		Policy: func() netguard.Policy { return netguard.Policy{} },
		Refused: func(host string) string {
			if host == "localhost" || host == "blocked.example" {
				return host + " is on this server's block list"
			}
			return ""
		},
	})
	if _, err := r.Read(context.Background(), Request{URL: "https://blocked.example/x"}, nil); err == nil || !strings.Contains(err.Error(), "block list") {
		t.Fatalf("blocked host: %v", err)
	}
	if _, err := r.Read(context.Background(), Request{URL: hop.URL}, nil); err == nil || !strings.Contains(err.Error(), "block list") {
		t.Fatalf("redirect to a blocked host: %v", err)
	}
}

const shellHTML = `<!doctype html><html><head><title>Timetable</title><script src="/bundle.js"></script></head>
<body><div id="root"></div><noscript>You need to enable JavaScript to run this app.</noscript></body></html>`

func TestShellIsRenderedInTheBox(t *testing.T) {
	s := serve(t, page("text/html", shellHTML))
	box := &fakeBox{html: fmt.Sprintf(articleHTML, para), status: 200}
	r := open()
	p, err := r.Read(context.Background(), Request{URL: s.URL, MaxChars: 20000}, box)
	if err != nil {
		t.Fatal(err)
	}
	if p.Via != "rendered" || box.renders != 1 || !strings.Contains(p.Text, "built in the twenties") || p.Problem != "" {
		t.Fatalf("rendered read: renders=%d %+v", box.renders, p)
	}

	// Without a machine the answer says what the page needs; it is not
	// an empty page, and it is not kept.
	for name, tc := range map[string]struct {
		box  Box
		rq   Request
		want string
	}{
		"no box":     {nil, Request{URL: s.URL + "/a"}, "no machine to render"},
		"box down":   {&fakeBox{err: ErrNoBox}, Request{URL: s.URL + "/b"}, "machine is not running"},
		"asked not":  {box, Request{URL: s.URL + "/c", Render: RenderNever}, "turned off for this read"},
		"box failed": {&fakeBox{err: fmt.Errorf("exit status 1")}, Request{URL: s.URL + "/d"}, "headless browser failed"},
	} {
		p, err := r.Read(context.Background(), tc.rq, tc.box)
		if err != nil || p.Problem != ProblemNeedsRender || !strings.Contains(p.Note, tc.want) {
			t.Fatalf("%s: %v %+v", name, err, p)
		}
	}

	off := New(Options{Policy: func() netguard.Policy { return netguard.Policy{} }, RenderOn: func() bool { return false }})
	p, err = off.Read(context.Background(), Request{URL: s.URL}, box)
	if err != nil || p.Problem != ProblemNeedsRender || !strings.Contains(p.Note, "turned off on this server") {
		t.Fatalf("rendering off: %v %+v", err, p)
	}
}

// A static page with real text is never rendered.
func TestArticleIsNotRendered(t *testing.T) {
	s := serve(t, page("text/html", fmt.Sprintf(articleHTML, para)))
	box := &fakeBox{html: "<html><body>other</body></html>"}
	if _, err := open().Read(context.Background(), Request{URL: s.URL}, box); err != nil || box.renders != 0 {
		t.Fatalf("renders=%d err=%v", box.renders, err)
	}
	// Unless it is asked for.
	box.html = fmt.Sprintf(articleHTML, para+"And then the rendered copy said more. ")
	p, err := open().Read(context.Background(), Request{URL: s.URL, Render: RenderAlways, MaxChars: 20000}, box)
	if err != nil || box.renders != 1 || p.Via != "rendered" || !strings.Contains(p.Text, "rendered copy said more") {
		t.Fatalf("always: renders=%d %v %+v", box.renders, err, p)
	}
}

func TestEmbeddedArticleSavesARender(t *testing.T) {
	body := strings.ReplaceAll(para, `"`, "")
	html := `<html><head><title>Story</title><script src="/a.js"></script>
<script type="application/ld+json">{"@graph":[{"@type":"NewsArticle","headline":"Story","articleBody":"` + body + body + `"}]}</script>
</head><body><div id="__next"></div></body></html>`
	s := serve(t, page("text/html", html))
	box := &fakeBox{html: "<html></html>"}
	p, err := open().Read(context.Background(), Request{URL: s.URL, MaxChars: 20000}, box)
	if err != nil || box.renders != 0 || !strings.Contains(p.Text, "built in the twenties") || p.Problem != "" {
		t.Fatalf("renders=%d %v %+v", box.renders, err, p)
	}
}

func TestBotWallIsNamedAndNotKept(t *testing.T) {
	var hits atomic.Int32
	wall := `<html><head><title>Just a moment...</title></head><body><script src="/cdn-cgi/challenge-platform/x.js"></script>Checking your browser</body></html>`
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(wall))
	})
	r := open()
	box := &fakeBox{html: wall, status: 403}
	for i := 1; i <= 2; i++ {
		p, err := r.Read(context.Background(), Request{URL: s.URL}, box)
		if err != nil || p.Problem != ProblemBlocked || p.Text != "" || !strings.Contains(p.Note, "bot check") {
			t.Fatalf("read %d: %v %+v", i, err, p)
		}
		if int(hits.Load()) != i || box.renders != i {
			t.Fatalf("read %d: a wall was served from the cache (hits %d, renders %d)", i, hits.Load(), box.renders)
		}
	}
	// The browser in the box getting through is the page.
	box.html, box.status = fmt.Sprintf(articleHTML, para), 200
	p, err := r.Read(context.Background(), Request{URL: s.URL, MaxChars: 20000}, box)
	if err != nil || p.Problem != "" || p.Via != "rendered" || !strings.Contains(p.Text, "built in the twenties") {
		t.Fatalf("past the wall: %v %+v", err, p)
	}
}

func TestSignInPageIsNamed(t *testing.T) {
	s := serve(t, page("text/html", `<html><head><title>Sign in</title></head><body><form><input type="text" name="u"><input type="password" name="p"><button>Sign in</button></form></body></html>`))
	p, err := open().Read(context.Background(), Request{URL: s.URL}, nil)
	if err != nil || p.Problem != ProblemLogin {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestErrorPageKeepsItsTextAndStatus(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, "<html><head><title>Not here</title></head><body><main><h1>Page not found</h1><p>%s</p></main></body></html>", para)
	})
	p, err := open().Read(context.Background(), Request{URL: s.URL}, nil)
	if err != nil || p.Problem != ProblemHTTP || p.Status != 404 || !strings.Contains(p.Note, "404 Not Found") || !strings.Contains(p.Text, "Page not found") {
		t.Fatalf("%v %+v", err, p)
	}
}

// longDoc is a markdown page of sections, each about one screen.
func longDoc() string {
	var b strings.Builder
	b.WriteString("# Handbook\n\nIntroduction to the handbook.\n\n")
	topics := []string{"Mooring", "Pilotage", "Customs", "Bunkering", "Weather", "Tides", "Tugs", "Berths"}
	for i, topic := range topics {
		fmt.Fprintf(&b, "## %s\n\n", topic)
		for j := 0; j < 6; j++ {
			fmt.Fprintf(&b, "Paragraph %d of section %d covers general harbour practice in plain words that say little. %s\n\n", j, i, strings.TrimSpace(strings.Repeat("Filler sentence about ships. ", 6)))
		}
	}
	b.WriteString("## Fees\n\nThe quarantine anchorage fee is 412 zloty per day, payable to the harbour master before departure.\n")
	return b.String()
}

func TestLongPageIsPagedWithAnOutline(t *testing.T) {
	var hits atomic.Int32
	text := longDoc()
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(text))
	})
	r := open()
	ctx := context.Background()
	p, err := r.Read(ctx, Request{URL: s.URL + "/handbook.md", MaxChars: 3000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "markdown" || p.NextOffset == 0 || p.NextOffset > 3000 || p.TotalChars != len([]rune(strings.TrimSpace(text))) {
		t.Fatalf("first window %+v", p)
	}
	if !strings.HasSuffix(text[:p.NextOffset], "\n\n") {
		t.Fatalf("the window was cut mid-paragraph: %q", p.Text[len(p.Text)-40:])
	}
	if len(p.Outline) != 10 || p.Outline[0].Heading != "# Handbook" || p.Outline[9].Heading != "## Fees" {
		t.Fatalf("outline %+v", p.Outline)
	}
	// The outline's offsets lead to their sections, and the rest of the
	// page is read from the copy that was kept.
	fees := p.Outline[9].Offset
	p2, err := r.Read(ctx, Request{URL: s.URL + "/handbook.md", Offset: fees, MaxChars: 3000}, nil)
	if err != nil || !strings.HasPrefix(p2.Text, "## Fees") || p2.NextOffset != 0 || p2.Offset != fees {
		t.Fatalf("from the outline: %v %+v", err, p2)
	}
	// Reading on from next_offset loses nothing and repeats nothing.
	got := p.Text
	for next := p.NextOffset; next != 0; {
		q, err := r.Read(ctx, Request{URL: s.URL + "/handbook.md", Offset: next, MaxChars: 3000}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got += "\n\n" + q.Text
		next = q.NextOffset
	}
	if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(text), " ") {
		t.Fatal("paging through the page did not give the page")
	}
	if hits.Load() != 1 {
		t.Fatalf("the site was asked %d times", hits.Load())
	}
	// Fresh asks it again.
	if _, err := r.Read(ctx, Request{URL: s.URL + "/handbook.md", Fresh: true}, nil); err != nil || hits.Load() != 2 {
		t.Fatalf("fresh: %v hits %d", err, hits.Load())
	}
}

func TestQueryReturnsThePassagesThatAnswerIt(t *testing.T) {
	s := serve(t, page("text/markdown", longDoc()))
	r := open()
	p, err := r.Read(context.Background(), Request{URL: s.URL, Query: "quarantine anchorage fee", MaxChars: 2000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Text, "412 zloty") || !strings.Contains(p.Text, "[Handbook › Fees]") || p.NextOffset != 0 || p.Passages == "" {
		t.Fatalf("passages %+v", p)
	}
	if len([]rune(p.Text)) > 2000 {
		t.Fatalf("%d characters returned", len([]rune(p.Text)))
	}
	// A query nothing matches gives the start of the page and says so.
	p, err = r.Read(context.Background(), Request{URL: s.URL, Query: "xylophone", MaxChars: 2000}, nil)
	if err != nil || !strings.HasPrefix(p.Text, "# Handbook") || !strings.Contains(p.Note, "Nothing on the page matched") || p.NextOffset == 0 {
		t.Fatalf("no match: %v %+v", err, p)
	}
	// A page that fits is returned whole: there is nothing to choose.
	p, err = r.Read(context.Background(), Request{URL: s.URL, Query: "fee", MaxChars: 100000}, nil)
	if err != nil || p.Passages != "" || !strings.Contains(p.Text, "## Mooring") {
		t.Fatalf("whole: %v %+v", err, p)
	}
}

func TestOtherKindsOfAnswer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/data", page("application/json", `{"a": 1}`))
	mux.HandleFunc("/notes.txt", page("text/plain", "plain words"))
	mux.HandleFunc("/paper", page("application/pdf", "%PDF-1.7 binary"))
	mux.HandleFunc("/lying.html", page("text/html", "%PDF-1.7 binary"))
	mux.HandleFunc("/pic", page("image/png", "\x89PNG\r\n\x1a\n"))
	mux.HandleFunc("/talk.mp3", page("audio/mpeg", "ID3"))
	mux.HandleFunc("/feed", page("application/rss+xml", `<?xml version="1.0"?><rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Dock Talk</title>
<item><title>Episode 9: Fog</title><link>https://example.com/9</link><pubDate>Mon, 02 Nov 2026 08:00:00 +0000</pubDate>
<description>&lt;p&gt;On &lt;b&gt;fog&lt;/b&gt; signals.&lt;/p&gt;</description><enclosure url="https://cdn.example.com/9.mp3" type="audio/mpeg"/><itunes:duration>41:07</itunes:duration></item></channel></rss>`))
	s := httptest.NewServer(mux)
	defer s.Close()
	r := open()
	ctx := context.Background()
	read := func(path string, box Box) Page {
		t.Helper()
		p, err := r.Read(ctx, Request{URL: s.URL + path}, box)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return p
	}
	if p := read("/data", nil); p.Kind != "json" || p.Text != "```json\n{\"a\": 1}\n```" {
		t.Fatalf("json %+v", p)
	}
	if p := read("/notes.txt", nil); p.Kind != "text" || p.Text != "plain words" {
		t.Fatalf("text %+v", p)
	}
	box := &fakeBox{docText: "Title of the paper\nfirst page\fsecond page"}
	if p := read("/paper", box); p.Kind != "pdf" || !strings.Contains(p.Text, "[page 2]\n\nsecond page") || box.docNames[0] != "document.pdf" {
		t.Fatalf("pdf %+v %v", p, box.docNames)
	}
	// The bytes are believed over the header.
	if p := read("/lying.html", box); p.Kind != "pdf" {
		t.Fatalf("pdf served as html %+v", p)
	}
	if p := read("/paper?no-box", nil); p.Problem != ProblemUnsupported || !strings.Contains(p.Note, "needs the Bot's machine") {
		t.Fatalf("pdf with no box %+v", p)
	}
	if p := read("/pic", nil); p.Problem != ProblemUnsupported || !strings.Contains(p.Note, "image file") {
		t.Fatalf("image %+v", p)
	}
	if p := read("/talk.mp3", nil); p.Problem != ProblemUnsupported || !strings.Contains(p.Note, "transcribe") {
		t.Fatalf("audio %+v", p)
	}
	p := read("/feed", nil)
	if p.Kind != "feed" || p.Title != "Dock Talk" || !strings.Contains(p.Text, "**Episode 9: Fog** (2026-11-02) https://example.com/9") ||
		!strings.Contains(p.Text, "On fog signals.") || !strings.Contains(p.Text, "audio: https://cdn.example.com/9.mp3 (41:07)") {
		t.Fatalf("feed %+v", p)
	}
}

func TestPageInAnotherCharset(t *testing.T) {
	// "Zażółć gęślą jaźń" in ISO-8859-2, named only in the markup.
	body := "<html><head><meta charset=\"iso-8859-2\"><title>Test</title></head><body><main><p>Za\xbf\xf3\xb3\xe6 g\xea\xb6l\xb1 ja\xbc\xf1. " + para + "</p></main></body></html>"
	s := serve(t, page("text/html", body))
	p, err := open().Read(context.Background(), Request{URL: s.URL}, nil)
	if err != nil || !strings.Contains(p.Text, "Zażółć gęślą jaźń") {
		t.Fatalf("%v\n%s", err, p.Text)
	}
}

func TestMarkdownAlternateIsPreferred(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/doc", page("text/html", `<html><head><title>Doc</title><link rel="alternate" type="text/markdown" href="/doc.md"></head><body><main><p>`+para+`</p></main></body></html>`))
	mux.HandleFunc("/doc.md", page("text/markdown", "# Doc\n\nThe site's own markdown. "+para))
	s := httptest.NewServer(mux)
	defer s.Close()
	p, err := open().Read(context.Background(), Request{URL: s.URL + "/doc"}, nil)
	if err != nil || p.Kind != "markdown" || !strings.HasPrefix(p.Text, "# Doc\n\nThe site's own markdown.") {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestBadRequests(t *testing.T) {
	r := open()
	for url, want := range map[string]string{
		"":                   "url required",
		"ftp://example.com":  "only http and https",
		"file:///etc/passwd": "is not a URL",
		"http://":            "is not a URL",
	} {
		if _, err := r.Read(context.Background(), Request{URL: url}, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: %v", url, err)
		}
	}
	if _, err := r.Read(context.Background(), Request{URL: "example.com", Render: "sometimes"}, nil); err == nil {
		t.Fatal("an unknown render mode was taken")
	}
}

func TestManyAnswersInOrder(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("page " + r.URL.Path))
	})
	out := open().Many(context.Background(), []Request{{URL: s.URL + "/a"}, {URL: "ftp://nope"}, {URL: s.URL + "/c"}}, nil)
	if out[0].Text != "page /a" || out[2].Text != "page /c" || out[1].Problem != "error" || !strings.Contains(out[1].Note, "only http") {
		t.Fatalf("%+v", out)
	}
}

func TestTidyClosesUpTablesAndCode(t *testing.T) {
	in := "| a<br /><br /><br />b | <br />c<br /> |\n\n```js\n\nfunction f() {\n\n  return 1;\n\n}\n```\n\n```\nkeep\n\nthis\n```"
	want := "| a<br>b | c |\n\n```js\nfunction f() {\n  return 1;\n}\n```\n\n```\nkeep\n\nthis\n```"
	if got := tidy(in); got != want {
		t.Fatalf("tidy:\n%q\nwant\n%q", got, want)
	}
}
