package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"silo.agent/internal/ids"
)

// A kernel is one persistent Python interpreter. exec_python used to start a
// new `python3 -c` for every call, so each call paid for its imports again and
// the model had to reload or recompute whatever the last call had in memory.
// Now a conversation's calls share an interpreter: what one defines, the next
// still has. A kernel is named by its session (the conversation), runs one
// cell at a time, and is closed after kernelIdle without use. Whenever a cell
// starts in a new interpreter — the first call, or after the old one was
// closed, killed or lost with the machine — its output opens with
// freshSession, so the model knows its variables are gone.
//
// Only the interpreter is ever signalled or killed, never its process group: a
// server a cell started in the background must outlive the session that
// started it, as it outlived the one-shot interpreter before.
const (
	freshSession = "[fresh Python session]"
	// kernelIdle is how long an unused session is kept.
	kernelIdle = 30 * time.Minute
	// kernelMax is how many sessions are kept at once; the least recently
	// used idle one makes room for a new one.
	kernelMax = 4
	// kernelGrace is how long an interrupted cell has to stop before the
	// interpreter is killed.
	kernelGrace = 3 * time.Second
)

//go:embed kernel.py
var kernelDriver string

type kernel struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	// out carries the interpreter's output while a cell runs; between cells
	// it is dropped.
	out  chan []byte
	busy atomic.Bool
	// exited closes when the interpreter is gone; state is why.
	exited chan struct{}
	state  string
	// mu makes cells take turns.
	mu    sync.Mutex
	fresh bool
	// used is guarded by kernels.mu.
	used time.Time
}

// kernels is the worker's set of sessions. The zero value is ready.
type kernels struct {
	mu     sync.Mutex
	m      map[string]*kernel
	max    int
	grace  time.Duration
	reaper sync.Once
}

func (ks *kernels) limits() (int, time.Duration) {
	limit, grace := ks.max, ks.grace
	if limit <= 0 {
		limit = kernelMax
	}
	if grace <= 0 {
		grace = kernelGrace
	}
	return limit, grace
}

// get returns the session's interpreter, starting one when it has none. A nil
// kernel means there is no room (every session is mid-cell) or Python would
// not start; the caller runs the code one-shot.
func (ks *kernels) get(w *worker, session string) *kernel {
	ks.reaper.Do(func() {
		go func() {
			for range time.Tick(time.Minute) {
				ks.reap(time.Now())
			}
		}()
	})
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.m == nil {
		ks.m = map[string]*kernel{}
	}
	if k := ks.m[session]; k != nil {
		select {
		case <-k.exited:
			delete(ks.m, session)
		default:
			k.used = time.Now()
			return k
		}
	}
	limit, _ := ks.limits()
	for len(ks.m) >= limit {
		oldest := ""
		for name, k := range ks.m {
			if k.busy.Load() {
				continue
			}
			if oldest == "" || k.used.Before(ks.m[oldest].used) {
				oldest = name
			}
		}
		if oldest == "" {
			return nil
		}
		ks.m[oldest].close()
		delete(ks.m, oldest)
	}
	k, err := startKernel(w)
	if err != nil {
		return nil
	}
	k.used = time.Now()
	ks.m[session] = k
	return k
}

// drop forgets k (a session whose interpreter died or was killed).
func (ks *kernels) drop(session string, k *kernel) {
	ks.mu.Lock()
	if ks.m[session] == k {
		delete(ks.m, session)
	}
	ks.mu.Unlock()
	k.close()
}

// reap closes the sessions nobody used for kernelIdle.
func (ks *kernels) reap(now time.Time) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	for name, k := range ks.m {
		if !k.busy.Load() && now.Sub(k.used) > kernelIdle {
			k.close()
			delete(ks.m, name)
		}
	}
}

func (ks *kernels) count() int {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return len(ks.m)
}

func (ks *kernels) closeAll() {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	for name, k := range ks.m {
		k.close()
		delete(ks.m, name)
	}
}

func startKernel(w *worker) (*kernel, error) {
	c := exec.Command("python3", "-u", "-c", kernelDriver)
	c.Dir = w.workspace
	c.Env = w.childEnv("")
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	// One pipe for stdout and stderr keeps their order. It is ours, not
	// os/exec's, so Wait does not wait for a background child that still
	// holds it.
	r, wr, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c.Stdout, c.Stderr = wr, wr
	if err := c.Start(); err != nil {
		r.Close()
		wr.Close()
		return nil, err
	}
	wr.Close()
	k := &kernel{cmd: c, stdin: stdin, out: make(chan []byte, 64), exited: make(chan struct{}), fresh: true}
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 && k.busy.Load() {
				k.out <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				r.Close()
				return
			}
		}
	}()
	go func() {
		err := c.Wait()
		k.state = "exit status 0"
		if err != nil {
			k.state = err.Error()
		}
		close(k.exited)
	}()
	return k, nil
}

// close ends the interpreter. Processes it started keep running.
func (k *kernel) close() {
	_ = k.stdin.Close()
	_ = k.cmd.Process.Kill()
}

// drain empties what the reader queued, which also frees it if it is waiting
// to queue more.
func (k *kernel) drain() {
	for {
		select {
		case <-k.out:
		default:
			return
		}
	}
}

// holdBack splits output into what can be shown now and a tail that may be
// the start of the end marker.
func holdBack(b, end []byte) (show, keep []byte) {
	for n := min(len(b), len(end)-1); n > 0; n-- {
		if bytes.HasPrefix(end, b[len(b)-n:]) {
			return b[:len(b)-n], b[len(b)-n:]
		}
	}
	return b, nil
}

// run executes one cell and returns its output, shaped like a one-shot
// interpreter's: a nonzero status ends it with "error: exit status N".
func (k *kernel) run(ctx context.Context, ks *kernels, session, runID, code string, chunk func(string)) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, grace := ks.limits()
	k.drain()
	k.busy.Store(true)
	defer func() {
		k.busy.Store(false)
		k.drain()
	}()

	var out bytes.Buffer
	emit := func(b []byte) {
		if len(b) == 0 {
			return
		}
		out.Write(b)
		if chunk != nil {
			chunk(string(b))
		}
	}
	if k.fresh {
		k.fresh = false
		emit([]byte(freshSession + "\n"))
	}
	nonce := ids.New()
	req, _ := json.Marshal(map[string]any{"n": nonce, "code": code, "env": map[string]string{"SILO_RUN_ID": runID}})
	// A failed write means the interpreter is gone; exited reports it.
	_, _ = k.stdin.Write(append(req, '\n'))

	end := []byte("\x00SILO-CELL-END " + nonce + " ")
	var pend []byte
	done := ctx.Done()
	var kill <-chan time.Time
	interrupted, killed := false, false
	for {
		select {
		case b := <-k.out:
			pend = append(pend, b...)
			if i := bytes.Index(pend, end); i >= 0 {
				emit(pend[:i])
				pend = pend[i:]
				rest := pend[len(end):]
				j := bytes.IndexByte(rest, 0)
				if j < 0 {
					continue // the status is still on its way
				}
				status, _ := strconv.Atoi(string(rest[:j]))
				switch {
				case interrupted:
					fmt.Fprintf(&out, "\nerror: interrupted (%v)", ctx.Err())
				case status != 0:
					fmt.Fprintf(&out, "\nerror: exit status %d", status)
				}
				return out.String()
			}
			show, keep := holdBack(pend, end)
			emit(show)
			pend = keep
		case <-k.exited:
			// The reader may still hold the interpreter's last words.
			for quiet := false; !quiet; {
				select {
				case b := <-k.out:
					pend = append(pend, b...)
				case <-time.After(100 * time.Millisecond):
					quiet = true
				}
			}
			emit(pend)
			if killed {
				fmt.Fprintf(&out, "\nerror: killed (%v); the Python session is gone", ctx.Err())
			} else {
				fmt.Fprintf(&out, "\nerror: the Python session ended (%s)", k.state)
			}
			ks.drop(session, k)
			return out.String()
		case <-done:
			done = nil
			interrupted = true
			_ = k.cmd.Process.Signal(os.Interrupt)
			kill = time.After(grace)
		case <-kill:
			kill = nil
			killed = true
			_ = k.cmd.Process.Kill()
		}
	}
}
