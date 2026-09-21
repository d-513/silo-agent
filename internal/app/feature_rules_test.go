package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

func TestSecretsPerName(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Secrets")
	for _, name := range []string{"pw", "other"} {
		if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
			BotId: bot.GetId(), Name: name, Value: "value-" + name,
		})); err != nil {
			t.Fatal(err)
		}
	}
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "secrets", Action: "pw", Decision: "allow",
	}))
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "secrets", Action: "other", Decision: "deny",
	}))

	wc := h.WorkerClient(bot.GetId())
	ok, err := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "pw"}))
	if err != nil || ok.Msg.GetValue() != "value-pw" {
		t.Fatalf("allowed secret: %v %+v", err, ok.Msg)
	}
	no, err := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "other"}))
	if err != nil || no.Msg.GetError() != "denied" {
		t.Fatalf("denied secret: %v %+v", err, no.Msg)
	}
}

func TestCallToolBuiltins(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Builtins")
	wc := h.WorkerClient(bot.GetId())

	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
	}))
	if err != nil || res.Msg.GetError() != "" || !strings.Contains(res.Msg.GetResultJson(), "ok") {
		t.Fatalf("desktop.click: %v %+v", err, res.Msg)
	}
	bad, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "python", Action: "run",
	}))
	if err != nil || bad.Msg.GetError() == "" {
		t.Fatalf("python must not be a CallTool connector: %v %+v", err, bad.Msg)
	}
}

func TestListRulesSectionsAndSweep(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("RuleSections")
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "imap_password", Value: "x",
	})); err != nil {
		t.Fatal(err)
	}
	// Seed legacy and ghost rules directly: the list endpoint must migrate
	// secrets.get to per-name and prune rules for connectors that no longer exist.
	h.DB.Create(&db.Rule{ID: "old", BotID: bot.GetId(), Connector: "secrets", Action: "get", Decision: "allow"})
	h.DB.Create(&db.Rule{ID: "ghost", BotID: bot.GetId(), Connector: "gone", Action: "send", Decision: "allow"})

	res, err := h.Client.ListRules(h.Ctx(), connect.NewRequest(&v1.ListRulesRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.GetSections()) < 2 || res.Msg.GetSections()[0].GetTitle() != "This Bot" {
		t.Fatalf("sections %+v", res.Msg.GetSections())
	}
	var sawSecrets bool
	for _, sec := range res.Msg.GetSections() {
		if sec.GetTitle() != "Secrets" {
			continue
		}
		sawSecrets = true
		for _, r := range sec.GetRules() {
			if r.GetAction() == "imap_password" && r.GetDecision() != "ask" {
				t.Fatalf("per-name secret rule decision %q", r.GetDecision())
			}
			if r.GetAction() == "get" {
				t.Fatal("legacy secrets.get rule survived")
			}
		}
	}
	if !sawSecrets {
		t.Fatal("no Secrets section")
	}
	var n int64
	h.DB.Model(&db.Rule{}).Where("id = ?", "ghost").Count(&n)
	if n != 0 {
		t.Fatal("ghost rule not swept")
	}
}

func TestDeleteSecretPrunesRule(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Prune")
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "pw", Value: "x",
	})); err != nil {
		t.Fatal(err)
	}
	h.DB.Create(&db.Rule{ID: "r1", BotID: bot.GetId(), Connector: "secrets", Action: "pw", Decision: "allow"})
	secs, _ := h.Client.ListSecrets(h.Ctx(), connect.NewRequest(&v1.ListSecretsRequest{BotId: bot.GetId()}))
	if len(secs.Msg.GetSecrets()) != 1 {
		t.Fatalf("secrets %+v", secs.Msg.GetSecrets())
	}
	if _, err := h.Client.DeleteSecret(h.Ctx(), connect.NewRequest(&v1.DeleteSecretRequest{
		BotId: bot.GetId(), Id: secs.Msg.GetSecrets()[0].GetId(),
	})); err != nil {
		t.Fatal(err)
	}
	var n int64
	h.DB.Model(&db.Rule{}).Where("id = ?", "r1").Count(&n)
	if n != 0 {
		t.Fatal("secret rule not pruned")
	}
}
