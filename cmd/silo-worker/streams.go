package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"connectrpc.com/connect"
	"github.com/creack/pty"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
)

// retryLoop runs one CP stream session after another, forever: a short pause
// between sessions, a long one when the CP does not know this token yet.
func retryLoop(name string, once func() error) {
	for {
		err := once()
		if err != nil {
			log.Printf("%s: %v", name, err)
		}
		time.Sleep(reconnectWait(err))
	}
}

func reconnectWait(err error) time.Duration {
	if err != nil && connect.CodeOf(err) == connect.CodeUnauthenticated {
		return 15 * time.Second
	}
	return 200 * time.Millisecond
}

// pump copies r to the CP in chunks until either side ends, then cancels the
// stream's context so the receive loop beside it stops too.
func pump(r io.Reader, cancel context.CancelFunc, send func([]byte) error) {
	defer cancel()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return
		}
		if send(bytes.Clone(buf[:n])) != nil {
			return
		}
	}
}

func (w *worker) vncOnce(client silov1connect.BotWorkerClient) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := client.VNC(ctx)
	if err := st.Send(&v1.Frame{}); err != nil {
		return err
	}
	if _, err := st.Receive(); err != nil {
		return err
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	var conn net.Conn
	var err error
	for i := 0; i < 25; i++ {
		conn, err = d.Dial("tcp", "127.0.0.1:5900")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	go pump(conn, cancel, func(b []byte) error { return st.Send(&v1.Frame{Data: b}) })
	for {
		fr, err := st.Receive()
		if err != nil {
			return err
		}
		if len(fr.GetData()) == 0 {
			continue
		}
		if _, err := conn.Write(fr.GetData()); err != nil {
			return err
		}
	}
}

func (w *worker) startPTY(rows, cols uint16) (*os.File, *exec.Cmd, error) {
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	c := exec.Command(shell)
	c.Dir = w.workspace
	c.Env = append(w.childEnv(""), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(c, &pty.Winsize{Rows: rows, Cols: cols})
	return ptmx, c, err
}

func (w *worker) consoleOnce(client silov1connect.BotWorkerClient) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := client.Console(ctx)
	if err := st.Send(&v1.ConsoleIO{}); err != nil {
		return err
	}
	first, err := st.Receive()
	if err != nil {
		return err
	}
	ptmx, cmd, err := w.startPTY(uint16(first.GetRows()), uint16(first.GetCols()))
	if err != nil {
		return err
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()
	if len(first.GetData()) > 0 {
		if _, err := ptmx.Write(first.GetData()); err != nil {
			return err
		}
	}
	go pump(ptmx, cancel, func(b []byte) error { return st.Send(&v1.ConsoleIO{Data: b}) })
	for {
		fr, err := st.Receive()
		if err != nil {
			return err
		}
		if fr.GetRows() > 0 && fr.GetCols() > 0 {
			_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(fr.GetRows()), Cols: uint16(fr.GetCols())})
		}
		if len(fr.GetData()) == 0 {
			continue
		}
		if _, err := ptmx.Write(fr.GetData()); err != nil {
			return err
		}
	}
}
