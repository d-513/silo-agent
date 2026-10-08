package account

import (
	"context"
	"crypto/rand"
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

// totpIssuer is the name an authenticator app files the account under.
const totpIssuer = "Silo"

const recoveryCodes = 10

// StartTOTP begins enrolling an authenticator: it makes a secret and shows it,
// but two-factor is not on until ConfirmTOTP proves the app has it.
func (s *Service) StartTOTP(ctx context.Context, req *connect.Request[v1.StartTOTPRequest]) (*connect.Response[v1.StartTOTPResponse], error) {
	u := access.User(ctx)
	if u.PasswordHash == "" {
		return nil, refused("two-factor protects password sign-in; set a password first")
	}
	if u.TOTPSecret != "" {
		return nil, refused("two-factor is already on")
	}
	ip := s.clientIP(access.Request(ctx))
	if err := s.held(u.Email, ip); err != nil {
		return nil, err
	}
	if !auth.CheckPassword(u.PasswordHash, req.Msg.GetPassword()) {
		s.limit.Fail(u.Email, ip)
		return nil, invalid("the password is not right")
	}
	s.limit.OK(u.Email, ip)
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return nil, err
	}
	if err := s.db.Model(&db.User{}).Where("id = ?", u.ID).Update("totp_pending", secret).Error; err != nil {
		return nil, err
	}
	link := auth.TOTPURL(totpIssuer, u.Email, secret)
	qr, err := auth.QRDataURL(link)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.StartTOTPResponse{Secret: secret, Url: link, Qr: qr}), nil
}

// ConfirmTOTP turns two-factor on once a code from the new secret checks out,
// and hands back the recovery codes: the only time they are shown.
func (s *Service) ConfirmTOTP(ctx context.Context, req *connect.Request[v1.ConfirmTOTPRequest]) (*connect.Response[v1.RecoveryCodes], error) {
	u := access.User(ctx)
	if u.TOTPSecret != "" {
		return nil, refused("two-factor is already on")
	}
	if u.TOTPPending == "" {
		return nil, refused("start two-factor setup first")
	}
	ip := s.clientIP(access.Request(ctx))
	if err := s.held(u.Email, ip); err != nil {
		return nil, err
	}
	step, ok := auth.CheckTOTP(u.TOTPPending, req.Msg.GetCode(), time.Now(), 0)
	if !ok {
		s.limit.Fail(u.Email, ip)
		return nil, invalid("that code is not right; check the time on your phone and try the next one")
	}
	s.limit.OK(u.Email, ip)
	var codes []string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&db.User{}).Where("id = ? AND totp_pending = ?", u.ID, u.TOTPPending).
			Updates(map[string]any{"totp_secret": u.TOTPPending, "totp_pending": "", "totp_step": step})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return refused("two-factor setup was restarted; scan the new code")
		}
		var err error
		codes, err = newRecoveryCodes(tx, u.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.codesResponse(u.ID, codes)
}

// DisableTOTP turns two-factor off, for a code from the app or a recovery code.
func (s *Service) DisableTOTP(ctx context.Context, req *connect.Request[v1.DisableTOTPRequest]) (*connect.Response[v1.User], error) {
	u := access.User(ctx)
	if u.TOTPSecret == "" {
		return nil, refused("two-factor is not on")
	}
	if err := s.proveCode(ctx, u, req.Msg.GetCode()); err != nil {
		return nil, err
	}
	if err := s.clearTOTP(u.ID); err != nil {
		return nil, err
	}
	fresh, err := s.reload(u.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(protoUser(fresh)), nil
}

// NewRecoveryCodes replaces the user's recovery codes; the old ones stop working.
func (s *Service) NewRecoveryCodes(ctx context.Context, req *connect.Request[v1.NewRecoveryCodesRequest]) (*connect.Response[v1.RecoveryCodes], error) {
	u := access.User(ctx)
	if u.TOTPSecret == "" {
		return nil, refused("two-factor is not on")
	}
	if err := s.proveCode(ctx, u, req.Msg.GetCode()); err != nil {
		return nil, err
	}
	var codes []string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		codes, err = newRecoveryCodes(tx, u.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.codesResponse(u.ID, codes)
}

func (s *Service) codesResponse(userID string, codes []string) (*connect.Response[v1.RecoveryCodes], error) {
	fresh, err := s.reload(userID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.RecoveryCodes{Codes: codes, User: protoUser(fresh)}), nil
}

// proveCode is spendCode for a signed-in user, throttled like a sign-in.
func (s *Service) proveCode(ctx context.Context, u *db.User, code string) error {
	ip := s.clientIP(access.Request(ctx))
	if err := s.held(u.Email, ip); err != nil {
		return err
	}
	if !s.spendCode(u, code) {
		s.limit.Fail(u.Email, ip)
		return invalid("that code is not right")
	}
	s.limit.OK(u.Email, ip)
	return nil
}

// spendCode reports whether code is a good second factor for u, and uses it
// up: an authenticator code's time step is recorded, a recovery code is
// crossed off. Each update is conditional, so two requests racing with the
// same code cannot both win.
func (s *Service) spendCode(u *db.User, code string) bool {
	if u.TOTPSecret == "" {
		return false
	}
	if step, ok := auth.CheckTOTP(u.TOTPSecret, code, time.Now(), u.TOTPStep); ok {
		res := s.db.Model(&db.User{}).Where("id = ? AND totp_step < ?", u.ID, step).Update("totp_step", step)
		return res.Error == nil && res.RowsAffected == 1
	}
	norm := normalRecovery(code)
	if norm == "" {
		return false
	}
	res := s.db.Model(&db.RecoveryCode{}).Where("id = ? AND user_id = ? AND used_at IS NULL", recoveryID(u.ID, norm), u.ID).Update("used_at", time.Now())
	return res.Error == nil && res.RowsAffected == 1
}

// clearTOTP turns a user's two-factor off and forgets their recovery codes.
func (s *Service) clearTOTP(userID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.User{}).Where("id = ?", userID).
			Updates(map[string]any{"totp_secret": "", "totp_pending": "", "totp_step": 0}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&db.RecoveryCode{}).Error
	})
}

// recoveryAlphabet leaves out the characters people misread (0/o, 1/l/i).
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// newRecoveryCodes replaces userID's recovery codes and returns the new ones,
// written the way they are shown: xxxxx-xxxxx.
func newRecoveryCodes(tx *gorm.DB, userID string) ([]string, error) {
	if err := tx.Where("user_id = ?", userID).Delete(&db.RecoveryCode{}).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, recoveryCodes)
	rows := make([]db.RecoveryCode, 0, recoveryCodes)
	for len(out) < recoveryCodes {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		code := make([]byte, len(raw))
		for i, b := range raw {
			code[i] = recoveryAlphabet[int(b)%len(recoveryAlphabet)]
		}
		out = append(out, string(code[:5])+"-"+string(code[5:]))
		rows = append(rows, db.RecoveryCode{ID: recoveryID(userID, string(code)), UserID: userID})
	}
	return out, tx.Create(&rows).Error
}

// normalRecovery strips what people add when typing a recovery code back; ""
// when what is left cannot be one.
func normalRecovery(code string) string {
	code = strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
	if len(code) != 10 {
		return ""
	}
	return code
}

func recoveryID(userID, code string) string { return ids.Hash(userID + ":" + code) }
