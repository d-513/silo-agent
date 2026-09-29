package drives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// oauthConfig renders the template's OAuth endpoints and scopes for v.
func (t *Template) oauthConfig(v Values, redirect string) (*oauth2.Config, error) {
	if t.Auth.Kind != AuthOAuth2 {
		return nil, errors.New(t.Title + " does not sign in with OAuth")
	}
	var miss []Missing
	for _, k := range []string{"client_id", "client_secret"} {
		if t.Value(v, KindSystem, k) == "" {
			miss = append(miss, Missing{Kind: KindSystem, Key: k})
		}
	}
	if len(miss) > 0 {
		return nil, &MissingError{Missing: miss}
	}
	var scopes []string
	for _, s := range t.Auth.Scopes {
		for f := range strings.FieldsSeq(t.Expand(s, v)) {
			scopes = append(scopes, f)
		}
	}
	return &oauth2.Config{
		ClientID:     t.Value(v, KindSystem, "client_id"),
		ClientSecret: t.Value(v, KindSystem, "client_secret"),
		RedirectURL:  redirect,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  t.Expand(t.Auth.AuthURL, v),
			TokenURL: t.Expand(t.Auth.TokenURL, v),
		},
	}, nil
}

// AuthCodeURL is where the sign-in popup goes.
func (t *Template) AuthCodeURL(v Values, redirect, state string) (string, error) {
	cfg, err := t.oauthConfig(v, redirect)
	if err != nil {
		return "", err
	}
	var opts []oauth2.AuthCodeOption
	for k, expr := range t.Auth.Params {
		opts = append(opts, oauth2.SetAuthURLParam(k, t.Expand(expr, v)))
	}
	return cfg.AuthCodeURL(state, opts...), nil
}

// CallbackValues picks the callback-sourced dynamic vars out of the redirect
// query (pCloud answers with the API host the account lives on).
func (t *Template) CallbackValues(q url.Values) map[string]string {
	out := map[string]string{}
	for _, d := range t.VarsOf(KindDynamic) {
		if d.Source != SourceCallback {
			continue
		}
		p := d.Param
		if p == "" {
			p = d.Key
		}
		if s := strings.TrimSpace(q.Get(p)); s != "" {
			out[d.Key] = s
		}
	}
	return out
}

// Exchange trades the callback code for a token, returned in rclone's config
// form (the JSON of an oauth2.Token). v must already hold the callback values.
func (t *Template) Exchange(ctx context.Context, v Values, redirect, code string) (string, error) {
	cfg, err := t.oauthConfig(v, redirect)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(code) == "" {
		return "", errors.New("the provider did not return an authorization code")
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	return TokenJSON(tok)
}

// TokenJSON is how rclone stores a token in its config.
func TokenJSON(tok *oauth2.Token) (string, error) {
	b, err := json.Marshal(struct {
		AccessToken  string    `json:"access_token"`
		TokenType    string    `json:"token_type,omitempty"`
		RefreshToken string    `json:"refresh_token,omitempty"`
		Expiry       time.Time `json:"expiry"`
	}{tok.AccessToken, tok.TokenType, tok.RefreshToken, tok.Expiry})
	return string(b), err
}

// ParseToken reads an rclone token JSON.
func ParseToken(raw string) (*oauth2.Token, error) {
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("token has no access_token")
	}
	return &tok, nil
}

// Refresh renews an expired (or nearly expired) token. It returns "" when the
// stored token is still good, and a MissingError for the token when the
// provider refuses the refresh (revoked access): the owner must reconnect.
func (t *Template) Refresh(ctx context.Context, v Values) (string, error) {
	tok, err := ParseToken(t.Value(v, KindDynamic, "token"))
	if err != nil {
		return "", &MissingError{Missing: []Missing{{Kind: KindDynamic, Key: "token"}}}
	}
	if tok.Expiry.IsZero() || time.Until(tok.Expiry) > time.Minute || tok.RefreshToken == "" {
		return "", nil
	}
	cfg, err := t.oauthConfig(v, "")
	if err != nil {
		return "", err
	}
	fresh, err := cfg.TokenSource(ctx, tok).Token()
	if err != nil {
		return "", fmt.Errorf("%w: %v", &MissingError{Missing: []Missing{{Kind: KindDynamic, Key: "token"}}}, err)
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	return TokenJSON(fresh)
}
