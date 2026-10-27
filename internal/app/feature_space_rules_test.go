package app_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/apptest"
	"silo.agent/internal/security"
)

func listedRule(t *testing.T, h *apptest.H, botID, conn, action string) *v1.Rule {
	t.Helper()
	res, err := h.Client.ListRules(h.Ctx(), connect.NewRequest(&v1.ListRulesRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Msg.GetRules() {
		if r.GetConnector() == conn && r.GetAction() == action {
			return r
		}
	}
	t.Fatalf("no rule %s.%s", conn, action)
	return nil
}

func TestSpaceRulesSitBetweenOperatorAndBot(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	bot := h.CreateBot("Scout")
	set := func(decision string, locked bool) {
		t.Helper()
		if _, err := h.Client.SetSpaceRule(h.Ctx(), connect.NewRequest(&v1.SetSpaceRuleRequest{SpaceId: sp, Connector: "python", Action: "run", Decision: decision, Locked: locked})); err != nil {
			t.Fatal(err)
		}
	}
	// Unlocked: a default for every Bot of the Workspace with no rule.
	set(security.Ask, false)
	if r := listedRule(t, h, bot.Id, "python", "run"); r.GetDecision() != security.Ask || r.GetSpaceDecision() != security.Ask || r.GetSpaceLocked() {
		t.Fatalf("an unlocked workspace default: %+v", r)
	}
	// The owner overrides a default.
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.Id, Connector: "python", Action: "run", Decision: security.Allow})); err != nil {
		t.Fatal(err)
	}
	if r := listedRule(t, h, bot.Id, "python", "run"); r.GetDecision() != security.Allow {
		t.Fatalf("the owner could not override an unlocked default: %+v", r)
	}
	// Locked: a floor. The stricter wins, and loosening is refused.
	set(security.Ask, true)
	if r := listedRule(t, h, bot.Id, "python", "run"); r.GetDecision() != security.Ask {
		t.Fatalf("a locked workspace floor did not hold: %+v", r)
	}
	_, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.Id, Connector: "python", Action: "run", Decision: security.Allow}))
	if code(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("loosening a workspace floor: %v", err)
	}
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{BotId: bot.Id, Connector: "python", Action: "run", Decision: security.Deny})); err != nil {
		t.Fatalf("tightening under a workspace floor: %v", err)
	}
	// Only admins set them.
	member, _ := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	if _, err := member.SetSpaceRule(h.Ctx(), connect.NewRequest(&v1.SetSpaceRuleRequest{SpaceId: sp, Connector: "python", Action: "run", Decision: security.Allow})); code(err) != connect.CodePermissionDenied {
		t.Fatalf("a member set a workspace rule: %v", err)
	}
}

func TestSpacePolicyJoinsTheLayers(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	if _, err := h.Client.UpdateSpacePolicy(h.Ctx(), connect.NewRequest(&v1.UpdateSpacePolicyRequest{SpaceId: sp, ApprovalPolicy: "Never email strangers."})); err != nil {
		t.Fatal(err)
	}
	res, err := h.Client.ListSpaceRules(h.Ctx(), connect.NewRequest(&v1.ListSpaceRulesRequest{SpaceId: sp}))
	if err != nil || res.Msg.GetApprovalPolicy() != "Never email strangers." {
		t.Fatalf("policy = %+v, %v", res, err)
	}
}

func TestSpaceActivity(t *testing.T) {
	h := apptest.New(t)
	sp := homeSpace(t, h)
	_, bu := joinSpace(t, h, sp, "bob@test.local", access.RoleMember)
	bot := h.CreateBot("Scout")
	if _, err := h.Client.SetSpaceRole(h.Ctx(), connect.NewRequest(&v1.SetSpaceRoleRequest{SpaceId: sp, UserId: bu.ID, Role: access.RoleGuest})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatal(err)
	}
	res, err := h.Client.ListSpaceActivity(h.Ctx(), connect.NewRequest(&v1.ListSpaceActivityRequest{SpaceId: sp}))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range res.Msg.GetEvents() {
		kinds[e.GetKind()] = true
	}
	for _, k := range []string{access.Joined, access.BotCreated, access.RoleChanged, access.BotDeleted} {
		if !kinds[k] {
			t.Errorf("activity has no %s: %v", k, kinds)
		}
	}
}
