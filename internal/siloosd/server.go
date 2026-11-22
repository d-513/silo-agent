// Package siloosd is the SiloOS host daemon: the one way Silo changes the
// machine it runs on (proto/silo/v1/siloos.proto). It runs as root, listens
// on a unix socket only the control plane's container has (Listen), and
// answers only the uids it was given.
//
// Every operation is a fixed command with validated arguments, run without a
// shell (Runner). There is no generic command, file write or package install,
// and there must never be one: the control plane faces the internet and runs
// tools models choose, so whatever this package offers is what a compromised
// control plane gets on the host.
package siloosd

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
)

// APIVersion moves when a call changes meaning.
const APIVersion = 1

// Config is what a Server is made with.
type Config struct {
	// Run executes the system's commands; nil is the real one.
	Run Runner
	// StateDir keeps the update policy and a pending network change.
	StateDir string
	// Root prefixes the files read for system facts (/proc, os-release), for
	// tests; empty is /.
	Root string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// After schedules fn after d (power actions); nil is time.AfterFunc.
	After func(d time.Duration, fn func())
}

// Server implements the SiloOSD service.
type Server struct {
	silov1connect.UnimplementedSiloOSDHandler

	run   Runner
	state string
	root  string
	now   func() time.Time
	after func(time.Duration, func())

	// mu guards the network change waiting for confirmation and the update
	// job and policy.
	mu      sync.Mutex
	pending *pendingNet
	job     *v1.UpdateJob
	policy  policyFile
	// jobs is held by the one update job running.
	jobs sync.Mutex
}

// New makes a Server and loads its state.
func New(c Config) *Server {
	s := &Server{run: c.Run, state: c.StateDir, root: c.Root, now: c.Now, after: c.After}
	if s.run == nil {
		s.run = Exec{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.after == nil {
		s.after = func(d time.Duration, fn func()) { time.AfterFunc(d, fn) }
	}
	if s.root == "" {
		s.root = "/"
	}
	s.policy = s.loadPolicy()
	s.pending = s.loadPending()
	return s
}

// Run keeps the update policy until ctx ends.
func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.scheduled(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) file(rel string) string { return filepath.Join(s.root, rel) }

func (s *Server) statePath(name string) string { return filepath.Join(s.state, name) }

func (s *Server) readFile(rel string) string {
	b, err := os.ReadFile(s.file(rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// audit writes what was changed to the journal, under the caller's uid.
func audit(ctx context.Context, format string, args ...any) {
	who := "?"
	if p, ok := PeerFrom(ctx); ok {
		who = p.String()
	}
	log.Printf("siloosd: [uid %s] "+format, append([]any{who}, args...)...)
}

func invalid(msg string) error {
	return connect.NewError(connect.CodeInvalidArgument, errorString(msg))
}

func refused(msg string) error {
	return connect.NewError(connect.CodeFailedPrecondition, errorString(msg))
}

type errorString string

func (e errorString) Error() string { return string(e) }
