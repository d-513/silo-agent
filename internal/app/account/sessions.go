package account

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

func (s *Service) ListSessions(ctx context.Context, _ *connect.Request[v1.ListSessionsRequest]) (*connect.Response[v1.ListSessionsResponse], error) {
	return s.sessions(ctx)
}

// sessions lists the signed-in user's live sessions, this one first.
func (s *Service) sessions(ctx context.Context) (*connect.Response[v1.ListSessionsResponse], error) {
	u := access.User(ctx)
	var rows []db.Session
	if err := s.db.Where("user_id = ? AND expires_at > ?", u.ID, time.Now()).Order("last_seen_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	cur := ""
	if c := access.Session(ctx); c != nil {
		cur = c.ID
	}
	out := &v1.ListSessionsResponse{}
	for _, r := range rows {
		p := &v1.Session{
			Id: r.ID, CreatedAt: stamp(r.CreatedAt), LastSeenAt: stamp(r.LastSeenAt), ExpiresAt: stamp(r.ExpiresAt),
			UserAgent: r.UserAgent, Ip: r.IP, Method: r.Method, Current: r.ID == cur,
		}
		if p.Current {
			out.Sessions = append([]*v1.Session{p}, out.Sessions...)
		} else {
			out.Sessions = append(out.Sessions, p)
		}
	}
	return connect.NewResponse(out), nil
}

// RevokeSession signs one other session of the user's out.
func (s *Service) RevokeSession(ctx context.Context, req *connect.Request[v1.RevokeSessionRequest]) (*connect.Response[v1.ListSessionsResponse], error) {
	id := req.Msg.GetId()
	if cur := access.Session(ctx); cur != nil && cur.ID == id {
		return nil, invalid("this is the session you are using; sign out instead")
	}
	ok, err := auth.EndSessionByID(s.db, access.User(ctx).ID, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("session not found"))
	}
	return s.sessions(ctx)
}

// RevokeOtherSessions signs the user out everywhere but here.
func (s *Service) RevokeOtherSessions(ctx context.Context, _ *connect.Request[v1.RevokeOtherSessionsRequest]) (*connect.Response[v1.ListSessionsResponse], error) {
	cur := access.Session(ctx)
	if cur == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if err := auth.EndUserSessions(s.db, access.User(ctx).ID, cur.ID); err != nil {
		return nil, err
	}
	return s.sessions(ctx)
}
