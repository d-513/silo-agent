package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
)

func (a *App) ListSecrets(ctx context.Context, req *connect.Request[v1.ListSecretsRequest]) (*connect.Response[v1.ListSecretsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var secs []db.Secret
	a.DB.Where("bot_id = ?", req.Msg.GetBotId()).Find(&secs)
	out := &v1.ListSecretsResponse{}
	for _, s := range secs {
		m := &v1.SecretMeta{Id: s.ID, Name: s.Name, CreatedAt: s.CreatedAt.Format(time.RFC3339)}
		if s.LastUsedAt != nil {
			m.LastUsedAt = s.LastUsedAt.Format(time.RFC3339)
		}
		out.Secrets = append(out.Secrets, m)
	}
	return connect.NewResponse(out), nil
}

func (a *App) AddSecret(ctx context.Context, req *connect.Request[v1.AddSecretRequest]) (*connect.Response[v1.SecretMeta], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	if req.Msg.GetName() == "" || req.Msg.GetValue() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name and value required"))
	}
	s := db.Secret{ID: ids.New(), BotID: req.Msg.GetBotId(), Name: req.Msg.GetName(), Value: req.Msg.GetValue(), CreatedAt: time.Now()}
	if err := a.DB.Create(&s).Error; err != nil {
		return nil, err
	}
	a.mu.Lock()
	delete(a.mask, s.BotID)
	a.mu.Unlock()
	return connect.NewResponse(&v1.SecretMeta{Id: s.ID, Name: s.Name, CreatedAt: s.CreatedAt.Format(time.RFC3339)}), nil
}

func (a *App) DeleteSecret(ctx context.Context, req *connect.Request[v1.DeleteSecretRequest]) (*connect.Response[v1.DeleteSecretResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	a.DB.Where("id = ? AND bot_id = ?", req.Msg.GetId(), req.Msg.GetBotId()).Delete(&db.Secret{})
	a.mu.Lock()
	delete(a.mask, req.Msg.GetBotId())
	a.mu.Unlock()
	return connect.NewResponse(&v1.DeleteSecretResponse{}), nil
}

func (a *App) ListApprovals(ctx context.Context, req *connect.Request[v1.ListApprovalsRequest]) (*connect.Response[v1.ListApprovalsResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.Approval
	a.DB.Where("bot_id = ? AND status = ?", req.Msg.GetBotId(), "pending").Find(&rows)
	out := &v1.ListApprovalsResponse{}
	for _, r := range rows {
		out.Approvals = append(out.Approvals, protoApproval(&r))
	}
	return connect.NewResponse(out), nil
}

func protoApproval(r *db.Approval) *v1.Approval {
	p := security.Describe(r.Connector, r.Action, r.ArgsJSON)
	out := &v1.Approval{
		Id: r.ID, BotId: r.BotID, RunId: r.RunID,
		Connector: r.Connector, Action: r.Action, ArgsJson: r.ArgsJSON, Status: r.Status,
		Title: p.Title, Summary: p.Summary,
	}
	for _, f := range p.Fields {
		out.Fields = append(out.Fields, &v1.ApprovalField{Label: f.Label, Value: f.Value})
	}
	return out
}

func (a *App) DecideApproval(ctx context.Context, req *connect.Request[v1.DecideApprovalRequest]) (*connect.Response[v1.Approval], error) {
	var row db.Approval
	if err := a.DB.First(&row, "id = ?", req.Msg.GetId()).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if _, err := a.ownBot(ctx, row.BotID); err != nil {
		return nil, err
	}
	dec := security.Vote(req.Msg.GetDecision())
	if dec == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("decision must be allow_once, always, or deny"))
	}
	row.Status = dec
	a.DB.Save(&row)
	u := currentUser(ctx)
	var bot db.Bot
	a.DB.First(&bot, "id = ?", row.BotID)
	a.DB.Create(&db.Audit{
		ID: ids.New(), BotID: bot.ID, BotName: bot.Name, Crest: bot.Crest,
		Actor: u.Email, Action: row.Connector + "." + row.Action, Decision: dec, CreatedAt: time.Now(),
	})
	if dec == "always" {
		var rule db.Rule
		if err := a.DB.First(&rule, "bot_id = ? AND connector = ? AND action = ?", row.BotID, row.Connector, row.Action).Error; err != nil {
			a.DB.Create(&db.Rule{ID: ids.New(), BotID: row.BotID, Connector: row.Connector, Action: row.Action, Decision: "allow"})
		} else {
			rule.Decision = "allow"
			a.DB.Save(&rule)
		}
	}
	a.mu.Lock()
	w := a.approvals[row.ID]
	delete(a.approvals, row.ID)
	a.mu.Unlock()
	if w != nil {
		select {
		case w.ch <- dec:
		default:
		}
	}
	a.recomputeStatus(row.BotID)
	return connect.NewResponse(protoApproval(&row)), nil
}

func (a *App) ListRules(ctx context.Context, req *connect.Request[v1.ListRulesRequest]) (*connect.Response[v1.ListRulesResponse], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rows []db.Rule
	a.DB.Where("bot_id = ?", req.Msg.GetBotId()).Find(&rows)
	out := &v1.ListRulesResponse{}
	for _, r := range rows {
		out.Rules = append(out.Rules, &v1.Rule{Id: r.ID, BotId: r.BotID, Connector: r.Connector, Action: r.Action, Decision: r.Decision})
	}
	return connect.NewResponse(out), nil
}

func (a *App) SetRule(ctx context.Context, req *connect.Request[v1.SetRuleRequest]) (*connect.Response[v1.Rule], error) {
	if _, err := a.ownBot(ctx, req.Msg.GetBotId()); err != nil {
		return nil, err
	}
	var rule db.Rule
	err := a.DB.First(&rule, "bot_id = ? AND connector = ? AND action = ?", req.Msg.GetBotId(), req.Msg.GetConnector(), req.Msg.GetAction()).Error
	if err != nil {
		rule = db.Rule{ID: ids.New(), BotID: req.Msg.GetBotId(), Connector: req.Msg.GetConnector(), Action: req.Msg.GetAction(), Decision: req.Msg.GetDecision()}
		a.DB.Create(&rule)
	} else {
		rule.Decision = req.Msg.GetDecision()
		a.DB.Save(&rule)
	}
	return connect.NewResponse(&v1.Rule{Id: rule.ID, BotId: rule.BotID, Connector: rule.Connector, Action: rule.Action, Decision: rule.Decision}), nil
}

func (a *App) setBotStatus(id, st string) {
	a.DB.Model(&db.Bot{}).Where("id = ?", id).Update("status", st)
}

func argsJSON(name string) string {
	b, _ := json.Marshal(map[string]string{"name": name})
	return string(b)
}
