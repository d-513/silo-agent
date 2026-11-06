package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// slowMCP is an echo MCP server whose first request waits until released, so
// a test can act while a grant's first connect is still in flight. It notes
// the X-Tenant header of every request.
type slowMCP struct {
	*httptest.Server
	arrived chan struct{}
	release chan struct{}
	first   sync.Once
	mu      sync.Mutex
	tenants []string
}

func newSlowMCP(t *testing.T) *slowMCP {
	t.Helper()
	m := &slowMCP{arrived: make(chan struct{}), release: make(chan struct{})}
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return apptest.NewEchoServer() }, nil)
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.tenants = append(m.tenants, r.Header.Get("X-Tenant"))
		m.mu.Unlock()
		held := false
		m.first.Do(func() { held = true })
		if held {
			close(m.arrived)
			<-m.release
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		m.open()
		// A session the CP kept holds a stream open; Close would wait on it.
		m.CloseClientConnections()
		m.Close()
	})
	return m
}

func (m *slowMCP) open() {
	select {
	case <-m.release:
	default:
		close(m.release)
	}
}

func (m *slowMCP) waitArrived(t *testing.T) {
	t.Helper()
	select {
	case <-m.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the connector never reached its server")
	}
}

func (m *slowMCP) lastTenant() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tenants[len(m.tenants)-1]
}

// A grant revoked while its first connect is still under way stays revoked:
// the connect finishing late must not put the row back.
func TestRevokedGrantStaysRevoked(t *testing.T) {
	srv := newSlowMCP(t)
	h := apptest.New(t)
	bot := h.CreateBot("Scout")
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: srv.URL, Auth: "none", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	srv.waitArrived(t)
	if _, err := h.Client.RevokeConnector(h.Ctx(), rq(&v1.RevokeConnectorRequest{BotId: bot.GetId(), ConnectorId: made.Msg.GetConnector().GetId()})); err != nil {
		t.Fatal(err)
	}
	srv.open()
	// Give the late connect time to finish and write.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var n int64
		h.DB.Model(&db.BotConnector{}).Where("id = ?", made.Msg.GetId()).Count(&n)
		if n != 0 {
			t.Fatal("the revoked grant came back when its connect finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An edit made while a grant is still connecting is what the grant ends up
// with: the session from before the edit is not kept, and the old connect
// finishing late does not have the last word on the grant's status.
func TestEditWhileConnectingWins(t *testing.T) {
	srv := newSlowMCP(t)
	h := apptest.New(t)
	bot := h.CreateBot("Scout")
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: srv.URL, Auth: "none", DefaultMode: "allow",
		Headers: []*v1.HeaderInput{{Name: "X-Tenant", Value: "old"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	conn := made.Msg.GetConnector()
	srv.waitArrived(t)
	if _, err := h.Client.UpdateConnector(h.Ctx(), rq(&v1.UpdateConnectorRequest{
		Id: conn.GetId(), Name: conn.GetName(), Headers: []*v1.HeaderInput{{Name: "X-Tenant", Value: "new"}},
	})); err != nil {
		t.Fatal(err)
	}
	srv.open()
	waitGrantStatus(t, h, made.Msg.GetId(), "authorized")
	if e := callEcho(t, h, bot.GetId(), "echo"); e != "" {
		t.Fatal(e)
	}
	if got := srv.lastTenant(); got != "new" {
		t.Fatalf("the call went out with X-Tenant %q, from the session made before the edit", got)
	}
}

// Signing a connection out while a refresh is under way leaves it signed
// out, whenever that refresh ends.
func TestSignOutWhileConnectingWins(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Scout")
	srv2 := newSlowMCP(t)
	made2, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo2", Transport: "http", HttpUrl: srv2.URL, Auth: "none", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	srv2.waitArrived(t)
	if _, err := h.Client.UpdateConnector(h.Ctx(), rq(&v1.UpdateConnectorRequest{
		Id: made2.Msg.GetConnector().GetId(), Name: "Echo2", Auth: "oauth",
	})); err != nil {
		t.Fatal(err)
	}
	srv2.open()
	time.Sleep(500 * time.Millisecond)
	waitGrantStatus(t, h, made2.Msg.GetId(), "needs_auth")
}

func waitGrantStatus(t *testing.T, h *apptest.H, grantID, want string) db.BotConnector {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var row db.BotConnector
	for {
		h.DB.Where("id = ?", grantID).Limit(1).Find(&row)
		if row.AuthStatus == want {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("grant is %q (%s), want %q", row.AuthStatus, row.LastError, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// oauthMCP is an echo MCP server behind OAuth: protected-resource metadata,
// an authorization server with client registration, and a token endpoint
// that hands out bearer tokens the MCP endpoint accepts.
type oauthMCP struct {
	*httptest.Server
	issued atomic.Int32
}

func newOAuthMCP(t *testing.T) *oauthMCP {
	t.Helper()
	m := &oauthMCP{}
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return apptest.NewEchoServer() }, nil)
	mux := http.NewServeMux()
	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Close)
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{"resource": m.URL + "/mcp", "authorization_servers": []string{m.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]any{
			"issuer": m.URL, "authorization_endpoint": m.URL + "/authorize", "token_endpoint": m.URL + "/token",
			"registration_endpoint": m.URL + "/register", "response_types_supported": []string{"code"},
			"code_challenge_methods_supported":               []string{"S256"},
			"authorization_response_iss_parameter_supported": true,
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		js(w, map[string]any{"client_id": "silo-test", "token_endpoint_auth_method": "none"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n := m.issued.Add(1)
		js(w, map[string]any{"access_token": "tok-" + string(rune('0'+n)), "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+m.URL+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
	return m
}

// startSignIn starts a sign-in of a connection and returns the state the
// provider will send back.
func startSignIn(t *testing.T, cl interface {
	StartConnectorAuth(ctx context.Context, req *connect.Request[v1.StartConnectorAuthRequest]) (*connect.Response[v1.StartConnectorAuthResponse], error)
}, h *apptest.H, connectorID string, shared bool) string {
	t.Helper()
	res, err := cl.StartConnectorAuth(h.Ctx(), rq(&v1.StartConnectorAuthRequest{ConnectorId: connectorID, Shared: shared}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(res.Msg.GetAuthorizeUrl())
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

// finishSignIn is the provider sending the browser back with a code.
func finishSignIn(t *testing.T, h *apptest.H, hc *http.Client, state string) (int, string) {
	t.Helper()
	resp, err := hc.Get(h.URL + "/oauth/callback?code=the-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// A sign-in nobody finishes leaves the connection as it was: a grant that
// works keeps working, and the popup's late callback says nothing was
// connected rather than "Authorized".
func TestAbandonedSignInChangesNothing(t *testing.T) {
	srv := newOAuthMCP(t)
	h := apptest.New(t)
	h.App.Connectors.SignInWait = 400 * time.Millisecond
	bot := h.CreateBot("Scout")
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: srv.URL + "/mcp", Auth: "oauth", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	conn := made.Msg.GetConnector()
	if made.Msg.GetAuthStatus() != "needs_auth" {
		t.Fatalf("a new OAuth grant is %q", made.Msg.GetAuthStatus())
	}

	// Signed in: the popup says so once the grant works.
	code, body := finishSignIn(t, h, h.HTTP, startSignIn(t, h.Client, h, conn.GetId(), false))
	if code != http.StatusOK || !strings.Contains(body, "Authorized") {
		t.Fatalf("finishing the sign-in: %d %s", code, body)
	}
	waitGrantStatus(t, h, made.Msg.GetId(), "authorized")
	if e := callEcho(t, h, bot.GetId(), "echo"); e != "" {
		t.Fatal(e)
	}

	// A second sign-in is started and never finished: the popup was closed.
	state := startSignIn(t, h.Client, h, conn.GetId(), false)
	time.Sleep(time.Second)
	row := waitGrantStatus(t, h, made.Msg.GetId(), "authorized")
	if row.LastError != "" {
		t.Fatalf("the abandoned sign-in left an error: %q", row.LastError)
	}
	if e := callEcho(t, h, bot.GetId(), "echo"); e != "" {
		t.Fatalf("the grant stopped working after an abandoned sign-in: %s", e)
	}
	// The provider answers after the sign-in gave up waiting.
	code, body = finishSignIn(t, h, h.HTTP, state)
	if code == http.StatusOK || strings.Contains(body, "Authorized") {
		t.Fatalf("a callback nobody waits for: %d %s", code, body)
	}
	time.Sleep(300 * time.Millisecond)
	if row := waitGrantStatus(t, h, made.Msg.GetId(), "authorized"); row.LastError != "" {
		t.Fatalf("the late callback left an error: %q", row.LastError)
	}
}

// The lending is asked again when the token arrives: a personal sign-in
// started while the Bot was its starter's alone is not kept once someone else
// is on the Bot.
func TestSignInIsCheckedWhenTheTokenArrives(t *testing.T) {
	srv := newOAuthMCP(t)
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bot := h.CreateBot("Mine")
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: srv.URL + "/mcp", Auth: "oauth", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	conn := made.Msg.GetConnector()
	state := startSignIn(t, h.Client, h, conn.GetId(), false)

	// Bob joins the Bot while the popup is open.
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	if _, err := h.Client.AddBotPerson(h.Ctx(), rq(&v1.AddBotPersonRequest{BotId: bot.GetId(), UserId: bu.ID, Role: access.RoleUse, Confirmed: true})); err != nil {
		t.Fatal(err)
	}
	code, body := finishSignIn(t, h, h.HTTP, state)
	if code == http.StatusOK || !strings.Contains(body, "Mine") {
		t.Fatalf("a personal sign-in for a Bot Bob now uses: %d %s", code, body)
	}
	var c db.Connector
	h.DB.Where("id = ?", conn.GetId()).Find(&c)
	if c.TokenJSON != "" || c.LentBy != "" {
		t.Fatalf("the refused sign-in was kept: lent by %q, token %q", c.LentBy, c.TokenJSON)
	}
	if row := waitGrantStatus(t, h, made.Msg.GetId(), "needs_auth"); row.LastError != "" {
		t.Fatalf("the grant: %q", row.LastError)
	}
}
