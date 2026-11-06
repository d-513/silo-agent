package app

import (
	"testing"
	"time"
)

// The conversation lock is per chat: holding one never holds up another, and
// a lock nobody wants any more is forgotten.
func TestConvLockIsPerConversation(t *testing.T) {
	a := &App{}
	unlockA := a.lockConv("a")
	got := make(chan struct{})
	go func() {
		a.lockConv("b")()
		close(got)
	}()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("chat b waited on chat a's lock")
	}
	again := make(chan struct{})
	go func() {
		a.lockConv("a")()
		close(again)
	}()
	select {
	case <-again:
		t.Fatal("chat a's lock was taken twice")
	case <-time.After(50 * time.Millisecond):
	}
	unlockA()
	select {
	case <-again:
	case <-time.After(time.Second):
		t.Fatal("chat a's lock was not handed on")
	}
	a.convs.mu.Lock()
	n := len(a.convs.m)
	a.convs.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d locks kept after release", n)
	}
}
