package app

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/security"
)

func botCtx(b *db.Bot) context.Context {
	return context.WithValue(context.Background(), botKey, b)
}

func TestRuleDecisionBuiltinAndStar(t *testing.T) {
	a := testApp(t, nil)
	if got := a.ruleDecision("b1", "python", "run", ""); got != security.Allow {
		t.Fatalf("python %s", got)
	}
	if got := a.ruleDecision("b1", "secrets", "pw", ""); got != security.Ask {
		t.Fatalf("secret %s", got)
	}
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "files", Action: "*", Decision: "deny"})
	if got := a.ruleDecision("b1", "files", "read", ""); got != security.Deny {
		t.Fatalf("star %s", got)
	}
	a.DB.Create(&db.Rule{ID: "r2", BotID: "b1", Connector: "files", Action: "read", Decision: "allow"})
	if got := a.ruleDecision("b1", "files", "read", ""); got != security.Allow {
		t.Fatalf("exact %s", got)
	}
}

func TestExecToolDenied(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.Bot{ID: "b1"})
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "terminal", Action: "run", Decision: "deny"})
	_, _, err := a.execTool(context.Background(), "b1", "run1", "terminal", `{"command":"ls"}`)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("%v", err)
	}
}

func TestGetSecretPerName(t *testing.T) {
	a := testApp(t, nil)
	bot := &db.Bot{ID: "b1"}
	a.DB.Create(bot)
	a.DB.Create(&db.Secret{ID: "s1", BotID: "b1", Name: "pw", Value: "hunter2"})
	a.DB.Create(&db.Secret{ID: "s2", BotID: "b1", Name: "other", Value: "x"})
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "secrets", Action: "pw", Decision: "allow"})
	a.DB.Create(&db.Rule{ID: "r2", BotID: "b1", Connector: "secrets", Action: "other", Decision: "deny"})
	ctx := botCtx(bot)
	ok, err := a.GetSecret(ctx, connect.NewRequest(&v1.SecretReq{Name: "pw"}))
	if err != nil || ok.Msg.GetValue() != "hunter2" {
		t.Fatalf("%v %+v", err, ok)
	}
	no, err := a.GetSecret(ctx, connect.NewRequest(&v1.SecretReq{Name: "other"}))
	if err != nil || no.Msg.GetError() != "denied" {
		t.Fatalf("%v %+v", err, no)
	}
}

func TestCallDesktopBuiltin(t *testing.T) {
	a := testApp(t, nil)
	bot := &db.Bot{ID: "b1"}
	a.DB.Create(bot)
	res, err := a.CallTool(botCtx(bot), connect.NewRequest(&v1.ToolReq{Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`}))
	if err != nil || res.Msg.GetError() != "" || !strings.Contains(res.Msg.GetResultJson(), "ok") {
		t.Fatalf("%v %+v", err, res)
	}
	bad, err := a.CallTool(botCtx(bot), connect.NewRequest(&v1.ToolReq{Connector: "python", Action: "run"}))
	if err != nil || bad.Msg.GetError() == "" {
		t.Fatal("python via CallTool")
	}
}

func TestReservedConnectorName(t *testing.T) {
	a := testApp(t, nil)
	_, err := a.putBotConnector(context.Background(), "b1", db.Connector{Name: "Python", Transport: "http", HTTPURL: "http://x"})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("%v", err)
	}
}

func TestListRulesSectionsAndSweep(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.User{ID: "u"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Secret{ID: "s1", BotID: "b1", Name: "imap_password", Value: "x"})
	a.DB.Create(&db.Rule{ID: "old", BotID: "b1", Connector: "secrets", Action: "get", Decision: "allow"})
	a.DB.Create(&db.Rule{ID: "ghost", BotID: "b1", Connector: "gone", Action: "send", Decision: "allow"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	res, err := a.ListRules(ctx, connect.NewRequest(&v1.ListRulesRequest{BotId: "b1"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.Sections) < 2 || res.Msg.Sections[0].Title != "This Bot" || res.Msg.Sections[1].Title != "Secrets" {
		t.Fatalf("%+v", res.Msg.Sections)
	}
	var py, secret bool
	for _, r := range res.Msg.Sections[0].Rules {
		if r.Connector == "python" && r.Action == "run" && r.Decision == "allow" {
			py = true
		}
	}
	for _, r := range res.Msg.Sections[1].Rules {
		if r.Action == "imap_password" && r.Decision == "ask" {
			secret = true
		}
		if r.Action == "get" {
			t.Fatal("secrets.get")
		}
	}
	if !py || !secret {
		t.Fatal("rows")
	}
	var n int64
	a.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ?", "b1", "gone").Count(&n)
	if n != 0 {
		t.Fatal("ghost")
	}
	a.DB.Model(&db.Rule{}).Where("action = ?", "get").Count(&n)
	if n != 0 {
		t.Fatal("get")
	}
}

func TestDetachPrunesRules(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.User{ID: "u"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	c := db.Connector{ID: "c1", Kind: catalog.KindCustom, BotID: "b1", Name: "Mail", Transport: "http", HTTPURL: "http://x"}
	a.DB.Create(&c)
	a.DB.Create(&db.BotConnector{ID: "bc1", BotID: "b1", ConnectorID: "c1"})
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "mail", Action: "send", Decision: "allow"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	if _, err := a.DetachConnector(ctx, connect.NewRequest(&v1.DetachConnectorRequest{BotId: "b1", Id: "bc1"})); err != nil {
		t.Fatal(err)
	}
	var n int64
	a.DB.Model(&db.Rule{}).Where("id = ?", "r1").Count(&n)
	if n != 0 {
		t.Fatal("rule remains")
	}
}

func TestDeleteSecretPrunesRule(t *testing.T) {
	a := testApp(t, nil)
	a.DB.Create(&db.User{ID: "u"})
	a.DB.Create(&db.Bot{ID: "b1", UserID: "u"})
	a.DB.Create(&db.Secret{ID: "s1", BotID: "b1", Name: "pw", Value: "x"})
	a.DB.Create(&db.Rule{ID: "r1", BotID: "b1", Connector: "secrets", Action: "pw", Decision: "allow"})
	ctx := context.WithValue(context.Background(), userKey, &db.User{ID: "u"})
	if _, err := a.DeleteSecret(ctx, connect.NewRequest(&v1.DeleteSecretRequest{BotId: "b1", Id: "s1"})); err != nil {
		t.Fatal(err)
	}
	var n int64
	a.DB.Model(&db.Rule{}).Where("id = ?", "r1").Count(&n)
	if n != 0 {
		t.Fatal("rule remains")
	}
}
