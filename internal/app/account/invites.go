package account

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

const (
	// inviteTTL is how long a link to a new account is good for; a link to a
	// new password for an existing account is good for resetTTL.
	inviteTTL = 7 * 24 * time.Hour
	resetTTL  = 24 * time.Hour
	// invitesKept is how long a used or expired invite row stays before the
	// next CreateInvite sweeps it.
	invitesKept = 30 * 24 * time.Hour
)

var errInviteGone = connect.NewError(connect.CodeNotFound, errors.New("this link is no longer valid; ask an admin for a new one"))

// CreateInvite makes a single-use link: to a new account for an email, or,
// given a user, to a new password for them. The link is returned once and only
// its hash is kept, so a lost link is replaced, not looked up.
func (s *Service) CreateInvite(ctx context.Context, req *connect.Request[v1.CreateInviteRequest]) (*connect.Response[v1.Invite], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	now := time.Now()
	inv := db.Invite{CreatedBy: access.User(ctx).ID, CreatedAt: now}
	if id := req.Msg.GetUserId(); id != "" {
		u, err := s.reload(id)
		if err != nil {
			return nil, err
		}
		inv.UserID, inv.Email, inv.ExpiresAt = u.ID, u.Email, now.Add(resetTTL)
	} else {
		email := auth.NormalizeEmail(req.Msg.GetEmail())
		if !validEmail(email) {
			return nil, invalid("enter an email address")
		}
		if u, err := auth.FindUser(s.db, email); err != nil {
			return nil, err
		} else if u != nil {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("a user with this email already exists"))
		}
		inv.Email, inv.Admin, inv.ExpiresAt = email, req.Msg.GetAdmin(), now.Add(inviteTTL)
	}
	token := ids.Token()
	inv.ID = ids.Hash(token)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// A new link replaces the one that was out for the same person.
		if err := tx.Where("email = ? AND user_id = ? AND used_at IS NULL", inv.Email, inv.UserID).Delete(&db.Invite{}).Error; err != nil {
			return err
		}
		if err := tx.Where("expires_at < ? OR used_at < ?", now.Add(-invitesKept), now.Add(-invitesKept)).Delete(&db.Invite{}).Error; err != nil {
			return err
		}
		return tx.Create(&inv).Error
	})
	if err != nil {
		return nil, err
	}
	out := protoInvite(&inv)
	out.Url = access.PublicURL(s.cfg().PublicURL, access.Request(ctx)) + "/invite/" + token
	return connect.NewResponse(out), nil
}

func (s *Service) DeleteInvite(ctx context.Context, req *connect.Request[v1.DeleteInviteRequest]) (*connect.Response[v1.DeleteInviteResponse], error) {
	if err := access.RequireAdmin(ctx); err != nil {
		return nil, err
	}
	res := s.db.Where("id = ?", req.Msg.GetId()).Delete(&db.Invite{})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invite not found"))
	}
	return connect.NewResponse(&v1.DeleteInviteResponse{}), nil
}

// GetInvite tells an invite link's page whose it is, before anyone is signed in.
func (s *Service) GetInvite(ctx context.Context, req *connect.Request[v1.GetInviteRequest]) (*connect.Response[v1.InviteInfo], error) {
	inv, err := s.openInvite(ctx, req.Msg.GetToken())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.InviteInfo{Email: inv.Email, PasswordReset: inv.UserID != ""}), nil
}

// AcceptInvite spends an invite link on a password: a new account signed in
// on the spot, or an existing one's new password (they then sign in with it,
// and with their second factor if they have one).
func (s *Service) AcceptInvite(ctx context.Context, req *connect.Request[v1.AcceptInviteRequest]) (*connect.Response[v1.AcceptInviteResponse], error) {
	inv, err := s.openInvite(ctx, req.Msg.GetToken())
	if err != nil {
		return nil, err
	}
	password := req.Msg.GetPassword()
	if err := auth.ValidPassword(password); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	var u db.User
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// Only the request that marks the link used may go on.
		res := tx.Model(&db.Invite{}).Where("id = ? AND used_at IS NULL", inv.ID).Update("used_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errInviteGone
		}
		if inv.UserID != "" {
			res := tx.Model(&db.User{}).Where("id = ?", inv.UserID).Update("password_hash", hash)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errInviteGone
			}
			return tx.First(&u, "id = ?", inv.UserID).Error
		}
		var taken int64
		if err := tx.Model(&db.User{}).Where("lower(email) = ?", inv.Email).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return connect.NewError(connect.CodeAlreadyExists, errors.New("a user with this email already exists"))
		}
		u = db.User{ID: ids.New(), Email: inv.Email, PasswordHash: hash, Admin: inv.Admin, CreatedAt: time.Now()}
		return tx.Create(&u).Error
	})
	if err != nil {
		return nil, err
	}
	out := &v1.AcceptInviteResponse{}
	if inv.UserID != "" {
		// Whoever was signed in with the forgotten password is out.
		if err := auth.EndUserSessions(s.db, u.ID, ""); err != nil {
			return nil, err
		}
	} else {
		if err := s.signIn(access.ResponseWriter(ctx), access.Request(ctx), &u, MethodInvite); err != nil {
			return nil, err
		}
		out.SignedIn = true
	}
	out.User = protoUser(&u)
	return connect.NewResponse(out), nil
}

// openInvite finds the live invite a link's token names. Tokens are guessable
// in principle, so misses count against the address like wrong passwords.
func (s *Service) openInvite(ctx context.Context, token string) (*db.Invite, error) {
	ip := s.clientIP(access.Request(ctx))
	if err := s.held("", ip); err != nil {
		return nil, err
	}
	var inv db.Invite
	if token = strings.TrimSpace(token); token != "" {
		if err := s.db.Where("id = ? AND used_at IS NULL AND expires_at > ?", ids.Hash(token), time.Now()).Limit(1).Find(&inv).Error; err != nil {
			return nil, err
		}
	}
	if inv.ID == "" {
		s.limit.Fail("", ip)
		return nil, errInviteGone
	}
	return &inv, nil
}

func protoInvite(i *db.Invite) *v1.Invite {
	return &v1.Invite{Id: i.ID, Email: i.Email, Admin: i.Admin, UserId: i.UserID, CreatedAt: stamp(i.CreatedAt), ExpiresAt: stamp(i.ExpiresAt)}
}

// validEmail is a light check: one bare address, with a dot in its domain.
func validEmail(email string) bool {
	if len(email) > 254 || strings.ContainsAny(email, " \t\r\n<>") {
		return false
	}
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email {
		return false
	}
	_, domain, _ := strings.Cut(email, "@")
	return strings.Contains(domain, ".") && !strings.HasSuffix(domain, ".")
}
