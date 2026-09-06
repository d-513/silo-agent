// Package mcpx dials remote MCP servers over HTTP for the control plane.
//
// All protocol logic (server/discover → initialize handshake, session IDs,
// SSE parsing, Mcp-* headers, reconnects) lives in the official MCP Go SDK.
// This wrapper only adds what the SDK does not: per-connector headers and the
// OAuth flow on the HTTP client, canonical URLs, POST-preserving redirects,
// and the spec's backwards-compatibility fallback to the legacy HTTP+SSE
// transport for servers that reject the streamable POST.
package mcpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// ErrSessionGone reports that the server no longer knows our session; callers
// reconnect and retry once.
var ErrSessionGone = mcp.ErrSessionMissing

type Dial struct {
	URL     string
	Headers map[string]string
	OAuth   auth.OAuthHandler
}

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type Session struct {
	cs *mcp.ClientSession
}

func Connect(ctx context.Context, d Dial) (*Session, error) {
	if d.URL == "" {
		return nil, errors.New("mcp url required")
	}
	client := &http.Client{
		Transport:     &headerRT{base: http.DefaultTransport, hdr: d.Headers, oauth: d.OAuth},
		CheckRedirect: keepPOSTRedirect,
	}
	urls := endpointURLs(strings.TrimSpace(d.URL))
	var transports []mcp.Transport
	for _, u := range urls {
		transports = append(transports, &mcp.StreamableClientTransport{Endpoint: u, HTTPClient: client})
	}
	// Spec backwards compatibility: servers still on the 2024-11-05 HTTP+SSE
	// transport reject the streamable POST; retry with the legacy transport.
	for _, u := range urls {
		transports = append(transports, &mcp.SSEClientTransport{Endpoint: u, HTTPClient: client})
	}
	var first error
	for _, t := range transports {
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "silo", Version: "v1"}, nil).Connect(ctx, t, nil)
		if err == nil {
			return &Session{cs: cs}, nil
		}
		if first == nil {
			first = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, first
}

func (s *Session) Close() error {
	if s == nil || s.cs == nil {
		return nil
	}
	return s.cs.Close()
}

func ListAll(ctx context.Context, sess *Session) ([]Tool, error) {
	var out []Tool
	for t, err := range sess.cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		out = append(out, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return out, nil
}

func Call(ctx context.Context, sess *Session, name string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := sess.cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	body := ""
	if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil && string(b) != "null" {
			body = string(b)
		}
	}
	if body == "" {
		var texts []string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok && t.Text != "" {
				texts = append(texts, t.Text)
			}
		}
		switch len(texts) {
		case 0:
			b, _ := json.Marshal(res)
			body = string(b)
		case 1:
			body = texts[0]
		default:
			b, _ := json.Marshal(texts)
			body = string(b)
		}
	}
	if res.IsError {
		return "", errors.New(body)
	}
	return body, nil
}

// endpointURLs returns the URL plus its trailing-slash twin: some servers
// only serve one of /mcp and /mcp/ and 404 the other without redirecting.
func endpointURLs(raw string) []string {
	out := []string{raw}
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" || u.Path == "/" {
		return out
	}
	alt := *u
	if strings.HasSuffix(u.Path, "/") {
		alt.Path = strings.TrimSuffix(u.Path, "/")
	} else {
		alt.Path += "/"
	}
	if s := alt.String(); s != raw {
		out = append(out, s)
	}
	return out
}

// keepPOSTRedirect re-issues the POST body when a server redirects with
// 301/302 (net/http would downgrade those to GET).
func keepPOSTRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 8 {
		return errors.New("too many redirects")
	}
	prev := via[len(via)-1]
	if prev.Method != http.MethodPost || req.Method == http.MethodPost {
		return nil
	}
	req.Method = http.MethodPost
	if prev.GetBody == nil {
		return errors.New("redirect dropped POST body")
	}
	body, err := prev.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	req.ContentLength = prev.ContentLength
	if ct := prev.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	return nil
}

// headerRT applies connector headers and the OAuth flow to every request, so
// both the streamable and the legacy SSE transport get identical auth: bearer
// token when available, and on 401/403 one Authorize (which may run the
// interactive flow) followed by one retry.
type headerRT struct {
	base  http.RoundTripper
	hdr   map[string]string
	oauth auth.OAuthHandler
}

func (h *headerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r, err := h.apply(req)
	if err != nil {
		return nil, err
	}
	resp, err := h.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if h.oauth == nil || (resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden) {
		return resp, nil
	}
	// A 401 with an empty body is a challenge: surface the OAuth error, never
	// swallow it behind "http 401: (empty)". Authorize closes resp.Body.
	// The OAuth resource identity is the URL without query or fragment: the
	// query stays on MCP requests (`?codemode=false` selects Cloudflare's full
	// tool list) but must not leak into `resource=` or .well-known lookups.
	ar := r.Clone(r.Context())
	ar.URL.RawQuery, ar.URL.Fragment = "", ""
	if err := h.oauth.Authorize(r.Context(), ar, resp); err != nil {
		return nil, fmt.Errorf("oauth: %w", err)
	}
	r2, err := h.apply(req)
	if err != nil {
		return nil, err
	}
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, errors.New("oauth retry: request body not replayable")
		}
		if r2.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	return h.base.RoundTrip(r2)
}

func (h *headerRT) apply(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	for k, v := range h.hdr {
		if k == "" || v == "" {
			continue
		}
		if strings.EqualFold(k, "Authorization") && r.Header.Get("Authorization") != "" {
			continue
		}
		r.Header.Set(k, v)
	}
	if h.oauth == nil || r.Header.Get("Authorization") != "" {
		return r, nil
	}
	ts, err := h.oauth.TokenSource(r.Context())
	if err != nil || ts == nil {
		return r, err
	}
	tok, err := ts.Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			return r, nil // expired refresh token: go unauthenticated, the 401 re-runs Authorize
		}
		return nil, err
	}
	if tok.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	return r, nil
}

func HeadersFromJSON(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("headers: %w", err)
	}
	return out, nil
}
