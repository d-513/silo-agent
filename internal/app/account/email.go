package account

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

// ChangeEmail sets the address the signed-in user signs in with. Nothing
// confirms it (Silo sends no mail), so it is marked as their own word: see
// setEmail.
func (s *Service) ChangeEmail(ctx context.Context, req *connect.Request[v1.ChangeEmailRequest]) (*connect.Response[v1.User], error) {
	u := access.User(ctx)
	if err := s.setEmail(u, req.Msg.GetEmail(), true); err != nil {
		return nil, err
	}
	fresh, err := s.reload(u.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(protoUser(fresh)), nil
}

// setEmail changes a user's email. selfSet says who vouches for it: an address
// the user typed themselves is never one single sign-on matches an account by
// (resolve), or anyone could take a colleague's address and be handed their
// identity, and their admin role, at the colleague's next sign-in. An address
// an admin sets is trusted again.
func (s *Service) setEmail(u *db.User, raw string, selfSet bool) error {
	email := auth.NormalizeEmail(raw)
	if !validEmail(email) {
		return invalid("enter an email address")
	}
	if email == u.Email && (selfSet || !u.EmailSelfSet) {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var taken int64
		if err := tx.Model(&db.User{}).Where("lower(email) = ? AND id <> ?", email, u.ID).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return connect.NewError(connect.CodeAlreadyExists, errors.New("another account already uses this email"))
		}
		if err := tx.Model(&db.User{}).Where("id = ?", u.ID).Updates(map[string]any{"email": email, "email_self_set": selfSet}).Error; err != nil {
			return err
		}
		// A reset link that is out for them stays theirs, under the new name.
		return tx.Model(&db.Invite{}).Where("user_id = ?", u.ID).Update("email", email).Error
	})
}
