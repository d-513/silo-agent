package siloos

import (
	"context"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
)

// SiloOS shows the first admin's generated password on the machine's console
// (siloos-init), at every boot, for as long as it is the way in: whoever sets
// a machine up may come back to it days later. It stops being shown when it
// stops being true, and the control plane is the one who knows: that password
// is its own bootstrap.password, so it can tell whether it still signs the
// bootstrap admin in. When it does not (the admin changed it, changed their
// email, or is gone), siloosd is told once, and takes it off the console now
// and for every later boot.

// CheckFirstPassword tells siloosd the first password is off the console
// once it no longer signs the bootstrap admin in. It does nothing off SiloOS,
// with no bootstrap password, while that password still works, and after
// siloosd took the word. Called at start and after an account changes.
func (s *Service) CheckFirstPassword(ctx context.Context) {
	d := s.daemon()
	if d == nil {
		return
	}
	s.checking.Lock()
	defer s.checking.Unlock()
	s.mu.Lock()
	done := s.retired
	s.mu.Unlock()
	if done || s.firstPasswordWorks() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := d.RetireFirstPassword(ctx, connect.NewRequest(&v1.RetireFirstPasswordRequest{})); err != nil {
		// Asked again at the next account change or start.
		log.Printf("siloos: the first password is still on the console: %v", err)
		return
	}
	s.mu.Lock()
	s.retired = true
	s.mu.Unlock()
	log.Printf("siloos: the first password no longer signs anyone in; it is off the console")
}

// firstPasswordWorks reports whether bootstrap.password still signs the
// bootstrap admin in. With no such password configured there is nothing on
// the console to take off, which counts as working: siloosd is not told.
func (s *Service) firstPasswordWorks() bool {
	b := s.cfg().Bootstrap
	email, pass := auth.NormalizeEmail(b.Email), b.Password
	if email == "" || strings.TrimSpace(pass) == "" {
		return true
	}
	var u db.User
	if err := s.db.Where("lower(email) = ?", email).Limit(1).Find(&u).Error; err != nil {
		// Not known is not changed: the banner stays until it is.
		return true
	}
	return u.ID != "" && !u.Disabled && auth.CheckPassword(u.PasswordHash, pass)
}
