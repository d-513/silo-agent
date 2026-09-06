package mcpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

type echoIn struct {
	Q string `json:"q"`
}

func testServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "echo"},
		func(context.Context, *mcp.CallToolRequest, echoIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil, nil
		})
	return srv
}

func streamableHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return testServer() }, nil)
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func mustConnect(t *testing.T, ctx context.Context, d Dial) *Session {
	t.Helper()
	sess, err := Connect(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

func checkEcho(t *testing.T, ctx context.Context, sess *Session) {
	t.Helper()
	tools, err := ListAll(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("%v", tools)
	}
	out, err := Call(ctx, sess, "echo", map[string]any{"q": "hi"})
	if err != nil || out != "pong" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestConnectListCall(t *testing.T) {
	srv := httptest.NewServer(streamableHandler())
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	checkEcho(t, ctx, mustConnect(t, ctx, Dial{URL: srv.URL}))
}

func TestConnectFallsBackToLegacySSE(t *testing.T) {
	srv := httptest.NewServer(mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return testServer() }, nil))
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	checkEcho(t, ctx, mustConnect(t, ctx, Dial{URL: srv.URL + "/sse"}))
}

func TestConnectKeepsPOSTOnRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp/", http.StatusFound)
	})
	mux.Handle("/mcp/", streamableHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	mustConnect(t, ctx, Dial{URL: srv.URL + "/mcp"})
}

func TestConnectRetriesTrailingSlashOn404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.Handle("/mcp/", streamableHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	mustConnect(t, ctx, Dial{URL: srv.URL + "/mcp"})
}

func TestConnectSendsDialHeaders(t *testing.T) {
	var got string
	inner := streamableHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got == "" {
			got = r.Header.Get("X-Api-Key")
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	mustConnect(t, ctx, Dial{URL: srv.URL, Headers: map[string]string{"X-Api-Key": "k"}})
	if got != "k" {
		t.Fatalf("X-Api-Key=%q", got)
	}
}

func TestHeadersFromJSON(t *testing.T) {
	h, err := HeadersFromJSON(`{"X-A":"b"}`)
	if err != nil || h["X-A"] != "b" {
		t.Fatal(h, err)
	}
}

type failAuth struct{}

func (failAuth) TokenSource(context.Context) (oauth2.TokenSource, error) { return nil, nil }

func (failAuth) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return errors.New("iss missing")
}

var _ auth.OAuthHandler = failAuth{}

func TestConnectSurfacesOAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="OAuth"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	_, err := Connect(ctx, Dial{URL: srv.URL, OAuth: failAuth{}})
	if err == nil || !strings.Contains(err.Error(), "oauth: iss missing") {
		t.Fatal(err)
	}
}

// grantAuth yields a token only after Authorize ran, so the 401 → Authorize →
// retry path is actually exercised. It records the URL Authorize was given.
type grantAuth struct {
	granted *bool
	authURL *string
}

func (g grantAuth) TokenSource(context.Context) (oauth2.TokenSource, error) {
	if !*g.granted {
		return nil, nil
	}
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok"}), nil
}

func (g grantAuth) Authorize(_ context.Context, req *http.Request, resp *http.Response) error {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	*g.granted = true
	*g.authURL = req.URL.String()
	return nil
}

// The retried POST after Authorize must carry a replayed body, not the
// consumed one from the first attempt.
func TestOAuthRetryReplaysPOSTBody(t *testing.T) {
	inner := streamableHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	checkEcho(t, ctx, mustConnect(t, ctx, Dial{URL: srv.URL, OAuth: grantAuth{granted: new(bool), authURL: new(string)}}))
}

// The query string selects server behavior (Cloudflare's ?codemode=false) and
// must reach the server on every request, but must be stripped from the URL
// handed to OAuth Authorize, which derives the resource identity from it.
func TestQueryStaysOnRequestsNotOAuthResource(t *testing.T) {
	inner := streamableHandler()
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		query = r.URL.RawQuery
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := testCtx(t)
	var authURL string
	mustConnect(t, ctx, Dial{URL: srv.URL + "/mcp?codemode=false", OAuth: grantAuth{granted: new(bool), authURL: &authURL}})
	if query != "codemode=false" {
		t.Fatalf("server saw query %q", query)
	}
	if authURL != srv.URL+"/mcp" {
		t.Fatalf("Authorize saw %q", authURL)
	}
}

func TestTwilioDocsLive(t *testing.T) {
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.Dial("tcp", "mcp.twilio.com:443")
	if err != nil {
		t.Skip("twilio unreachable: ", err)
	}
	_ = c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	sess, err := Connect(ctx, Dial{URL: "https://mcp.twilio.com/docs"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := ListAll(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
}
