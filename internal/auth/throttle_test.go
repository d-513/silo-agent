package auth

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) forward(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter() (*Limiter, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewLimiter()
	l.now = c.now
	return l, c
}

func TestLimiterBacksOffAPairAfterFiveFailures(t *testing.T) {
	l, clock := newTestLimiter()
	for i := range 4 {
		l.Fail("a@b.c", "1.1.1.1")
		if w := l.Wait("a@b.c", "1.1.1.1"); w != 0 {
			t.Fatalf("blocked after %d failures: %v", i+1, w)
		}
	}
	l.Fail("a@b.c", "1.1.1.1")
	if w := l.Wait("a@b.c", "1.1.1.1"); w != 30*time.Second {
		t.Fatalf("after 5 failures wait %v, want 30s", w)
	}
	// Someone else on that address, and the same person elsewhere, are not held up.
	if w := l.Wait("other@b.c", "1.1.1.1"); w != 0 {
		t.Fatalf("another account on the address waits %v", w)
	}
	if w := l.Wait("a@b.c", "2.2.2.2"); w != 0 {
		t.Fatalf("the account from another address waits %v", w)
	}
	clock.forward(30 * time.Second)
	if w := l.Wait("a@b.c", "1.1.1.1"); w != 0 {
		t.Fatalf("still blocked after the wait: %v", w)
	}
	// Each further failure doubles the wait, up to 15 minutes.
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, d := range want {
		l.Fail("a@b.c", "1.1.1.1")
		if w := l.Wait("a@b.c", "1.1.1.1"); w != d {
			t.Fatalf("failure %d: wait %v, want %v", 6+i, w, d)
		}
		clock.forward(d)
	}
}

func TestLimiterSuccessClearsThePair(t *testing.T) {
	l, _ := newTestLimiter()
	for range 4 {
		l.Fail("a@b.c", "1.1.1.1")
	}
	l.OK("a@b.c", "1.1.1.1")
	for range 4 {
		l.Fail("a@b.c", "1.1.1.1")
	}
	if w := l.Wait("a@b.c", "1.1.1.1"); w != 0 {
		t.Fatalf("a success did not clear the count: %v", w)
	}
}

func TestLimiterForgetsAnIdlePair(t *testing.T) {
	l, clock := newTestLimiter()
	for range 4 {
		l.Fail("a@b.c", "1.1.1.1")
	}
	clock.forward(time.Hour + time.Second)
	l.Fail("a@b.c", "1.1.1.1")
	if w := l.Wait("a@b.c", "1.1.1.1"); w != 0 {
		t.Fatalf("an hour-old count still applied: %v", w)
	}
}

// One address trying many accounts is stopped even though no single pair is.
func TestLimiterBlocksAnAddress(t *testing.T) {
	l, clock := newTestLimiter()
	for i := range 20 {
		if w := l.Wait("x@b.c", "1.1.1.1"); w != 0 {
			t.Fatalf("blocked after %d failures: %v", i, w)
		}
		l.Fail(fmt.Sprintf("u%d@b.c", i), "1.1.1.1")
		clock.forward(time.Second)
	}
	w := l.Wait("x@b.c", "1.1.1.1")
	if w <= 14*time.Minute || w > 15*time.Minute {
		t.Fatalf("address wait %v", w)
	}
	if w := l.Wait("x@b.c", "2.2.2.2"); w != 0 {
		t.Fatalf("another address waits %v", w)
	}
	clock.forward(15 * time.Minute)
	if w := l.Wait("x@b.c", "1.1.1.1"); w != 0 {
		t.Fatalf("address still blocked: %v", w)
	}
}

// One account tried from many addresses is slowed down as well.
func TestLimiterBlocksAnAccount(t *testing.T) {
	l, clock := newTestLimiter()
	for i := range 50 {
		l.Fail("a@b.c", fmt.Sprintf("10.0.%d.%d", i/250, i%250))
		clock.forward(time.Second)
	}
	if w := l.Wait("a@b.c", "9.9.9.9"); w <= 0 || w > time.Hour {
		t.Fatalf("account wait %v", w)
	}
	if w := l.Wait("other@b.c", "9.9.9.9"); w != 0 {
		t.Fatalf("another account waits %v", w)
	}
}

// With no account (an invite link being guessed) only the address counts.
func TestLimiterWithoutAnAccount(t *testing.T) {
	l, _ := newTestLimiter()
	for range 19 {
		l.Fail("", "1.1.1.1")
	}
	if w := l.Wait("", "1.1.1.1"); w != 0 {
		t.Fatalf("blocked early: %v", w)
	}
	l.Fail("", "1.1.1.1")
	if w := l.Wait("", "1.1.1.1"); w == 0 {
		t.Fatal("not blocked after 20 failures")
	}
}

func TestLimiterPrunes(t *testing.T) {
	l, clock := newTestLimiter()
	for i := range 3000 {
		l.Fail(fmt.Sprintf("u%d@b.c", i), fmt.Sprintf("10.%d.%d.1", i/250, i%250))
	}
	clock.forward(2 * time.Hour)
	l.Fail("a@b.c", "1.1.1.1")
	l.mu.Lock()
	n := len(l.pairs) + len(l.addrs) + len(l.accounts)
	l.mu.Unlock()
	if n > 10 {
		t.Fatalf("%d stale entries kept", n)
	}
}
