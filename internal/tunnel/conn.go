// Package tunnel turns a framed bidirectional byte stream (one ConnectRPC bidi
// stream) into a net.Conn. The Control Plane hands such a conn to
// httputil.ReverseProxy as the result of a dial, and the Worker pumps it to a
// TCP port inside the Bot, so HTTP keep-alive, SSE and WebSocket upgrades all
// work as they would on a real socket.
package tunnel

import (
	"bytes"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// MaxFrame is the largest payload sent in one frame.
const MaxFrame = 32 * 1024

// Stream is one end of a frame pipe. Send and Recv may each be called from one
// goroutine at a time; Close must make a blocked Recv return.
type Stream interface {
	Send([]byte) error
	Recv() ([]byte, error)
	Close() error
}

type addr struct{}

func (addr) Network() string { return "tunnel" }
func (addr) String() string  { return "tunnel" }

// deadline is a resettable timer exposed as a channel that closes when it
// fires (the same shape net.Pipe uses).
type deadline struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel chan struct{}
}

func newDeadline() *deadline { return &deadline{cancel: make(chan struct{})} }

func (d *deadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel // the timer already fired; wait for it to finish closing
	}
	d.timer = nil
	closed := isClosed(d.cancel)
	if t.IsZero() {
		if closed {
			d.cancel = make(chan struct{})
		}
		return
	}
	if dur := time.Until(t); dur > 0 {
		if closed {
			d.cancel = make(chan struct{})
		}
		ch := d.cancel
		d.timer = time.AfterFunc(dur, func() { close(ch) })
		return
	}
	if !closed {
		close(d.cancel)
	}
}

func (d *deadline) wait() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cancel
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// Conn is a net.Conn over a Stream.
type Conn struct {
	st Stream

	frames chan []byte // fed by the reader goroutine, one frame at a time
	rem    []byte      // unread tail of the last frame; guarded by rmu
	rmu    sync.Mutex

	wlock chan struct{} // cap 1: serializes Send

	rd, wd *deadline

	closed    chan struct{}
	closeOnce sync.Once
}

// New wraps st and starts reading it.
func New(st Stream) *Conn {
	c := &Conn{
		st:     st,
		frames: make(chan []byte),
		wlock:  make(chan struct{}, 1),
		rd:     newDeadline(),
		wd:     newDeadline(),
		closed: make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Conn) readLoop() {
	defer close(c.frames)
	for {
		b, err := c.st.Recv()
		if err != nil {
			return
		}
		if len(b) == 0 {
			continue
		}
		select {
		case c.frames <- b:
		case <-c.closed:
			return
		}
	}
}

func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if len(c.rem) == 0 {
		select {
		case <-c.closed:
			return 0, net.ErrClosed
		default:
		}
		if isClosed(c.rd.wait()) {
			return 0, os.ErrDeadlineExceeded
		}
		select {
		case b, ok := <-c.frames:
			if !ok {
				return 0, io.EOF
			}
			c.rem = b
		case <-c.closed:
			return 0, net.ErrClosed
		case <-c.rd.wait():
			return 0, os.ErrDeadlineExceeded
		}
	}
	n := copy(p, c.rem)
	c.rem = c.rem[n:]
	return n, nil
}

func (c *Conn) Write(p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	// Take the send lock, honoring the write deadline.
	select {
	case c.wlock <- struct{}{}:
	case <-c.closed:
		return 0, net.ErrClosed
	case <-c.wd.wait():
		return 0, os.ErrDeadlineExceeded
	}
	if isClosed(c.wd.wait()) {
		<-c.wlock
		return 0, os.ErrDeadlineExceeded
	}
	// The send goroutine can outlive a deadline, and a Conn must not touch p
	// after Write returns, so it works on its own copy.
	buf := bytes.Clone(p)
	var sent atomic.Int64
	res := make(chan error, 1)
	go func() {
		defer func() { <-c.wlock }()
		for len(buf) > 0 {
			chunk := buf[:min(len(buf), MaxFrame)]
			if err := c.st.Send(chunk); err != nil {
				res <- err
				return
			}
			sent.Add(int64(len(chunk)))
			buf = buf[len(chunk):]
		}
		res <- nil
	}()
	select {
	case err := <-res:
		if err != nil && isClosed(c.closed) {
			err = net.ErrClosed
		}
		return int(sent.Load()), err
	case <-c.closed:
		return int(sent.Load()), net.ErrClosed
	case <-c.wd.wait():
		// The send goroutine keeps the lock until Send returns, so a later
		// Write queues behind it and frames never interleave.
		return int(sent.Load()), os.ErrDeadlineExceeded
	}
}

// Close ends the conn and the stream under it. It is safe to call twice.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.st.Close()
	})
	return nil
}

func (c *Conn) LocalAddr() net.Addr  { return addr{} }
func (c *Conn) RemoteAddr() net.Addr { return addr{} }

func (c *Conn) SetDeadline(t time.Time) error {
	if isClosed(c.closed) {
		return net.ErrClosed
	}
	c.rd.set(t)
	c.wd.set(t)
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	if isClosed(c.closed) {
		return net.ErrClosed
	}
	c.rd.set(t)
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	if isClosed(c.closed) {
		return net.ErrClosed
	}
	c.wd.set(t)
	return nil
}
