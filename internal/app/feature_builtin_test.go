package app_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/builtin"
	"silo.agent/internal/builtin/email/emailtest"
	"silo.agent/internal/db"
)

func libraryEmail(t *testing.T, h *apptest.H) *v1.Connector {
	t.Helper()
	res, err := h.Client.ListConnectors(h.Ctx(), connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Msg.GetConnectors() {
		if c.GetBuiltin() == "email" {
			return c
		}
	}
	t.Fatal("email preset not seeded")
	return nil
}

// TestBuiltinEmailConnector drives the built-in Email connector through the
// same engine as MCP connectors: attach from the library, wait for setup,
// fill in the config, call tools from the worker, and gate sends on a slip.
func TestBuiltinEmailConnector(t *testing.T) {
	mail := emailtest.Start(t)
	mail.Deliver(t, "INBOX", "From: Alice <alice@example.org>\r\nTo: me@example.com\r\nSubject: Hello\r\n\r\nHi there\r\n")
	h := apptest.New(t)
	bot := h.CreateBot("Mailer")
	lib := libraryEmail(t, h)
	if lib.GetTransport() != "builtin" || len(lib.GetFields()) == 0 || lib.GetConfig()["provider"] != "gmail" || lib.GetConfig()["save_sent"] != "true" {
		t.Fatalf("library preset shape %+v", lib)
	}

	// Attaching without a password waits for the human.
	created, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: lib.GetId(), Config: map[string]string{"provider": "custom", "address": emailtest.Address},
	}))
	if err != nil {
		t.Fatal(err)
	}
	row := h.WaitConnector(bot.GetId(), "Email")
	if row.GetAuthStatus() != "needs_auth" || !strings.Contains(row.GetStatusDetail(), "App password") {
		t.Fatalf("want needs_auth for the password, got %q %q", row.GetAuthStatus(), row.GetStatusDetail())
	}
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: lib.GetId(), Config: map[string]string{"nope": "x"},
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown field should be refused: %v", err)
	}

	// Filling in the rest checks the login and publishes the tools.
	connID := created.Msg.GetConnector().GetId()
	if _, err := h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: connID, Config: mail.Config(),
	})); err != nil {
		t.Fatal(err)
	}
	row = waitStatus(t, h, bot.GetId(), "authorized")
	c := row.GetConnector()
	if c.GetConfig()["password"] != "" || strings.Join(c.GetSecretsSet(), ",") != "password" || c.GetConfig()["imap_host"] != "127.0.0.1" {
		t.Fatalf("config view leaks or misses values: %v %v", c.GetConfig(), c.GetSecretsSet())
	}
	var stored db.Connector
	h.DB.First(&stored, "id = ?", connID)
	if strings.Contains(stored.ConfigJSON, emailtest.Password) || !strings.Contains(stored.SecretsJSON, emailtest.Password) {
		t.Fatalf("secret stored in the wrong column: %q / %q", stored.ConfigJSON, stored.SecretsJSON)
	}
	var link db.BotConnector
	h.DB.First(&link, "id = ?", row.GetId())
	if !strings.Contains(link.ToolsJSON, `"send"`) || !strings.Contains(link.ToolsJSON, `"save_attachment"`) {
		t.Fatalf("tools not cached: %s", link.ToolsJSON)
	}

	// An update with a blank secret keeps the stored password.
	if _, err := h.Client.UpdateConnector(h.Ctx(), connect.NewRequest(&v1.UpdateConnectorRequest{
		Id: connID, Config: map[string]string{"password": "", "from_name": "Mailer Bot"},
	})); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h, bot.GetId(), "authorized")

	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{Connector: "email", Action: "search", ArgsJson: `{}`}))
	if err != nil || res.Msg.GetError() != "" {
		t.Fatalf("search: %v %+v", err, res.Msg)
	}
	var found struct {
		Messages []struct {
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(res.Msg.GetResultJson()), &found); err != nil || len(found.Messages) != 1 || found.Messages[0].Subject != "Hello" {
		t.Fatalf("search result %s", res.Msg.GetResultJson())
	}

	// Sending asks first (per-action default), even though search did not.
	done := make(chan *v1.ToolRes, 1)
	go func() {
		r, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
			Connector: "email", Action: "send",
			ArgsJson: `{"to":"alice@example.org","subject":"Re","body":"pw is ` + emailtest.Password + `"}`,
		}))
		if err != nil {
			done <- &v1.ToolRes{Error: err.Error()}
			return
		}
		done <- r.Msg
	}()
	ap := h.WaitApproval(bot.GetId())
	if ap.GetConnector() != "email" || ap.GetAction() != "send" {
		t.Fatalf("approval %+v", ap)
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if out.GetError() != "" || !strings.Contains(out.GetResultJson(), `"sent":true`) {
		t.Fatalf("send: %+v", out)
	}
	if len(mail.Sent()) != 1 {
		t.Fatalf("smtp got %d", len(mail.Sent()))
	}

	// The Rules tab lists every action with its own default.
	rules, err := h.Client.ListRules(h.Ctx(), connect.NewRequest(&v1.ListRulesRequest{BotId: bot.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]string{}
	for _, r := range rules.Msg.GetRules() {
		if r.GetConnector() == "email" {
			modes[r.GetAction()] = r.GetDecision()
		}
	}
	if modes["read"] != "allow" || modes["send"] != "ask" || modes["delete"] != "ask" || modes["set_flags"] != "allow" {
		t.Fatalf("rule defaults %v", modes)
	}
}

func TestBuiltinFailedLoginIsAnError(t *testing.T) {
	mail := emailtest.Start(t)
	h := apptest.New(t)
	bot := h.CreateBot("BadMail")
	cfg := mail.Config()
	cfg["password"] = "wrong"
	if _, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), SourceId: libraryEmail(t, h).GetId(), Config: cfg,
	})); err != nil {
		t.Fatal(err)
	}
	row := h.WaitConnector(bot.GetId(), "Email")
	if row.GetAuthStatus() != "error" || !strings.Contains(row.GetLastError(), "IMAP login failed") {
		t.Fatalf("want login error, got %q %q", row.GetAuthStatus(), row.GetLastError())
	}
	if strings.Contains(row.GetLastError(), "wrong") {
		t.Fatalf("error leaks the password: %q", row.GetLastError())
	}
}

func TestBuiltinCatalogKeysAreRegistered(t *testing.T) {
	h := apptest.New(t)
	res, err := h.Client.ListConnectors(h.Ctx(), connect.NewRequest(&v1.ListConnectorsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Msg.GetConnectors() {
		if c.GetTransport() != "builtin" {
			continue
		}
		if _, ok := builtin.Lookup(c.GetBuiltin()); !ok {
			t.Errorf("library preset %s names unregistered builtin %q", c.GetName(), c.GetBuiltin())
		}
	}
}

// waitStatus polls the bot's only connector until it reaches want.
func waitStatus(t *testing.T, h *apptest.H, botID, want string) *v1.BotConnector {
	t.Helper()
	var last *v1.BotConnector
	for i := 0; i < 300; i++ {
		res, err := h.Client.ListBotConnectors(h.Ctx(), connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: botID}))
		if err == nil && len(res.Msg.GetConnectors()) > 0 {
			last = res.Msg.GetConnectors()[0]
			if last.GetAuthStatus() == want {
				return last
			}
			if last.GetAuthStatus() == "error" {
				t.Fatalf("connector error: %s", last.GetLastError())
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("connector never reached %s: %+v", want, last)
	return nil
}
