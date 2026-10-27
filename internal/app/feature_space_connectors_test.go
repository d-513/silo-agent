package app_test

import (
	"encoding/json"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// callEcho calls tools.echo.echo from a Bot's machine and returns the error
// the gateway gave (empty when the call went through).
func callEcho(t *testing.T, h *apptest.H, botID, slug string) string {
	t.Helper()
	res, err := h.WorkerClient(botID).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: slug, Action: "echo", ArgsJson: `{"q":"hi"}`}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.GetError()
}

// A connection is the Workspace's: added once, granted to several Bots, each
// with its own session, and taking it from one Bot leaves the others.
func TestConnectionIsGrantedToBots(t *testing.T) {
	mcpSrv := echoHTTPServer(t)
	h := apptest.New(t)
	scout := h.CreateBot("Scout")
	ledger, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Ledger", SpaceId: scout.GetSpaceId()}))
	if err != nil {
		t.Fatal(err)
	}
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: scout.GetId(), Name: "Echo", Transport: "http", HttpUrl: mcpSrv.URL, Auth: "none", DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	conn := made.Msg.GetConnector()
	if conn.GetSpaceId() != scout.GetSpaceId() || conn.GetCreatedBy() != h.Email || !conn.GetCanEdit() {
		t.Fatalf("connection: %+v", conn)
	}
	h.WaitConnector(scout.GetId(), "Echo")

	// Ledger gets the same connection, without adding it again.
	granted, err := h.Client.GrantConnector(h.Ctx(), rq(&v1.GrantConnectorRequest{BotId: ledger.Msg.GetId(), ConnectorId: conn.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(granted.Msg.GetAlsoIn()) != 1 || granted.Msg.GetAlsoIn()[0] != "Scout" {
		t.Fatalf("Ledger's grant is also in %v", granted.Msg.GetAlsoIn())
	}
	if row := h.WaitConnector(ledger.Msg.GetId(), "Echo"); row.GetAuthStatus() != "authorized" {
		t.Fatalf("Ledger's session: %q %q", row.GetAuthStatus(), row.GetLastError())
	}
	for _, b := range []string{scout.GetId(), ledger.Msg.GetId()} {
		if e := callEcho(t, h, b, "echo"); e != "" {
			t.Fatalf("calling from %s: %s", b, e)
		}
	}
	hub := func() *v1.Connector {
		t.Helper()
		listed, err := h.Client.ListSpaceConnectors(h.Ctx(), rq(&v1.ListSpaceConnectorsRequest{SpaceId: scout.GetSpaceId()}))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range listed.Msg.GetConnectors() {
			if c.GetId() == conn.GetId() {
				return c
			}
		}
		return nil
	}
	if c := hub(); c == nil || len(c.GetUsedBy()) != 2 {
		t.Fatalf("the hub: %v", c)
	}

	// Taking it from Scout leaves Ledger's.
	if _, err := h.Client.RevokeConnector(h.Ctx(), rq(&v1.RevokeConnectorRequest{BotId: scout.GetId(), ConnectorId: conn.GetId()})); err != nil {
		t.Fatal(err)
	}
	if e := callEcho(t, h, scout.GetId(), "echo"); e == "" {
		t.Fatal("Scout still reaches a connector taken from it")
	}
	if e := callEcho(t, h, ledger.Msg.GetId(), "echo"); e != "" {
		t.Fatalf("Ledger lost it too: %s", e)
	}
	if c := hub(); c == nil || len(c.GetUsedBy()) != 1 {
		t.Fatalf("the connection stays in the workspace: %v", c)
	}

	// Deleting the connection takes it from every Bot.
	if _, err := h.Client.DeleteConnector(h.Ctx(), rq(&v1.DeleteConnectorRequest{Id: conn.GetId()})); err != nil {
		t.Fatal(err)
	}
	var grants int64
	h.DB.Model(&db.BotConnector{}).Where("connector_id = ?", conn.GetId()).Count(&grants)
	if grants != 0 {
		t.Fatalf("%d grants outlived their connection", grants)
	}
}

// TestGrantsStayInSpace: a connection reaches only Bots of its own Workspace,
// even another Workspace of the same person.
func TestGrantsStayInSpace(t *testing.T) {
	h := apptest.New(t)
	home := h.CreateBot("Home")
	other, err := h.Client.CreateSpace(h.Ctx(), rq(&v1.CreateSpaceRequest{Name: "Other"}))
	if err != nil {
		t.Fatal(err)
	}
	away, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "Away", SpaceId: other.Msg.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		BotId: home.GetId(), Name: "Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1/mcp", Auth: "none",
	}))
	if err != nil {
		t.Fatal(err)
	}
	connID := made.Msg.GetConnector().GetId()
	refused := func(what string, err error) {
		t.Helper()
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("%s across workspaces: %v", what, err)
		}
	}
	_, err = h.Client.GrantConnector(h.Ctx(), rq(&v1.GrantConnectorRequest{BotId: away.Msg.GetId(), ConnectorId: connID}))
	refused("GrantConnector", err)
	_, err = h.Client.AttachConnector(h.Ctx(), rq(&v1.AttachConnectorRequest{BotId: away.Msg.GetId(), ConnectorId: connID}))
	refused("AttachConnector", err)
	_, err = h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		SpaceId: home.GetSpaceId(), BotIds: []string{away.Msg.GetId()}, Name: "More", Transport: "http", HttpUrl: "http://127.0.0.1:1/mcp", Auth: "none",
	}))
	refused("CreateBotConnector", err)
	var grants int64
	h.DB.Model(&db.Connector{}).Joins("JOIN bot_connectors g ON g.connector_id = connectors.id").
		Where("g.bot_id = ? AND connectors.space_id <> ?", away.Msg.GetId(), other.Msg.GetId()).Count(&grants)
	if grants != 0 {
		t.Fatalf("a Bot holds %d grants of another workspace's connections", grants)
	}
}

// A connection made from the hub goes to the Bots chosen for it.
func TestHubAddGrantsTheChosenBots(t *testing.T) {
	h := apptest.New(t)
	a := h.CreateBot("A")
	b, _ := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "B", SpaceId: a.GetSpaceId()}))
	c, _ := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "C", SpaceId: a.GetSpaceId()}))
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		SpaceId: a.GetSpaceId(), BotIds: []string{a.GetId(), b.Msg.GetId()}, Name: "Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1/mcp", Auth: "none",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if made.Msg.GetId() != "" || len(made.Msg.GetConnector().GetUsedBy()) != 2 {
		t.Fatalf("hub add: %+v", made.Msg)
	}
	var onC int64
	h.DB.Model(&db.BotConnector{}).Where("bot_id = ? AND connector_id = ?", c.Msg.GetId(), made.Msg.GetConnector().GetId()).Count(&onC)
	if onC != 0 {
		t.Fatal("a Bot nobody chose got the connection")
	}
}

// Renaming a connection keeps every granted Bot's rules for it.
func TestRenameMovesRules(t *testing.T) {
	h := apptest.New(t)
	a := h.CreateBot("A")
	b, _ := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "B", SpaceId: a.GetSpaceId()}))
	made, err := h.Client.CreateBotConnector(h.Ctx(), rq(&v1.CreateBotConnectorRequest{
		SpaceId: a.GetSpaceId(), BotIds: []string{a.GetId(), b.Msg.GetId()}, Name: "Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1/mcp", Auth: "none",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, bot := range []string{a.GetId(), b.Msg.GetId()} {
		if _, err := h.Client.SetRule(h.Ctx(), rq(&v1.SetRuleRequest{BotId: bot, Connector: "docs", Action: "search", Decision: "deny"})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.Client.UpdateConnector(h.Ctx(), rq(&v1.UpdateConnectorRequest{Id: made.Msg.GetConnector().GetId(), Name: "Wiki"})); err != nil {
		t.Fatal(err)
	}
	for _, bot := range []string{a.GetId(), b.Msg.GetId()} {
		var old, moved int64
		h.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ?", bot, "docs").Count(&old)
		h.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ? AND action = ? AND decision = ?", bot, "wiki", "search", "deny").Count(&moved)
		if old != 0 || moved != 1 {
			t.Fatalf("bot %s: %d rules left on the old slug, %d moved", bot, old, moved)
		}
	}
}

// Two Bots that start with the same preset share one connection.
func TestDefaultsShareOneConnection(t *testing.T) {
	dir := t.TempDir()
	h := apptest.New(t, apptest.WithDataDir(dir), apptest.WithYAML(apptest.DefaultYAML(dir)+"autoenable_connectors:\n  - shared_docs\n"))
	if _, err := h.Client.CreateConnector(h.Ctx(), rq(&v1.CreateConnectorRequest{Name: "Shared Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1"})); err != nil {
		t.Fatal(err)
	}
	a := h.CreateBot("A")
	if _, err := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "B", SpaceId: a.GetSpaceId()})); err != nil {
		t.Fatal(err)
	}
	var conns []db.Connector
	h.DB.Where("space_id = ? AND name = ?", a.GetSpaceId(), "Shared Docs").Find(&conns)
	if len(conns) != 1 {
		t.Fatalf("%d connections for one preset", len(conns))
	}
	var grants int64
	h.DB.Model(&db.BotConnector{}).Where("connector_id = ?", conns[0].ID).Count(&grants)
	if grants != 2 {
		t.Fatalf("%d grants, want one per Bot", grants)
	}
}

// ${secrets.NAME} in a connection is each Bot's own secret.
func TestConnectorSecretVarsArePerBot(t *testing.T) {
	h := apptest.New(t)
	a := h.CreateBot("A")
	b, _ := h.Client.CreateBot(h.Ctx(), rq(&v1.CreateBotRequest{Name: "B", SpaceId: a.GetSpaceId()}))
	for bot, value := range map[string]string{a.GetId(): "key-of-a", b.Msg.GetId(): "key-of-b"} {
		if _, err := h.Client.AddSecret(h.Ctx(), rq(&v1.AddSecretRequest{BotId: bot, Name: "API_KEY", Value: value})); err != nil {
			t.Fatal(err)
		}
	}
	c := &db.Connector{SpaceID: a.GetSpaceId(), HTTPURL: "https://api.example.com/mcp", HeadersJSON: `{"Authorization":"Bearer ${secrets.API_KEY}"}`}
	for bot, want := range map[string]string{a.GetId(): "Bearer key-of-a", b.Msg.GetId(): "Bearer key-of-b"} {
		var hdr map[string]string
		_ = json.Unmarshal([]byte(h.App.Connectors.Resolve(c, bot).HeadersJSON), &hdr)
		if hdr["Authorization"] != want {
			t.Fatalf("bot %s sends %q, want %q", bot, hdr["Authorization"], want)
		}
	}
}
