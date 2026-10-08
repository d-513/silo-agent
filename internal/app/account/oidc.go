package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// Signing in with the operator's OIDC provider is two plain HTTP routes, since
// the browser has to leave the app and come back:
//
//	/auth/oidc/start     sends the browser to the provider
//	/auth/oidc/callback  takes the code it comes back with and signs in
//
// A failure returns to /signin?error=<code>. The page words the code itself
// (web/src/signinError.ts), so nothing in a link can put text on it.
const (
	OIDCStartPath    = "/auth/oidc/start"
	OIDCCallbackPath = "/auth/oidc/callback"

	oidcCookie  = "silo_oidc"
	oidcPending = 10 * time.Minute
)

// Why an OIDC sign-in did not happen, as the sign-in page is told.
const (
	oidcErrOff        = "oidc_off"         // not configured
	oidcErrProvider   = "oidc_unavailable" // the provider could not be reached or is misconfigured
	oidcErrExpired    = "oidc_expired"     // the round trip took too long, or was not started here
	oidcErrDenied     = "oidc_denied"      // the provider said no, or the person cancelled
	oidcErrFailed     = "oidc_failed"      // the provider's answer did not check out
	oidcErrUnverified = "oidc_unverified"  // no verified email to match an account by
	oidcErrNoAccount  = "oidc_no_account"  // nobody here with that email, and accounts are not made on sign-in
	oidcErrConflict   = "oidc_conflict"    // the email's account belongs to a different identity
	oidcErrDisabled   = "oidc_disabled"
)

type oidcState struct {
	mu sync.Mutex
	// provider is the discovered provider for issuer; a changed setting drops it.
	issuer   string
	provider *oidc.Provider
	// flows are sign-ins that left for the provider and have not come back.
	flows map[string]oidcFlow
}

type oidcFlow struct {
	nonce, verifier, rd string
	exp                 time.Time
}

// OIDCRedirectURL is the callback address to register at the provider.
func (s *Service) OIDCRedirectURL(r *http.Request) string {
	return access.PublicURL(s.cfg().PublicURL, r) + OIDCCallbackPath
}

// provider discovers the configured issuer, once per issuer. Providers
// disagree on whether their issuer ends in a slash and the match must be
// exact, so the other spelling is tried before giving up.
func (s *Service) provider(ctx context.Context, fresh bool) (*oidc.Provider, error) {
	issuer := strings.TrimSpace(s.cfg().OIDC.Issuer)
	s.oidc.mu.Lock()
	if !fresh && s.oidc.provider != nil && s.oidc.issuer == issuer {
		p := s.oidc.provider
		s.oidc.mu.Unlock()
		return p, nil
	}
	s.oidc.mu.Unlock()
	ctx = oidc.ClientContext(ctx, s.HTTP)
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		other := strings.TrimSuffix(issuer, "/")
		if other == issuer {
			other = issuer + "/"
		}
		if p2, err2 := oidc.NewProvider(ctx, other); err2 == nil {
			p, err = p2, nil
		}
	}
	if err != nil {
		return nil, err
	}
	s.oidc.mu.Lock()
	s.oidc.issuer, s.oidc.provider = issuer, p
	s.oidc.mu.Unlock()
	return p, nil
}

func (s *Service) oauthConfig(p *oidc.Provider, o config.OIDC, r *http.Request) *oauth2.Config {
	return &oauth2.Config{
		ClientID: strings.TrimSpace(o.ClientID), ClientSecret: o.ClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: s.OIDCRedirectURL(r), Scopes: o.ScopeList(),
	}
}

// ServeOIDCStart sends the browser to the provider. ?rd= is where to land
// afterwards: a plain path on this site.
func (s *Service) ServeOIDCStart(w http.ResponseWriter, r *http.Request) {
	o := s.cfg().OIDC
	if !o.On() {
		s.oidcFail(w, r, oidcErrOff, nil)
		return
	}
	p, err := s.provider(r.Context(), false)
	if err != nil {
		s.oidcFail(w, r, oidcErrProvider, err)
		return
	}
	state, nonce, verifier := ids.Token(), ids.Token(), oauth2.GenerateVerifier()
	now := time.Now()
	s.oidc.mu.Lock()
	if s.oidc.flows == nil {
		s.oidc.flows = map[string]oidcFlow{}
	}
	for k, f := range s.oidc.flows {
		if now.After(f.exp) {
			delete(s.oidc.flows, k)
		}
	}
	s.oidc.flows[state] = oidcFlow{nonce: nonce, verifier: verifier, rd: landing(r.URL.Query().Get("rd")), exp: now.Add(oidcPending)}
	s.oidc.mu.Unlock()
	// The cookie ties the round trip to this browser: a callback link made in
	// someone else's browser signs nobody in here.
	http.SetCookie(w, &http.Cookie{
		Name: oidcCookie, Value: state, Path: "/auth/oidc", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: access.SecureCookies(s.cfg().PublicURL, r), MaxAge: int(oidcPending / time.Second),
	})
	http.Redirect(w, r, s.oauthConfig(p, o, r).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// ServeOIDCCallback finishes the round trip: it trades the code for an ID
// token, checks it, finds (or links, or makes) the Silo user, and signs in.
func (s *Service) ServeOIDCCallback(w http.ResponseWriter, r *http.Request) {
	o := s.cfg().OIDC
	if !o.On() {
		s.oidcFail(w, r, oidcErrOff, nil)
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	s.oidc.mu.Lock()
	flow, ok := s.oidc.flows[state]
	delete(s.oidc.flows, state)
	s.oidc.mu.Unlock()
	c, _ := r.Cookie(oidcCookie)
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/auth/oidc", MaxAge: -1, HttpOnly: true})
	if !ok || state == "" || time.Now().After(flow.exp) || c == nil || c.Value != state {
		s.oidcFail(w, r, oidcErrExpired, nil)
		return
	}
	if e := q.Get("error"); e != "" {
		s.oidcFail(w, r, oidcErrDenied, fmt.Errorf("provider answered %s: %s", e, q.Get("error_description")))
		return
	}
	p, err := s.provider(r.Context(), false)
	if err != nil {
		s.oidcFail(w, r, oidcErrProvider, err)
		return
	}
	ctx := oidc.ClientContext(r.Context(), s.HTTP)
	conf := s.oauthConfig(p, o, r)
	tok, err := conf.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(flow.verifier))
	if err != nil {
		s.oidcFail(w, r, oidcErrFailed, fmt.Errorf("code exchange: %w", err))
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := p.Verifier(&oidc.Config{ClientID: conf.ClientID}).Verify(ctx, raw)
	if err != nil {
		s.oidcFail(w, r, oidcErrFailed, fmt.Errorf("id token: %w", err))
		return
	}
	if idt.Nonce != flow.nonce {
		s.oidcFail(w, r, oidcErrFailed, errors.New("id token nonce does not match"))
		return
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		s.oidcFail(w, r, oidcErrFailed, err)
		return
	}
	// Some providers keep the email out of the ID token and only say it at
	// the userinfo endpoint.
	if _, has := claims["email"]; !has && p.UserInfoEndpoint() != "" {
		if info, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil && info.Subject == idt.Subject {
			more := map[string]any{}
			if info.Claims(&more) == nil {
				for k, v := range more {
					if _, has := claims[k]; !has {
						claims[k] = v
					}
				}
			}
		}
	}
	who := identity{
		issuer: idt.Issuer, subject: idt.Subject,
		email: auth.NormalizeEmail(text(claims["email"])), verified: truthy(claims["email_verified"]),
		groups: list(claims[o.Claim()]),
	}
	u, code, err := s.resolve(who, o)
	if code != "" {
		s.oidcFail(w, r, code, err)
		return
	}
	if err := s.signIn(w, r, u, MethodOIDC); err != nil {
		log.Printf("oidc sign-in: session: %v", err)
		http.Error(w, "could not sign you in", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, flow.rd, http.StatusFound)
}

// identity is who the provider says signed in.
type identity struct {
	issuer, subject, email string
	verified               bool
	groups                 []string
}

// resolve finds the Silo user for an identity, in this order: the user already
// linked to it; the user with its email, when the provider vouches for the
// email and that user has no other identity (they are linked now); a new user,
// when the operator lets sign-ins make accounts. A failure is a code for the
// sign-in page and, when there is something to log, an error.
func (s *Service) resolve(who identity, o config.OIDC) (*db.User, string, error) {
	if who.subject == "" {
		return nil, oidcErrFailed, errors.New("id token has no subject")
	}
	var u db.User
	if err := s.db.Where("oidc_issuer = ? AND oidc_subject = ?", who.issuer, who.subject).Limit(1).Find(&u).Error; err != nil {
		return nil, oidcErrFailed, err
	}
	if u.ID == "" {
		// An email the provider has not verified proves nothing about who
		// this is, so it neither opens an existing account nor claims a new one.
		if who.email == "" || !who.verified {
			return nil, oidcErrUnverified, fmt.Errorf("no verified email for subject %s", who.subject)
		}
		found, err := auth.FindUser(s.db, who.email)
		if err != nil {
			return nil, oidcErrFailed, err
		}
		switch {
		case found != nil && found.OIDCSubject != "":
			return nil, oidcErrConflict, fmt.Errorf("%s is linked to another identity", who.email)
		case found != nil:
			u = *found
			u.OIDCIssuer, u.OIDCSubject = who.issuer, who.subject
			if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Updates(map[string]any{"oidc_issuer": who.issuer, "oidc_subject": who.subject}).Error; err != nil {
				return nil, oidcErrFailed, err
			}
		case o.AutoCreate && o.DomainAllowed(who.email):
			u = db.User{ID: ids.New(), Email: who.email, OIDCIssuer: who.issuer, OIDCSubject: who.subject, CreatedAt: time.Now()}
			if err := s.db.Create(&u).Error; err != nil {
				return nil, oidcErrFailed, err
			}
		default:
			return nil, oidcErrNoAccount, fmt.Errorf("no account for %s", who.email)
		}
	}
	if u.Disabled {
		return nil, oidcErrDisabled, nil
	}
	if group := strings.TrimSpace(o.AdminGroup); group != "" {
		want := false
		for _, g := range who.groups {
			want = want || g == group
		}
		// The provider decides who is an admin, except that it cannot leave
		// Silo with none.
		if want != u.Admin && (want || !s.lastAdmin(&u)) {
			if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Update("admin", want).Error; err != nil {
				return nil, oidcErrFailed, err
			}
			u.Admin = want
		}
	}
	return &u, "", nil
}

func (s *Service) oidcFail(w http.ResponseWriter, r *http.Request, code string, err error) {
	if err != nil {
		log.Printf("oidc sign-in: %s: %v", code, s.maskOIDC(err.Error()))
	}
	http.Redirect(w, r, "/signin?error="+url.QueryEscape(code), http.StatusFound)
}

// maskOIDC keeps the client secret out of anything shown or logged.
func (s *Service) maskOIDC(msg string) string {
	if secret := s.cfg().OIDC.ClientSecret; secret != "" {
		msg = strings.ReplaceAll(msg, secret, "••••")
	}
	return msg
}

// CheckOIDC reads the provider's discovery document with the saved settings,
// so a wrong issuer shows up in Settings instead of at the next sign-in.
func (s *Service) CheckOIDC(ctx context.Context, _ *connect.Request[v1.CheckOIDCRequest]) (*connect.Response[v1.CheckOIDCResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	if !s.cfg().OIDC.On() {
		return nil, refused("set the issuer and client ID first, and save")
	}
	p, err := s.provider(ctx, true)
	if err != nil {
		// Never Unavailable: the control plane is fine, the provider is not.
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("the provider did not answer as an OIDC issuer: %s", s.maskOIDC(err.Error())))
	}
	var doc struct {
		Issuer string `json:"issuer"`
	}
	_ = p.Claims(&doc)
	return connect.NewResponse(&v1.CheckOIDCResponse{Issuer: doc.Issuer, AuthorizationEndpoint: p.Endpoint().AuthURL}), nil
}

// landing is where a sign-in ends up: a plain path on this site, and never
// back into the sign-in round trip itself.
func landing(rd string) string {
	rd = access.SafeReturn(rd)
	if strings.HasPrefix(rd, "/auth/") || strings.HasPrefix(rd, "/signin") {
		return "/"
	}
	return rd
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// truthy reads a claim that should be a boolean; some providers send "true".
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	}
	return false
}

// list reads a claim that is a list of strings, or one string.
func list(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
