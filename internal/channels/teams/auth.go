package teams

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

	"github.com/coreos/go-oidc/v3/oidc"
)

// --- who is calling us ---

// keySet is the Bot Framework's signing keys, read from where its OpenID
// metadata says they are and asked for again once a day.
func (a *Adapter) keySet(ctx context.Context) (oidc.KeySet, error) {
	a.keyMu.Lock()
	defer a.keyMu.Unlock()
	if a.keys != nil && time.Since(a.keysAt) < keysFor {
		return a.keys, nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openIDMetadataURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var meta struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&meta) != nil || meta.JWKSURI == "" {
		return nil, errors.New("the Bot Framework's OpenID metadata could not be read")
	}
	// The key set fetches on its own later, so it keeps the client, not this
	// request's context.
	a.keys = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), a.client), meta.JWKSURI)
	a.keysAt = time.Now()
	return a.keys, nil
}

// verify checks the token on an activity: signed by the Bot Framework, issued
// by it, for this bot's app id, and not expired. It answers the service
// address the token was made for, which the activity must name too.
func (a *Adapter) verify(ctx context.Context, appID, raw string) (serviceURL string, err error) {
	if appID == "" || raw == "" {
		return "", errors.New("no token")
	}
	keys, err := a.keySet(ctx)
	if err != nil {
		return "", err
	}
	tok, err := oidc.NewVerifier(tokenIssuer, keys, &oidc.Config{ClientID: appID}).Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	var claims map[string]any
	if err := tok.Claims(&claims); err != nil {
		return "", err
	}
	// Microsoft's reference writes the claim "serviceUrl"; the service sends
	// it lower-cased.
	for k, v := range claims {
		if s, ok := v.(string); ok && strings.EqualFold(k, "serviceurl") {
			return s, nil
		}
	}
	return "", errors.New("no service address in the token")
}

func bearer(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}

// sameService compares two service addresses, a trailing slash aside.
func sameService(x, y string) bool {
	return x != "" && strings.TrimRight(x, "/") == strings.TrimRight(y, "/")
}

// --- the bot's own token ---

// token is what the bot calls the connector with.
type token struct {
	creds   credentials
	value   string
	expires time.Time
}

// token answers the bot's token for the connector, asking Microsoft for one
// only when the kept one is about to run out or the credentials changed.
func (a *Adapter) token(ctx context.Context, channelID string, c credentials) (string, error) {
	if err := c.check(); err != nil {
		return "", err
	}
	a.tokMu.Lock()
	defer a.tokMu.Unlock()
	if t, ok := a.tokens[channelID]; ok && t.creds == c && time.Until(t.expires) > 5*time.Minute {
		return t.value, nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.appID},
		"client_secret": {c.secret},
		"scope":         {connectorScope},
	}
	endpoint := strings.TrimRight(loginURL, "/") + "/" + url.PathEscape(c.tenant) + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach Microsoft's sign-in service: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out)
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", loginError(resp.StatusCode, out.Error, out.Description, c)
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	a.tokens[channelID] = token{creds: c, value: out.AccessToken, expires: time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)}
	return out.AccessToken, nil
}

// loginError words Microsoft's refusal for the owner. Its description leads
// with an AADSTS code and a sentence; the trace ids after it are dropped.
func loginError(status int, code, desc string, c credentials) error {
	switch {
	case strings.Contains(desc, "AADSTS7000215"), strings.Contains(desc, "AADSTS7000222"):
		return errors.New("Microsoft rejected the client secret — it is wrong or expired. Make a new one under the app registration's Certificates & secrets and paste its Value (not its Secret ID)")
	case strings.Contains(desc, "AADSTS700016"):
		return errors.New("Microsoft does not know this Microsoft App ID in that tenant — copy the App ID and the App Tenant ID from the Azure Bot's Configuration")
	case strings.Contains(desc, "AADSTS90002"), strings.Contains(desc, "AADSTS900023"), code == "invalid_tenant":
		return errors.New("Microsoft does not know that tenant — copy the App Tenant ID from the Azure Bot's Configuration")
	}
	line, _, _ := strings.Cut(desc, "\n")
	line = strings.TrimSpace(strings.ReplaceAll(line, c.secret, "…"))
	if line == "" {
		line = code
	}
	if line == "" {
		return fmt.Errorf("Microsoft's sign-in service answered %d for the bot's credentials", status)
	}
	return fmt.Errorf("Microsoft refused the bot's credentials: %s", line)
}
