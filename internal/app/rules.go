package app

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

func (a *App) ListRules(ctx context.Context, req *connect.Request[v1.ListRulesRequest]) (*connect.Response[v1.ListRulesResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := a.ownBot(ctx, botID); err != nil {
		return nil, err
	}
	a.sweepRules(botID)
	stored := a.ruleMap(botID)
	out := &v1.ListRulesResponse{}
	botSec := &v1.RuleSection{
		Id:      "bot",
		Title:   "This Bot",
		Summary: "Python and Terminal are allowed until you change them. Files and Desktop cover the chat tools (and the Python desktop helpers).",
	}
	for _, row := range security.BuiltinRows() {
		r := a.effectiveRule(botID, row.Connector, row.Action, row.Title, stored, "")
		botSec.Rules = append(botSec.Rules, r)
		out.Rules = append(out.Rules, r)
	}
	out.Sections = append(out.Sections, botSec)

	var secs []db.Secret
	a.DB.Where("bot_id = ?", botID).Order("name").Find(&secs)
	secSec := &v1.RuleSection{
		Id:      "secrets",
		Title:   "Secrets",
		Summary: "Each secret is asked the first time. Always on a slip keeps only that secret.",
	}
	if len(secs) == 0 {
		secSec.Summary = "Add secrets on the Secrets tab. Each is asked the first time unless you allow it here."
	}
	for _, s := range secs {
		r := a.effectiveRule(botID, security.Secrets, s.Name, s.Name, stored, "")
		secSec.Rules = append(secSec.Rules, r)
		out.Rules = append(out.Rules, r)
	}
	out.Sections = append(out.Sections, secSec)

	var attached []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Order("created_at").Find(&attached)
	for i := range attached {
		var c db.Connector
		if a.DB.First(&c, "id = ?", attached[i].ConnectorID).Error != nil {
			continue
		}
		slug := toolsgen.Slug(c.Name)
		sec := &v1.RuleSection{
			Id:      slug,
			Title:   c.Name,
			Summary: fmt.Sprintf("When there is no override, this connector uses %s.", modeWord(c.DefaultMode)),
		}
		var tools []mcpx.Tool
		_ = json.Unmarshal([]byte(attached[i].ToolsJSON), &tools)
		if len(tools) == 0 {
			sec.Summary = "Authorize on Connectors, then actions show here. Unset actions use " + modeWord(c.DefaultMode) + "."
		}
		for _, t := range tools {
			if t.Name == "" {
				continue
			}
			r := a.effectiveRule(botID, slug, t.Name, t.Name, stored, c.DefaultMode)
			sec.Rules = append(sec.Rules, r)
			out.Rules = append(out.Rules, r)
		}
		out.Sections = append(out.Sections, sec)
	}
	return connect.NewResponse(out), nil
}

func (a *App) SetRule(ctx context.Context, req *connect.Request[v1.SetRuleRequest]) (*connect.Response[v1.Rule], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	dec := security.Rule(req.Msg.GetDecision())
	var rule db.Rule
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", req.Msg.GetBotId(), req.Msg.GetConnector(), req.Msg.GetAction()).Limit(1).Find(&rule)
	if rule.ID == "" {
		rule = db.Rule{ID: ids.New(), BotID: req.Msg.GetBotId(), Connector: req.Msg.GetConnector(), Action: req.Msg.GetAction(), Decision: dec}
		a.DB.Create(&rule)
	} else {
		rule.Decision = dec
		a.DB.Save(&rule)
	}
	return connect.NewResponse(&v1.Rule{
		Id: rule.ID, BotId: rule.BotID, Connector: rule.Connector, Action: rule.Action, Decision: rule.Decision,
		Title: security.Describe(rule.Connector, rule.Action, "").Title,
	}), nil
}

func (a *App) pruneConnectorRules(botID, slug string, keepActions []string) {
	if keepActions == nil {
		a.DB.Where("bot_id = ? AND connector = ?", botID, slug).Delete(&db.Rule{})
		return
	}
	keep := map[string]bool{security.Star: true}
	for _, n := range keepActions {
		keep[n] = true
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ? AND connector = ?", botID, slug).Find(&rows)
	for i := range rows {
		if !keep[rows[i].Action] {
			a.DB.Delete(&rows[i])
		}
	}
}

func (a *App) sweepRules(botID string) {
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", botID, security.Secrets, "get").Delete(&db.Rule{})
	live := map[string]bool{
		security.Python: true, security.Terminal: true, security.Files: true,
		security.Desktop: true, security.Bot: true, security.Secrets: true,
	}
	secretOK := map[string]bool{}
	var secs []db.Secret
	a.DB.Where("bot_id = ?", botID).Find(&secs)
	for _, s := range secs {
		secretOK[s.Name] = true
	}
	var attached []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&attached)
	actions := map[string]map[string]bool{}
	for i := range attached {
		var c db.Connector
		if a.DB.First(&c, "id = ?", attached[i].ConnectorID).Error != nil {
			continue
		}
		slug := toolsgen.Slug(c.Name)
		live[slug] = true
		keep := map[string]bool{security.Star: true}
		var tools []mcpx.Tool
		_ = json.Unmarshal([]byte(attached[i].ToolsJSON), &tools)
		for _, t := range tools {
			if t.Name != "" {
				keep[t.Name] = true
			}
		}
		actions[slug] = keep
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	for i := range rows {
		r := rows[i]
		if !live[r.Connector] {
			a.DB.Delete(&r)
			continue
		}
		if r.Connector == security.Secrets {
			if !secretOK[r.Action] {
				a.DB.Delete(&r)
			}
			continue
		}
		if keep, ok := actions[r.Connector]; ok && !keep[r.Action] {
			a.DB.Delete(&r)
		}
	}
}

func (a *App) ruleMap(botID string) map[string]db.Rule {
	var rows []db.Rule
	a.DB.Where("bot_id = ?", botID).Find(&rows)
	out := map[string]db.Rule{}
	for _, r := range rows {
		out[security.Key(r.Connector, r.Action)] = r
	}
	return out
}

func (a *App) effectiveRule(botID, conn, action, title string, stored map[string]db.Rule, mcpDefault string) *v1.Rule {
	dec := security.Rule(a.ruleDecision(botID, conn, action, mcpDefault))
	r := &v1.Rule{BotId: botID, Connector: conn, Action: action, Decision: dec, Title: title}
	if row, ok := stored[security.Key(conn, action)]; ok {
		r.Id = row.ID
	}
	return r
}

func modeWord(s string) string {
	switch security.Rule(s) {
	case security.Allow:
		return "Allow"
	case security.Deny:
		return "Deny"
	default:
		return "Ask"
	}
}
