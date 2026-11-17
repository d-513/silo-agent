package main

import (
	"context"
	"testing"
	"time"
)

// A command's clock runs out on its own work, not on time it spent waiting
// on the control plane (an approval behind a tool call from code).
func TestWaitingOnTheCPDoesNotCount(t *testing.T) {
	w := &worker{pending: map[string]*job{}}
	ctx, j := newJob(context.Background(), "run-1", 150*time.Millisecond)
	defer j.end()
	w.pending["a"] = j
	other, o := newJob(context.Background(), "run-2", 150*time.Millisecond)
	defer o.end()
	w.pending["b"] = o

	release := w.waitOnCP("run-1")
	time.Sleep(300 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("the clock ran while the command waited on the CP")
	}
	if context.Cause(other) != errTimedOut {
		t.Fatalf("another run's command was held too: %v", context.Cause(other))
	}
	release()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the clock did not start again")
	}
	if context.Cause(ctx) != errTimedOut {
		t.Fatalf("a timeout reads as %v", context.Cause(ctx))
	}
}

// Holds nest (threads call at once), and a call that names no run holds
// nothing.
func TestHoldsNest(t *testing.T) {
	w := &worker{pending: map[string]*job{}}
	ctx, j := newJob(context.Background(), "run-1", 100*time.Millisecond)
	defer j.end()
	w.pending["a"] = j
	w.waitOnCP("")()
	r1, r2 := w.waitOnCP("run-1"), w.waitOnCP("run-1")
	r1()
	time.Sleep(200 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("one call still waiting, but the clock ran")
	}
	r2()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the clock did not start again")
	}
}
