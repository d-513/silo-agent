package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"

	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
)

// A Bot's token is readable by code in its own box (the worker runs as the
// same user, who has sudo), so code that was talked into it can send the
// token out. What keeps the token from being worth much outside the box: the
// worker API answers it only while that Bot's box is running. A stopped or
// vanished box's token opens nothing, and a box that is removed hands its
// next one a new token (destroyBot).

// errBoxDown refuses a worker call whose Bot has no running box. It is not
// Unauthenticated: the worker waits long after that, and a box that is just
// starting must get in at its next try.
var errBoxDown = errors.New("this Bot's machine is not running")

const (
	// boxUpFor is how long a box seen running is believed without asking the
	// engine again. A stop through Silo forgets it at once (haltBot); one out
	// of band is noticed within this.
	boxUpFor = 5 * time.Second
	// boxUpGrace is how long that belief holds when the engine cannot be
	// asked (an error that is not "not found"), so a hiccup on the engine
	// socket does not fail the tools of a Bot that is running.
	boxUpGrace = 2 * time.Minute
)

type boxSeen struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func (s *boxSeen) at(botID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.seen[botID]
	return t, ok
}

func (s *boxSeen) mark(botID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]time.Time{}
	}
	s.seen[botID] = time.Now()
}

func (s *boxSeen) forget(botID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, botID)
}

// boxUp reports whether the Bot's box is running, asking the engine at most
// every boxUpFor.
func (a *App) boxUp(ctx context.Context, b *db.Bot) bool {
	last, known := a.boxes.at(b.ID)
	if known && time.Since(last) < boxUpFor {
		return true
	}
	st, err := a.inspectBot(ctx, b)
	switch {
	case err == nil && st.Running:
		a.boxes.mark(b.ID)
		return true
	case err != nil && !dockerx.IsNotFound(err) && known && time.Since(last) < boxUpGrace:
		return true
	}
	a.boxes.forget(b.ID)
	return false
}

// workerBot is the Bot a worker call's token belongs to, refused while its box
// is not running.
func (a *App) workerBot(ctx context.Context, header string) (*db.Bot, string, error) {
	bot, tok, err := a.botFromToken(header)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !a.boxUp(ctx, bot) {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, errBoxDown)
	}
	return bot, tok, nil
}
