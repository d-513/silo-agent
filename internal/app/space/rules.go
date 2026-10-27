package space

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/security"
	"silo.agent/internal/textx"
)

// policyMax caps a Workspace policy text, in runes.
const policyMax = 8000

func ruleProto(r *db.SpaceRule) *v1.OperatorRule {
	return &v1.OperatorRule{Connector: r.Connector, Action: r.Action, Decision: r.Decision, Output: r.Output, Locked: r.Locked}
}

func (s *Service) rules(sp *db.Space) *v1.ListSpaceRulesResponse {
	var rows []db.SpaceRule
	s.db.Where("space_id = ?", sp.ID).Order("connector, action").Find(&rows)
	out := &v1.ListSpaceRulesResponse{ApprovalPolicy: sp.ApprovalPolicy, OutputPolicy: sp.OutputPolicy}
	for i := range rows {
		out.Rules = append(out.Rules, ruleProto(&rows[i]))
	}
	return out
}

func (s *Service) ListSpaceRules(ctx context.Context, req *connect.Request[v1.ListSpaceRulesRequest]) (*connect.Response[v1.ListSpaceRulesResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Build)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(s.rules(sp)), nil
}

// SetSpaceRule writes one Workspace row; both sides empty removes it.
func (s *Service) SetSpaceRule(ctx context.Context, req *connect.Request[v1.SetSpaceRuleRequest]) (*connect.Response[v1.OperatorRule], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	m := req.Msg
	conn, action := strings.TrimSpace(m.GetConnector()), strings.TrimSpace(m.GetAction())
	if conn == "" || action == "" {
		return nil, invalid("connector and action are required")
	}
	var row db.SpaceRule
	s.db.Where("space_id = ? AND connector = ? AND action = ?", sp.ID, conn, action).Limit(1).Find(&row)
	fresh := row.ID == ""
	if fresh {
		row = db.SpaceRule{ID: ids.New(), SpaceID: sp.ID, Connector: conn, Action: action}
	}
	row.Decision, row.Output = "", ""
	if d := m.GetDecision(); d != "" {
		row.Decision = security.Rule(d)
	}
	if o := m.GetOutput(); o != "" {
		row.Output = security.Output(o)
	}
	row.Locked, row.UpdatedAt = m.GetLocked(), time.Now()
	if row.Decision == "" && row.Output == "" {
		if !fresh {
			s.db.Delete(&row)
		}
		return connect.NewResponse(&v1.OperatorRule{Connector: conn, Action: action}), nil
	}
	if fresh {
		err = s.db.Create(&row).Error
	} else {
		err = s.db.Save(&row).Error
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(ruleProto(&row)), nil
}

func (s *Service) DeleteSpaceRule(ctx context.Context, req *connect.Request[v1.DeleteSpaceRuleRequest]) (*connect.Response[v1.DeleteSpaceRuleResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	s.db.Where("space_id = ? AND connector = ? AND action = ?", sp.ID, req.Msg.GetConnector(), req.Msg.GetAction()).Delete(&db.SpaceRule{})
	return connect.NewResponse(&v1.DeleteSpaceRuleResponse{}), nil
}

func (s *Service) UpdateSpacePolicy(ctx context.Context, req *connect.Request[v1.UpdateSpacePolicyRequest]) (*connect.Response[v1.ListSpaceRulesResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	sp.ApprovalPolicy = textx.CapRunes(strings.TrimSpace(req.Msg.GetApprovalPolicy()), policyMax)
	sp.OutputPolicy = textx.CapRunes(strings.TrimSpace(req.Msg.GetOutputPolicy()), policyMax)
	if err := s.db.Model(&db.Space{}).Where("id = ?", sp.ID).
		Updates(map[string]any{"approval_policy": sp.ApprovalPolicy, "output_policy": sp.OutputPolicy}).Error; err != nil {
		return nil, err
	}
	return connect.NewResponse(s.rules(sp)), nil
}

func (s *Service) ListSpaceActivity(ctx context.Context, req *connect.Request[v1.ListSpaceActivityRequest]) (*connect.Response[v1.ListSpaceActivityResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	var rows []db.AuditEvent
	s.db.Where("space_id = ?", sp.ID).Order("created_at desc").Limit(500).Find(&rows)
	out := &v1.ListSpaceActivityResponse{}
	for _, r := range rows {
		out.Events = append(out.Events, &v1.SpaceActivity{
			Id: r.ID, Actor: r.Actor, Kind: r.Decision, Subject: r.Subject, BotName: r.BotName, Detail: r.Detail,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return connect.NewResponse(out), nil
}
