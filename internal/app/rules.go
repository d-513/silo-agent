package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/app/connector"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/mcpx"
	"silo.agent/internal/prompts"
	"silo.agent/internal/security"
	"silo.agent/internal/toolsgen"
)

func (a *App) ListRules(ctx context.Context, req *connect.Request[v1.ListRulesRequest]) (*connect.Response[v1.ListRulesResponse], error) {
	botID := req.Msg.GetBotId()
	if _, err := a.bot(ctx, botID, access.Manage); err != nil {
		return nil, err
	}
	a.sweepRules(botID)
	stored := a.ruleMap(botID)
	out := &v1.ListRulesResponse{}
	botSec := &v1.RuleSection{
		Id:            "bot",
		Title:         "This Bot",
		Summary:       "Python, Terminal, Web search, and Artifacts are allowed until you change them. Files and Desktop cover the chat tools (and the Python desktop helpers).",
		OutputSummary: "What comes from outside (web search, mail, chat history, transcripts) is checked until you change it. The Bot's own tools pass; set Python or Terminal to Check to cover what a script fetched itself.",
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

	var chans []db.Channel
	a.DB.Where("bot_id = ?", botID).Order("created_at").Find(&chans)
	chanSec := &v1.RuleSection{
		Id:      "channels",
		Title:   "Channels",
		Summary: "Sending to a channel is allowed until you change it. Each channel has its own rule.",
	}
	if len(chans) == 0 {
		chanSec.Summary = "Add a channel on the Channels tab. Each channel then gets its own send rule."
	}
	for i := range chans {
		r := a.effectiveRule(botID, security.Channels, chans[i].ID, chans[i].Name, stored, "")
		chanSec.Rules = append(chanSec.Rules, r)
		out.Rules = append(out.Rules, r)
	}
	out.Sections = append(out.Sections, chanSec)

	var attached []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Order("created_at").Find(&attached)
	for i := range attached {
		var c db.Connector
		if a.DB.First(&c, "id = ?", attached[i].ConnectorID).Error != nil {
			continue
		}
		slug := toolsgen.Slug(c.Name)
		sec := &v1.RuleSection{
			Id:            slug,
			Title:         c.Name,
			Summary:       fmt.Sprintf("When there is no override, this connector uses %s.", modeWord(c.DefaultMode)),
			OutputSummary: "Everything this connector answers was written somewhere else, so each result is checked until you change it.",
			ConnectorId:   c.ID,
			HasImage:      len(c.Image) > 0,
		}
		if c.Transport == connector.TransportBuiltin {
			sec.Summary = "Each action has its own default: reading is allowed, anything that sends or changes mail asks."
		}
		var tools []mcpx.Tool
		_ = json.Unmarshal([]byte(attached[i].ToolsJSON), &tools)
		if len(tools) == 0 {
			sec.Summary = "Authorize on Connectors, then actions show here. Unset actions use " + modeWord(c.DefaultMode) + "."
			// A connection that failed says why, so an empty section does not
			// read as broken rules.
			if e := strings.TrimSpace(attached[i].LastError); attached[i].AuthStatus == connector.StatusErr && e != "" {
				sec.Summary = "Not connected: " + e + ". Its actions show here once it connects."
			}
		}
		for _, t := range tools {
			if t.Name == "" {
				continue
			}
			r := a.effectiveRule(botID, slug, t.Name, t.Name, stored, connector.BuiltinMode(&c, t.Name))
			sec.Rules = append(sec.Rules, r)
			out.Rules = append(out.Rules, r)
		}
		out.Sections = append(out.Sections, sec)
	}
	out.DefaultOutputPolicy = strings.TrimSpace(prompts.FilterPolicy)
	out.FilterEnabled = a.cfg().Filter.Enabled
	return connect.NewResponse(out), nil
}

func (a *App) SetRule(ctx context.Context, req *connect.Request[v1.SetRuleRequest]) (*connect.Response[v1.Rule], error) {
	m := req.Msg
	if _, err := a.bot(ctx, m.GetBotId(), access.Manage); err != nil {
		return nil, err
	}
	// A request that names an output and no decision is about the output
	// rule alone; the call rule stays as it is.
	setDecision := m.Output == nil || m.GetDecision() != ""
	// A locked operator row is the floor: the owner may tighten it, never
	// loosen it. Refused here, so the UI cannot be talked around.
	// So is a locked row of the Bot's Workspace.
	if sp, ok := a.spaceRule(m.GetBotId(), m.GetConnector(), m.GetAction()); ok {
		if setDecision && sp.Decision != "" && security.Looser(security.Rule(m.GetDecision()), sp.Decision) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf(
				"your workspace holds %s.%s at %s", m.GetConnector(), m.GetAction(), modeWord(sp.Decision)))
		}
		if m.Output != nil && sp.Output != "" && security.LooserOutput(security.Output(m.GetOutput()), sp.Output) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf(
				"your workspace holds %s.%s results at %s", m.GetConnector(), m.GetAction(), outputWord(sp.Output)))
		}
	}
	if op, ok := a.operatorRule(m.GetConnector(), m.GetAction()); ok && op.Locked {
		if setDecision && op.Decision != "" && security.Looser(security.Rule(m.GetDecision()), op.Decision) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf(
				"your operator holds %s.%s at %s", m.GetConnector(), m.GetAction(), modeWord(op.Decision)))
		}
		if m.Output != nil && op.Output != "" && security.LooserOutput(security.Output(m.GetOutput()), op.Output) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf(
				"your operator holds %s.%s results at %s", m.GetConnector(), m.GetAction(), outputWord(op.Output)))
		}
	}
	var rule db.Rule
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", m.GetBotId(), m.GetConnector(), m.GetAction()).Limit(1).Find(&rule)
	fresh := rule.ID == ""
	if fresh {
		rule = db.Rule{ID: ids.New(), BotID: m.GetBotId(), Connector: m.GetConnector(), Action: m.GetAction()}
	}
	if setDecision {
		rule.Decision = security.Rule(m.GetDecision())
	}
	if m.Output != nil {
		rule.Output = security.Output(m.GetOutput())
	}
	if fresh {
		a.DB.Create(&rule)
	} else {
		a.DB.Save(&rule)
	}
	out := a.effectiveRule(rule.BotID, rule.Connector, rule.Action, security.Describe(rule.Connector, rule.Action, "").Title,
		map[string]db.Rule{security.Key(rule.Connector, rule.Action): rule}, a.callFallback(rule.BotID, rule.Connector, rule.Action))
	return connect.NewResponse(out), nil
}

func (a *App) PruneRules(botID, slug string, keepActions []string) {
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

// RenameRules moves a Bot's rules for one connector slug to another, for a
// connection that was renamed: its rules are the same connection's. A row the
// new slug already has wins.
func (a *App) RenameRules(botID, from, to string) {
	if from == to {
		return
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ? AND connector = ?", botID, from).Find(&rows)
	for i := range rows {
		var n int64
		a.DB.Model(&db.Rule{}).Where("bot_id = ? AND connector = ? AND action = ?", botID, to, rows[i].Action).Count(&n)
		if n > 0 {
			a.DB.Delete(&rows[i])
			continue
		}
		a.DB.Model(&rows[i]).Update("connector", to)
	}
}

func (a *App) sweepRules(botID string) {
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", botID, security.Secrets, "get").Delete(&db.Rule{})
	a.DB.Where("bot_id = ? AND connector = ? AND action = ?", botID, security.Skills, "propose").Delete(&db.Rule{})
	// The built-ins come from the rows the page lists, never a second list kept
	// by hand: one that fell behind dropped every tunnels and mailbox rule the
	// owner set on the next look at the page.
	live := map[string]bool{security.Secrets: true, security.Channels: true}
	for _, row := range security.BuiltinRows() {
		live[row.Connector] = true
	}
	secretOK := map[string]bool{}
	var secs []db.Secret
	a.DB.Where("bot_id = ?", botID).Find(&secs)
	for _, s := range secs {
		secretOK[s.Name] = true
	}
	chanOK := map[string]bool{}
	var chans []db.Channel
	a.DB.Where("bot_id = ?", botID).Find(&chans)
	for i := range chans {
		chanOK[chans[i].ID] = true
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
		if r.Connector == security.Channels {
			if !chanOK[r.Action] {
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
	if outputApplies(conn, action) {
		r.Output = a.ruleOutput(botID, conn, action, outputFallback(conn))
	}
	if mine, ok := stored[security.Key(conn, action)]; ok {
		r.Id = mine.ID
	}
	if op, ok := a.operatorRule(conn, action); ok {
		r.OperatorDecision, r.OperatorOutput, r.Locked = op.Decision, op.Output, op.Locked
	}
	if sp, ok := a.spaceRule(botID, conn, action); ok {
		r.SpaceDecision, r.SpaceOutput = sp.Decision, sp.Output
	}
	return r
}

func modeWord(s string) string {
	switch security.Rule(s) {
	case security.Allow:
		return "Allow"
	case security.Deny:
		return "Deny"
	case security.Auto:
		return "Auto"
	default:
		return "Ask"
	}
}

func outputWord(s string) string {
	switch security.Output(s) {
	case security.OutPass:
		return "Pass"
	case security.OutFlag:
		return "Flag"
	case security.OutWithhold:
		return "Withhold"
	default:
		return "Check"
	}
}

// outputApplies reports whether an action has a result the output filter
// sees. A secret's value and a channel send do not, and tunnels.public is a
// permission inside open_tunnel, not a tool of its own.
func outputApplies(conn, action string) bool {
	switch {
	case conn == security.Secrets, conn == security.Channels:
		return false
	case conn == security.Tunnels && action == "public", conn == security.Artifact && action == "app_network":
		return false
	}
	return true
}

// outputFallback is the output mode of an action the catalog does not know:
// a connector's, which is checked, since everything it answers was written
// somewhere else.
func outputFallback(conn string) string {
	if security.Reserved(conn) {
		return security.OutPass
	}
	return security.OutCheck
}

// callFallback is the call mode an attached connector gives its action when
// the Bot has no rule for it; "" for a built-in.
func (a *App) callFallback(botID, slug, action string) string {
	if security.Reserved(slug) {
		return ""
	}
	var links []db.BotConnector
	a.DB.Where("bot_id = ?", botID).Find(&links)
	for i := range links {
		var c db.Connector
		if a.DB.First(&c, "id = ?", links[i].ConnectorID).Error == nil && toolsgen.Slug(c.Name) == slug {
			return connector.BuiltinMode(&c, action)
		}
	}
	return ""
}
