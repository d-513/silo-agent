package app_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"silo.agent/internal/apptest"
	"silo.agent/internal/llm/dummy"
)

// echoServer listens on a free loopback port and echoes every connection, like
// a service the Bot runs on localhost.
func echoServer(t *testing.T) (port int, accepted <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	acc := make(chan struct{}, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			acc <- struct{}{}
			go func() {
				defer c.Close()
				io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, acc
}

func dialTunnel(t *testing.T, h *apptest.H, botID string, port int) (net.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.App.Hub.DialTunnel(ctx, botID, port)
}

// The CP asks the worker for a connection, the worker dials the port inside the
// box and pumps bytes both ways: what the dialer writes comes back echoed.
func TestTunnelPumpsBytesBothWays(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Tunneler").GetId()
	h.StartWorker(id)
	port, _ := echoServer(t)

	c, err := dialTunnel(t, h, id, port)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "ping" {
		t.Fatalf("echo = %q, %v", got, err)
	}

	// Several MB, much more than a frame, comes back intact.
	payload := bytes.Repeat([]byte("0123456789abcdef"), 256*1024) // 4 MiB
	errc := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		errc <- err
	}()
	back := make([]byte, len(payload))
	if _, err := io.ReadFull(c, back); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, payload) {
		t.Fatal("payload corrupted in transit")
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

// Closing the CP end closes the connection inside the box, and the box closing
// it ends the CP end with EOF.
func TestTunnelCloseReachesBothEnds(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Closer").GetId()
	h.StartWorker(id)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srvSawEOF := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(io.Discard, c) // returns when the peer closes
		close(srvSawEOF)
	}()

	c, err := dialTunnel(t, h, id, ln.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	select {
	case <-srvSawEOF:
	case <-time.After(5 * time.Second):
		t.Fatal("the service in the box never saw the connection close")
	}

	// And the other direction: the service hangs up.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	go func() {
		c, err := ln2.Accept()
		if err == nil {
			c.Write([]byte("bye"))
			c.Close()
		}
	}()
	c2, err := dialTunnel(t, h, id, ln2.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	all, err := io.ReadAll(c2)
	if err != nil || string(all) != "bye" {
		t.Fatalf("ReadAll = %q, %v", all, err)
	}
}

func TestTunnelDialFailures(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Refused").GetId()
	h.StartWorker(id)

	// A closed port is a clear error, not a hang.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if _, err := dialTunnel(t, h, id, closed); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("closed port err = %v, want connection refused", err)
	}

	// Ports the box itself uses are never tunnelled, and nothing is dialed.
	for _, p := range []int{5900, 9222, 0, 70000} {
		if _, err := dialTunnel(t, h, id, p); err == nil {
			t.Fatalf("port %d was tunnelled", p)
		}
	}

	// A Bot with no worker is a distinct, fast error.
	other := h.CreateBot("Offline").GetId()
	if _, err := dialTunnel(t, h, other, closed); err == nil || !strings.Contains(err.Error(), "worker not connected") {
		t.Fatalf("offline err = %v", err)
	}
}

// Connections made one after another do not interfere, and each is its own
// stream to the service.
func TestTunnelConcurrentConnections(t *testing.T) {
	dummy.Reset()
	h := apptest.New(t)
	id := h.CreateBot("Many").GetId()
	h.StartWorker(id)
	port, accepted := echoServer(t)

	const n = 8
	errs := make(chan error, n)
	for i := range n {
		go func() {
			c, err := dialTunnel(t, h, id, port)
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()
			msg := bytes.Repeat([]byte{byte('a' + i)}, 50_000)
			go c.Write(msg)
			back := make([]byte, len(msg))
			if _, err := io.ReadFull(c, back); err != nil {
				errs <- err
				return
			}
			if !bytes.Equal(back, msg) {
				errs <- io.ErrUnexpectedEOF
				return
			}
			errs <- nil
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if len(accepted) != n {
		t.Fatalf("service saw %d connections, want %d", len(accepted), n)
	}
}
