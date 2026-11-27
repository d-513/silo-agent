package mattermost

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

// retryWithin is the longest rate-limit wait a call sits out; past it the
// call fails, worded, rather than hold a run up.
const retryWithin = 30 * time.Second

// api is a REST API v4 client for one server and token. The server's address
// is the owner's word, so every connection goes through the operator's
// outbound policy.
type api struct {
	base  string // scheme://host[/subpath], no trailing slash
	token string
	hc    *http.Client
}

// newAPI reads the server and token out of a channel's config.
func newAPI(cfg channels.Config) (*api, error) {
	base, err := serverURL(cfg.Get("server_url"))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.Get("bot_token"))
	if token == "" {
		return nil, errors.New("bot_token is required: the bot account's access token")
	}
	// Calls are bounded by their contexts, not a client timeout, so a large
	// upload is not cut short and the websocket can stay open.
	return &api{base: base, token: token, hc: cfg.Net.Client(0)}, nil
}

// close lets go of the client's idle connections.
func (c *api) close() { c.hc.CloseIdleConnections() }

// serverURL tidies what the owner typed: a missing scheme is https, and a
// pasted API or page address is cut back to the server's own.
func serverURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("server_url is required: where your Mattermost is, e.g. https://chat.example.com")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("server_url is not a web address — it looks like https://chat.example.com")
	}
	path := strings.TrimRight(u.Path, "/")
	if i := strings.Index(path, "/api/v4"); i >= 0 {
		path = path[:i]
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// apiError is a refusal from the server.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the server answered %d", e.Status)
	}
	return e.Message
}

// call makes one request and returns the answer's body, waiting out a short
// rate limit.
func (c *api) call(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v4"+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > maxDownload {
			return nil, fmt.Errorf("the answer is over %d MB", maxDownload>>20)
		}
		if resp.StatusCode < 300 {
			return data, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			// X-Ratelimit-Reset is the seconds until the limit lifts.
			secs, _ := strconv.Atoi(resp.Header.Get("X-Ratelimit-Reset"))
			if wait := max(time.Duration(secs)*time.Second, time.Second); wait <= retryWithin {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
		}
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return nil, &apiError{Status: resp.StatusCode, Message: e.Message}
	}
}

func (c *api) get(ctx context.Context, path string, out any) error {
	data, err := c.call(ctx, "GET", path, "", nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *api) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	data, err := c.call(ctx, "POST", path, "application/json", body)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// friendly words a refusal in terms of the setup, and marks the one that
// starting again will not fix.
func friendly(err error) error {
	if err == nil {
		return nil
	}
	var e *apiError
	if !errors.As(err, &e) {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return errors.New("the server's answer was not Mattermost's — check the server address")
		}
		return fmt.Errorf("cannot reach Mattermost: %w", err)
	}
	switch e.Status {
	case http.StatusUnauthorized:
		return channels.Permanent(errors.New("Mattermost rejected the access token — copy the bot account's token again (System Console → Integrations → Bot Accounts), and check the account is still active"))
	case http.StatusForbidden:
		return fmt.Errorf("Mattermost does not let the bot do that (%v) — add it to the team and the channel, and check its permissions", e)
	case http.StatusNotFound:
		if e.Message == "" {
			return errors.New("the server does not answer as Mattermost does — check the server address")
		}
		return fmt.Errorf("Mattermost cannot find that (%v) — pick the channel again in Set up", e)
	case http.StatusTooManyRequests:
		return errors.New("Mattermost is rate limiting the bot — try again in a minute")
	}
	return fmt.Errorf("mattermost: %w", err)
}
