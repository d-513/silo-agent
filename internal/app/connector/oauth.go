package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/drive"
	"silo.agent/internal/db"
	"silo.agent/internal/mcpx"
)

func (s *Service) StartConnectorAuth(ctx context.Context, req *connect.Request[v1.StartConnectorAuthRequest]) (*connect.Response[v1.StartConnectorAuthResponse], error) {
	if _, err := access.OwnBot(ctx, s.db, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := s.db.First(&row, "id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	var c db.Connector
	if s.db.First(&c, "id = ?", row.ConnectorID).Error != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	if c.Auth != AuthOAuth {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("connector does not use oauth"))
	}
	c = *s.ResolveConnector(&c)
	redirect := s.PublicURL(ctx) + "/oauth/callback"
	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	codeCh := make(chan *mcpauth.AuthorizationResult, 1)
	h, err := s.oauthHandler(&c, &row, redirect, func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		u := args.URL
		if st := queryParam(u, "state"); st != "" {
			s.mu.Lock()
			s.oauth[st] = &oauthWait{ch: codeCh, issuer: originOf(u)}
			s.mu.Unlock()
		}
		select {
		case urlCh <- u:
		default:
		}
		select {
		case res := <-codeCh:
			return res, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sess, err := mcpx.Connect(cctx, s.dial(&c, h))
		if err != nil {
			err = wrapOAuth(err)
			s.db.Model(&row).Updates(map[string]any{"auth_status": StatusErr, "last_error": err.Error()})
			select {
			case errCh <- err:
			default:
			}
			return
		}
		defer sess.Close()
		s.persistOAuthToken(h, &row)
		row.AuthStatus = StatusOK
		row.LastError = ""
		row.StatusDetail = ""
		s.db.Save(&row)
		_ = s.refreshTools(cctx, &row, &c)
	}()
	select {
	case u := <-urlCh:
		return connect.NewResponse(&v1.StartConnectorAuthResponse{AuthorizeUrl: u}), nil
	case err := <-errCh:
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("authorization server did not start a redirect"))
	}
}

func (s *Service) ServeOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	s.mu.Lock()
	wait := s.oauth[state]
	delete(s.oauth, state)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if wait == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "<p>Unknown or expired authorization. You can close this window.</p>")
		return
	}
	if wait.drive != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = io.WriteString(w, drive.PopupPage(wait.drive(ctx, q)))
		return
	}
	iss := q.Get("iss")
	if iss == "" {
		iss = wait.issuer
	}
	select {
	case wait.ch <- &mcpauth.AuthorizationResult{Code: q.Get("code"), State: state, Iss: iss}:
	default:
	}
	_, _ = io.WriteString(w, "<p>Authorized — you can close this.</p>")
}

func (s *Service) oauthHandler(c *db.Connector, row *db.BotConnector, redirect string, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	if fetch == nil {
		fetch = func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return nil, errors.New("authorization required")
		}
	}
	cfg := &mcpauth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirect,
		AuthorizationCodeFetcher: fetch,
		RequestRefreshToken:      true,
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs:            []string{redirect},
				ClientName:              "Silo Agent",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				TokenEndpointAuthMethod: "none",
			},
		},
		NewTokenSource: func(ctx context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			src := oc.TokenSource(ctx, tok)
			return persistSrc{inner: src, save: func(t *oauth2.Token) { s.saveToken(row.ID, t) }}, nil
		},
	}
	if creds := preregisteredClient(c); creds != nil {
		cfg.PreregisteredClient = creds
	}
	if tok := tokenFromJSON(row.TokenJSON); tok != nil {
		cfg.InitialTokenSource = persistSrc{
			inner: oauth2.ReuseTokenSource(tok, oauth2.StaticTokenSource(tok)),
			save:  func(t *oauth2.Token) { s.saveToken(row.ID, t) },
		}
	}
	return mcpauth.NewAuthorizationCodeHandler(cfg)
}

func (s *Service) persistOAuthToken(h *mcpauth.AuthorizationCodeHandler, row *db.BotConnector) {
	src, err := h.TokenSource(context.Background())
	if err != nil || src == nil {
		return
	}
	tok, err := src.Token()
	if err != nil || tok == nil {
		return
	}
	s.saveToken(row.ID, tok)
	row.TokenJSON = mustJSON(tok)
}

func (s *Service) saveToken(id string, tok *oauth2.Token) {
	if tok == nil {
		return
	}
	s.db.Model(&db.BotConnector{}).Where("id = ?", id).Update("token_json", mustJSON(tok))
}

// PublicURL is the address the browser (and an OAuth provider) reaches the
// control plane at.
func (s *Service) PublicURL(ctx context.Context) string {
	return access.PublicURL(s.cfg().PublicURL, access.Request(ctx))
}

func (s *Service) redirectURL() string {
	if u := strings.TrimSpace(s.cfg().PublicURL); u != "" {
		return strings.TrimRight(u, "/") + "/oauth/callback"
	}
	return "http://127.0.0.1:5173/oauth/callback"
}

func applyOAuthClient(c *db.Connector, id, secret string, replaceSecret bool) {
	c.OAuthClientID = strings.TrimSpace(id)
	if replaceSecret || secret != "" {
		c.OAuthClientSecret = secret
	}
}

// preregisteredClient is the MCP-spec fallback when the authorization server
// has no registration_endpoint (GitHub) and no CIMD.
func preregisteredClient(c *db.Connector) *oauthex.ClientCredentials {
	id := strings.TrimSpace(c.OAuthClientID)
	if id == "" {
		return nil
	}
	out := &oauthex.ClientCredentials{ClientID: id}
	if s := c.OAuthClientSecret; s != "" {
		out.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: s}
	}
	return out
}

func wrapOAuth(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "no configured client registration methods") {
		return fmt.Errorf("%w — this server does not register clients automatically; set an OAuth Client ID and Secret on the connector", err)
	}
	return err
}

func queryParam(raw, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// oauthWait is one pending sign-in keyed by its state. MCP connectors hand the
// code to the SDK through ch; drives finish the exchange in drive and the
// popup shows the outcome.
type oauthWait struct {
	ch     chan *mcpauth.AuthorizationResult
	issuer string
	drive  func(ctx context.Context, q url.Values) error
}

// AwaitOAuth registers a pending sign-in whose callback finishes in complete
// (the drives' flow); the entry lapses after ttl.
func (s *Service) AwaitOAuth(state string, ttl time.Duration, complete func(ctx context.Context, q url.Values) error) {
	s.mu.Lock()
	s.oauth[state] = &oauthWait{drive: complete}
	s.mu.Unlock()
	time.AfterFunc(ttl, func() {
		s.mu.Lock()
		delete(s.oauth, state)
		s.mu.Unlock()
	})
}

func tokenFromJSON(raw string) *oauth2.Token {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var t oauth2.Token
	if json.Unmarshal([]byte(raw), &t) != nil || t.AccessToken == "" {
		return nil
	}
	return &t
}

type persistSrc struct {
	inner oauth2.TokenSource
	save  func(*oauth2.Token)
}

func (p persistSrc) Token() (*oauth2.Token, error) {
	t, err := p.inner.Token()
	if err == nil && t != nil && p.save != nil {
		p.save(t)
	}
	return t, err
}
