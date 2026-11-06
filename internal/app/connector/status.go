package connector

import (
	"sync"

	"silo.agent/internal/db"
)

// A grant's status (auth_status, status_detail, last_error) has several
// writers: a refresh job that may run for minutes, an edit or a sign-out that
// starts over, a sign-in that failed. Each refresh job carries the generation
// of the grant it started under; whatever sets a newer state moves the
// generation on, and a job whose generation has moved on writes nothing. So an
// old job finishing late never puts "authorized" over a newer "needs_auth".
// Every write is a column update of the row as it stands: a grant revoked
// while its job ran stays gone.
type statusGens struct {
	mu  sync.Mutex
	gen map[string]uint64
}

// newJob starts a refresh generation for a grant and returns it.
func (s *Service) newJob(grantID string) uint64 {
	s.status.mu.Lock()
	defer s.status.mu.Unlock()
	return s.bumpLocked(grantID)
}

func (s *Service) bumpLocked(grantID string) uint64 {
	if s.status.gen == nil {
		s.status.gen = map[string]uint64{}
	}
	s.status.gen[grantID]++
	return s.status.gen[grantID]
}

// setStatus is a refresh job's write: it lands only while gen is still the
// grant's and the grant still exists, and says whether it did.
func (s *Service) setStatus(grantID string, gen uint64, cols map[string]any) bool {
	s.status.mu.Lock()
	defer s.status.mu.Unlock()
	if s.status.gen[grantID] != gen {
		return false
	}
	return s.db.Model(&db.BotConnector{}).Where("id = ?", grantID).Updates(cols).RowsAffected > 0
}

// resetStatus sets a grant's status from outside any job: whatever job is
// running for it no longer has the last word.
func (s *Service) resetStatus(grantID string, cols map[string]any) {
	s.status.mu.Lock()
	defer s.status.mu.Unlock()
	s.bumpLocked(grantID)
	s.db.Model(&db.BotConnector{}).Where("id = ?", grantID).Updates(cols)
}

// forgetStatus drops a revoked grant's generation: a job still running for
// it finds no match and writes nothing.
func (s *Service) forgetStatus(grantID string) {
	s.status.mu.Lock()
	defer s.status.mu.Unlock()
	delete(s.status.gen, grantID)
}
