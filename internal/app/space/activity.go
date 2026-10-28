package space

import (
	"context"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
)

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
