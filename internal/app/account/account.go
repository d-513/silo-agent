// Package account is who may sign in and how: passwords, sessions, two-factor
// codes, the single OIDC provider, the invite links that make new accounts,
// and the admin's list of users.
package account

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"gorm.io/gorm"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/access"
	"silo.agent/internal/auth"
	"silo.agent/internal/config"
	"silo.agent/internal/db"
)

// Host is what accounts need from the App around them: a user's Bots are the
// App's to stop and to delete.
type Host interface {
	// DropUserBots deletes every Bot the user owns, with its box and files.
	DropUserBots(ctx context.Context, userID string)
	// SuspendUser stops what the user's Bots are doing (runs, channels, boxes)
	// when the user is disabled; ResumeUser lets them work again.
	SuspendUser(ctx context.Context, userID string)
	ResumeUser(userID string)
}

// Service owns accounts: their RPCs, the OIDC routes and the sign-in limiter.
type Service struct {
	db    *gorm.DB
	cfg   func() config.Config
	host  Host
	limit *auth.Limiter
	oidc  oidcState
	// HTTP talks to the OIDC provider.
	HTTP *http.Client
}

func New(gdb *gorm.DB, cfg func() config.Config, host Host) *Service {
	return &Service{db: gdb, cfg: cfg, host: host, limit: auth.NewLimiter(), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Sign-in methods recorded on a session.
const (
	MethodPassword = "password"
	MethodOIDC     = "oidc"
	MethodInvite   = "invite"
)

func protoUser(u *db.User) *v1.User {
	out := &v1.User{
		Id: u.ID, Email: u.Email, Admin: u.Admin, Disabled: u.Disabled,
		HasPassword: u.PasswordHash != "", Totp: u.TOTPSecret != "", Oidc: u.OIDCSubject != "",
		CreatedAt: stamp(u.CreatedAt),
	}
	if u.LastSignInAt != nil {
		out.LastSignInAt = stamp(*u.LastSignInAt)
	}
	return out
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// clientIP is the address the request came from, through the operator's
// trusted proxies.
func (s *Service) clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	return auth.ClientIP(r, s.cfg().TrustedProxies())
}

// signIn starts a session for u on the response of the request in ctx.
func (s *Service) signIn(w http.ResponseWriter, r *http.Request, u *db.User, method string) error {
	if w == nil || r == nil {
		return connect.NewError(connect.CodeInternal, errors.New("no response to sign in on"))
	}
	_, err := auth.NewSession(s.db, u.ID, w, auth.SessionMeta{
		UserAgent: r.UserAgent(), IP: s.clientIP(r), Method: method,
		Secure: access.SecureCookies(s.cfg().PublicURL, r),
	})
	return err
}

// held is ResourceExhausted while account at addr has to wait before another
// guess, else nil.
func (s *Service) held(account, addr string) error {
	w := s.limit.Wait(account, addr)
	if w <= 0 {
		return nil
	}
	return connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("too many attempts; try again in %s", waitWords(w)))
}

func waitWords(d time.Duration) string {
	if d < time.Minute {
		n := int((d + time.Second - 1) / time.Second)
		if n == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", n)
	}
	n := int((d + time.Minute - 1) / time.Minute)
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

// reload reads the user's row again: the one on the context is from before
// this request changed it.
func (s *Service) reload(id string) (*db.User, error) {
	var u db.User
	res := s.db.Where("id = ?", id).Limit(1).Find(&u)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return &u, nil
}

func invalid(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
}

func refused(format string, args ...any) error {
	return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(format, args...))
}
