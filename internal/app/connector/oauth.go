package connector

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
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
	codeCh := make(chan *mcpauth.AuthorizationResult)
	outcome := make(chan error, 1)
	// arrived: the provider sent the browser back with a code. kept: the
	// token it was exchanged for is the connection's now. refused: it was
	// not kept, because the lending no longer allows it.
	var arrived, kept atomic.Bool
	var refused atomic.Pointer[error]
	signIn := &signIn{
		// The Bots may have gained people since the sign-in started: asked
		// again when the token arrives, before anything is kept.
		check: func() error {
			err := access.CheckSignIn(s.db, c.Name, starter, shared, s.Grantees(c.ID))
			if err != nil {
				refused.Store(&err)
			}
			return err
		},
		// The sign-in is now the connection's, lent by whoever made it.
		kept: func() {
			kept.Store(true)
			s.db.Model(&db.Connector{}).Where("id = ?", c.ID).Updates(map[string]any{"lent_by": starter, "shared": shared})
		},
	}
	h, err := s.oauthHandler(&conn, redirect, signIn, func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		// One sign-in is one trip to the provider: once a code came back,
		// the client trying its other transports must not open another one
		// that nobody will answer.
		if arrived.Load() {
			return nil, errors.New("the sign-in did not work")
		}
		u := args.URL
		// The server names this address and the browser is about to open it.
		if !webURL(u) {
			return nil, errors.New("the server's authorization address is not a web address")
		}
		fetched := make(chan struct{})
		defer close(fetched)
		st := queryParam(u, "state")
		if st != "" {
			w := &oauthWait{ch: codeCh, done: fetched, outcome: outcome, issuer: originOf(u), user: starter}
			s.mu.Lock()
			s.oauth[st] = w
			s.mu.Unlock()
			// The pending sign-in lasts as long as this wait: a callback after
			// it finds nothing to finish and says so.
			defer func() {
				s.mu.Lock()
				if s.oauth[st] == w {
					delete(s.oauth, st)
				}
				s.mu.Unlock()
			}()
		}
		select {
		case urlCh <- u:
		default:
		}
		select {
		case res := <-codeCh:
			arrived.Store(true)
			return res, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), s.signInWait())
		defer cancel()
		sess, err := mcpx.Connect(cctx, s.dial(&conn, h))
		if err == nil {
			_ = sess.Close()
		}
		switch {
		case kept.Load():
			// Whatever the session made of it, the token is the
			// connection's: every grant reconnects with it and says how
			// that went.
			if err == nil {
				access.Activity(s.db, c.SpaceID, starter, access.SignedInTo, c.Name, "", "connector")
			}
			s.refreshGrants(c.ID)
		case refused.Load() != nil:
			err = *refused.Load()
		case err != nil && arrived.Load():
			err = wrapOAuth(err)
			s.signInFailed(c.ID, err)
		}
		// A sign-in nobody finished (the popup closed, the wait ran out)
		// changes nothing: the connection stays as it was.
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
		outcome <- err
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
	case <-wait.done:
		// The sign-in stopped waiting (it ran out of time) as this arrived.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "<p>This sign-in expired before it was finished. Nothing was connected. Start it again from Silo.</p>")
		return
	}
	if wait.outcome == nil {
		_, _ = io.WriteString(w, "<p>Authorized — you can close this.</p>")
		return
	}
	// Say how it went, not just that the code arrived: the exchange and the
	// first session are still to come.
	t := time.NewTimer(callbackWait)
	defer t.Stop()
	select {
	case err := <-wait.outcome:
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<p>Signing in did not work: "+html.EscapeString(err.Error())+"</p><p>Nothing was changed. You can close this window.</p>")
			return
		}
		_, _ = io.WriteString(w, "<p>Authorized — you can close this.</p>")
	case <-t.C:
		_, _ = io.WriteString(w, "<p>Still connecting. You can close this window; the connector shows how it went.</p>")
	case <-r.Context().Done():
	}
}

// callbackWait bounds how long the sign-in popup waits to say how it went.
const callbackWait = 45 * time.Second

func (s *Service) signInWait() time.Duration {
	if s.SignInWait > 0 {
		return s.SignInWait
	}
	return 5 * time.Minute
}

// signInFailed records a sign-in that came back from the provider and still
// failed, on the grants that were waiting for one. A grant that works keeps
// working: its token was not replaced.
func (s *Service) signInFailed(connectorID string, err error) {
	for _, row := range s.grants(connectorID) {
		if row.AuthStatus == StatusNeedsAuth || row.AuthStatus == StatusErr {
			s.resetStatus(row.ID, map[string]any{"auth_status": StatusErr, "last_error": err.Error(), "status_detail": ""})
		}
	}
}

// signIn is what a fresh sign-in does with the token it gets: check asks
// whether it may be kept, kept follows keeping it.
type signIn struct {
	check func() error
	kept  func()
}

// oauthHandler is the OAuth side of a session on connection c. Every session
// on the connection shares its token (connToken): the one a sign-in gets is
// kept for all of them. fresh starts a new sign-in instead of using the one
// kept, and says what to do with its token.
func (s *Service) oauthHandler(c *db.Connector, redirect string, fresh *signIn, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
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
			if fresh != nil && fresh.check != nil {
				if err := fresh.check(); err != nil {
					return nil, err
				}
			}
			s.keepSignIn(c.ID, oc, tok)
			if fresh != nil && fresh.kept != nil {
				fresh.kept()
			}
			return shared, nil
		},
	}
	if creds := preregisteredClient(c); creds != nil {
		cfg.PreregisteredClient = creds
	}
	if fresh == nil && s.signedIn(c) {
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
	ch chan *mcpauth.AuthorizationResult
	// done closes when the sign-in stops waiting for the code; outcome then
	// says how it ended. Both nil for a drive.
	done    chan struct{}
	outcome chan error
	issuer  string
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
