package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
)

func TestRequireAdmin(t *testing.T) {
	a := testApp(t, nil)
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: false})
	if _, err := a.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{})); err == nil {
		t.Fatal("expected deny")
	}
	ctx = context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: true})
	if _, err := a.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{})); err != nil {
		t.Fatal(err)
	}
}

func TestCreateConnectorRejectsStdio(t *testing.T) {
	a := testApp(t, nil)
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: true})
	_, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "x", Transport: "stdio", HttpUrl: "http://x",
	}))
	if err == nil || !strings.Contains(err.Error(), "stdio") {
		t.Fatalf("got %v", err)
	}
}

func TestCreateAndAttachConnector(t *testing.T) {
	a := testApp(t, nil)
	admin := &db.User{ID: "u", Email: "a@b.c", Admin: true}
	a.DB.Create(admin)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, admin)
	c, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Wolfram", Description: "Compute", Transport: "http",
		HttpUrl: "http://127.0.0.1:1", Auth: "oauth",
		Headers: []*v1.HeaderInput{{Name: "X-Test", Value: "secret"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Msg.Name != "Wolfram" || c.Msg.Auth != "oauth" || c.Msg.DefaultMode != "ask" {
		t.Fatal(c.Msg)
	}
	if len(c.Msg.HeaderKeys) != 1 || c.Msg.HeaderKeys[0].Name != "X-Test" {
		t.Fatalf("%v", c.Msg.HeaderKeys)
	}
	listed, err := a.ListConnectors(ctx, connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil || len(listed.Msg.Connectors) != 1 {
		t.Fatal(err, listed)
	}
	att, err := a.AttachConnector(ctx, connect.NewRequest(&v1.AttachConnectorRequest{
		BotId: "b1", ConnectorId: c.Msg.Id,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if att.Msg.AuthStatus != statusNeedsAuth {
		t.Fatal(att.Msg.AuthStatus, att.Msg.LastError)
	}
	xs, err := a.ListBotConnectors(ctx, connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: "b1"}))
	if err != nil || len(xs.Msg.Connectors) != 1 {
		t.Fatal(err, xs)
	}
	if xs.Msg.Connectors[0].Connector.Id == c.Msg.Id {
		t.Fatal("attach should copy the library row")
	}
	if xs.Msg.Connectors[0].Connector.SourceId != c.Msg.Id {
		t.Fatal(xs.Msg.Connectors[0].Connector.SourceId)
	}
	if xs.Msg.Connectors[0].Connector.Kind != "custom" {
		t.Fatal(xs.Msg.Connectors[0].Connector.Kind)
	}
	if len(xs.Msg.Connectors[0].Connector.HeaderKeys) != 1 {
		t.Fatal("owner should see header names")
	}
	listed2, err := a.ListConnectors(ctx, connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil || len(listed2.Msg.Connectors) != 1 {
		t.Fatal("library grew", err, listed2)
	}
	blurb := a.connectorBlurb("b1")
	if !strings.Contains(blurb, "wolfram") && !strings.Contains(blurb, "needs Authorize") {
		t.Fatal(blurb)
	}
}

func TestRuleDecisionConnectorDefault(t *testing.T) {
	a := testApp(t, nil)
	if got := a.ruleDecision("b1", "twilio_docs", "search", "allow"); got != "allow" {
		t.Fatal(got)
	}
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "twilio_docs", Action: "search", Decision: "deny"})
	if got := a.ruleDecision("b1", "twilio_docs", "search", "allow"); got != "deny" {
		t.Fatal(got)
	}
}

func TestCallTitle(t *testing.T) {
	if got := callTitle("Twilio Docs", "twilio__retrieve"); got != "Twilio Docs · retrieve" {
		t.Fatal(got)
	}
}

func TestCreateConnectorDefaultAllow(t *testing.T) {
	a := testApp(t, nil)
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u", Admin: true})
	c, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1", DefaultMode: "allow",
	}))
	if err != nil || c.Msg.DefaultMode != "allow" {
		t.Fatal(err, c)
	}
}

func TestCreateBotConnector(t *testing.T) {
	a := testApp(t, nil)
	u := &db.User{ID: "u", Email: "a@b.c", Admin: false}
	a.DB.Create(u)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, u)
	att, err := a.CreateBotConnector(ctx, connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: "b1", Name: "Home MCP", Transport: "http", HttpUrl: "http://127.0.0.1:1", Auth: "none",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if att.Msg.Connector.Kind != "custom" || att.Msg.Connector.SourceId != "" {
		t.Fatal(att.Msg.Connector)
	}
	lib, err := a.ListConnectors(ctx, connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil || len(lib.Msg.Connectors) != 0 {
		t.Fatal("custom leaked into library", err, lib)
	}
	_, err = a.CreateBotConnector(ctx, connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: "b1", Name: "Home MCP", Transport: "http", HttpUrl: "http://127.0.0.1:2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	xs, err := a.ListBotConnectors(ctx, connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: "b1"}))
	if err != nil || len(xs.Msg.Connectors) != 2 {
		t.Fatal(err, xs)
	}
	names := map[string]bool{}
	for _, r := range xs.Msg.Connectors {
		names[r.Connector.Name] = true
	}
	if !names["Home MCP"] || !names["Home MCP 2"] {
		t.Fatal(names)
	}
	other := context.WithValue(context.Background(), userKey, &db.User{ID: "x", Admin: false})
	if _, err := a.CreateBotConnector(other, connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: "b1", Name: "X", Transport: "http", HttpUrl: "http://127.0.0.1:1",
	})); err == nil {
		t.Fatal("expected deny")
	}
}

func TestAttachTwice(t *testing.T) {
	a := testApp(t, nil)
	admin := &db.User{ID: "u", Email: "a@b.c", Admin: true}
	a.DB.Create(admin)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, admin)
	c, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "Docs", Transport: "http", HttpUrl: "http://127.0.0.1:1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AttachConnector(ctx, connect.NewRequest(&v1.AttachConnectorRequest{BotId: "b1", ConnectorId: c.Msg.Id})); err != nil {
		t.Fatal(err)
	}
	second, err := a.AttachConnector(ctx, connect.NewRequest(&v1.AttachConnectorRequest{BotId: "b1", ConnectorId: c.Msg.Id}))
	if err != nil {
		t.Fatal(err)
	}
	if second.Msg.Connector.Name != "Docs 2" {
		t.Fatal(second.Msg.Connector.Name)
	}
	if second.Msg.Connector.SourceId != c.Msg.Id {
		t.Fatal(second.Msg.Connector.SourceId)
	}
}

func TestBackfillCopiesSharedAttachment(t *testing.T) {
	a := testApp(t, nil)
	lib := db.Connector{ID: "lib1", Kind: catalog.KindLibrary, Name: "Old", Type: "mcp", Transport: "http", HTTPURL: "http://x", Auth: "none"}
	a.DB.Create(&lib)
	a.DB.Create(&db.BotConnector{ID: "bc1", BotID: "b1", ConnectorID: "lib1"})
	a.initConnectors()
	var bc db.BotConnector
	a.DB.First(&bc, "id = ?", "bc1")
	if bc.ConnectorID == "lib1" {
		t.Fatal("still pointing at library")
	}
	var inst db.Connector
	a.DB.First(&inst, "id = ?", bc.ConnectorID)
	if inst.Kind != catalog.KindCustom || inst.SourceID != "lib1" || inst.HTTPURL != "http://x" {
		t.Fatalf("%+v", inst)
	}
}

func TestMergeHeadersKeepsSecret(t *testing.T) {
	old, err := mergeHeaders(`{"A":"one"}`, []*v1.HeaderInput{{Name: "A", Value: ""}, {Name: "B", Value: "two"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(old, `"A":"one"`) || !strings.Contains(old, `"B":"two"`) {
		t.Fatal(old)
	}
}

func TestOriginOf(t *testing.T) {
	if got := originOf("https://mcp.cloudflare.com/authorize?x=1"); got != "https://mcp.cloudflare.com" {
		t.Fatal(got)
	}
}

func TestCreateBotConnectorFromLibrary(t *testing.T) {
	a := testApp(t, nil)
	admin := &db.User{ID: "u", Email: "a@b.c", Admin: true}
	a.DB.Create(admin)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, admin)
	lib, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "GitHub", Description: "short", Transport: "http",
		HttpUrl: "https://api.githubcopilot.com/mcp/", Auth: "oauth",
		OauthClientId: "iv1.abc", OauthClientSecret: "s3cret",
		Headers: []*v1.HeaderInput{{Name: "X-Test", Value: "secret"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	a.DB.Model(&db.Connector{}).Where("id = ?", lib.Msg.Id).Update("seed_key", "github")
	listed, err := a.ListConnectors(ctx, connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil || len(listed.Msg.Connectors) != 1 || !strings.Contains(listed.Msg.Connectors[0].CatalogGuide, "OAuth App") {
		t.Fatal(err, listed)
	}
	att, err := a.CreateBotConnector(ctx, connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: "b1", Name: "GitHub", Description: "short", Transport: "http",
		HttpUrl: "https://api.githubcopilot.com/mcp/", Auth: "oauth",
		SourceId: lib.Msg.Id, OauthClientId: "iv1.changed",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if att.Msg.Connector.SourceId != lib.Msg.Id || att.Msg.Connector.Kind != "custom" {
		t.Fatal(att.Msg.Connector)
	}
	var inst db.Connector
	a.DB.First(&inst, "id = ?", att.Msg.Connector.Id)
	if inst.OAuthClientID != "iv1.changed" || inst.OAuthClientSecret != "s3cret" {
		t.Fatalf("oauth %+v", inst)
	}
	if !strings.Contains(inst.HeadersJSON, "X-Test") {
		t.Fatal(inst.HeadersJSON)
	}
	again, err := a.CreateBotConnector(ctx, connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: "b1", Name: "GitHub", Transport: "http", HttpUrl: "http://x", SourceId: lib.Msg.Id,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if again.Msg.Connector.Name != "GitHub 2" || again.Msg.Connector.SourceId != lib.Msg.Id {
		t.Fatal(again.Msg.Connector)
	}
}

func TestOAuthClientCopiedOnAttach(t *testing.T) {
	a := testApp(t, nil)
	admin := &db.User{ID: "u", Email: "a@b.c", Admin: true}
	a.DB.Create(admin)
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	ctx := context.WithValue(context.Background(), userKey, admin)
	c, err := a.CreateConnector(ctx, connect.NewRequest(&v1.CreateConnectorRequest{
		Name: "GitHub", Transport: "http", HttpUrl: "https://api.githubcopilot.com/mcp/",
		Auth: "oauth", OauthClientId: "iv1.abc", OauthClientSecret: "s3cret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Msg.OauthClientId != "iv1.abc" || !c.Msg.HasOauthClientSecret {
		t.Fatalf("%+v", c.Msg)
	}
	att, err := a.AttachConnector(ctx, connect.NewRequest(&v1.AttachConnectorRequest{
		BotId: "b1", ConnectorId: c.Msg.Id,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var inst db.Connector
	a.DB.First(&inst, "id = ?", att.Msg.Connector.Id)
	if inst.OAuthClientID != "iv1.abc" || inst.OAuthClientSecret != "s3cret" {
		t.Fatalf("%+v", inst)
	}
	if _, err := a.UpdateConnector(ctx, connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: inst.ID, OauthClientId: "iv1.abc", OauthClientSecret: "",
	})); err != nil {
		t.Fatal(err)
	}
	a.DB.First(&inst, "id = ?", inst.ID)
	if inst.OAuthClientSecret != "s3cret" {
		t.Fatal("empty secret wiped stored secret")
	}
}

func TestPreregisteredClient(t *testing.T) {
	if preregisteredClient(&db.Connector{}) != nil {
		t.Fatal("empty")
	}
	c := preregisteredClient(&db.Connector{OAuthClientID: " id ", OAuthClientSecret: "s"})
	if c == nil || c.ClientID != "id" || c.ClientSecretAuth == nil || c.ClientSecretAuth.ClientSecret != "s" {
		t.Fatalf("%+v", c)
	}
	pub := preregisteredClient(&db.Connector{OAuthClientID: "id"})
	if pub == nil || pub.ClientSecretAuth != nil {
		t.Fatalf("%+v", pub)
	}
}

func TestWrapOAuthHint(t *testing.T) {
	err := wrapOAuth(errors.New("oauth: no configured client registration methods are supported by the authorization server"))
	if err == nil || !strings.Contains(err.Error(), "OAuth Client ID") {
		t.Fatal(err)
	}
}

func TestOAuthCallbackFillsMissingIss(t *testing.T) {
	a := testApp(t, nil)
	ch := make(chan *mcpauth.AuthorizationResult, 1)
	a.oauth["st"] = &oauthWait{ch: ch, issuer: "https://mcp.cloudflare.com"}
	r := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc&state=st", nil)
	w := httptest.NewRecorder()
	a.handleOAuthCallback(w, r)
	res := <-ch
	if res.Code != "abc" || res.Iss != "https://mcp.cloudflare.com" {
		t.Fatalf("%+v", res)
	}
}
