package tunnel

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/nettest"
)

// pipeStream is one end of an in-memory frame pipe: what one end Sends the
// other Recvs, like the two halves of a ConnectRPC bidi stream.
type pipeStream struct {
	out    chan<- []byte
	in     <-chan []byte
	closed chan struct{}
	once   sync.Once
	peer   *pipeStream
}

func newPipe() (*pipeStream, *pipeStream) {
	ab, ba := make(chan []byte, 16), make(chan []byte, 16)
	a := &pipeStream{out: ab, in: ba, closed: make(chan struct{})}
	b := &pipeStream{out: ba, in: ab, closed: make(chan struct{})}
	a.peer, b.peer = b, a
	return a, b
}

func (p *pipeStream) Send(b []byte) error {
	select {
	case <-p.closed:
		return io.ErrClosedPipe
	default:
	}
	select {
	case p.out <- append([]byte(nil), b...):
		return nil
	case <-p.closed:
		return io.ErrClosedPipe
	case <-p.peer.closed:
		return io.ErrClosedPipe
	}
}

func (p *pipeStream) Recv() ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-p.closed:
		return nil, io.EOF
	case <-p.peer.closed:
		// drain what the peer sent before it closed
		select {
		case b := <-p.in:
			return b, nil
		default:
		}
		return nil, io.EOF
	}
}

func (p *pipeStream) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func connPair() (net.Conn, net.Conn) {
	a, b := newPipe()
	return New(a), New(b)
}

func TestConnIsANetConn(t *testing.T) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		c1, c2 = connPair()
		return c1, c2, func() { c1.Close(); c2.Close() }, nil
	})
}

func TestReadKeepsTheRemainderOfAFrame(t *testing.T) {
	a, b := connPair()
	defer a.Close()
	defer b.Close()
	if _, err := a.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	var got []byte
	for len(got) < 11 {
		n, err := b.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteSplitsLargePayloads(t *testing.T) {
	a, b := connPair()
	defer a.Close()
	defer b.Close()
	payload := bytes.Repeat([]byte("x"), 3*MaxFrame+17)
	errc := make(chan error, 1)
	go func() {
		_, err := a.Write(payload)
		errc <- err
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(b, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mangled")
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestSendNeverExceedsMaxFrame(t *testing.T) {
	a, b := newPipe()
	c := New(a)
	defer c.Close()
	go c.Write(bytes.Repeat([]byte("y"), 2*MaxFrame+1))
	total := 0
	for total < 2*MaxFrame+1 {
		f, err := b.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if len(f) > MaxFrame || len(f) == 0 {
			t.Fatalf("frame of %d bytes", len(f))
		}
		total += len(f)
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	a, b := connPair()
	defer b.Close()
	done := make(chan error, 1)
	go func() {
		_, err := a.Read(make([]byte, 8))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	a.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read after close returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read still blocked after Close")
	}
}

func TestCloseIsIdempotentAndWriteAfterCloseFails(t *testing.T) {
	a, b := connPair()
	defer b.Close()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := a.Write([]byte("x")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Write after Close = %v, want net.ErrClosed", err)
	}
}

func TestPeerCloseIsEOF(t *testing.T) {
	a, b := connPair()
	defer a.Close()
	a.Write([]byte("bye"))
	b2 := b
	buf := make([]byte, 3)
	if _, err := io.ReadFull(b2, buf); err != nil {
		t.Fatal(err)
	}
	b.Close()
	if _, err := a.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("Read = %v, want EOF", err)
	}
}

func TestConcurrentWritesDoNotInterleaveFrames(t *testing.T) {
	a, b := newPipe()
	c := New(a)
	defer c.Close()
	const writers = 8
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Write(bytes.Repeat([]byte{byte('a' + i)}, 1000))
		}(i)
	}
	go func() { wg.Wait() }()
	seen := 0
	for seen < writers*1000 {
		f, err := b.Recv()
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range f {
			if x != f[0] {
				t.Fatalf("frame mixes writers: %q", f)
			}
		}
		seen += len(f)
	}
}

func TestAddrs(t *testing.T) {
	a, b := connPair()
	defer a.Close()
	defer b.Close()
	if a.LocalAddr().Network() != "tunnel" || a.RemoteAddr().Network() != "tunnel" {
		t.Fatalf("addr = %v / %v", a.LocalAddr(), a.RemoteAddr())
	}
}
