package app

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
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
	if xs.Msg.Connectors[0].Connector.HeaderKeys != nil {
		t.Fatal("bot list leaked header keys")
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

func TestMergeHeadersKeepsSecret(t *testing.T) {
	old, err := mergeHeaders(`{"A":"one"}`, []*v1.HeaderInput{{Name: "A", Value: ""}, {Name: "B", Value: "two"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(old, `"A":"one"`) || !strings.Contains(old, `"B":"two"`) {
		t.Fatal(old)
	}
}
