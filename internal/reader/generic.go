package reader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// generic reads a page with no handler of its own: one static request, then
// whatever its content type calls for.
func (r *Reader) generic(ctx context.Context, u *url.URL, rq Request, box Box) (*doc, error) {
	resp, err := r.send(ctx, &fetchReq{url: u.String(), lang: rq.Language})
	if err != nil {
		return nil, err
	}
	d := &doc{url: resp.url.String(), site: resp.url.Hostname(), status: resp.status, via: "static"}
	kind := sniff(resp.contentType(), resp.body, resp.url.Path)
	switch kind {
	case "html":
		return r.html(ctx, resp, rq, box), nil
	case "markdown", "text":
		d.kind, d.text = kind, tidy(string(decode(resp.body, resp.header.Get("Content-Type"))))
	case "json":
		d.kind, d.text = kind, "```json\n"+strings.TrimSpace(string(bytes.ToValidUTF8(resp.body, []byte("�"))))+"\n```"
	case "feed":
		feedDoc(d, resp.body)
	case "pdf", "document":
		r.document(ctx, d, kind, resp, box)
	default:
		d.kind = kind
		d.problem = ProblemUnsupported
		d.note = fmt.Sprintf("This address is a %s file (%s), not a page of text. Download it in exec_python if you need the file itself.", kind, first(resp.contentType(), "unknown type"))
		if kind == "audio" || kind == "video" {
			d.note += " Speech in it can be put into words with silo_runtime.transcribe once it is in the workspace."
		}
	}
	if resp.truncated && d.problem == "" {
		d.note = join(d.note, "The file was longer than this reader takes; the text stops where it was cut.")
	}
	httpProblem(d)
	return d, nil
}

// html reads an HTML answer, rendering it when the static copy is a shell or
// a wall.
func (r *Reader) html(ctx context.Context, resp *response, rq Request, box Box) *doc {
	d, sig := fromHTML(resp.url, decode(resp.body, resp.header.Get("Content-Type")), rq)
	d.status, d.via = resp.status, "static"
	if strings.EqualFold(resp.header.Get("cf-mitigated"), "challenge") {
		sig.blocked = true
	}
	if resp.status == http.StatusUnauthorized {
		sig.login = true
	}

	// A site that offers the page as markdown has already done this work.
	if sig.alternate != "" && !sig.blocked && !rq.FullPage {
		if text := r.alternate(ctx, sig.alternate, rq); text != "" {
			d.text, d.kind, sig.thin = text, "markdown", false
		}
	}

	want := rq.Render == RenderAlways
	if rq.Render == RenderAuto {
		want = sig.blocked || (sig.thin && sig.scripts > 0)
		// The article in the page's own structured data saves a render.
		if want && !sig.blocked && sig.embedded != "" {
			d.text, d.kind, sig.thin, want = sig.embedded, "article", false, false
		}
	}
	rendered, why := false, "rendering was turned off for this read"
	if want {
		switch {
		case !r.renderOn():
			why = "rendering is turned off on this server"
		case box == nil:
			why = "there is no machine to render it on"
		default:
			rd, err := box.Render(ctx, resp.url.String())
			switch {
			case errors.Is(err, ErrNoBox):
				why = "the machine is not running"
			case err != nil:
				why = "the headless browser failed: " + err.Error()
			default:
				rendered = true
				at := resp.url
				if u, err := url.Parse(rd.URL); err == nil && u.Hostname() != "" {
					at = u
				}
				d2, sig2 := fromHTML(at, []byte(rd.HTML), rq)
				if runes(d2.text) > runes(d.text) || (sig.blocked && !sig2.blocked) {
					d2.status, d2.via = first2(rd.Status, resp.status), "rendered"
					d, sig = d2, sig2
				}
			}
		}
	}
	if sig.thin && runes(sig.nextData) > runes(d.text) {
		d.text, sig.thin = sig.nextData, false
	}

	switch {
	case sig.blocked:
		d.text, d.problem = "", ProblemBlocked
		d.note = "The site answered with a bot check instead of the page. Reading it again will not help. If you need this page, open it in the desktop Chromium; otherwise look for the same information elsewhere."
	case sig.login:
		d.problem = ProblemLogin
		d.note = "The page is behind a sign-in. It can only be read in the desktop Chromium, signed in."
	case sig.thin && sig.scripts > 0 && !rendered:
		d.problem = ProblemNeedsRender
		d.note = "The page builds itself with scripts and has almost no text without them; it was not rendered because " + why + ". Open it in the desktop Chromium."
	case sig.thin && rendered:
		d.note = "The page has almost no text even after its scripts ran. If more was expected, open it in the desktop Chromium."
	case want && !rendered && rq.Render == RenderAlways:
		d.note = "Not rendered: " + why + ". This is the page as the site sent it."
	}
	httpProblem(d)
	return d
}

// alternate fetches the markdown version a page links to.
func (r *Reader) alternate(ctx context.Context, link string, rq Request) string {
	u, err := url.Parse(link)
	if err != nil || r.refused(u.Hostname()) != "" {
		return ""
	}
	resp, err := r.send(ctx, &fetchReq{url: link, lang: rq.Language})
	if err != nil || resp.status != http.StatusOK {
		return ""
	}
	if k := sniff(resp.contentType(), resp.body, resp.url.Path); k != "markdown" && k != "text" {
		return ""
	}
	text := tidy(string(decode(resp.body, resp.header.Get("Content-Type"))))
	if runes(text) < mainMin {
		return ""
	}
	return text
}

// document reads a PDF or an office file on the caller's machine: the tools
// that open them live there, not on the Control Plane.
func (r *Reader) document(ctx context.Context, d *doc, kind string, resp *response, box Box) {
	d.kind = kind
	name := path.Base(resp.url.Path)
	if ext := docExt(resp.contentType(), resp.body, resp.url.Path); !strings.HasSuffix(strings.ToLower(name), ext) {
		name = "document" + ext
	}
	cannot := func(why string) {
		d.problem = ProblemUnsupported
		d.note = "This address is a " + kind + " file, which " + why + ". Download it in exec_python and open it there."
	}
	if box == nil {
		cannot("needs the Bot's machine to open")
		return
	}
	text, err := box.Document(ctx, name, resp.body)
	switch {
	case errors.Is(err, ErrNoBox):
		cannot("needs the Bot's machine to open, and it is not running")
	case err != nil:
		cannot("could not be opened (" + err.Error() + ")")
	case strings.TrimSpace(text) == "":
		cannot("has no text in it (a scan, or pictures)")
	default:
		d.text = pages(text)
		if i := strings.IndexByte(d.text, '\n'); d.title == "" && i > 0 && i < 200 {
			d.title = strings.TrimSpace(d.text[:i])
		}
	}
}

// pages marks a document's page breaks (form feeds) so a quote can be found
// again.
func pages(text string) string {
	parts := strings.Split(text, "\f")
	if len(parts) < 2 {
		return tidy(text)
	}
	var b strings.Builder
	for i, p := range parts {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		fmt.Fprintf(&b, "[page %d]\n\n%s\n\n", i+1, p)
	}
	return tidy(b.String())
}

// httpProblem marks a page the site answered with an error status. Its text
// is kept: an error page often says what is wrong.
func httpProblem(d *doc) {
	if d.status < 400 || d.problem != "" {
		return
	}
	d.problem = ProblemHTTP
	d.note = join(fmt.Sprintf("The site answered %d %s.", d.status, http.StatusText(d.status)), d.note)
}

var documentTypes = map[string]string{
	"application/pdf":    ".pdf",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.oasis.opendocument.text":                                 ".odt",
	"application/rtf":      ".rtf",
	"text/rtf":             ".rtf",
	"application/epub+zip": ".epub",
}

var documentExts = map[string]bool{".pdf": true, ".doc": true, ".docx": true, ".odt": true, ".rtf": true, ".epub": true}

func isDocument(ct string) bool { return documentTypes[ct] != "" }

// docExt is the extension the box's tools know a document by.
func docExt(ct string, body []byte, p string) string {
	if bytes.HasPrefix(body, []byte("%PDF-")) {
		return ".pdf"
	}
	if ext := documentTypes[ct]; ext != "" {
		return ext
	}
	if ext := strings.ToLower(path.Ext(p)); documentExts[ext] {
		return ext
	}
	return ".pdf"
}

// sniff names what an answer is, from its header, then its bytes (a server's
// header is not always true), then its path.
func sniff(ct string, body []byte, p string) string {
	ext := strings.ToLower(path.Ext(p))
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	low := bytes.ToLower(bytes.TrimSpace(head))
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")) || ct == "application/pdf":
		return "pdf"
	case isDocument(ct):
		return "document"
	case strings.Contains(ct, "html"):
		return "html"
	case ct == "text/markdown" || ct == "text/x-markdown":
		return "markdown"
	case ct == "application/rss+xml" || ct == "application/atom+xml" || ct == "application/feed+json":
		return "feed"
	case strings.HasSuffix(ct, "xml"):
		if bytes.Contains(low, []byte("<rss")) || bytes.Contains(low, []byte("<feed")) {
			return "feed"
		}
		return "text"
	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		return "json"
	case strings.HasPrefix(ct, "text/"):
		if ext == ".md" || ext == ".markdown" {
			return "markdown"
		}
		return "text"
	case strings.HasPrefix(ct, "image/"), strings.HasPrefix(ct, "audio/"), strings.HasPrefix(ct, "video/"):
		return ct[:strings.IndexByte(ct, '/')]
	}
	// No type, or one that says nothing: look at the bytes.
	switch {
	case documentExts[ext] && (ct == "" || ct == "application/octet-stream"):
		if ext == ".pdf" {
			return "pdf"
		}
		return "document"
	case bytes.HasPrefix(low, []byte("<!doctype html")) || bytes.HasPrefix(low, []byte("<html")):
		return "html"
	case bytes.HasPrefix(low, []byte("<?xml")) && (bytes.Contains(low, []byte("<rss")) || bytes.Contains(low, []byte("<feed"))):
		return "feed"
	}
	guess := http.DetectContentType(head)
	switch {
	case strings.HasPrefix(guess, "text/html"):
		return "html"
	case strings.HasPrefix(guess, "text/"):
		if ext == ".md" || ext == ".markdown" {
			return "markdown"
		}
		return "text"
	case strings.HasPrefix(guess, "image/"), strings.HasPrefix(guess, "audio/"), strings.HasPrefix(guess, "video/"):
		return guess[:strings.IndexByte(guess, '/')]
	}
	return "binary"
}

func join(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

func first2(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
