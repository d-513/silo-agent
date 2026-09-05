package hub

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitViewer(t *testing.T) {
	s := New().Attach("bot")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.BeginViewer()
	}()
	if err := s.WaitViewer(ctx); err != nil {
		t.Fatal(err)
	}
	s.EndViewer(1)
	select {
	case <-s.ViewerGone():
	default:
		t.Fatal("viewer still on")
	}
}

func TestVNCPipes(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	go func() { s.ToBrowser <- []byte("rfb") }()
	if got := string(<-s.ToBrowser); got != "rfb" {
		t.Fatalf("worker→browser: %q", got)
	}
	go func() { s.ToWorker <- []byte("ptr") }()
	if got := string(<-s.ToWorker); got != "ptr" {
		t.Fatalf("browser→worker: %q", got)
	}
}

func TestReplaceWakesWaitViewer(t *testing.T) {
	h := New()
	old := h.Attach("bot")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- old.WaitViewer(ctx) }()
	time.Sleep(20 * time.Millisecond)
	h.Attach("bot")
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitViewer stuck on replaced session")
	}
}

func TestWaitViewerAlreadyAttached(t *testing.T) {
	s := New().Attach("bot")
	s.BeginViewer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitViewer(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitViewer(ctx); err != nil {
		t.Fatal("second wait while viewer still on")
	}
}

func TestViewerChurnKeepsLatestSignal(t *testing.T) {
	s := New().Attach("bot")
	s.BeginViewer()
	s.EndViewer(1)
	s.BeginViewer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitViewer(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestOldEndViewerDoesNotDropNew(t *testing.T) {
	s := New().Attach("bot")
	old, _, _ := s.BeginViewer()
	cur, toB, _ := s.BeginViewer()
	s.EndViewer(old)
	if !s.HasViewer() {
		t.Fatal("latest viewer dropped by stale EndViewer")
	}
	select {
	case <-toB:
		t.Fatal("new viewer saw leftover from previous pipe")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitViewer(ctx); err != nil {
		t.Fatal(err)
	}
	s.EndViewer(cur)
	if s.HasViewer() {
		t.Fatal("current viewer still on")
	}
}

func TestBeginViewerIsolatesPipes(t *testing.T) {
	s := New().Attach("bot")
	_, oldB, _ := s.BeginViewer()
	ctx := context.Background()
	if err := s.PushBrowser(ctx, []byte("stale")); err != nil {
		t.Fatal(err)
	}
	_, newB, _ := s.BeginViewer()
	select {
	case got := <-oldB:
		if string(got) != "stale" {
			t.Fatalf("old pipe: %q", got)
		}
	default:
		t.Fatal("stale frame should stay on old pipe")
	}
	select {
	case <-newB:
		t.Fatal("new viewer received stale RFB bytes")
	default:
	}
}

func TestDetachWakesWaitViewer(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.WaitViewer(ctx) }()
	time.Sleep(20 * time.Millisecond)
	h.Detach("bot", s)
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitViewer stuck after detach")
	}
}

func TestFailAllOnDisconnect(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ch := s.Expect("cmd")
	h.Detach("bot", s)
	select {
	case r := <-ch:
		if r.Err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(time.Second):
		t.Fatal("Expect stuck")
	}
}
