package hub

import (
	"context"
	"errors"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

var (
	ErrNoWorker = errors.New("worker not connected")
	ErrClosed   = errors.New("session closed")
)

type Result struct {
	Out string
	// Image is an optional data: URL of a fresh desktop screenshot taken while
	// the command ran (Python/terminal look). Empty when there is none.
	Image string
	Err   error
}

// ConsoleMsg is one console websocket payload: PTY bytes and/or a resize.
type ConsoleMsg struct {
	Data []byte
	Rows int32
	Cols int32
}

// lane is one viewer-facing duplex pipe between a browser socket and the
// worker's stream: VNC bytes, or console messages. The newest viewer owns it —
// begin hands out fresh channels and an older viewer's end is ignored — and a
// lane has no viewer until one begins. Lanes are independent of each other.
type lane[T any] struct {
	mu        sync.Mutex
	on        chan struct{} // pinged (never blocks) when a viewer begins
	toBrowser chan T
	toWorker  chan T
	leave     chan struct{} // nil without a viewer; closed when it goes
	id        uint64
}

func newLane[T any]() *lane[T] {
	return &lane[T]{on: make(chan struct{}, 1), toBrowser: make(chan T, 64), toWorker: make(chan T, 64)}
}

func (l *lane[T]) begin() (id uint64, toBrowser <-chan T, toWorker chan T) {
	l.mu.Lock()
	if l.leave != nil {
		close(l.leave)
	}
	l.toBrowser = make(chan T, 64)
	l.toWorker = make(chan T, 64)
	l.leave = make(chan struct{})
	l.id++
	id, toBrowser, toWorker = l.id, l.toBrowser, l.toWorker
	l.mu.Unlock()
	select {
	case l.on <- struct{}{}:
	default:
	}
	return id, toBrowser, toWorker
}

// push hands v to the viewer. With no viewer it drops v; it gives up when the
// viewer leaves, ctx ends, or the session dies.
func (l *lane[T]) push(ctx context.Context, dead <-chan struct{}, v T) error {
	l.mu.Lock()
	ch := l.toBrowser
	leave := l.leave
	l.mu.Unlock()
	if leave == nil {
		return nil
	}
	select {
	case ch <- v:
		return nil
	case <-leave:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-dead:
		return ErrClosed
	}
}

func (l *lane[T]) pipes() (toBrowser chan T, toWorker chan T) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.toBrowser, l.toWorker
}

// end releases the lane, unless a newer viewer has taken it over.
func (l *lane[T]) end(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == 0 || id != l.id || l.leave == nil {
		return
	}
	close(l.leave)
	l.leave = nil
}

func (l *lane[T]) has() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.leave != nil
}

// wait blocks until a viewer is attached.
func (l *lane[T]) wait(ctx context.Context, dead <-chan struct{}) error {
	for {
		if l.has() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-dead:
			return ErrClosed
		case <-l.on:
		}
	}
}

// gone is closed when the current viewer leaves; already closed with none.
func (l *lane[T]) gone() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leave == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return l.leave
}

func (l *lane[T]) shutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.leave != nil {
		close(l.leave)
		l.leave = nil
	}
}

type Session struct {
	BotID string
	Send  chan *v1.Cmd

	vnc *lane[[]byte] // x11vnc bytes out, RFB input in
	con *lane[ConsoleMsg]

	mu   sync.Mutex
	wait map[string]chan Result
	dead chan struct{}
	once sync.Once
}

func newSession(botID string) *Session {
	return &Session{
		BotID: botID,
		Send:  make(chan *v1.Cmd, 8),
		vnc:   newLane[[]byte](),
		con:   newLane[ConsoleMsg](),
		wait:  map[string]chan Result{},
		dead:  make(chan struct{}),
	}
}

func (s *Session) Done() <-chan struct{} { return s.dead }

func (s *Session) close() {
	s.once.Do(func() {
		close(s.dead)
		s.FailAll(ErrClosed)
		s.vnc.shutdown()
		s.con.shutdown()
	})
}

func (s *Session) Expect(id string) chan Result {
	ch := make(chan Result, 1)
	s.mu.Lock()
	s.wait[id] = ch
	s.mu.Unlock()
	return ch
}

func (s *Session) Resolve(id, out, image string, err error) {
	s.mu.Lock()
	ch := s.wait[id]
	delete(s.wait, id)
	s.mu.Unlock()
	if ch != nil {
		ch <- Result{Out: out, Image: image, Err: err}
	}
}

func (s *Session) FailAll(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.wait {
		ch <- Result{Err: err}
		delete(s.wait, id)
	}
}

type Hub struct {
	mu sync.Mutex
	m  map[string]*Session
}

func New() *Hub { return &Hub{m: map[string]*Session{}} }

func (h *Hub) Attach(botID string) *Session {
	s := newSession(botID)
	h.mu.Lock()
	if old, ok := h.m[botID]; ok {
		old.close()
	}
	h.m[botID] = s
	h.mu.Unlock()
	return s
}

func (h *Hub) Detach(botID string, s *Session) {
	h.mu.Lock()
	if h.m[botID] == s {
		delete(h.m, botID)
	}
	h.mu.Unlock()
	s.close()
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	m := h.m
	h.m = map[string]*Session{}
	h.mu.Unlock()
	for _, s := range m {
		s.close()
	}
}

func (h *Hub) Drop(botID string) {
	h.mu.Lock()
	s := h.m[botID]
	delete(h.m, botID)
	h.mu.Unlock()
	if s != nil {
		s.close()
	}
}

func (h *Hub) Get(botID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.m[botID]
}

func (h *Hub) Connected(botID string) bool {
	return h.Get(botID) != nil
}

// WaitConnected waits up to d for the Bot's worker to connect, so work that
// arrives while the box is still starting can go on once it is up. It reports
// whether the worker is connected when the wait ends (or ctx is done).
func (h *Hub) WaitConnected(ctx context.Context, botID string, d time.Duration) bool {
	if h.Connected(botID) {
		return true
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return h.Connected(botID)
		case <-time.After(250 * time.Millisecond):
		}
		if h.Connected(botID) {
			return true
		}
	}
	return h.Connected(botID)
}

func (s *Session) BeginViewer() (id uint64, toBrowser <-chan []byte, toWorker chan []byte) {
	return s.vnc.begin()
}

func (s *Session) PushBrowser(ctx context.Context, b []byte) error {
	return s.vnc.push(ctx, s.dead, b)
}

// Pipes returns the VNC channels: x11vnc bytes from the Worker (the browser
// WebSocket reads them) and RFB input from the browser (the Worker stream
// sends it to x11vnc).
func (s *Session) Pipes() (toBrowser chan []byte, toWorker chan []byte) { return s.vnc.pipes() }

func (s *Session) EndViewer(id uint64) { s.vnc.end(id) }

func (s *Session) HasViewer() bool { return s.vnc.has() }

func (s *Session) WaitViewer(ctx context.Context) error { return s.vnc.wait(ctx, s.dead) }

func (s *Session) ViewerGone() <-chan struct{} { return s.vnc.gone() }

func (s *Session) BeginConsole() (id uint64, toBrowser <-chan ConsoleMsg, toWorker chan ConsoleMsg) {
	return s.con.begin()
}

func (s *Session) PushConsole(ctx context.Context, m ConsoleMsg) error {
	return s.con.push(ctx, s.dead, m)
}

func (s *Session) ConsolePipes() (toBrowser chan ConsoleMsg, toWorker chan ConsoleMsg) {
	return s.con.pipes()
}

func (s *Session) EndConsole(id uint64) { s.con.end(id) }

func (s *Session) HasConsole() bool { return s.con.has() }

func (s *Session) WaitConsole(ctx context.Context) error { return s.con.wait(ctx, s.dead) }

func (s *Session) ConsoleGone() <-chan struct{} { return s.con.gone() }

func (h *Hub) Exec(ctx context.Context, botID string, cmd *v1.Cmd) (string, error) {
	r, err := h.ExecResult(ctx, botID, cmd)
	return r.Out, err
}

// ExecResult is Exec with the full result, including any screenshot the worker
// captured while the command ran.
func (h *Hub) ExecResult(ctx context.Context, botID string, cmd *v1.Cmd) (Result, error) {
	s := h.Get(botID)
	if s == nil {
		return Result{}, ErrNoWorker
	}
	ch := s.Expect(cmd.GetId())
	select {
	case s.Send <- cmd:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-s.dead:
		return Result{}, ErrClosed
	}
	select {
	case r := <-ch:
		return r, r.Err
	case <-ctx.Done():
		select {
		case s.Send <- &v1.Cmd{Id: cmd.GetId() + "-stop", Body: &v1.Cmd_Cancel{Cancel: &v1.CancelCmd{CmdId: cmd.GetId()}}}:
		default:
		}
		return Result{}, ctx.Err()
	case <-s.dead:
		return Result{}, ErrClosed
	}
}
