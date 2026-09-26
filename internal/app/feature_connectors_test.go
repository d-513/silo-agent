package app_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/toolsgen"
)

// echoHTTPServer mounts the fake MCP echo server over streamable HTTP.
func echoHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return apptest.NewEchoServer() }, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// TestHTTPConnectorEndToEnd attaches a real MCP HTTP server as a custom
// connector, waits for the CP's MCP client to authorize, and calls a tool —
// the full connector path minus Docker.
func TestHTTPConnectorEndToEnd(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	h := apptest.New(t)
	bot := h.CreateBot("MCP")

	created, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: mcpSrv.URL,
		Auth: "none", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatalf("CreateBotConnector: %v", err)
	}
	if created.Msg.GetConnector().GetKind() != "custom" || created.Msg.GetConnector().GetSourceId() != "" {
		t.Fatalf("custom connector shape %+v", created.Msg.GetConnector())
	}
	row := h.WaitConnector(bot.GetId(), "Echo")
	if row.GetAuthStatus() != "authorized" {
		t.Fatalf("auth status %q error %q", row.GetAuthStatus(), row.GetLastError())
	}

	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: toolsgen.Slug("Echo"), Action: "echo", ArgsJson: `{"q":"hello-mcp"}`,
	}))
	if err != nil || res.Msg.GetError() != "" {
		t.Fatalf("CallTool: %v %+v", err, res.Msg)
	}
	if res.Msg.GetResultJson() != "hello-mcp" {
		t.Fatalf("result %q", res.Msg.GetResultJson())
	}
}

func TestHTTPConnectorRespectsDenyRule(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	h := apptest.New(t)
	bot := h.CreateBot("MCPRules")
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: mcpSrv.URL,
		Auth: "none", DefaultMode: "allow",
	})); err != nil {
		t.Fatal(err)
	}
	h.WaitConnector(bot.GetId(), "Echo")
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "echo", Action: "echo", Decision: "deny",
	})); err != nil {
		t.Fatal(err)
	}
	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "echo", Action: "echo", ArgsJson: `{"q":"nope"}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Msg.GetError(), "denied") {
		t.Fatalf("expected deny, got %+v", res.Msg)
	}
}

func TestLibraryConnectorAttachCopies(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Library")
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Docs", Description: "d", Category: "Documentation", Transport: "http", HttpUrl: "http://127.0.0.1:1",
	}))
	if err != nil {
		t.Fatalf("CreateConnector: %v", err)
	}
	if lib.Msg.GetCategory() != "Documentation" {
		t.Fatalf("expected category Documentation, got %q", lib.Msg.GetCategory())
	}
	att, err := h.Client.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{
		BotId: bot.GetId(), ConnectorId: lib.Msg.GetId(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if att.Msg.GetConnector().GetKind() != "custom" || att.Msg.GetConnector().GetSourceId() != lib.Msg.GetId() {
		t.Fatalf("attach shape %+v", att.Msg.GetConnector())
	}
	if att.Msg.GetConnector().GetCategory() != "Documentation" {
		t.Fatalf("expected attached connector category Documentation, got %q", att.Msg.GetConnector().GetCategory())
	}
	// The library row set is unchanged; attach copies rather than shares.
	listed, _ := h.Client.ListConnectors(h.Ctx(), connect.NewRequest(&v1.ListConnectorsRequest{}))
	count := 0
	for _, c := range listed.Msg.GetConnectors() {
		if c.GetId() == lib.Msg.GetId() {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("library rows with id: %d", count)
	}
	// Attaching the same preset again makes a second, distinct copy.
	second, err := h.Client.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{
		BotId: bot.GetId(), ConnectorId: lib.Msg.GetId(),
	}))
	if err != nil || second.Msg.GetConnector().GetName() != "Docs 2" {
		t.Fatalf("second attach: %v %+v", err, second.Msg.GetConnector())
	}
	if _, err := h.Client.DetachConnector(h.Ctx(), connect.NewRequest(&v1.DetachConnectorRequest{
		BotId: bot.GetId(), Id: att.Msg.GetId(),
	})); err != nil {
		t.Fatal(err)
	}
	xs, _ := h.Client.ListBotConnectors(h.Ctx(), connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: bot.GetId()}))
	if n := len(copiesOf(xs.Msg.GetConnectors(), lib.Msg.GetId())); n != 1 {
		t.Fatalf("expected one attachment left, got %d", n)
	}
}

// copiesOf filters a Bot's attachments to those copied from one library row,
// so seeded auto-attach presets (Lightpanda) do not skew counts.
func copiesOf(xs []*v1.BotConnector, libID string) []*v1.BotConnector {
	var out []*v1.BotConnector
	for _, x := range xs {
		if x.GetConnector().GetSourceId() == libID {
			out = append(out, x)
		}
	}
	return out
}

func TestAutoAttachCopiedOnce(t *testing.T) {
	h := apptest.New(t)
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "AutoDocs", Transport: "http", HttpUrl: "http://127.0.0.1:1",
		AutoAttach: true, Prompt: "Use for docs.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	bot := h.CreateBot("Auto")
	xs, _ := h.Client.ListBotConnectors(h.Ctx(), connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: bot.GetId()}))
	docs := copiesOf(xs.Msg.GetConnectors(), lib.Msg.GetId())
	if len(docs) != 1 || !strings.HasPrefix(docs[0].GetConnector().GetName(), "AutoDocs") {
		t.Fatalf("auto attach %+v", xs.Msg.GetConnectors())
	}
	// The seeded Lightpanda preset is auto-attached too, URL left as the variable.
	lightpanda := false
	for _, x := range xs.Msg.GetConnectors() {
		if x.GetConnector().GetName() == "Lightpanda" && x.GetConnector().GetHttpUrl() == "${LIGHTPANDA_URL}/mcp" {
			lightpanda = true
			// The chat maps a `call` event's `<slug>.<action>` back to this row's mark.
			if s := x.GetConnector().GetSlug(); s != "lightpanda" {
				t.Fatalf("lightpanda slug %q", s)
			}
		}
	}
	if !lightpanda {
		t.Fatalf("lightpanda not auto-attached: %+v", xs.Msg.GetConnectors())
	}
	if _, err := h.Client.DetachConnector(h.Ctx(), connect.NewRequest(&v1.DetachConnectorRequest{
		BotId: bot.GetId(), Id: docs[0].GetId(),
	})); err != nil {
		t.Fatal(err)
	}
	// A second ListBots/CreateBot must not re-add a preset the human removed.
	again, _ := h.Client.ListBotConnectors(h.Ctx(), connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: bot.GetId()}))
	if left := copiesOf(again.Msg.GetConnectors(), lib.Msg.GetId()); len(left) != 0 {
		t.Fatalf("removed auto-attach came back: %+v", left)
	}
}

func TestConnectorValidation(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Validation")
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Shell", Transport: "stdio", StdioCommand: "bash",
		StdioArgs: []string{"-c", "id"},
	})); err == nil || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("shell command should be rejected: %v", err)
	}
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Python", Transport: "http", HttpUrl: "http://x",
	})); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved name should be rejected: %v", err)
	}
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "NoTransport", Transport: "stdio",
	})); err == nil {
		t.Fatal("stdio without a command should be rejected")
	}
}

func TestOAuthClientCopiedOnAttach(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("OAuth")
	lib, err := h.Client.CreateConnector(h.Ctx(), connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "GitHub", Transport: "http", HttpUrl: "https://api.githubcopilot.com/mcp/",
		Auth: "oauth", OauthClientId: "iv1.abc", OauthClientSecret: "s3cret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Msg.GetOauthClientId() != "iv1.abc" || !lib.Msg.GetHasOauthClientSecret() {
		t.Fatalf("library oauth metadata %+v", lib.Msg)
	}
	att, err := h.Client.AttachConnector(h.Ctx(), connect.NewRequest(&v1.AttachConnectorRequest{
		BotId: bot.GetId(), ConnectorId: lib.Msg.GetId(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	var inst db.Connector
	h.DB.First(&inst, "id = ?", att.Msg.GetConnector().GetId())
	if inst.OAuthClientID != "iv1.abc" || inst.OAuthClientSecret != "s3cret" {
		t.Fatalf("oauth not copied: %+v", inst)
	}
	// An update that leaves the secret blank must keep the stored one.
	if _, err := h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: inst.ID, OauthClientId: "iv1.abc", OauthClientSecret: "",
	})); err != nil {
		t.Fatal(err)
	}
	h.DB.First(&inst, "id = ?", inst.ID)
	if inst.OAuthClientSecret != "s3cret" {
		t.Fatal("blank update wiped the stored oauth secret")
	}
}

// TestConnectorVarsResolveAtConnect attaches a connector whose URL is a
// ${NAME} reference and checks the CP connects using the resolved value.
func TestConnectorVarsResolveAtConnect(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	dir := t.TempDir()
	y := apptest.DefaultYAML(dir) + "connector_vars:\n  ECHO_URL: \"" + mcpSrv.URL + "\"\n"
	h := apptest.New(t, apptest.WithDataDir(dir), apptest.WithYAML(y))
	bot := h.CreateBot("Vars")
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "http", HttpUrl: "${ECHO_URL}",
		Auth: "none", DefaultMode: "allow",
	})); err != nil {
		t.Fatalf("CreateBotConnector: %v", err)
	}
	row := h.WaitConnector(bot.GetId(), "Echo")
	if row.GetAuthStatus() != "authorized" {
		t.Fatalf("auth status %q error %q", row.GetAuthStatus(), row.GetLastError())
	}
	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: toolsgen.Slug("Echo"), Action: "echo", ArgsJson: `{"q":"vars-ok"}`,
	}))
	if err != nil || res.Msg.GetError() != "" {
		t.Fatalf("CallTool: %v %+v", err, res.Msg)
	}
	if res.Msg.GetResultJson() != "vars-ok" {
		t.Fatalf("result %q", res.Msg.GetResultJson())
	}
}

func TestSetConnectorVarsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(path, []byte(apptest.DefaultYAML(dir)), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithConfigPath(path))
	got, err := h.Client.SetConnectorVars(h.Ctx(), connect.NewRequest(&v1.SetConnectorVarsRequest{
		ConnectorVars: []*v1.ConnectorVar{{Name: "TENANT", Value: "acme"}},
	}))
	if err != nil {
		t.Fatalf("SetConnectorVars: %v", err)
	}
	if len(got.Msg.GetConnectorVars()) != 1 || got.Msg.GetConnectorVars()[0].GetValue() != "acme" {
		t.Fatalf("settings vars %+v", got.Msg.GetConnectorVars())
	}
	if got.Msg.GetConnectorVars()[0].GetEnvName() != "SILO_CONNECTOR_VARS__TENANT" {
		t.Fatalf("env name %q", got.Msg.GetConnectorVars()[0].GetEnvName())
	}
	if _, err := h.Client.SetConnectorVars(h.Ctx(), connect.NewRequest(&v1.SetConnectorVarsRequest{
		ConnectorVars: []*v1.ConnectorVar{{Name: "1BAD", Value: "x"}},
	})); err == nil {
		t.Fatal("invalid name should be rejected")
	}
}
