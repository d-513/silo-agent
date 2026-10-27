package connector

import (
	"context"
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
	siloauth "silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/mcpx"
)

// StartConnectorAuth signs a connection in: from the hub by its id
// (connector_id), or from a Bot through one of its grants (bot_id + id). The
// sign-in is the connection's, so every Bot it is granted to uses it, and it
// acts as whoever signed in (its lender).
func (s *Service) StartConnectorAuth(ctx context.Context, req *connect.Request[v1.StartConnectorAuthRequest]) (*connect.Response[v1.StartConnectorAuthResponse], error) {
	c, err := s.authTarget(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	if c.Auth != AuthOAuth {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("connector does not use oauth"))
	}
	// Whoever signs in lends their identity: a personal sign-in may only
	// reach Bots they alone use.
	shared := req.Msg.GetShared()
	if err := access.CheckSignIn(s.db, c.Name, access.User(ctx).ID, shared, s.Grantees(c.ID)); err != nil {
		return nil, err
	}
	conn := *s.Resolve(c, "")
	redirect := s.PublicURL(ctx) + "/oauth/callback"
	starter := access.User(ctx).ID
	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)
	codeCh := make(chan *mcpauth.AuthorizationResult, 1)
	h, err := s.oauthHandler(&conn, redirect, true, func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		u := args.URL
		// The server names this address and the browser is about to open it.
		if !webURL(u) {
			return nil, errors.New("the server's authorization address is not a web address")
		}
		if st := queryParam(u, "state"); st != "" {
			s.mu.Lock()
			s.oauth[st] = &oauthWait{ch: codeCh, issuer: originOf(u), user: starter}
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
		sess, err := mcpx.Connect(cctx, s.dial(&conn, h))
		if err != nil {
			err = wrapOAuth(err)
			s.db.Model(&db.BotConnector{}).Where("connector_id = ?", c.ID).Updates(map[string]any{"auth_status": StatusErr, "last_error": err.Error()})
			select {
			case errCh <- err:
			default:
			}
			return
		}
		_ = sess.Close()
		// The sign-in is now the connection's, lent by whoever made it.
		s.db.Model(&db.Connector{}).Where("id = ?", c.ID).Updates(map[string]any{"lent_by": starter, "shared": shared})
		s.refreshGrants(c.ID)
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

// authTarget is the connection a sign-in is for, if the caller may sign it in:
// one of a Workspace's (they can build there), or one granted to a Bot they
// manage.
func (s *Service) authTarget(ctx context.Context, m *v1.StartConnectorAuthRequest) (*db.Connector, error) {
	if id := m.GetConnectorId(); id != "" {
		c, err := s.connection(id)
		if err != nil {
			return nil, err
		}
		if _, _, err := access.Space(ctx, s.db, c.SpaceID, access.Build); err != nil {
			return nil, err
		}
		return c, nil
	}
	if _, err := access.Bot(ctx, s.db, m.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	var row db.BotConnector
	if err := s.db.First(&row, "id = ? AND bot_id = ?", m.GetId(), m.GetBotId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	var c db.Connector
	if s.db.First(&c, "id = ?", row.ConnectorID).Error != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown connector"))
	}
	return &c, nil
}

func (s *Service) ServeOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	s.mu.Lock()
	wait := s.oauth[state]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if wait == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "<p>Unknown or expired authorization. You can close this window.</p>")
		return
	}
	// A connector's sign-in ends in the browser of whoever started it. The
	// authorization link can be handed to anyone: without this, a user could
	// start a sign-in for their own Bot, send the link to somebody else, and
	// have that person's consent land their account's token on the Bot. The
	// state is left in place, so the real owner can still finish.
	if wait.user != "" {
		u, err := siloauth.UserFromRequest(s.db, r, s.cfg().HostCookies())
		if err != nil || u.ID != wait.user {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "<p>This sign-in was started by someone else, or you are not signed in to Silo in this browser. Nothing was connected. You can close this window.</p>")
			return
		}
	}
	s.mu.Lock()
	if s.oauth[state] != wait {
		wait = nil
	}
	delete(s.oauth, state)
	s.mu.Unlock()
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

// oauthHandler is the OAuth side of a session on connection c. Every session
// on the connection shares its token (connToken): the one a sign-in gets is
// kept for all of them. fresh starts a new sign-in instead of using the one
// kept.
func (s *Service) oauthHandler(c *db.Connector, redirect string, fresh bool, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	if fetch == nil {
		fetch = func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return nil, errNoToken
		}
	}
	client := s.outbound(c).Client(0)
	shared := connToken{s: s, id: c.ID, client: client}
	cfg := &mcpauth.AuthorizationCodeHandlerConfig{
		// The server names its own authorization and token endpoints, so they
		// are held to the same policy as its URL.
		Client:                   client,
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
		NewTokenSource: func(_ context.Context, oc *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			s.keepSignIn(c.ID, oc, tok)
			return shared, nil
		},
	}
	if creds := preregisteredClient(c); creds != nil {
		cfg.PreregisteredClient = creds
	}
	if !fresh && s.signedIn(c) {
		cfg.InitialTokenSource = shared
	}
	return mcpauth.NewAuthorizationCodeHandler(cfg)
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

// webURL reports whether raw is an http(s) address: the only kind a browser
// should be sent to on a server's say-so.
func webURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")
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
	// user is who started a connector's sign-in; only their browser may
	// finish it. Empty for a drive, whose mobile flow ends in another browser.
	user  string
	drive func(ctx context.Context, q url.Values) error
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
