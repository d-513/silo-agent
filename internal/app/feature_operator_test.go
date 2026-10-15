package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// listRule finds one row of the Bot's Rules page by connector and action.
func listRule(t *testing.T, h *apptest.H, botID, connector, action string) *v1.Rule {
	t.Helper()
	res, err := h.Client.ListRules(h.Ctx(), connect.NewRequest(&v1.ListRulesRequest{BotId: botID}))
	if err != nil {
		t.Fatalf("ListRules: %v", err)
	}
	for _, r := range res.Msg.GetRules() {
		if r.GetConnector() == connector && r.GetAction() == action {
			return r
		}
	}
	t.Fatalf("no rule %s.%s", connector, action)
	return nil
}

func setOperatorRule(t *testing.T, h *apptest.H, connector, action, decision, output string, locked bool) *v1.OperatorRule {
	t.Helper()
	res, err := h.Client.SetOperatorRule(h.Ctx(), connect.NewRequest(&v1.SetOperatorRuleRequest{
		Connector: connector, Action: action, Decision: decision, Output: &output, Locked: locked,
	}))
	if err != nil {
		t.Fatalf("SetOperatorRule %s.%s: %v", connector, action, err)
	}
	return res.Msg
}

// An operator row the operator did not lock is only a default: the owner
// overrides it either way.
func TestOperatorRuleUnlockedIsADefault(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Operator")
	setOperatorRule(t, h, "terminal", "run", "deny", "", false)

	if r := listRule(t, h, bot.GetId(), "terminal", "run"); r.GetDecision() != "deny" || r.GetLocked() {
		// The row speaks while the owner has none of their own.
		t.Fatalf("operator default not in effect: %+v", r)
	}
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "allow",
	}))
	r := listRule(t, h, bot.GetId(), "terminal", "run")
	if r.GetDecision() != "allow" {
		t.Fatalf("owner must override an unlocked operator default: %+v", r)
	}
	if r.GetOperatorDecision() != "deny" || r.GetLocked() {
		t.Fatalf("the operator's value should still be shown: %+v", r)
	}
}

// A locked operator row is a floor: the owner may tighten it, never loosen it,
// and the engine takes the stricter of the two.
func TestOperatorRuleLockedIsAFloor(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Floor")
	setOperatorRule(t, h, "terminal", "run", "ask", "", true)

	_, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "allow",
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("loosening a locked rule must be refused: %v", err)
	}
	if r := listRule(t, h, bot.GetId(), "terminal", "run"); r.GetDecision() != "ask" {
		t.Fatalf("a refused rule must not be written: %+v", r)
	}

	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "deny",
	})); err != nil {
		t.Fatalf("tightening a locked rule: %v", err)
	}
	if r := listRule(t, h, bot.GetId(), "terminal", "run"); r.GetDecision() != "deny" || !r.GetLocked() {
		t.Fatalf("owner should be able to tighten: %+v", r)
	}

	// Lifting the lock lets the owner loosen it again.
	setOperatorRule(t, h, "terminal", "run", "ask", "", false)
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "terminal", Action: "run", Decision: "allow",
	})); err != nil {
		t.Fatalf("after the lock is lifted: %v", err)
	}
}

// The output side layers the same way: pass < flag < check < withhold.
func TestOperatorOutputRuleLockedIsAFloor(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Output")
	setOperatorRule(t, h, "web", "search", "", "check", true)

	pass := "pass"
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "web", Action: "search", Output: &pass,
	})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("loosening a locked output rule must be refused: %v", err)
	}

	withhold := "withhold"
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "web", Action: "search", Output: &withhold,
	})); err != nil {
		t.Fatalf("tightening a locked output rule: %v", err)
	}
	r := listRule(t, h, bot.GetId(), "web", "search")
	if r.GetOutput() != "withhold" {
		t.Fatalf("the stricter output rule must win: %+v", r)
	}
	if r.GetOperatorOutput() != "check" || !r.GetLocked() {
		t.Fatalf("the operator's output value should be shown: %+v", r)
	}
}

// A connector-wide * row covers its actions.
func TestOperatorStarRowCoversActions(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Star")
	setOperatorRule(t, h, "web", "*", "deny", "", true)

	if r := listRule(t, h, bot.GetId(), "web", "search"); r.GetDecision() != "deny" {
		t.Fatalf("a * row should cover the connector's actions: %+v", r)
	}
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "web", Action: "search", Decision: "allow",
	})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a locked * row must floor every action under it: %v", err)
	}
}

// Empty decision and output removes the row, so nothing is left behind.
func TestOperatorRuleRemovedWhenEmpty(t *testing.T) {
	h := apptest.New(t)
	h.CreateBot("Empty")
	setOperatorRule(t, h, "web", "search", "deny", "", true)
	setOperatorRule(t, h, "web", "search", "", "", false)

	res, err := h.Client.ListOperatorRules(h.Ctx(), connect.NewRequest(&v1.ListOperatorRulesRequest{}))
	if err != nil {
		t.Fatalf("ListOperatorRules: %v", err)
	}
	if len(res.Msg.GetRules()) != 0 {
		t.Fatalf("row should be gone: %+v", res.Msg.GetRules())
	}
}

// The operator's approval policy is read by the gate even when the Bot's owner
// wrote none: the dummy answers approve only because of that text.
func TestOperatorApprovalPolicyJudges(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+"rules:\n  approval_policy: \"Auto-approve clicks. Test_Approve\"\n"))
	bot := h.CreateBot("OperatorPolicy")
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "desktop", Action: "click", Decision: "auto",
	}))

	res, err := h.WorkerClient(bot.GetId()).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: "desktop", Action: "click", ArgsJson: `{"x":1,"y":2}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.GetError() != "" || !strings.Contains(res.Msg.GetResultJson(), "ok") {
		t.Fatalf("the operator's policy should have allowed the click: %+v", res.Msg)
	}
}

// The operator's output policy is read by the filter even when the Bot's owner
// wrote none. The result itself carries no token: only the operator's text
// makes the dummy answer flag.
func TestOperatorOutputPolicyFilters(t *testing.T) {
	h, id, _ := echoBot(t, "rules:\n  output_policy: Test_Flag\n")
	res := echoFromPython(t, h, id, "a page about hotels")
	if res.GetError() != "" || res.GetResultJson() != `{"result":"a page about hotels"}` {
		t.Fatalf("a flagged result keeps its shape from Python: %+v", res)
	}
	var a db.AuditEvent
	h.DB.Where("bot_id = ? AND actor = ?", id, "filter").Last(&a)
	if a.Decision != "output_flagged" {
		t.Fatalf("the operator's policy was not read by the filter: %q", a.Decision)
	}
}
