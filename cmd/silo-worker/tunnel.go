package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/tunnel"
)

// Ports the box runs for itself: x11vnc and Chromium's debugging port. The CP
// already refuses to hand them out, and the worker refuses them too, so a
// tunnel can never reach the desktop or the browser's control socket.
var reservedTunnelPorts = map[int]string{5900: "x11vnc", 9222: "Chromium debugging"}

func checkTunnelPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is out of range", port)
	}
	if what, ok := reservedTunnelPorts[port]; ok {
		return fmt.Errorf("port %d is reserved (%s)", port, what)
	}
	return nil
}

// openTunnel serves one OpenTunnel command: it dials the port inside the box,
// opens a Tunnel stream named connID, and pumps bytes until either side hangs
// up. A failed dial is reported on the stream's first frame, so the CP's dialer
// gets the reason instead of waiting out its timeout.
func (w *worker) openTunnel(client silov1connect.BotWorkerClient, connID string, port int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var tcp net.Conn
	err := checkTunnelPort(port)
	if err == nil {
		d := net.Dialer{Timeout: 3 * time.Second}
		tcp, err = d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}

	st := client.Tunnel(ctx)
	first := &v1.TunnelFrame{ConnId: connID}
	if err != nil {
		first.Error = validUTF8(err.Error())
	}
	// This first Send also flushes the bidi headers.
	if sendErr := st.Send(first); sendErr != nil {
		if tcp != nil {
			_ = tcp.Close()
		}
		log.Printf("tunnel %s: %v", connID, sendErr)
		return
	}
	if err != nil {
		// Let the CP read the reason: cancelling now could drop the frame
		// before it is flushed. The CP ends the stream once it has the error.
		_ = st.CloseRequest()
		stop := time.AfterFunc(5*time.Second, cancel)
		defer stop.Stop()
		for {
			if _, rerr := st.Receive(); rerr != nil {
				return
			}
		}
	}

	// Closing the conn must not cancel the stream: that can drop frames still
	// in flight (a service that writes and hangs up at once). Instead the
	// request side is half-closed, which ends the stream after what was sent,
	// and the stream is cancelled only once the CP has ended its side too (or
	// after a grace period). Send and CloseRequest share a lock; a Send stuck on
	// a stalled CP is released by that cancel.
	var smu sync.Mutex
	streamDone := make(chan struct{})
	var doneOnce sync.Once
	conn := tunnel.New(tunnel.Funcs{
		SendFn: func(b []byte) error {
			smu.Lock()
			defer smu.Unlock()
			return st.Send(&v1.TunnelFrame{Data: b})
		},
		RecvFn: func() ([]byte, error) {
			fr, err := st.Receive()
			if err != nil {
				doneOnce.Do(func() { close(streamDone) })
				return nil, err
			}
			return fr.GetData(), nil
		},
		CloseFn: func() error {
			go func() {
				smu.Lock()
				defer smu.Unlock()
				_ = st.CloseRequest()
			}()
			return nil
		},
	})
	tunnel.Join(tcp, conn)
	select {
	case <-streamDone:
	case <-time.After(3 * time.Second):
	}
}
