package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"gorm.io/gorm"

	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

const cookieName = "silo_session"

var ErrAuth = errors.New("unauthorized")

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := scrypt.Key([]byte(pw), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(salt) + hex.EncodeToString(dk), nil
}

// MinPassword and MaxPassword bound a password's length in characters. The
// upper bound only keeps a huge body from being hashed.
const (
	MinPassword = 8
	MaxPassword = 256
)

// ValidPassword is nil when pw may be set as a password.
func ValidPassword(pw string) error {
	switch n := utf8.RuneCountInString(pw); {
	case n < MinPassword:
		return fmt.Errorf("password must be at least %d characters", MinPassword)
	case n > MaxPassword:
		return fmt.Errorf("password must be at most %d characters", MaxPassword)
	}
	return nil
}

// CheckPassword reports whether pw is the password hash was made from. A hash
// that cannot be one (a user with no password, or no such user) still costs a
// derivation, so the answer's timing does not say which emails have accounts.
func CheckPassword(hash, pw string) bool {
	salt, want, ok := splitHash(hash)
	if !ok {
		salt, want = make([]byte, 16), make([]byte, 32)
	}
	dk, err := scrypt.Key([]byte(pw), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, want) == 1 && ok
}

func splitHash(hash string) (salt, key []byte, ok bool) {
	if len(hash) < 64 {
		return nil, nil, false
	}
	salt, err := hex.DecodeString(hash[:32])
	if err != nil {
		return nil, nil, false
	}
	key, err = hex.DecodeString(hash[32:])
	if err != nil || len(key) != 32 {
		return nil, nil, false
	}
	return salt, key, true
}

// SessionTTL is how long a sign-in lasts.
const SessionTTL = 30 * 24 * time.Hour

// seenEvery is how stale a session's last-seen time may get before a request
// refreshes it; one write per request would be all churn.
const seenEvery = 5 * time.Minute

// SessionMeta is what a session remembers about the sign-in that made it.
type SessionMeta struct {
	UserAgent string
	IP        string
	// Method is password, oidc, or invite.
	Method string
	// Secure marks the cookie HTTPS-only.
	Secure bool
}

// NewSession signs userID in on w. The cookie carries a random token; the row
// is keyed by its hash.
func NewSession(gdb *gorm.DB, userID string, w http.ResponseWriter, m SessionMeta) (*db.Session, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	now := time.Now()
	s := db.Session{
		ID: ids.Hash(token), UserID: userID, ExpiresAt: now.Add(SessionTTL), CreatedAt: now, LastSeenAt: now,
		UserAgent: clip(m.UserAgent, 300), IP: clip(m.IP, 64), Method: m.Method,
	}
	if err := gdb.Create(&s).Error; err != nil {
		return nil, err
	}
	// Housekeeping, not part of signing in: neither failing undoes it.
	gdb.Where("user_id = ? AND expires_at < ?", userID, now).Delete(&db.Session{})
	gdb.Model(&db.User{}).Where("id = ?", userID).Update("last_sign_in_at", now)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.ExpiresAt,
	})
	return &s, nil
}

func clip(s string, n int) string {
	r := []rune(strings.ToValidUTF8(s, ""))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// NormalizeEmail is the form an email is stored and looked up in.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// FindUser is the user with this email, or nil when there is none.
func FindUser(gdb *gorm.DB, email string) (*db.User, error) {
	var u db.User
	res := gdb.Where("lower(email) = ?", NormalizeEmail(email)).Limit(1).Find(&u)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &u, nil
}

// EnsureBootstrap makes sure someone can administer Silo, from bootstrap.* in
// silo.yaml: with no admin who can sign in, the user with that email becomes
// one, and is created with that password when there is none. Once an admin
// exists it does nothing. It must not: people can change their email, and
// whoever took the bootstrap address after the first admin left it would
// otherwise be promoted at the next start.
func EnsureBootstrap(gdb *gorm.DB, email, pass string) error {
	if email == "" || pass == "" {
		return nil
	}
	var admins int64
	if err := gdb.Model(&db.User{}).Where("admin = ? AND disabled = ?", true, false).Count(&admins).Error; err != nil {
		return err
	}
	if admins > 0 {
		return nil
	}
	email = NormalizeEmail(email)
	u, err := FindUser(gdb, email)
	if err != nil {
		return err
	}
	if u != nil {
		return gdb.Model(u).Updates(map[string]any{"admin": true, "disabled": false}).Error
	}
	hash, err := HashPassword(pass)
	if err != nil {
		return err
	}
	return gdb.Create(&db.User{ID: ids.New(), Email: email, PasswordHash: hash, Admin: true, CreatedAt: time.Now()}).Error
}

func EnsureAdmin(gdb *gorm.DB) error {
	var n int64
	if err := gdb.Model(&db.User{}).Where("admin = ?", true).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var u db.User
	if err := gdb.Order("created_at").First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	return gdb.Model(&u).Update("admin", true).Error
}

// SessionFromRequest resolves the session cookie to the session and its user.
// Only a missing, unknown, or expired session, or one of a disabled user, is
// ErrAuth; a store failure is returned as-is so callers do not report a
// sign-out when Postgres is briefly unreachable.
func SessionFromRequest(gdb *gorm.DB, r *http.Request) (*db.Session, *db.User, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil, nil, ErrAuth
	}
	var s db.Session
	if err := gdb.Where("id = ?", ids.Hash(c.Value)).Limit(1).Find(&s).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if s.ID == "" || now.After(s.ExpiresAt) {
		return nil, nil, ErrAuth
	}
	var u db.User
	if err := gdb.Where("id = ?", s.UserID).Limit(1).Find(&u).Error; err != nil {
		return nil, nil, err
	}
	if u.ID == "" || u.Disabled {
		return nil, nil, ErrAuth
	}
	if now.Sub(s.LastSeenAt) > seenEvery {
		// Best effort: a failed touch must not fail the request.
		if gdb.Model(&db.Session{}).Where("id = ?", s.ID).Update("last_seen_at", now).Error == nil {
			s.LastSeenAt = now
		}
	}
	return &s, &u, nil
}

// UserFromRequest is the signed-in user of a request, or ErrAuth.
func UserFromRequest(gdb *gorm.DB, r *http.Request) (*db.User, error) {
	_, u, err := SessionFromRequest(gdb, r)
	return u, err
}

// EndSession signs the request's session out on the server: the row goes, so a
// copied cookie is worthless, and so does everything granted from it (a private
// tunnel's access). A request with no session has nothing to end.
func EndSession(gdb *gorm.DB, r *http.Request) error {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	return endSessions(gdb, []string{ids.Hash(c.Value)})
}

// EndSessionByID ends one of userID's sessions by its row id; false when they
// have no such session.
func EndSessionByID(gdb *gorm.DB, userID, id string) (bool, error) {
	var n int64
	if err := gdb.Model(&db.Session{}).Where("id = ? AND user_id = ?", id, userID).Count(&n).Error; err != nil || n == 0 {
		return false, err
	}
	return true, endSessions(gdb, []string{id})
}

// EndUserSessions ends every session of userID but the one with id keep ("" to
// keep none): after a password change, a disable, or "sign out everywhere".
func EndUserSessions(gdb *gorm.DB, userID, keep string) error {
	var sel []string
	if err := gdb.Model(&db.Session{}).Where("user_id = ? AND id <> ?", userID, keep).Pluck("id", &sel).Error; err != nil {
		return err
	}
	return endSessions(gdb, sel)
}

// endSessions deletes sessions by row id, and with each the tunnel grants made
// from it.
func endSessions(gdb *gorm.DB, sel []string) error {
	if len(sel) == 0 {
		return nil
	}
	return gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id IN ?", sel).Delete(&db.TunnelGrant{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", sel).Delete(&db.Session{}).Error
	})
}
