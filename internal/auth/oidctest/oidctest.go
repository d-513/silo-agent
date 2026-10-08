// Package oidctest is an OpenID Connect provider in memory, for tests of
// signing in with one: discovery, the authorization redirect, a token endpoint
// that signs real ID tokens, JWKS and userinfo. Nobody types a password; the
// test says who "signs in" next.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Identity is who the provider says signed in.
type Identity struct {
	Subject string
	Email   string
	// Verified is the email_verified claim; nil leaves the claim out.
	Verified *bool
	Groups   []string
}

// Yes and No are for Identity.Verified.
var (
	yes, no = true, false
	Yes     = &yes
	No      = &no
)

type Provider struct {
	Server       *httptest.Server
	ClientID     string
	ClientSecret string

	mu    sync.Mutex
	key   *rsa.PrivateKey
	who   Identity
	deny  string
	codes map[string]grant
	// knobs a test turns to make the provider misbehave
	wrongNonce   bool
	emailOnlyInU bool
	tokens       map[string]Identity
	// Authorizations counts visits to the authorization endpoint.
	Authorizations int
}

type grant struct {
	who                              Identity
	nonce, challenge, redirect, code string
}

// New starts a provider that knows one client, "silo" with secret "s3cret".
func New(t testing.TB) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{ClientID: "silo", ClientSecret: "s3cret", key: key, codes: map[string]grant{}, tokens: map[string]Identity{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("/auth", p.authorize)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/keys", p.keys)
	mux.HandleFunc("/userinfo", p.userinfo)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Server.Close)
	return p
}

// Issuer is the provider's issuer URL.
func (p *Provider) Issuer() string { return p.Server.URL }

// SignInAs sets who the next authorization signs in.
func (p *Provider) SignInAs(who Identity) {
	p.mu.Lock()
	p.who, p.deny = who, ""
	p.mu.Unlock()
}

// Deny makes the next authorization come back with this OAuth error.
func (p *Provider) Deny(code string) {
	p.mu.Lock()
	p.deny = code
	p.mu.Unlock()
}

// WrongNonce makes ID tokens carry a nonce the client never sent.
func (p *Provider) WrongNonce() {
	p.mu.Lock()
	p.wrongNonce = true
	p.mu.Unlock()
}

// EmailOnlyInUserInfo keeps the email claims out of the ID token, as some
// providers do; they are at the userinfo endpoint.
func (p *Provider) EmailOnlyInUserInfo() {
	p.mu.Lock()
	p.emailOnlyInU = true
	p.mu.Unlock()
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	base := p.Server.URL
	writeJSON(w, map[string]any{
		"issuer": base, "authorization_endpoint": base + "/auth", "token_endpoint": base + "/token",
		"jwks_uri": base + "/keys", "userinfo_endpoint": base + "/userinfo",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), "openid") {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.Authorizations++
	back := url.Values{"state": {q.Get("state")}}
	if p.deny != "" {
		back.Set("error", p.deny)
	} else {
		code := randomText()
		p.codes[code] = grant{who: p.who, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), code: code}
		back.Set("code", code)
	}
	p.mu.Unlock()
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	if id != p.ClientID || secret != p.ClientSecret {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	wrongNonce, hideEmail := p.wrongNonce, p.emailOnlyInU
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || g.redirect != r.PostForm.Get("redirect_uri") || g.challenge == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.Server.URL, "sub": g.who.Subject, "aud": p.ClientID,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce,
	}
	if wrongNonce {
		claims["nonce"] = "not-the-nonce"
	}
	if g.who.Groups != nil {
		claims["groups"] = g.who.Groups
	}
	if !hideEmail {
		addEmail(claims, g.who)
	}
	access := randomText()
	p.mu.Lock()
	p.tokens[access] = g.who
	p.mu.Unlock()
	writeJSON(w, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300, "id_token": p.sign(claims)})
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	who, ok := p.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	p.mu.Unlock()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	claims := map[string]any{"sub": who.Subject}
	addEmail(claims, who)
	writeJSON(w, claims)
}

func addEmail(claims map[string]any, who Identity) {
	if who.Email != "" {
		claims["email"] = who.Email
	}
	if who.Verified != nil {
		claims["email_verified"] = *who.Verified
	}
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	pub := p.key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// sign makes an RS256 JWT of claims.
func (p *Provider) sign(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	body := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(body))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func randomText() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
