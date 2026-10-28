package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/catalog"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
	"silo.agent/internal/toolsgen"
)

// A Workspace's guardrails: for an action, the least strict mode any of its
// Bots may use. They are listed in the sections a Bot's Rules page has, so a
// Workspace admin sets them the same way; a rule's decision and output are
// the Workspace's floor, and Allow and Pass mean none.

// policyMax caps a Workspace policy text, in runes.
const policyMax = 8000

func (a *App) spaceRules(sp *db.Space) *v1.ListSpaceRulesResponse {
	var stored []db.SpaceRule
	a.DB.Where("space_id = ?", sp.ID).Find(&stored)
	floors := map[string]db.SpaceRule{}
	for _, r := range stored {
		floors[security.Key(r.Connector, r.Action)] = r
	}
	row := func(conn, action, title string) *v1.Rule {
		f := floors[security.Key(conn, action)]
		r := &v1.Rule{Connector: conn, Action: action, Title: title, Decision: security.Allow}
		if f.Decision != "" {
			r.Decision = f.Decision
		}
		if outputApplies(conn, action) {
			r.Output = security.OutPass
			if f.Output != "" {
				r.Output = f.Output
			}
		}
		if op, ok := a.operatorRule(conn, action); ok {
			r.OperatorDecision, r.OperatorOutput, r.Locked = op.Decision, op.Output, op.Locked
		}
		return r
	}
	out := &v1.ListSpaceRulesResponse{ApprovalPolicy: sp.ApprovalPolicy, OutputPolicy: sp.OutputPolicy}
	builtin := &v1.RuleSection{
		Id:            "bot",
		Title:         "Built-in tools",
		Summary:       "The least strict a Bot of this workspace may set each of its own tools to. Allow sets no limit.",
		OutputSummary: "The least a Bot of this workspace may do with each result. Pass sets no limit.",
	}
	for _, r := range security.BuiltinRows() {
		builtin.Rules = append(builtin.Rules, row(r.Connector, r.Action, r.Title))
	}
	out.Sections = append(out.Sections, builtin,
		&v1.RuleSection{Id: "secrets", Title: "Secrets", Summary: "Reading any secret a Bot holds.", Rules: []*v1.Rule{row(security.Secrets, security.Star, "Any secret")}},
		&v1.RuleSection{Id: "channels", Title: "Channels", Summary: "Sending to any channel a Bot has.", Rules: []*v1.Rule{row(security.Channels, security.Star, "Any channel")}},
	)
	var conns []db.Connector
	a.DB.Where("space_id = ? AND kind = ?", sp.ID, catalog.KindCustom).Order("name").Find(&conns)
	for _, c := range conns {
		slug := toolsgen.Slug(c.Name)
		sec := &v1.RuleSection{
			Id: slug, Title: c.Name,
			Summary:       "Every action of this connector, in every Bot that uses it.",
			OutputSummary: "What this connector answers, in every Bot that uses it.",
			Rules:         []*v1.Rule{row(slug, security.Star, "All actions")},
			ConnectorId:   c.ID,
			HasImage:      len(c.Image) > 0,
		}
		// The actions any Bot's session listed, once each.
		seen := map[string]bool{}
		var grants []db.BotConnector
		a.DB.Where("connector_id = ?", c.ID).Find(&grants)
		for _, g := range grants {
			var tools []mcpx.Tool
			_ = json.Unmarshal([]byte(g.ToolsJSON), &tools)
			for _, t := range tools {
				if t.Name != "" && !seen[t.Name] {
					seen[t.Name] = true
					sec.Rules = append(sec.Rules, row(slug, t.Name, t.Name))
				}
			}
		}
		if c.Transport == connector.TransportBuiltin && len(seen) == 0 {
			sec.Summary = "Every action of this connector. Its actions show here once a Bot has it set up."
		}
		out.Sections = append(out.Sections, sec)
	}
	return out
}

func (a *App) ListSpaceRules(ctx context.Context, req *connect.Request[v1.ListSpaceRulesRequest]) (*connect.Response[v1.ListSpaceRulesResponse], error) {
	sp, _, err := access.Space(ctx, a.DB, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(a.spaceRules(sp)), nil
}

// SetSpaceRule sets one action's floors; Allow and Pass (or nothing) remove
// a side, and a row with neither side goes.
func (a *App) SetSpaceRule(ctx context.Context, req *connect.Request[v1.SetSpaceRuleRequest]) (*connect.Response[v1.Rule], error) {
	sp, _, err := access.Space(ctx, a.DB, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	m := req.Msg
	conn, action := strings.TrimSpace(m.GetConnector()), strings.TrimSpace(m.GetAction())
	if conn == "" || action == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("connector and action are required"))
	}
	decision, output := "", ""
	if d := m.GetDecision(); d != "" && security.Rule(d) != security.Allow {
		decision = security.Rule(d)
	}
	if o := m.GetOutput(); o != "" && security.Output(o) != security.OutPass {
		output = security.Output(o)
	}
	var row db.SpaceRule
	a.DB.Where("space_id = ? AND connector = ? AND action = ?", sp.ID, conn, action).Limit(1).Find(&row)
	switch {
	case decision == "" && output == "":
		if row.ID != "" {
			a.DB.Delete(&row)
		}
	case row.ID == "":
		row = db.SpaceRule{ID: ids.New(), SpaceID: sp.ID, Connector: conn, Action: action, Decision: decision, Output: output, UpdatedAt: time.Now()}
		err = a.DB.Create(&row).Error
	default:
		err = a.DB.Model(&row).Updates(map[string]any{"decision": decision, "output": output, "updated_at": time.Now()}).Error
	}
	if err != nil {
		return nil, err
	}
	out := &v1.Rule{Connector: conn, Action: action, Decision: security.Allow}
	if decision != "" {
		out.Decision = decision
	}
	if outputApplies(conn, action) {
		out.Output = security.OutPass
		if output != "" {
			out.Output = output
		}
	}
	return connect.NewResponse(out), nil
}

func (a *App) UpdateSpacePolicy(ctx context.Context, req *connect.Request[v1.UpdateSpacePolicyRequest]) (*connect.Response[v1.ListSpaceRulesResponse], error) {
	sp, _, err := access.Space(ctx, a.DB, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	sp.ApprovalPolicy = textx.CapRunes(strings.TrimSpace(req.Msg.GetApprovalPolicy()), policyMax)
	sp.OutputPolicy = textx.CapRunes(strings.TrimSpace(req.Msg.GetOutputPolicy()), policyMax)
	if err := a.DB.Model(&db.Space{}).Where("id = ?", sp.ID).
		Updates(map[string]any{"approval_policy": sp.ApprovalPolicy, "output_policy": sp.OutputPolicy}).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(a.spaceRules(sp)), nil
}
