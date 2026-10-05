package channels

import "sync"

// Seen is a bounded set of message ids an adapter has already handled, so a
// platform that redelivers after a reconnect does not start a second run. When
// it fills it forgets everything at once: ids are only ever replayed close
// behind their first delivery, so the cheap reset is enough.
type Seen struct {
	mu  sync.Mutex
	max int
	ids map[string]struct{}
}

// NewSeen makes a Seen that holds up to max ids.
func NewSeen(max int) *Seen {
	return &Seen{max: max, ids: map[string]struct{}{}}
}

// Add records id and reports whether it was already there.
func (s *Seen) Add(id string) (dup bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; ok {
		return true
	}
	if len(s.ids) >= s.max {
		s.ids = map[string]struct{}{}
	}
	s.ids[id] = struct{}{}
	return false
}

// Has reports whether id was recorded, without recording it.
func (s *Seen) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}
