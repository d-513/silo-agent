package account

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

// AuthOptions is what the sign-in page offers.
func (s *Service) AuthOptions(context.Context, *connect.Request[v1.AuthOptionsRequest]) (*connect.Response[v1.AuthOptionsResponse], error) {
	cfg := s.cfg()
	out := &v1.AuthOptionsResponse{Password: cfg.PasswordSignIn(), Oidc: cfg.OIDC.On()}
	if out.Oidc {
		out.OidcLabel = cfg.OIDC.ButtonLabel()
	}
	return connect.NewResponse(out), nil
}

var errCredentials = errors.New("invalid credentials")

func (s *Service) SignIn(ctx context.Context, req *connect.Request[v1.SignInRequest]) (*connect.Response[v1.SignInResponse], error) {
	email := auth.NormalizeEmail(req.Msg.GetEmail())
	pass := req.Msg.GetPassword()
	if email == "" || pass == "" {
		return nil, invalid("email and password required")
	}
	cfg := s.cfg()
	if !cfg.PasswordSignIn() {
		return nil, refused("password sign-in is turned off; use %s", cfg.OIDC.ButtonLabel())
	}
	r := access.Request(ctx)
	ip := s.clientIP(r)
	if err := s.held(email, ip); err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.Model(&db.User{}).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, refused("no users; set bootstrap.email and bootstrap.password in silo.yaml")
	}
	u, err := auth.FindUser(s.db, email)
	if err != nil {
		return nil, err
	}
	wrong := func() error {
		s.limit.Fail(email, ip)
		return connect.NewError(connect.CodeUnauthenticated, errCredentials)
	}
	// The hash is checked even with no user, so a miss takes as long as a hit.
	hash := ""
	if u != nil {
		hash = u.PasswordHash
	}
	if !auth.CheckPassword(hash, pass) || u == nil {
		return nil, wrong()
	}
	if u.Disabled {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("this account is disabled; ask an admin"))
	}
	if u.TOTPSecret != "" {
		code := req.Msg.GetCode()
		if code == "" {
			return connect.NewResponse(&v1.SignInResponse{NeedsCode: true}), nil
		}
		if !s.spendCode(u, code) {
			s.limit.Fail(email, ip)
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("that code is not right"))
		}
	}
	s.limit.OK(email, ip)
	if err := s.signIn(access.ResponseWriter(ctx), r, u, MethodPassword); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.SignInResponse{User: protoUser(u)}), nil
}

func (s *Service) SignOut(ctx context.Context, _ *connect.Request[v1.SignOutRequest]) (*connect.Response[v1.SignOutResponse], error) {
	// End the session on the server first: clearing the cookie alone leaves it
	// valid for anyone who has a copy, and keeps tunnel grants alive.
	if r := access.Request(ctx); r != nil {
		if err := auth.EndSession(s.db, r); err != nil {
			return nil, access.SessionError(err)
		}
	}
	if w := access.ResponseWriter(ctx); w != nil {
		auth.ClearSession(w)
	}
	return connect.NewResponse(&v1.SignOutResponse{}), nil
}

func (s *Service) Me(ctx context.Context, _ *connect.Request[v1.MeRequest]) (*connect.Response[v1.MeResponse], error) {
	return connect.NewResponse(&v1.MeResponse{User: protoUser(access.User(ctx))}), nil
}

// ChangePassword sets the signed-in user's password. It needs the current one
// when there is one, and signs out every other session of theirs: whoever else
// knew the old password is gone.
func (s *Service) ChangePassword(ctx context.Context, req *connect.Request[v1.ChangePasswordRequest]) (*connect.Response[v1.User], error) {
	u := access.User(ctx)
	if u.PasswordHash != "" {
		ip := s.clientIP(access.Request(ctx))
		if err := s.held(u.Email, ip); err != nil {
			return nil, err
		}
		// Never Unauthenticated: that would read as the session having ended.
		if !auth.CheckPassword(u.PasswordHash, req.Msg.GetCurrent()) {
			s.limit.Fail(u.Email, ip)
			return nil, invalid("the current password is not right")
		}
		s.limit.OK(u.Email, ip)
	}
	keep := ""
	if cur := access.Session(ctx); cur != nil {
		keep = cur.ID
	}
	if err := s.setPassword(u.ID, req.Msg.GetPassword(), keep); err != nil {
		return nil, err
	}
	fresh, err := s.reload(u.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(protoUser(fresh)), nil
}

// setPassword stores a new password for a user, ends their sessions except
// keep, and voids any reset link that was out for them.
func (s *Service) setPassword(userID, password, keep string) error {
	if err := auth.ValidPassword(password); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.db.Model(&db.User{}).Where("id = ?", userID).Update("password_hash", hash).Error; err != nil {
		return err
	}
	if err := s.db.Where("user_id = ?", userID).Delete(&db.Invite{}).Error; err != nil {
		return err
	}
	return auth.EndUserSessions(s.db, userID, keep)
}
