package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/claudetest"
)

// The test binary doubles as the fake Claude Code binary (claudetest).
func init() { claudetest.Main() }

// claudeHarness is a control plane whose chat model is the Claude provider,
// run by the fake binary, with its directory left at the default.
func claudeHarness(t *testing.T, extra ...string) *apptest.H {
	t.Helper()
	yaml := apptest.DefaultYAML(t.TempDir()) + strings.Join(extra, "")
	yaml = strings.Replace(yaml, "model: dummy/echo\n", "model: claude/opus\n", 1)
	yaml = strings.Replace(yaml, "  - dummy/echo\n", "  - dummy/echo\n  - claude/opus\n", 1)
	yaml = strings.Replace(yaml, "providers:\n", "providers:\n  claude:\n    binary: "+claudetest.Install(t)+"\n", 1)
	// On disk, so a sign-in can save a setting.
	path := filepath.Join(t.TempDir(), "silo.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return apptest.New(t, apptest.WithConfigPath(path))
}

func eventsOf(events []db.RunEvent, kind string) []db.RunEvent {
	var out []db.RunEvent
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// A tool the model calls through Claude Code is run by Silo's own engine: it
// waits for the same approval and its result is what the engine made. The
// binary executes nothing, so no rule can be skipped by choosing this provider.
func TestClaudeProviderRunsSilosTools(t *testing.T) {
	// soul is offered as a chat tool so a call with no worker can ask.
	h := claudeHarness(t, apptest.ChatTools)
	bot := h.CreateBot("Calm")
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.GetId(), Connector: "bot", Action: "soul", Decision: "ask"})); err != nil {
		t.Fatal(err)
	}
	run, chat := h.Send(bot.GetId(), "", `Test_Call_soul {"content":"I am calm."}`)

	// The call is parked in the binary while the human decides.
	ap := h.WaitApproval(bot.GetId())
	var before db.Bot
	h.DB.Select("soul").First(&before, "id = ?", bot.GetId())
	if before.Soul == "I am calm." {
		t.Fatal("the tool ran before it was approved")
	}
	if _, err := h.Client.DecideApproval(h.Ctx(), connect.NewRequest(&v1.DecideApprovalRequest{Id: ap.GetId(), Decision: "allow_once"})); err != nil {
		t.Fatal(err)
	}
	events := h.WaitRun(run)
	var after db.Bot
	h.DB.Select("soul").First(&after, "id = ?", bot.GetId())
	if after.Soul != "I am calm." {
		t.Fatalf("soul %q", after.Soul)
	}
	// The call is recorded under Silo's name for the tool, and the reply is
	// the model's reading of the engine's result.
	turns := eventsOf(events, "turn")
	if len(turns) != 1 || !strings.Contains(turns[0].Meta, `"soul"`) || strings.Contains(turns[0].Meta, "mcp__") {
		t.Fatalf("turn events: %+v", turns)
	}
	replies := eventsOf(events, "assistant")
	if len(replies) != 1 || !strings.HasPrefix(replies[0].Body, "result: ") {
		t.Fatalf("replies: %+v", replies)
	}
	if u := eventsOf(events, "usage"); len(u) == 0 {
		t.Fatal("no usage recorded")
	}

	// The next message goes on in the binary's session: it is sent that one
	// message, not the history again.
	run2, _ := h.Send(bot.GetId(), chat, "Test_Dump")
	replies = eventsOf(h.WaitRun(run2), "assistant")
	if len(replies) != 1 {
		t.Fatalf("second run: %+v", replies)
	}
	var d claudetest.Dump
	if err := json.Unmarshal([]byte(replies[0].Body), &d); err != nil {
		t.Fatalf("not a dump: %q", replies[0].Body)
	}
	if len(d.History) != 2 || !strings.Contains(d.History[1], "Test_Dump") || strings.Contains(d.History[1], "<record>") {
		t.Fatalf("history: %q", d.History)
	}
	// Silo's prompt and tools, none of the binary's.
	if !strings.Contains(d.System, "Calm") || d.Model != "opus" {
		t.Fatalf("system lacks the Bot, or model %q", d.Model)
	}
	for _, want := range []string{"soul", "read", "exec_python"} {
		if !slices.Contains(d.Tools, want) {
			t.Errorf("tools lack %s: %v", want, d.Tools)
		}
	}
	// Its state is beside the rest of Silo's data.
	if !strings.HasPrefix(d.Cwd, h.DataDir) || !strings.HasSuffix(d.Cwd, "/claude/work") {
		t.Fatalf("cwd %s, data dir %s", d.Cwd, h.DataDir)
	}
}

// A provider the operator signs in to: who is signed in, the page to open,
// the code it shows, and a long-lived token saved like any other setting.
func TestClaudeProviderSignIn(t *testing.T) {
	h := claudeHarness(t)
	ctx := h.Ctx()

	settings, err := h.Client.GetSettings(ctx, connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var p *v1.Provider
	for _, x := range settings.Msg.GetProviders() {
		if x.GetId() == "claude" {
			p = x
		}
	}
	if p == nil || !p.GetSignIn() || !strings.Contains(p.GetWarning(), "own use") {
		t.Fatalf("provider %+v", p)
	}

	acc, err := h.Client.GetProviderAccount(ctx, connect.NewRequest(&v1.GetProviderAccountRequest{Provider: "claude"}))
	if err != nil || acc.Msg.GetSignedIn() || len(acc.Msg.GetMethods()) != 2 || acc.Msg.GetProblem() != "" {
		t.Fatalf("fresh account: %+v %v", acc, err)
	}
	// A provider with a key has no sign-in.
	if _, err := h.Client.GetProviderAccount(ctx, connect.NewRequest(&v1.GetProviderAccountRequest{Provider: "dummy"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("dummy: %v", err)
	}

	begin, err := h.Client.BeginProviderSignIn(ctx, connect.NewRequest(&v1.BeginProviderSignInRequest{Provider: "claude"}))
	if err != nil || !strings.HasPrefix(begin.Msg.GetUrl(), "https://claude.com/") {
		t.Fatalf("begin: %+v %v", begin, err)
	}
	// A wrong code is the provider's refusal, never the control plane down.
	if _, err := h.Client.FinishProviderSignIn(ctx, connect.NewRequest(&v1.FinishProviderSignInRequest{Provider: "claude", Code: "wrong"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := h.Client.BeginProviderSignIn(ctx, connect.NewRequest(&v1.BeginProviderSignInRequest{Provider: "claude", Method: "login"})); err != nil {
		t.Fatal(err)
	}
	acc, err = h.Client.FinishProviderSignIn(ctx, connect.NewRequest(&v1.FinishProviderSignInRequest{Provider: "claude", Code: claudetest.GoodCode}))
	if err != nil || !acc.Msg.GetSignedIn() || acc.Msg.GetEmail() != claudetest.Email || acc.Msg.GetPlan() != "max" || acc.Msg.GetExpiresAt() == 0 {
		t.Fatalf("signed in: %+v %v", acc, err)
	}

	// Its models, ready for the allowlist.
	list, err := h.Client.ListProviderModels(ctx, connect.NewRequest(&v1.ListProviderModelsRequest{Provider: "claude"}))
	if err != nil || len(list.Msg.GetModels()) != 3 || list.Msg.GetModels()[0].GetId() != "claude/opus" || list.Msg.GetModels()[0].GetKind() != "text" {
		t.Fatalf("models: %+v %v", list, err)
	}

	// A long-lived token is issued by the binary and saved as the setting.
	if _, err := h.Client.BeginProviderSignIn(ctx, connect.NewRequest(&v1.BeginProviderSignInRequest{Provider: "claude", Method: "token"})); err != nil {
		t.Fatal(err)
	}
	acc, err = h.Client.FinishProviderSignIn(ctx, connect.NewRequest(&v1.FinishProviderSignInRequest{Provider: "claude", Code: claudetest.GoodCode}))
	if err != nil || !acc.Msg.GetSignedIn() || acc.Msg.GetMethod() != "oauth_token" {
		t.Fatalf("token: %+v %v", acc, err)
	}
	if got := h.Store.Config().Providers["claude"].OAuthToken; got != claudetest.Token {
		t.Fatalf("saved token %q", got)
	}

	// Signing out forgets both.
	acc, err = h.Client.SignOutProvider(ctx, connect.NewRequest(&v1.SignOutProviderRequest{Provider: "claude"}))
	if err != nil || acc.Msg.GetSignedIn() {
		t.Fatalf("signed out: %+v %v", acc, err)
	}
	if got := h.Store.Config().Providers["claude"].OAuthToken; got != "" {
		t.Fatalf("token kept: %q", got)
	}
}
