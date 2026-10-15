package app

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

// The operator's layer of the rules engine. Its rows live in their own table
// (db.OperatorRule), not on one Bot: they are the installation's rules, and
// every Bot obeys them. A row is looked up like an owner's — the action's row,
// then the connector's * row — and an owner may tighten a locked one but not
// loosen it, so the engine takes the stricter of the two.
//
// Operator policy text (config.rules) is read before the Bot's own by both the
// auto-approval gate and the output filter.

// botRule is the owner's call rule for an action: the action's own row, else
// the connector's * row. set is false when the owner has no rule for it, so
// the catalog default speaks.
func (a *App) botRule(botID, conn, action string) (string, bool) {
	actions := []string{action}
	if action != security.Star {
		actions = append(actions, security.Star)
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ? AND connector = ? AND action IN ? AND decision <> ''", botID, conn, actions).Find(&rows)
	var star string
	for _, r := range rows {
		if r.Action == action {
			return r.Decision, true
		}
		star = r.Decision
	}
	if star != "" {
		return star, true
	}
	return "", false
}

// botOutput is the owner's output rule for an action, "" when there is none.
func (a *App) botOutput(botID, conn, action string) string {
	actions := []string{action}
	if action != security.Star {
		actions = append(actions, security.Star)
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ? AND connector = ? AND action IN ? AND output <> ''", botID, conn, actions).Find(&rows)
	var star string
	for _, r := range rows {
		if r.Action == action {
			return r.Output
		}
		star = r.Output
	}
	return star
}

// operatorRule is the operator's row for an action, the action's own or the
// connector's *.
func (a *App) operatorRule(conn, action string) (db.OperatorRule, bool) {
	actions := []string{action}
	if action != security.Star {
		actions = append(actions, security.Star)
	}
	var rows []db.OperatorRule
	a.DB.Where("connector = ? AND action IN ?", conn, actions).Find(&rows)
	var star db.OperatorRule
	var starSet bool
	for _, r := range rows {
		if r.Action == action {
			return r, true
		}
		star, starSet = r, true
	}
	return star, starSet
}

// layeredDecision combines the owner's call rule for an action with the
// operator's. A locked operator row is a floor: the stricter value wins. An
// unlocked one is only a default, so it applies while the owner has no rule.
func (a *App) layeredDecision(botID, conn, action string) (string, bool) {
	bot, botSet := a.botRule(botID, conn, action)
	op, opSet := a.operatorRule(conn, action)
	opVal := ""
	if opSet {
		opVal = op.Decision
	}
	switch {
	case opVal != "" && op.Locked && botSet:
		return security.Stricter(bot, opVal), true
	case botSet:
		return bot, true
	case opVal != "":
		return opVal, true
	}
	return "", false
}

// layeredOutput is the output rule in effect for an action: the owner's, then
// the operator's, with a locked operator row the floor.
func (a *App) layeredOutput(botID, conn, action string) (string, bool) {
	bot := a.botOutput(botID, conn, action)
	op, opSet := a.operatorRule(conn, action)
	opVal := ""
	if opSet {
		opVal = op.Output
	}
	switch {
	case opVal != "" && op.Locked && bot != "":
		return security.StricterOutput(bot, opVal), true
	case bot != "":
		return bot, true
	case opVal != "":
		return opVal, true
	}
	return "", false
}

// ListOperatorRules lists the installation's rule rows and its policy text.
func (a *App) ListOperatorRules(ctx context.Context, _ *connect.Request[v1.ListOperatorRulesRequest]) (*connect.Response[v1.ListOperatorRulesResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var rows []db.OperatorRule
	a.DB.Order("connector, action").Find(&rows)
	res := &v1.ListOperatorRulesResponse{
		ApprovalPolicy: a.cfg().Rules.ApprovalPolicy,
		OutputPolicy:   a.cfg().Rules.OutputPolicy,
	}
	for _, r := range rows {
		res.Rules = append(res.Rules, protoOperatorRule(r))
	}
	return connect.NewResponse(res), nil
}

// SetOperatorRule writes one operator row. An empty decision and empty output
// removes it, so a key the operator no longer cares about leaves nothing
// behind.
func (a *App) SetOperatorRule(ctx context.Context, req *connect.Request[v1.SetOperatorRuleRequest]) (*connect.Response[v1.OperatorRule], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	m := req.Msg
	if m.GetConnector() == "" || m.GetAction() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("connector and action are required"))
	}
	var row db.OperatorRule
	a.DB.Where("connector = ? AND action = ?", m.GetConnector(), m.GetAction()).Limit(1).Find(&row)
	fresh := row.ID == ""
	if fresh {
		row = db.OperatorRule{ID: ids.New(), Connector: m.GetConnector(), Action: m.GetAction()}
	}
	// The row is replaced: an empty side clears that side (it is never the
	// mode an empty string normalises to), and both empty removes the row.
	row.Decision = ""
	if d := m.GetDecision(); d != "" {
		row.Decision = security.Rule(d)
	}
	row.Output = ""
	if m.GetOutput() != "" {
		row.Output = security.Output(m.GetOutput())
	}
	row.Locked = m.GetLocked()
	if row.Decision == "" && row.Output == "" {
		if !fresh {
			a.DB.Delete(&row)
		}
		return connect.NewResponse(&v1.OperatorRule{Connector: row.Connector, Action: row.Action}), nil
	}
	if fresh {
		a.DB.Create(&row)
	} else {
		a.DB.Save(&row)
	}
	return connect.NewResponse(protoOperatorRule(row)), nil
}

// DeleteOperatorRule removes one operator row.
func (a *App) DeleteOperatorRule(ctx context.Context, req *connect.Request[v1.DeleteOperatorRuleRequest]) (*connect.Response[v1.DeleteOperatorRuleResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	m := req.Msg
	a.DB.Where("connector = ? AND action = ?", m.GetConnector(), m.GetAction()).Delete(&db.OperatorRule{})
	return connect.NewResponse(&v1.DeleteOperatorRuleResponse{}), nil
}

func protoOperatorRule(r db.OperatorRule) *v1.OperatorRule {
	return &v1.OperatorRule{Connector: r.Connector, Action: r.Action, Decision: r.Decision, Output: r.Output, Locked: r.Locked}
}
