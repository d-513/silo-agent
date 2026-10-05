package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

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

func CheckPassword(hash, pw string) bool {
	if len(hash) < 64 {
		return false
	}
	salt, err := hex.DecodeString(hash[:32])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(hash[32:])
	if err != nil {
		return false
	}
	dk, err := scrypt.Key([]byte(pw), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return false
	}
	if len(dk) != len(want) {
		return false
	}
	var v byte
	for i := range dk {
		v |= dk[i] ^ want[i]
	}
	return v == 0
}

func NewSession(gdb *gorm.DB, userID string, w http.ResponseWriter) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	id := hex.EncodeToString(b)
	s := db.Session{ID: id, UserID: userID, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
	if err := gdb.Create(&s).Error; err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.ExpiresAt,
	})
	return nil
}

func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func EnsureBootstrap(gdb *gorm.DB, email, pass string) error {
	if email == "" || pass == "" {
		return nil
	}
	var u db.User
	// Find leaves an empty result as a nil error, so a first bootstrap does not
	// emit an avoidable record-not-found database log.
	res := gdb.Where("email = ?", email).Limit(1).Find(&u)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		if !u.Admin {
			u.Admin = true
			return gdb.Save(&u).Error
		}
		return nil
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
	u.Admin = true
	return gdb.Save(&u).Error
}

// SessionFromRequest resolves the session cookie to the session and its user.
// Only a missing, unknown, or expired session is ErrAuth; a store failure is
// returned as-is so callers do not report a sign-out when Postgres is briefly
// unreachable.
func SessionFromRequest(gdb *gorm.DB, r *http.Request) (*db.Session, *db.User, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil, nil, ErrAuth
	}
	var s db.Session
	if err := gdb.Where("id = ?", c.Value).Limit(1).Find(&s).Error; err != nil {
		return nil, nil, err
	}
	if s.ID == "" || time.Now().After(s.ExpiresAt) {
		return nil, nil, ErrAuth
	}
	var u db.User
	if err := gdb.Where("id = ?", s.UserID).Limit(1).Find(&u).Error; err != nil {
		return nil, nil, err
	}
	if u.ID == "" {
		return nil, nil, ErrAuth
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
	return gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", c.Value).Delete(&db.TunnelGrant{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", c.Value).Delete(&db.Session{}).Error
	})
}
