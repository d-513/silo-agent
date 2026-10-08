package account

import (
	"context"
	"os"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/skills"
)

// ListUsers is every account, oldest first, and the invite links still out.
func (s *Service) ListUsers(ctx context.Context, _ *connect.Request[v1.ListUsersRequest]) (*connect.Response[v1.ListUsersResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	var users []db.User
	if err := s.db.Order("created_at, id").Find(&users).Error; err != nil {
		return nil, err
	}
	var counts []struct {
		UserID string
		N      int32
	}
	if err := s.db.Model(&db.Bot{}).Select("user_id, count(*) AS n").Group("user_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	bots := map[string]int32{}
	for _, c := range counts {
		bots[c.UserID] = c.N
	}
	out := &v1.ListUsersResponse{}
	for i := range users {
		p := protoUser(&users[i])
		p.Bots = bots[users[i].ID]
		out.Users = append(out.Users, p)
	}
	var invites []db.Invite
	if err := s.db.Where("used_at IS NULL AND expires_at > ?", time.Now()).Order("created_at desc").Find(&invites).Error; err != nil {
		return nil, err
	}
	for i := range invites {
		out.Invites = append(out.Invites, protoInvite(&invites[i]))
	}
	return connect.NewResponse(out), nil
}

// UpdateUser changes another account's role, access, password or two-factor.
// An admin cannot take their own role or access away, which also means there
// is always one admin left.
func (s *Service) UpdateUser(ctx context.Context, req *connect.Request[v1.UpdateUserRequest]) (*connect.Response[v1.User], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	me := access.User(ctx)
	u, err := s.reload(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	m := req.Msg
	self := u.ID == me.ID
	if self && m.Admin != nil && !m.GetAdmin() {
		return nil, invalid("you cannot remove your own admin role; ask another admin")
	}
	if self && m.Disabled != nil && m.GetDisabled() {
		return nil, invalid("you cannot disable your own account")
	}
	if m.GetPassword() != "" {
		keep := ""
		if cur := access.Session(ctx); self && cur != nil {
			keep = cur.ID
		}
		if err := s.setPassword(u.ID, m.GetPassword(), keep); err != nil {
			return nil, err
		}
	}
	if m.GetResetTotp() {
		if err := s.clearTOTP(u.ID); err != nil {
			return nil, err
		}
	}
	if m.Admin != nil && m.GetAdmin() != u.Admin {
		if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Update("admin", m.GetAdmin()).Error; err != nil {
			return nil, err
		}
	}
	if m.Disabled != nil && m.GetDisabled() != u.Disabled {
		if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Update("disabled", m.GetDisabled()).Error; err != nil {
			return nil, err
		}
		if m.GetDisabled() {
			if err := auth.EndUserSessions(s.db, u.ID, ""); err != nil {
				return nil, err
			}
			s.host.SuspendUser(ctx, u.ID)
		} else {
			s.host.ResumeUser(u.ID)
		}
	}
	fresh, err := s.reload(u.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(protoUser(fresh)), nil
}

// DeleteUser removes an account and everything it owns: its Bots with their
// boxes and files, its personal skills, its sessions.
func (s *Service) DeleteUser(ctx context.Context, req *connect.Request[v1.DeleteUserRequest]) (*connect.Response[v1.DeleteUserResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	u, err := s.reload(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if u.ID == access.User(ctx).ID {
		return nil, invalid("you cannot delete your own account")
	}
	// Signed out and marked first, so nothing of theirs starts while it goes.
	if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Update("disabled", true).Error; err != nil {
		return nil, err
	}
	if err := auth.EndUserSessions(s.db, u.ID, ""); err != nil {
		return nil, err
	}
	s.host.DropUserBots(ctx, u.ID)
	if dir := s.cfg().DataDir; dir != "" {
		_ = os.RemoveAll(skills.PersonalDir(dir, u.ID))
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", u.ID).Delete(&db.RecoveryCode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&db.Invite{}).Error; err != nil {
			return err
		}
		return tx.Delete(&db.User{}, "id = ?", u.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.DeleteUserResponse{}), nil
}

// lastAdmin reports whether u is the only admin who can still sign in.
func (s *Service) lastAdmin(u *db.User) bool {
	if !u.Admin || u.Disabled {
		return false
	}
	var others int64
	s.db.Model(&db.User{}).Where("admin = ? AND disabled = ? AND id <> ?", true, false, u.ID).Count(&others)
	return others == 0
}
