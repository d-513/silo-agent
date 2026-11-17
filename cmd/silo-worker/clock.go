package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// cmdTimeout is how long one command may work on the machine. The time a
// command spends waiting on the control plane (a tool call or a secret its
// code asked for, which may sit on the owner's approval) does not count: the
// CP bounds that itself (a run's max_duration, Stop), and an approval must
// not be cut off by a clock that was meant for runaway code.
const cmdTimeout = 5 * time.Minute

// errTimedOut is the cause a command's context is canceled with when its
// time is up; the output says so (context.Cause), where a Stop says canceled.
var errTimedOut = fmt.Errorf("timed out after %v of work on the machine", cmdTimeout)

// job is one running command: its cancel and the clock that stops it.
type job struct {
	runID  string
	cancel context.CancelCauseFunc

	mu    sync.Mutex
	left  time.Duration // what the clock had when it last started
	since time.Time     // when it last started
	timer *time.Timer   // nil while held or ended
	holds int           // calls to the CP the command is waiting on
	ended bool
}

// newJob starts a command's clock: its context ends with errTimedOut once it
// has had budget of time not spent waiting on the CP.
func newJob(parent context.Context, runID string, budget time.Duration) (context.Context, *job) {
	ctx, cancel := context.WithCancelCause(parent)
	j := &job{runID: runID, cancel: cancel, left: budget, since: time.Now()}
	j.timer = time.AfterFunc(budget, j.expire)
	return ctx, j
}

func (j *job) expire() { j.cancel(errTimedOut) }

// hold stops the clock while the command waits on the CP; release starts it
// again with what was left. Holds nest: calls from threads run at once.
func (j *job) hold() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ended {
		return
	}
	if j.holds == 0 && j.timer != nil {
		j.timer.Stop()
		j.timer = nil
		j.left -= time.Since(j.since)
	}
	j.holds++
}

func (j *job) release() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ended || j.holds == 0 {
		return
	}
	if j.holds--; j.holds == 0 {
		j.since = time.Now()
		j.timer = time.AfterFunc(max(j.left, 0), j.expire)
	}
}

// end stops the clock for good and releases the context.
func (j *job) end() {
	j.mu.Lock()
	j.ended = true
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	j.mu.Unlock()
	j.cancel(nil)
}

// waitOnCP holds the clock of every command of the run a call from code
// belongs to, until the returned func is called. A call that names no run
// (a background process, the console) holds nothing.
func (w *worker) waitOnCP(runID string) func() {
	if runID == "" {
		return func() {}
	}
	w.mu.Lock()
	var held []*job
	for _, j := range w.pending {
		if j.runID == runID {
			j.hold()
			held = append(held, j)
		}
	}
	w.mu.Unlock()
	return func() {
		for _, j := range held {
			j.release()
		}
	}
}
