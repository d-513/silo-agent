package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"silo.agent/internal/db"
)

// storedToken is a connection's OAuth sign-in as connectors.token_json keeps
// it: the token, and what refreshing it takes (the token endpoint and the
// client the authorization server knows Silo as). With those any session, on
// any Bot it is granted to and after a restart, can refresh it.
type storedToken struct {
	Token        *oauth2.Token    `json:"token"`
	TokenURL     string           `json:"token_url,omitempty"`
	AuthStyle    oauth2.AuthStyle `json:"auth_style,omitempty"`
	ClientID     string           `json:"client_id,omitempty"`
	ClientSecret string           `json:"client_secret,omitempty"`
	Scopes       []string         `json:"scopes,omitempty"`
}

// readToken is a stored sign-in; empty when there is none.
func readToken(raw string) storedToken {
	var st storedToken
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &st) != nil || st.Token == nil || st.Token.AccessToken == "" {
		return storedToken{}
	}
	return st
}

// config is what refreshes the token, or nil when nothing can.
func (st storedToken) config() *oauth2.Config {
	if st.TokenURL == "" || st.Token == nil || st.Token.RefreshToken == "" {
		return nil
	}
	return &oauth2.Config{
		ClientID: st.ClientID, ClientSecret: st.ClientSecret, Scopes: st.Scopes,
		Endpoint: oauth2.Endpoint{TokenURL: st.TokenURL, AuthStyle: st.AuthStyle},
	}
}

var errNoToken = errors.New("authorization required")

// tokenCell is one connection's token in memory, behind the lock every
// session on the connection takes to read or refresh it.
type tokenCell struct {
	mu     sync.Mutex
	loaded bool
	st     storedToken
}

func (s *Service) cell(connectorID string) *tokenCell {
	v, _ := s.tokens.LoadOrStore(connectorID, &tokenCell{})
	return v.(*tokenCell)
}

// signedIn reports whether the connection has a token.
func (s *Service) signedIn(c *db.Connector) bool {
	return readToken(c.TokenJSON).Token != nil
}

// keepSignIn stores what a sign-in got, for every session on the connection.
func (s *Service) keepSignIn(connectorID string, oc *oauth2.Config, tok *oauth2.Token) {
	st := storedToken{Token: tok}
	if oc != nil {
		st.TokenURL, st.AuthStyle = oc.Endpoint.TokenURL, oc.Endpoint.AuthStyle
		st.ClientID, st.ClientSecret, st.Scopes = oc.ClientID, oc.ClientSecret, oc.Scopes
	}
	c := s.cell(connectorID)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st, c.loaded = st, true
	s.writeToken(connectorID, st)
}

func (s *Service) writeToken(connectorID string, st storedToken) {
	raw := ""
	if st.Token != nil {
		b, _ := json.Marshal(st)
		raw = string(b)
	}
	s.db.Model(&db.Connector{}).Where("id = ?", connectorID).Update("token_json", raw)
}

// forgetToken signs a connection out.
func (s *Service) forgetToken(connectorID string) {
	c := s.cell(connectorID)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st, c.loaded = storedToken{}, true
	s.writeToken(connectorID, storedToken{})
}

// connToken is a connection's token as the TokenSource every session on it
// shares. Refreshing happens once, under the connection's lock, from the
// stored token as it is now: two Bots' sessions meeting an expired token make
// one refresh between them, so a provider that rotates refresh tokens (and
// kills the old one) does not strand the other.
type connToken struct {
	s      *Service
	id     string
	client *http.Client
}

func (t connToken) Token() (*oauth2.Token, error) {
	c := t.s.cell(t.id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		var row db.Connector
		t.s.db.Select("token_json").Where("id = ?", t.id).Limit(1).Find(&row)
		c.st, c.loaded = readToken(row.TokenJSON), true
	}
	if c.st.Token == nil {
		return nil, errNoToken
	}
	if c.st.Token.Valid() {
		return c.st.Token, nil
	}
	cfg := c.st.config()
	if cfg == nil {
		// Nothing can refresh it: the server's 401 asks for a new sign-in.
		return c.st.Token, nil
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, t.client)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	nt, err := cfg.TokenSource(ctx, c.st.Token).Token()
	if err != nil {
		return nil, fmt.Errorf("refresh the sign-in: %w", err)
	}
	if nt.RefreshToken == "" {
		nt.RefreshToken = c.st.Token.RefreshToken
	}
	c.st.Token = nt
	t.s.writeToken(t.id, c.st)
	return nt, nil
}
