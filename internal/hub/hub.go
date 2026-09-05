package hub

import (
	"context"
	"errors"
	"sync"

	v1 "silo.agent/gen/silo/v1"
)

var (
	ErrNoWorker = errors.New("worker not connected")
	ErrClosed   = errors.New("session closed")
)

type Result struct {
	Out string
	Err error
}

// ConsoleMsg is one console websocket payload: PTY bytes and/or a resize.
type ConsoleMsg struct {
	Data []byte
	Rows int32
	Cols int32
}

type Session struct {
	BotID string
	Send  chan *v1.Cmd
	// ToBrowser: x11vnc bytes from the Worker. Browser WebSocket reads this.
	ToBrowser chan []byte
	// ToWorker: RFB input from the browser. Worker stream sends this to x11vnc.
	ToWorker chan []byte

	mu           sync.Mutex
	wait         map[string]chan Result
	viewOn       chan struct{}
	leave        chan struct{}
	viewID       uint64
	conOn        chan struct{}
	conLeave     chan struct{}
	conID        uint64
	conToBrowser chan ConsoleMsg
	conToWorker  chan ConsoleMsg
	dead         chan struct{}
	once         sync.Once
}

func newSession(botID string) *Session {
	return &Session{
		BotID:        botID,
		Send:         make(chan *v1.Cmd, 8),
		ToBrowser:    make(chan []byte, 64),
		ToWorker:     make(chan []byte, 64),
		wait:         map[string]chan Result{},
		viewOn:       make(chan struct{}, 1),
		conOn:        make(chan struct{}, 1),
		conToBrowser: make(chan ConsoleMsg, 64),
		conToWorker:  make(chan ConsoleMsg, 64),
		dead:         make(chan struct{}),
	}
}

func (s *Session) Done() <-chan struct{} { return s.dead }

func (s *Session) close() {
	s.once.Do(func() {
		close(s.dead)
		s.FailAll(ErrClosed)
		s.mu.Lock()
		if s.leave != nil {
			close(s.leave)
			s.leave = nil
		}
		if s.conLeave != nil {
			close(s.conLeave)
			s.conLeave = nil
		}
		s.mu.Unlock()
	})
}

func (s *Session) Expect(id string) chan Result {
	ch := make(chan Result, 1)
	s.mu.Lock()
	s.wait[id] = ch
	s.mu.Unlock()
	return ch
}

func (s *Session) Resolve(id, out string, err error) {
	s.mu.Lock()
	ch := s.wait[id]
	delete(s.wait, id)
	s.mu.Unlock()
	if ch != nil {
		ch <- Result{Out: out, Err: err}
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

func (s *Session) BeginViewer() (id uint64, toBrowser <-chan []byte, toWorker chan []byte) {
	s.mu.Lock()
	if s.leave != nil {
		close(s.leave)
	}
	s.ToBrowser = make(chan []byte, 64)
	s.ToWorker = make(chan []byte, 64)
	s.leave = make(chan struct{})
	s.viewID++
	id = s.viewID
	toBrowser = s.ToBrowser
	toWorker = s.ToWorker
	s.mu.Unlock()
	select {
	case s.viewOn <- struct{}{}:
	default:
	}
	return id, toBrowser, toWorker
}

func (s *Session) PushBrowser(ctx context.Context, b []byte) error {
	s.mu.Lock()
	ch := s.ToBrowser
	leave := s.leave
	s.mu.Unlock()
	if leave == nil {
		return nil
	}
	select {
	case ch <- b:
		return nil
	case <-leave:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.dead:
		return ErrClosed
	}
}

func (s *Session) Pipes() (toBrowser chan []byte, toWorker chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ToBrowser, s.ToWorker
}

func (s *Session) EndViewer(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == 0 || id != s.viewID || s.leave == nil {
		return
	}
	close(s.leave)
	s.leave = nil
}

func (s *Session) HasViewer() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leave != nil
}

func (s *Session) WaitViewer(ctx context.Context) error {
	for {
		if s.HasViewer() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.dead:
			return ErrClosed
		case <-s.viewOn:
		}
	}
}

func (s *Session) ViewerGone() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leave == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.leave
}

func (s *Session) BeginConsole() (id uint64, toBrowser <-chan ConsoleMsg, toWorker chan ConsoleMsg) {
	s.mu.Lock()
	if s.conLeave != nil {
		close(s.conLeave)
	}
	s.conToBrowser = make(chan ConsoleMsg, 64)
	s.conToWorker = make(chan ConsoleMsg, 64)
	s.conLeave = make(chan struct{})
	s.conID++
	id = s.conID
	toBrowser = s.conToBrowser
	toWorker = s.conToWorker
	s.mu.Unlock()
	select {
	case s.conOn <- struct{}{}:
	default:
	}
	return id, toBrowser, toWorker
}

func (s *Session) PushConsole(ctx context.Context, m ConsoleMsg) error {
	s.mu.Lock()
	ch := s.conToBrowser
	leave := s.conLeave
	s.mu.Unlock()
	if leave == nil {
		return nil
	}
	select {
	case ch <- m:
		return nil
	case <-leave:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.dead:
		return ErrClosed
	}
}

func (s *Session) ConsolePipes() (toBrowser chan ConsoleMsg, toWorker chan ConsoleMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conToBrowser, s.conToWorker
}

func (s *Session) EndConsole(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == 0 || id != s.conID || s.conLeave == nil {
		return
	}
	close(s.conLeave)
	s.conLeave = nil
}

func (s *Session) HasConsole() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conLeave != nil
}

func (s *Session) WaitConsole(ctx context.Context) error {
	for {
		if s.HasConsole() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.dead:
			return ErrClosed
		case <-s.conOn:
		}
	}
}

func (s *Session) ConsoleGone() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conLeave == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.conLeave
}

func (h *Hub) Exec(ctx context.Context, botID string, cmd *v1.Cmd) (string, error) {
	s := h.Get(botID)
	if s == nil {
		return "", ErrNoWorker
	}
	ch := s.Expect(cmd.GetId())
	select {
	case s.Send <- cmd:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.dead:
		return "", ErrClosed
	}
	select {
	case r := <-ch:
		return r.Out, r.Err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.dead:
		return "", ErrClosed
	}
}
