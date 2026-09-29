// Package drivetest is a fake rclone for tests: processes "mount" at once,
// listings answer from a map, and tests can crash a mount or inspect argv.
package drivetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"silo.agent/internal/drivehost"
)

// Runner implements drivehost.Runner in memory.
type Runner struct {
	mu      sync.Mutex
	mounted map[string]bool
	starts  []Start
	procs   map[string]*proc
	// Dirs answers lsjson: remote path ("files:bucket") -> folder names.
	Dirs map[string][]string
	// ListErr, when set, fails every listing with this stderr.
	ListErr string
}

// Start is one recorded `rclone mount`.
type Start struct {
	Args []string
	Env  []string
}

func New() *Runner {
	return &Runner{mounted: map[string]bool{}, procs: map[string]*proc{}, Dirs: map[string][]string{}}
}

type proc struct {
	r    *Runner
	mp   string
	done chan struct{}
	once sync.Once
	tail string
}

func (p *proc) Wait() error  { <-p.done; return nil }
func (p *proc) Stop()        { p.exit("") }
func (p *proc) Tail() string { p.r.mu.Lock(); defer p.r.mu.Unlock(); return p.tail }

func (p *proc) exit(stderr string) {
	p.once.Do(func() {
		p.r.mu.Lock()
		p.tail = stderr
		delete(p.r.mounted, p.mp)
		p.r.mu.Unlock()
		close(p.done)
	})
}

func (r *Runner) Start(args, env []string) (drivehost.Proc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mp := args[2]
	p := &proc{r: r, mp: mp, done: make(chan struct{})}
	r.starts = append(r.starts, Start{Args: slices.Clone(args), Env: slices.Clone(env)})
	r.procs[mp] = p
	r.mounted[mp] = true
	return p, nil
}

func (r *Runner) Output(_ context.Context, args, _ []string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ListErr != "" {
		return nil, errors.New(r.ListErr)
	}
	target := args[len(args)-1]
	type item struct {
		Path, Name string
		IsDir      bool
	}
	var out []item
	for _, n := range r.Dirs[target] {
		out = append(out, item{Path: n, Name: n, IsDir: true})
	}
	b, _ := json.Marshal(out)
	return b, nil
}

func (r *Runner) Mounted(p string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mounted[p]
}

func (r *Runner) Unmount(p string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mounted, p)
	return nil
}

// Starts is every mount started so far.
func (r *Runner) Starts() []Start {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.starts)
}

// MountedDirs lists the mount points that are up, by their last path part.
func (r *Runner) MountedDirs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for p, up := range r.mounted {
		if up {
			out = append(out, p[strings.LastIndex(p, "/")+1:])
		}
	}
	slices.Sort(out)
	return out
}

// Crash ends the mount at the dir as rclone would on a fatal error.
func (r *Runner) Crash(dir, stderr string) {
	r.mu.Lock()
	var p *proc
	for mp, pr := range r.procs {
		if strings.HasSuffix(mp, "/"+dir) {
			p = pr
		}
	}
	r.mu.Unlock()
	if p != nil {
		p.exit(stderr)
	}
}
