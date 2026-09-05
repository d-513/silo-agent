package mcpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

var ErrSessionGone = errors.New("session not found")

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
	url       string
	client    *http.Client
	sessionID string
	protocol  string
	seq       atomic.Int64
}

func Connect(ctx context.Context, d Dial) (*Session, error) {
	if d.URL == "" {
		return nil, errors.New("mcp url required")
	}
	client := &http.Client{
		Transport:     &headerRT{base: http.DefaultTransport, hdr: d.Headers, oauth: d.OAuth},
		CheckRedirect: keepPOSTRedirect,
	}
	var last error
	for _, u := range endpointURLs(d.URL) {
		sess, err := handshake(ctx, client, u)
		if err == nil {
			return sess, nil
		}
		last = err
		if !isMissingSession(err) {
			return nil, err
		}
	}
	return nil, last
}

func handshake(ctx context.Context, client *http.Client, endpoint string) (*Session, error) {
	s := &Session{url: endpoint, client: client, protocol: "2025-03-26"}
	raw, hdr, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": s.protocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "silo", "version": "v1"},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if sid := hdr.Get("Mcp-Session-Id"); sid != "" {
		s.sessionID = sid
	}
	var ir struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(raw, &ir)
	if ir.ProtocolVersion != "" {
		s.protocol = ir.ProtocolVersion
	}
	_ = s.notify(ctx, "notifications/initialized", map[string]any{})
	return s, nil
}

func (s *Session) Close() error {
	if s == nil || s.sessionID == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, s.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Mcp-Session-Id", s.sessionID)
	if s.protocol != "" {
		req.Header.Set("Mcp-Protocol-Version", s.protocol)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

func ListAll(ctx context.Context, sess *Session) ([]Tool, error) {
	var out []Tool
	var cursor string
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, _, err := sess.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var res struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			return out, nil
		}
		cursor = res.NextCursor
	}
}

func Call(ctx context.Context, sess *Session, name string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, _, err := sess.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var res struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return string(raw), nil
	}
	body := ""
	if len(res.StructuredContent) > 0 && string(res.StructuredContent) != "null" {
		body = string(res.StructuredContent)
	} else {
		var texts []string
		for _, c := range res.Content {
			if c.Type == "text" && c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		switch len(texts) {
		case 0:
			body = string(raw)
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

func (s *Session) call(ctx context.Context, method string, params any) (json.RawMessage, http.Header, error) {
	id := s.seq.Add(1)
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.post(ctx, payload)
	if err != nil {
		return nil, nil, err
	}
	hdr := resp.Header.Clone()
	raw, err := readBody(resp)
	if err != nil {
		return nil, hdr, err
	}
	if err := httpStatusErr(resp.StatusCode, raw, s.sessionID != ""); err != nil {
		return nil, hdr, err
	}
	var msg struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, hdr, fmt.Errorf("%s: %s", method, truncate(raw))
	}
	if msg.Error != nil {
		return nil, hdr, fmt.Errorf("%s: %s", method, msg.Error.Message)
	}
	return msg.Result, hdr, nil
}

func (s *Session) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	resp, err := s.post(ctx, payload)
	if err != nil {
		return err
	}
	raw, err := readBody(resp)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	return httpStatusErr(resp.StatusCode, raw, s.sessionID != "")
}

func (s *Session) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if s.protocol != "" {
		req.Header.Set("Mcp-Protocol-Version", s.protocol)
	}
	if s.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	return s.client.Do(req)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		var data []string
		flush := func() []byte {
			if len(data) == 0 {
				return nil
			}
			b := []byte(strings.Join(data, "\n"))
			data = nil
			return b
		}
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case line == "":
				if b := flush(); len(b) > 0 && bytes.Contains(b, []byte(`"jsonrpc"`)) {
					return b, nil
				}
			}
		}
		if b := flush(); len(b) > 0 {
			return b, sc.Err()
		}
		return nil, sc.Err()
	}
	return io.ReadAll(resp.Body)
}

func httpStatusErr(code int, raw []byte, hadSession bool) error {
	if code >= 200 && code < 300 {
		return nil
	}
	if looksHTML(raw) {
		return fmt.Errorf("http %d: not an MCP endpoint (server returned HTML)", code)
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(code)
	}
	if code == http.StatusNotFound && (hadSession || isMissingSession(errors.New(msg))) {
		return fmt.Errorf("%w: %s", ErrSessionGone, truncate(raw))
	}
	return fmt.Errorf("http %d: %s", code, truncate(raw))
}

func looksHTML(b []byte) bool {
	s := bytes.ToLower(bytes.TrimSpace(b))
	return bytes.HasPrefix(s, []byte("<!doctype html")) || bytes.HasPrefix(s, []byte("<html"))
}

func isMissingSession(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSessionGone) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "session not found")
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

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

type headerRT struct {
	base  http.RoundTripper
	hdr   map[string]string
	oauth auth.OAuthHandler
}

func (h *headerRT) RoundTrip(req *http.Request) (*http.Response, error) {
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
	if err := h.bearer(r); err != nil {
		return nil, err
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if h.oauth == nil || (resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden) {
		return resp, nil
	}
	if err := h.oauth.Authorize(r.Context(), r, resp); err != nil {
		return resp, nil
	}
	r2 := req.Clone(req.Context())
	for k, v := range h.hdr {
		if k != "" && v != "" {
			r2.Header.Set(k, v)
		}
	}
	if err := h.bearer(r2); err != nil {
		return nil, err
	}
	return base.RoundTrip(r2)
}

func (h *headerRT) bearer(r *http.Request) error {
	if h.oauth == nil {
		return nil
	}
	ts, err := h.oauth.TokenSource(r.Context())
	if err != nil || ts == nil {
		return err
	}
	tok, err := ts.Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			return nil
		}
		return err
	}
	if tok != nil && tok.AccessToken != "" && r.Header.Get("Authorization") == "" {
		r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	return nil
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
