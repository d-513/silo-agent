package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm/dummy"
)

// list_secrets names the Bot's secrets so it can ask for the right one with
// get_secret: each name, how the box sees it and where a stand-in is sent.
// Never a value, never a stand-in, and never a channel's own credentials,
// which are the CP's and not the Bot's.
func TestListSecretsNamesWithoutValues(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	bot := h.CreateBot("Keys")
	id := bot.GetId()

	const token = "ghp_REALVALUE11111111111111"
	const password = "hunter2-correct-horse"
	for _, req := range []*v1.AddSecretRequest{
		{BotId: id, Name: "GITHUB_TOKEN", Value: token, Mode: "surrogate", Hosts: []string{"api.github.com"}},
		{BotId: id, Name: "vendor_password", Value: password, Mode: "plain"},
	} {
		if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(req)); err != nil {
			t.Fatalf("AddSecret %s: %v", req.GetName(), err)
		}
	}
	// A channel's credential is stored as a secret row too, and is not the Bot's.
	h.DB.Create(&db.Secret{ID: ids.New(), BotID: id, Name: "channel.tg.token", Value: "bot-token", Mode: "plain"})

	res := pyCall(t, h, id, "", "bot", "list_secrets", `{}`)
	if res.GetError() != "" {
		t.Fatalf("list_secrets: %+v", res)
	}
	raw := res.GetResultJson()
	for _, leak := range []string{token, password, "bot-token", "channel.tg"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("list_secrets leaked %q: %s", leak, raw)
		}
	}
	var got struct {
		Secrets []struct {
			Name  string   `json:"name"`
			Mode  string   `json:"mode"`
			Hosts []string `json:"hosts"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, raw)
	}
	if len(got.Secrets) != 2 {
		t.Fatalf("secrets = %+v", got.Secrets)
	}
	gh, pw := got.Secrets[0], got.Secrets[1]
	if gh.Name != "GITHUB_TOKEN" || gh.Mode != "surrogate" || len(gh.Hosts) != 1 || gh.Hosts[0] != "api.github.com" {
		t.Fatalf("GITHUB_TOKEN = %+v", gh)
	}
	if pw.Name != "vendor_password" || pw.Mode != "plain" || len(pw.Hosts) != 0 {
		t.Fatalf("vendor_password = %+v", pw)
	}
	// The surrogate itself is not handed out either: get_secret is the one
	// way to it, under that secret's own rule.
	var row db.Secret
	h.DB.Where("bot_id = ? AND name = ?", id, "GITHUB_TOKEN").Find(&row)
	if row.Surrogate == "" || strings.Contains(raw, row.Surrogate) {
		t.Fatalf("list_secrets handed out the stand-in: %s", raw)
	}
}

// From the chat surface (tools.expose_completion) it is prose the model reads,
// and with nothing stored it says so rather than answering nothing.
func TestListSecretsInChatIsProse(t *testing.T) {
	dummy.Reset()
	dummy.Script("Test_340",
		memCall("list_secrets", `{}`),
		dummy.Turn{Text: "done"},
	)
	h := apptest.New(t, apptest.WithChatTools())
	bot := h.CreateBot("Prose")
	id := bot.GetId()
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: id, Name: "STRIPE_KEY", Value: "sk_live_" + strings.Repeat("a", 24), Mode: "surrogate", Hosts: []string{"api.stripe.com"},
	})); err != nil {
		t.Fatal(err)
	}
	run, _ := h.Send(id, h.FirstChat(id), "Test_340_Input")
	res := memResults(h.WaitRun(run))
	if len(res) != 1 || !strings.Contains(res[0], "STRIPE_KEY") || !strings.Contains(res[0], "api.stripe.com") || strings.Contains(res[0], "sk_live") {
		t.Fatalf("chat answer = %q\n%s", res, h.RunBody(run))
	}

	empty := h.CreateBot("Empty")
	run, _ = h.Send(empty.GetId(), h.FirstChat(empty.GetId()), "Test_340_Input")
	res = memResults(h.WaitRun(run))
	if len(res) != 1 || !strings.Contains(res[0], "no secrets") {
		t.Fatalf("empty = %q", res)
	}
}
