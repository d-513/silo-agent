package space

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// inviteTTL is how long a Workspace invite link lasts.
const inviteTTL = 7 * 24 * time.Hour

// An invite link is a bearer: whoever opens it signed in joins. It is never
// matched by email, since a user can set their own with no confirmation.

func (s *Service) inviteProto(inv *db.SpaceInvite) *v1.SpaceInvite {
	return &v1.SpaceInvite{Id: inv.ID, Role: inv.Role, CreatedBy: access.Email(s.db, inv.CreatedBy), CreatedAt: stamp(inv.CreatedAt), ExpiresAt: stamp(inv.ExpiresAt)}
}

func (s *Service) CreateSpaceInvite(ctx context.Context, req *connect.Request[v1.CreateSpaceInviteRequest]) (*connect.Response[v1.SpaceInviteLink], error) {
	sp, caller, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	role := req.Msg.GetRole()
	switch role {
	case access.RoleMember, access.RoleGuest:
	case access.RoleAdmin:
		if caller != access.RoleOwner {
			return nil, deny("Only the workspace's owner can invite an admin.")
		}
	default:
		return nil, invalid("a role is admin, member or guest")
	}
	token := ids.Token()
	now := time.Now()
	inv := db.SpaceInvite{ID: ids.Hash(token), SpaceID: sp.ID, Role: role, CreatedBy: access.User(ctx).ID, CreatedAt: now, ExpiresAt: now.Add(inviteTTL)}
	if err := s.db.Create(&inv).Error; err != nil {
		return nil, err
	}
	url := access.PublicURL(s.cfg().PublicURL, access.Request(ctx)) + "/join/" + token
	return connect.NewResponse(&v1.SpaceInviteLink{Invite: s.inviteProto(&inv), Url: url}), nil
}

func (s *Service) ListSpaceInvites(ctx context.Context, req *connect.Request[v1.ListSpaceInvitesRequest]) (*connect.Response[v1.ListSpaceInvitesResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	var rows []db.SpaceInvite
	s.db.Where("space_id = ? AND used_at IS NULL AND expires_at > ?", sp.ID, time.Now()).Order("created_at DESC").Find(&rows)
	out := &v1.ListSpaceInvitesResponse{}
	for i := range rows {
		out.Invites = append(out.Invites, s.inviteProto(&rows[i]))
	}
	return connect.NewResponse(out), nil
}

func (s *Service) DeleteSpaceInvite(ctx context.Context, req *connect.Request[v1.DeleteSpaceInviteRequest]) (*connect.Response[v1.DeleteSpaceInviteResponse], error) {
	sp, _, err := access.Space(ctx, s.db, req.Msg.GetSpaceId(), access.Admin)
	if err != nil {
		return nil, err
	}
	s.db.Where("space_id = ? AND id = ?", sp.ID, req.Msg.GetId()).Delete(&db.SpaceInvite{})
	return connect.NewResponse(&v1.DeleteSpaceInviteResponse{}), nil
}

// invite finds the link a token names, and says whether it is still open.
func (s *Service) invite(token string) (*db.SpaceInvite, string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, "expired"
	}
	var inv db.SpaceInvite
	if s.db.Where("id = ?", ids.Hash(token)).Limit(1).Find(&inv); inv.ID == "" {
		return nil, "expired"
	}
	switch {
	case inv.UsedAt != nil:
		return &inv, "used"
	case time.Now().After(inv.ExpiresAt):
		return &inv, "expired"
	}
	return &inv, "ok"
}

func (s *Service) GetSpaceInvite(ctx context.Context, req *connect.Request[v1.GetSpaceInviteRequest]) (*connect.Response[v1.SpaceInviteInfo], error) {
	inv, state := s.invite(req.Msg.GetToken())
	out := &v1.SpaceInviteInfo{State: state}
	if state != "ok" {
		return connect.NewResponse(out), nil
	}
	var sp db.Space
	s.db.Where("id = ?", inv.SpaceID).Limit(1).Find(&sp)
	out.SpaceName, out.SpaceMark, out.Role = sp.Name, sp.Mark, inv.Role
	out.InvitedBy = access.Email(s.db, inv.CreatedBy)
	out.Signup = access.User(ctx) == nil && s.cfg().Spaces.InviteSignup
	return connect.NewResponse(out), nil
}

var errLinkGone = connect.NewError(connect.CodeFailedPrecondition, errors.New("this invite link has been used or has expired"))

func (s *Service) JoinSpace(ctx context.Context, req *connect.Request[v1.JoinSpaceRequest]) (*connect.Response[v1.JoinSpaceResponse], error) {
	inv, state := s.invite(req.Msg.GetToken())
	if state != "ok" {
		return nil, errLinkGone
	}
	out := &v1.JoinSpaceResponse{}
	u := access.User(ctx)
	if u == nil {
		if !s.cfg().Spaces.InviteSignup {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in to join; ask the instance admin for an account"))
		}
		nu, err := s.host.SignUp(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
		if err != nil {
			return nil, err
		}
		u = nu
		out.Email, out.Admin = nu.Email, nu.Admin
	}
	var sp db.Space
	s.db.Where("id = ?", inv.SpaceID).Limit(1).Find(&sp)
	if role := access.SpaceRole(s.db, sp.ID, u.ID); role != "" {
		// Already in: the link stays for whoever it was meant for.
		out.Space = s.view(&sp, role, u.ID)
		return connect.NewResponse(out), nil
	}
	res := s.db.Model(&db.SpaceInvite{}).Where("id = ? AND used_at IS NULL", inv.ID).Updates(map[string]any{"used_at": time.Now(), "used_by": u.ID})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, errLinkGone
	}
	if err := s.db.Create(&db.SpaceMember{SpaceID: sp.ID, UserID: u.ID, Role: inv.Role, InvitedBy: inv.CreatedBy, CreatedAt: time.Now()}).Error; err != nil {
		return nil, err
	}
	out.Space = s.view(&sp, inv.Role, u.ID)
	return connect.NewResponse(out), nil
}
