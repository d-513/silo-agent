package hub

import (
	"context"
	"errors"
	"net"
	"os"
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
	toBrowser, toWorker := s.Pipes()
	go func() { toBrowser <- []byte("rfb") }()
	if got := string(<-toBrowser); got != "rfb" {
		t.Fatalf("worker→browser: %q", got)
	}
	go func() { toWorker <- []byte("ptr") }()
	if got := string(<-toWorker); got != "ptr" {
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

func TestWaitConnected(t *testing.T) {
	h := New()
	ctx := context.Background()
	if h.WaitConnected(ctx, "b1", 10*time.Millisecond) {
		t.Fatal("no worker yet")
	}
	go func() {
		time.Sleep(60 * time.Millisecond)
		h.Attach("b1")
	}()
	if !h.WaitConnected(ctx, "b1", 2*time.Second) {
		t.Fatal("the worker connected during the wait")
	}
	if !h.WaitConnected(ctx, "b1", 0) {
		t.Fatal("an already connected worker needs no wait")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if h.WaitConnected(cctx, "b2", 5*time.Second) {
		t.Fatal("a canceled wait must give up")
	}
}

// openedTunnel waits for the OpenTunnel command DialTunnel queues.
func openedTunnel(t *testing.T, s *Session) *v1.OpenTunnelCmd {
	t.Helper()
	select {
	case cmd := <-s.Send:
		open := cmd.GetOpenTunnel()
		if open == nil {
			t.Fatalf("queued %T, want open_tunnel", cmd.GetBody())
		}
		if cmd.GetId() != open.GetConnId() {
			t.Fatalf("cmd id %q != conn id %q", cmd.GetId(), open.GetConnId())
		}
		return open
	case <-time.After(time.Second):
		t.Fatal("no open_tunnel command queued")
		return nil
	}
}

func TestDialTunnelReturnsTheClaimedConn(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	got := make(chan net.Conn, 1)
	go func() {
		c, err := h.DialTunnel(context.Background(), "bot", 8000)
		if err != nil {
			t.Error(err)
		}
		got <- c
	}()
	open := openedTunnel(t, s)
	if open.GetPort() != 8000 {
		t.Fatalf("port = %d", open.GetPort())
	}
	deliver, ok := s.ClaimTunnel(open.GetConnId())
	if !ok {
		t.Fatal("claim refused")
	}
	a, b := net.Pipe()
	defer b.Close()
	deliver(a, nil)
	select {
	case c := <-got:
		if c != a {
			t.Fatal("DialTunnel returned another conn")
		}
	case <-time.After(time.Second):
		t.Fatal("DialTunnel never returned")
	}
}

func TestDialTunnelNoWorker(t *testing.T) {
	if _, err := New().DialTunnel(context.Background(), "bot", 80); !errors.Is(err, ErrNoWorker) {
		t.Fatalf("err = %v, want ErrNoWorker", err)
	}
}

func TestDialTunnelDeliveredError(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	errc := make(chan error, 1)
	go func() {
		_, err := h.DialTunnel(context.Background(), "bot", 80)
		errc <- err
	}()
	deliver, _ := s.ClaimTunnel(openedTunnel(t, s).GetConnId())
	deliver(nil, errors.New("connection refused"))
	if err := <-errc; err == nil || err.Error() != "connection refused" {
		t.Fatalf("err = %v", err)
	}
}

func TestDialTunnelClaimIsOneShot(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	go h.DialTunnel(context.Background(), "bot", 80)
	id := openedTunnel(t, s).GetConnId()
	if _, ok := s.ClaimTunnel("nope"); ok {
		t.Fatal("claimed an unknown id")
	}
	if _, ok := s.ClaimTunnel(id); !ok {
		t.Fatal("first claim refused")
	}
	if _, ok := s.ClaimTunnel(id); ok {
		t.Fatal("second claim of the same id succeeded")
	}
}

func TestDialTunnelContextEnds(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := h.DialTunnel(ctx, "bot", 80)
		errc <- err
	}()
	id := openedTunnel(t, s).GetConnId()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := s.ClaimTunnel(id); ok {
		t.Fatal("a late worker could still claim an abandoned dial")
	}
}

func TestDialTunnelTimesOut(t *testing.T) {
	old := TunnelTimeout
	TunnelTimeout = 30 * time.Millisecond
	defer func() { TunnelTimeout = old }()
	h := New()
	h.Attach("bot")
	if _, err := h.DialTunnel(context.Background(), "bot", 80); !errors.Is(err, ErrTunnelTimeout) {
		t.Fatalf("err = %v, want ErrTunnelTimeout", err)
	}
}

func TestDialTunnelSessionDies(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	errc := make(chan error, 1)
	go func() {
		_, err := h.DialTunnel(context.Background(), "bot", 80)
		errc <- err
	}()
	openedTunnel(t, s)
	h.Detach("bot", s)
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

// A worker that claims just as the dialer gives up must not leave the conn open.
func TestDialTunnelClosesAConnDeliveredAfterTheDialerLeft(t *testing.T) {
	h := New()
	s := h.Attach("bot")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.DialTunnel(ctx, "bot", 80)
		close(done)
	}()
	deliver, ok := s.ClaimTunnel(openedTunnel(t, s).GetConnId())
	if !ok {
		t.Fatal("claim refused")
	}
	cancel()
	<-done
	a, b := net.Pipe()
	defer b.Close()
	deliver(a, nil)
	b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := b.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("delivered conn was left open: %v", err)
	}
}
