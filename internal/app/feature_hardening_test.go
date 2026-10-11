package app_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// A page on another origin must not be able to open a Bot's console or
// desktop with the owner's cookie: a WebSocket is not held back by CORS, so
// the Origin header is the only thing between such a page and a shell.
func TestBotSocketsRefuseForeignOrigin(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Sockets")
	h.StartWorker(bot.GetId())
	h.WaitBotStatus(bot.GetId(), "online")

	dial := func(path, origin string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, h.URL+path+"?bot="+bot.GetId(), nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		// HTTP/1.1, as a browser's socket handshake is.
		hc := &http.Client{Jar: h.HTTP.Jar, Transport: &http.Transport{}}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for _, path := range []string{"/console", "/vnc"} {
		if got := dial(path, "https://evil.example"); got != http.StatusForbidden {
			t.Fatalf("%s from a foreign origin: status %d, want 403", path, got)
		}
		// A tunnelled app shares the site, and so the cookie, but not the origin.
		if got := dial(path, "http://app.tunnels.example"); got != http.StatusForbidden {
			t.Fatalf("%s from a sibling origin: status %d, want 403", path, got)
		}
		if got := dial(path, h.URL); got != http.StatusSwitchingProtocols {
			t.Fatalf("%s from the console's own origin: status %d, want 101", path, got)
		}
		// A native client (the iOS app) sends no Origin at all.
		if got := dial(path, ""); got != http.StatusSwitchingProtocols {
			t.Fatalf("%s with no origin: status %d, want 101", path, got)
		}
	}
}

// A file the Bot wrote is a stranger's bytes. Asked for inline, an HTML or SVG
// one must still never run as a page in the control plane's own origin.
func TestArtifactFileIsNeverAPage(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Files")
	w := h.StartWorker(bot.GetId())
	h.WaitBotStatus(bot.GetId(), "online")
	for name, body := range map[string]string{
		"page.html": "<script>alert(1)</script>",
		"pic.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"shot.png":  "\x89PNG\r\n\x1a\n",
	} {
		if err := os.WriteFile(filepath.Join(w.Workspace, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	get := func(name string) *http.Response {
		t.Helper()
		q := url.Values{"bot_id": {bot.GetId()}, "path": {name}, "inline": {"1"}}
		res, err := h.HTTP.Get(h.URL + "/artifacts/file?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", name, res.StatusCode)
		}
		return res
	}
	for _, name := range []string{"page.html", "pic.svg"} {
		res := get(name)
		if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") {
			t.Fatalf("%s served inline: %q", name, res.Header.Get("Content-Disposition"))
		}
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%s has no sandbox policy: %q", name, res.Header.Get("Content-Security-Policy"))
		}
		if res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s may be sniffed", name)
		}
	}
	// A plain image still opens in place.
	if res := get("shot.png"); !strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("png not inline: %q", res.Header.Get("Content-Disposition"))
	}
}

// A connector's mark is uploaded by any user and shown to every other one, so
// it is an image or it is refused, whatever type the upload claims.
func TestConnectorImageIsOnlyAnImage(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Marks")
	add := func(img []byte, typ string) (*v1.BotConnector, error) {
		res, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
			BotId: bot.GetId(), Name: "Mark", Transport: "http", HttpUrl: "http://127.0.0.1:1",
			Image: img, ImageType: typ,
		}))
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}
	if _, err := add([]byte("<script>alert(1)</script>"), "text/html"); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("an HTML page was taken as a connector image: %v", err)
	}
	if _, err := add([]byte("<html><script>alert(1)</script></html>"), ""); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("sniffed HTML was taken as a connector image: %v", err)
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	// The claimed type is not believed: the bytes say what it is.
	row, err := add(png, "text/html")
	if err != nil {
		t.Fatalf("png refused: %v", err)
	}
	res, err := h.HTTP.Get(h.URL + "/connectors/" + row.GetConnector().GetId() + "/image")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type %q", got)
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("image headers %v", res.Header)
	}
	// A row from before the check is never served as a page either.
	h.DB.Model(&db.Connector{}).Where("id = ?", row.GetConnector().GetId()).Updates(map[string]any{"image_type": "text/html", "image": []byte("<script>alert(1)</script>")})
	res, err = h.HTTP.Get(h.URL + "/connectors/" + row.GetConnector().GetId() + "/image")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if strings.Contains(res.Header.Get("Content-Type"), "html") {
		t.Fatalf("an old row is served as %q", res.Header.Get("Content-Type"))
	}
}

// headerSink is a server that records one request header of everything sent
// to it: where a re-pointed connector would deliver a preset's secret.
func headerSink(t *testing.T, name string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if v := r.Header.Get(name); v != "" {
			got = append(got, v)
		}
		mu.Unlock()
		http.Error(w, "nope", http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// A library preset's header values and OAuth client secret are the operator's.
// A user's copy may use them where the preset points, and loses them the
// moment it is pointed anywhere else.
func TestPresetSecretsDoNotFollowARetargetedCopy(t *testing.T) {
	h := apptest.New(t)
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Shared", Transport: "http", HttpUrl: "https://mcp.example.com/mcp", Auth: "oauth",
		OauthClientId: "client", OauthClientSecret: "operator-secret",
		Headers: []*v1.HeaderInput{{Name: "X-Api-Key", Value: "operator-key"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	sink, seen := headerSink(t, "X-Api-Key")

	// Anyone with a Bot can attach the preset.
	user, _ := h.SignedInUser("mallory@example.com")
	created, err := user.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Mallory"}))
	if err != nil {
		t.Fatal(err)
	}
	botID := created.Msg.GetId()
	att, err := user.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{BotId: botID, ConnectorId: lib.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	connID := att.Msg.GetConnector().GetId()
	stored := func() db.Connector {
		var c db.Connector
		h.DB.First(&c, "id = ?", connID)
		return c
	}
	if c := stored(); !strings.Contains(c.HeadersJSON, "operator-key") || c.OAuthClientSecret != "operator-secret" {
		t.Fatalf("the copy should start with the preset's values: %+v", c)
	}
	// An edit that leaves the address alone keeps them.
	if _, err := user.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{Id: connID, Description: "mine"})); err != nil {
		t.Fatal(err)
	}
	if c := stored(); !strings.Contains(c.HeadersJSON, "operator-key") || c.OAuthClientSecret != "operator-secret" {
		t.Fatalf("an unrelated edit dropped the preset's values: %+v", c)
	}
	// Pointing the copy at another server keeps only what the edit itself gave.
	if _, err := user.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: connID, HttpUrl: sink.URL, Auth: "none",
		Headers: []*v1.HeaderInput{{Name: "X-Api-Key"}, {Name: "X-Mine", Value: "my-own"}},
	})); err != nil {
		t.Fatal(err)
	}
	c := stored()
	if strings.Contains(c.HeadersJSON, "operator-key") || c.OAuthClientSecret != "" {
		t.Fatalf("the preset's secrets followed the copy to %s: headers %s secret %q", sink.URL, c.HeadersJSON, c.OAuthClientSecret)
	}
	if !strings.Contains(c.HeadersJSON, "my-own") {
		t.Fatalf("the user's own header was dropped: %s", c.HeadersJSON)
	}
	if _, err := user.RefreshBotConnector(h.Ctx(), connect.NewRequest(&v1.RefreshBotConnectorRequest{BotId: botID, Id: att.Msg.GetId()})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := seen(); len(got) > 0 {
		t.Fatalf("the operator's key reached the user's server: %v", got)
	}

	// The same holds for a copy that starts out pointed elsewhere.
	made, err := user.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: botID, SourceId: lib.Msg.GetId(), Name: "Shared", Transport: "http", HttpUrl: sink.URL, Auth: "none",
	}))
	if err != nil {
		t.Fatal(err)
	}
	connID = made.Msg.GetConnector().GetId()
	if c := stored(); strings.Contains(c.HeadersJSON, "operator-key") || c.OAuthClientSecret != "" {
		t.Fatalf("a copy made at another address took the preset's secrets: headers %s secret %q", c.HeadersJSON, c.OAuthClientSecret)
	}
	time.Sleep(300 * time.Millisecond)
	if got := seen(); len(got) > 0 {
		t.Fatalf("the operator's key reached the user's server: %v", got)
	}
}

// A STDIO preset's environment is the operator's too: a copy that runs another
// program does not get it.
func TestPresetEnvDoesNotFollowAnotherCommand(t *testing.T) {
	// The fake host starts no sidecar, so nothing here runs the command.
	h := apptest.New(t)
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Tooling", Transport: "stdio", StdioCommand: "npx", StdioArgs: []string{"-y", "some-server"},
		Env: []*v1.EnvInput{{Name: "API_KEY", Value: "operator-key"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	user, _ := h.SignedInUser("mallory@example.com")
	created, err := user.CreateBot(h.Ctx(), connect.NewRequest(&v1.CreateBotRequest{Name: "Mallory"}))
	if err != nil {
		t.Fatal(err)
	}
	att, err := user.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{BotId: created.Msg.GetId(), ConnectorId: lib.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	connID := att.Msg.GetConnector().GetId()
	if _, err := user.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: connID, StdioCommand: "node", StdioArgs: []string{"-e", "console.log(process.env.API_KEY)"},
	})); err != nil {
		t.Fatal(err)
	}
	var c db.Connector
	h.DB.First(&c, "id = ?", connID)
	if strings.Contains(c.EnvJSON, "operator-key") {
		t.Fatalf("the preset's environment followed the copy to another command: %s", c.EnvJSON)
	}
}

// Sidecars run whatever package their Bot's owner names, as root. They must
// not share a writable npm directory across Bots: npx runs what it finds
// there, so one owner's sidecar could plant code for everyone else's.
func TestStdioSidecarCacheIsPerBot(t *testing.T) {
	h := newStdioHarness(t)
	cmd, args, env := stdioChild(t)
	specOf := func(name string) (string, string) {
		bot := h.CreateBot(name)
		att, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
			BotId: bot.GetId(), Name: "Echo", Transport: "stdio",
			StdioCommand: cmd, StdioArgs: args, Env: env, DefaultMode: "allow",
		}))
		if err != nil {
			t.Fatal(err)
		}
		h.WaitConnectorID(bot.GetId(), att.Msg.GetId())
		spec, ok := h.Fake.StdioSpec(att.Msg.GetId())
		if !ok {
			t.Fatal("no sidecar spec recorded")
		}
		return bot.GetId(), spec.BotID
	}
	a, specA := specOf("One")
	b, specB := specOf("Two")
	if specA != a || specB != b {
		t.Fatalf("sidecars are not told whose they are: %q for %s, %q for %s", specA, a, specB, b)
	}
}

// With auth.host_cookie on, signing in hands out the __Host- cookie and the
// plain name stops being a session everywhere, so a cookie planted by a
// tunnelled app on a sibling subdomain signs nobody in.
func TestHostCookieSetting(t *testing.T) {
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithSignedOut(), apptest.WithDataDir(dir), apptest.WithYAML(apptest.DefaultYAML(dir)+
		"public_url: https://silo.example.com\nauth:\n  host_cookie: true\n"))
	// Read the Set-Cookie by hand: a jar would not send a Secure cookie back
	// to the harness's plain-http listener.
	body := strings.NewReader(`{"email":"` + h.Email + `","password":"` + h.Password + `"}`)
	req, _ := http.NewRequest(http.MethodPost, h.URL+"/silo.v1.UI/SignIn", body)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || len(res.Cookies()) != 1 {
		t.Fatalf("sign in: status %d cookies %v", res.StatusCode, res.Cookies())
	}
	session := res.Cookies()[0]
	if session.Name != "__Host-silo_session" || !session.Secure || session.Path != "/" || session.Domain != "" {
		t.Fatalf("session cookie %+v", session)
	}
	me := func(c *http.Cookie) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, h.URL+"/silo.v1.UI/Me", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(c)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := me(session); got != http.StatusOK {
		t.Fatalf("the prefixed cookie: status %d", got)
	}
	// The very same token under the plain name is what a sibling could plant.
	if got := me(&http.Cookie{Name: "silo_session", Value: session.Value}); got != http.StatusUnauthorized {
		t.Fatalf("the plain name is still a session: status %d", got)
	}
	// And the plain routes agree with the RPCs.
	raw, _ := http.NewRequest(http.MethodGet, h.URL+"/artifacts/file?bot_id=x&path=y", nil)
	raw.AddCookie(&http.Cookie{Name: "silo_session", Value: session.Value})
	res, err = http.DefaultClient.Do(raw)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/artifacts/file took the plain name: status %d", res.StatusCode)
	}
}
