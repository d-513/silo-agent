package hub

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
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

func TestConsoleIsolatedFromVNC(t *testing.T) {
	s := New().Attach("bot")
	_, vncB, vncW := s.BeginViewer()
	_, conB, conW := s.BeginConsole()
	ctx := context.Background()
	if err := s.PushBrowser(ctx, []byte("rfb")); err != nil {
		t.Fatal(err)
	}
	if err := s.PushConsole(ctx, ConsoleMsg{Data: []byte("pty")}); err != nil {
		t.Fatal(err)
	}
	if got := string(<-vncB); got != "rfb" {
		t.Fatalf("vnc: %q", got)
	}
	if got := string((<-conB).Data); got != "pty" {
		t.Fatalf("con: %q", got)
	}
	conW <- ConsoleMsg{Rows: 24, Cols: 80}
	select {
	case <-vncW:
		t.Fatal("resize leaked onto VNC")
	default:
	}
	msg := <-conW
	if msg.Rows != 24 || msg.Cols != 80 {
		t.Fatalf("resize %+v", msg)
	}
	old, _, _ := s.BeginConsole()
	cur, newB, _ := s.BeginConsole()
	s.EndConsole(old)
	if !s.HasConsole() {
		t.Fatal("latest console dropped")
	}
	select {
	case <-newB:
		t.Fatal("new console saw leftover")
	default:
	}
	s.EndConsole(cur)
	if s.HasConsole() {
		t.Fatal("console still on")
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

func TestExecCancelsWorker(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := h.Exec(ctx, "bot", &v1.Cmd{Id: "c1", Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: "sleep 9"}}})
		done <- err
	}()
	select {
	case cmd := <-s.Send:
		if cmd.GetId() != "c1" {
			t.Fatalf("cmd %s", cmd.GetId())
		}
	case <-time.After(time.Second):
		t.Fatal("exec not sent")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Exec stuck")
	}
	select {
	case cmd := <-s.Send:
		if cmd.GetCancel().GetCmdId() != "c1" {
			t.Fatalf("cancel %+v", cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("no CancelCmd")
	}
}

func TestCloseAllWakesWaitViewer(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.WaitViewer(ctx) }()
	time.Sleep(20 * time.Millisecond)
	h.CloseAll()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitViewer stuck after CloseAll")
	}
}
