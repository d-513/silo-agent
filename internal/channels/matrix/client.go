package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"silo.agent/internal/channels"
)

// retryWithin is the longest wait a rate limit may ask for and still be
// waited out; past it the call fails, worded, rather than hold a run up.
const retryWithin = 30 * time.Second

// client speaks the client-server API of one homeserver as one account.
type client struct {
	base  string // the API's address, no trailing slash
	token string
	// http is the operator's outbound policy: the homeserver is an address
	// its owner typed. Calls are bounded by their contexts, not a client
	// timeout, so a long poll and a large upload are not cut short.
	http *http.Client
}

func newClient(cfg channels.Config) (*client, error) {
	base, err := homeserver(cfg.Get("homeserver_url"))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.Get("access_token"))
	if token == "" {
		return nil, errors.New("access_token is required")
	}
	return &client{base: base, token: token, http: cfg.Net.Client(0)}, nil
}

// homeserver reads the address the owner typed: a URL, or a bare server name,
// which is taken as https.
func homeserver(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("homeserver_url is required")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", errors.New("the homeserver URL should look like https://matrix.example.org")
	}
	// Someone who copied an API address meant the server it is on.
	path, _, _ := strings.Cut(u.Path, "/_matrix")
	return u.Scheme + "://" + u.Host + strings.TrimRight(path, "/"), nil
}

// apiError is a refusal from the homeserver. Code is Matrix's errcode, empty
// when the answer was not a Matrix one at all.
type apiError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return e.Code + ": " + e.Message
	case e.Code != "":
		return e.Code
	case e.Message != "":
		return fmt.Sprintf("HTTP %d, %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// notMatrix reports an answer no homeserver gives to a client-server call:
// another kind of server, or the server name where the API is elsewhere.
func notMatrix(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Code == "" || (ae.Code == "M_UNRECOGNIZED" && ae.Status == 404))
}

// readError reads a failed answer's Matrix error and how long it asks to wait.
func readError(resp *http.Response) *apiError {
	ae := &apiError{Status: resp.StatusCode}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var m struct {
		Code    string  `json:"errcode"`
		Message string  `json:"error"`
		RetryMS float64 `json:"retry_after_ms"`
	}
	if json.Unmarshal(body, &m) == nil {
		ae.Code, ae.Message = m.Code, m.Message
		ae.RetryAfter = time.Duration(m.RetryMS) * time.Millisecond
	}
	if ae.RetryAfter == 0 {
		if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil {
			ae.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return ae
}

// do makes one authenticated request and returns its answer when the
// homeserver accepted it. A rate limit that asks for a short wait is waited
// out: a long answer sent in parts, or a busy server, meets one.
func (c *client) do(ctx context.Context, method, path string, q url.Values, ctype string, body []byte) (*http.Response, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 == 2 {
			return resp, nil
		}
		ae := readError(resp)
		resp.Body.Close()
		if ae.Status != http.StatusTooManyRequests || attempt >= 3 || ae.RetryAfter > retryWithin {
			return nil, ae
		}
		wait := ae.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// call makes a JSON request: in is sent as the body when not nil, and the
// answer is read into out when not nil.
func (c *client) call(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body []byte
	ctype := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ctype = b, "application/json"
	}
	resp, err := c.do(ctx, method, path, q, ctype, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &apiError{Status: resp.StatusCode, Message: "not a JSON answer"}
	}
	return nil
}

// discover asks the typed address where its homeserver's API is
// (.well-known/matrix/client), without the token: the address may be an
// ordinary website. "" when it does not say.
func (c *client) discover(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/.well-known/matrix/client", nil)
	if err != nil {
		return ""
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var wk struct {
		Homeserver struct {
			BaseURL string `json:"base_url"`
		} `json:"m.homeserver"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&wk) != nil {
		return ""
	}
	base, err := homeserver(wk.Homeserver.BaseURL)
	if err != nil {
		return ""
	}
	return base
}

// download fetches a file by its mxc address. Media needs the token since
// Matrix 1.11; a homeserver from before that knows only the old, open path.
func (c *client) download(ctx context.Context, server, id string) ([]byte, error) {
	tail := url.PathEscape(server) + "/" + url.PathEscape(id)
	resp, err := c.do(ctx, "GET", "/_matrix/client/v1/media/download/"+tail, nil, "", nil)
	var ae *apiError
	if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Code == "M_UNRECOGNIZED") {
		resp, err = c.do(ctx, "GET", "/_matrix/media/v3/download/"+tail, nil, "", nil)
	}
	if err != nil {
		return nil, friendly(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("matrix: reading the file: %w", err)
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("file is over %d MB", maxDownload>>20)
	}
	return data, nil
}

// upload stores a file on the homeserver and returns its mxc address.
func (c *client) upload(ctx context.Context, name, mime string, data []byte) (string, error) {
	resp, err := c.do(ctx, "POST", "/_matrix/media/v3/upload", url.Values{"filename": {name}}, mime, data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var up struct {
		URI string `json:"content_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&up); err != nil || up.URI == "" {
		return "", errors.New("the homeserver took the file but gave no address for it")
	}
	return up.URI, nil
}

// friendly words a homeserver's refusal in terms of the setup, and marks the
// ones that starting again will not fix.
func friendly(err error) error {
	if err == nil || channels.IsPermanent(err) || errors.Is(err, context.Canceled) {
		return err
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		return fmt.Errorf("matrix: %w", err)
	}
	switch {
	case ae.Code == "M_UNKNOWN_TOKEN", ae.Code == "M_MISSING_TOKEN", ae.Status == http.StatusUnauthorized:
		return channels.Permanent(errors.New("the homeserver rejected the access token — it was revoked or the account signed out; copy the bot account's token again"))
	case ae.Code == "M_USER_DEACTIVATED":
		return channels.Permanent(errors.New("the bot's Matrix account was deactivated"))
	case ae.Code == "M_USER_LOCKED", ae.Code == "M_USER_SUSPENDED":
		return channels.Permanent(errors.New("the homeserver has locked or suspended the bot's Matrix account"))
	case ae.Code == "M_FORBIDDEN":
		return fmt.Errorf("the homeserver refused (%s) — is the bot in that room, and allowed to post there?", ae.Message)
	case ae.Code == "M_TOO_LARGE", ae.Status == http.StatusRequestEntityTooLarge:
		return errors.New("the homeserver refused it as too large")
	case ae.Code == "M_LIMIT_EXCEEDED", ae.Status == http.StatusTooManyRequests:
		return errors.New("the homeserver is rate limiting the bot — try again in a while")
	}
	return fmt.Errorf("matrix: %w", ae)
}
