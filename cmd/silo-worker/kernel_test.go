package main

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func kernelWorker(t *testing.T) *worker {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	w := &worker{workspace: t.TempDir()}
	t.Cleanup(w.kernels.closeAll)
	return w
}

func cell(t *testing.T, w *worker, session, code string) string {
	t.Helper()
	out, err := w.python(context.Background(), "run1", session, code, nil)
	if err != nil {
		t.Fatalf("python: %v\n%s", err, out)
	}
	return out
}

// What one call defines, the next call in the same conversation still has.
func TestKernelKeepsStateBetweenCalls(t *testing.T) {
	w := kernelWorker(t)
	first := cell(t, w, "chat1", "import json\nrows = [1, 2, 3]\nprint('loaded', len(rows))")
	if first != freshSession+"\nloaded 3\n" {
		t.Fatalf("first cell %q", first)
	}
	if got := cell(t, w, "chat1", "print(json.dumps(rows), sum(rows))"); got != "[1, 2, 3] 6\n" {
		t.Fatalf("second cell %q", got)
	}
	// Another conversation has its own interpreter.
	other := cell(t, w, "chat2", "print('rows' in globals())")
	if other != freshSession+"\nFalse\n" {
		t.Fatalf("other session %q", other)
	}
}

// Without a session the call is one throwaway interpreter, as before.
func TestPythonWithoutSessionIsOneShot(t *testing.T) {
	w := kernelWorker(t)
	if got := cell(t, w, "", "x = 5\nprint(x)"); got != "5\n" {
		t.Fatalf("one-shot %q", got)
	}
	if got := cell(t, w, "", "print('x' in globals())"); got != "False\n" {
		t.Fatalf("one-shot kept state: %q", got)
	}
}

// A failing cell returns its traceback and a nonzero status; the session and
// everything defined before the failure survive.
func TestKernelSurvivesAFailingCell(t *testing.T) {
	w := kernelWorker(t)
	cell(t, w, "s", "kept = 41")
	out := cell(t, w, "s", "kept += 1\nraise ValueError('boom')")
	for _, want := range []string{"Traceback", "raise ValueError('boom')", "ValueError: boom", "error: exit status 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("failing cell lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `File "<string>"`) {
		t.Fatalf("the traceback shows the driver's own frames:\n%s", out)
	}
	if got := cell(t, w, "s", "print(kept)"); got != "42\n" {
		t.Fatalf("state after a failure %q", got)
	}
	out = cell(t, w, "s", "import sys\nprint('bye')\nsys.exit(3)")
	if !strings.Contains(out, "bye\n") || !strings.Contains(out, "error: exit status 3") {
		t.Fatalf("sys.exit %q", out)
	}
	if got := cell(t, w, "s", "print(kept)"); got != "42\n" {
		t.Fatalf("state after sys.exit %q", got)
	}
}

// Each cell runs for its own run: a call from Python reaches the Control Plane
// under the run that made it.
func TestKernelSetsTheRunPerCell(t *testing.T) {
	w := kernelWorker(t)
	code := "import os\nprint(os.environ.get('SILO_RUN_ID'))"
	if out, _ := w.python(context.Background(), "runA", "s", code, nil); !strings.HasSuffix(out, "runA\n") {
		t.Fatalf("first run %q", out)
	}
	if out, _ := w.python(context.Background(), "runB", "s", code, nil); out != "runB\n" {
		t.Fatalf("second run %q", out)
	}
	if out, _ := w.python(context.Background(), "", "s", code, nil); out != "None\n" {
		t.Fatalf("no run %q", out)
	}
}

// Output streams while the cell runs, stdout and stderr in the order written,
// and the end marker never reaches the caller.
func TestKernelStreamsOutput(t *testing.T) {
	w := kernelWorker(t)
	cell(t, w, "s", "pass")
	var mu sync.Mutex
	var early string
	done := make(chan string, 1)
	go func() {
		out, _ := w.python(context.Background(), "r", "s",
			"import sys, time\nprint('one')\nprint('two', file=sys.stderr)\ntime.sleep(1.5)\nsys.stdout.write('three')", func(s string) {
				mu.Lock()
				early += s
				mu.Unlock()
			})
		done <- out
	}()
	time.Sleep(700 * time.Millisecond)
	mu.Lock()
	sofar := early
	mu.Unlock()
	if sofar != "one\ntwo\n" {
		t.Fatalf("streamed so far %q", sofar)
	}
	if out := <-done; out != "one\ntwo\nthree" {
		t.Fatalf("full output %q", out)
	}
	if strings.Contains(early, "SILO") || strings.Contains(early, "\x00") {
		t.Fatalf("the end marker leaked: %q", early)
	}
}

// Stop interrupts the cell, not the session.
func TestKernelCancelInterruptsTheCell(t *testing.T) {
	w := kernelWorker(t)
	cell(t, w, "s", "kept = 7")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out, err := w.python(ctx, "r", "s", "import time\nprint('start', flush=True)\ntime.sleep(60)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("cancel took %s", time.Since(start))
	}
	if !strings.Contains(out, "start\n") || !strings.Contains(out, "KeyboardInterrupt") || !strings.Contains(out, "error: interrupted") {
		t.Fatalf("interrupted cell %q", out)
	}
	if got := cell(t, w, "s", "print(kept)"); got != "7\n" {
		t.Fatalf("state after an interrupt %q", got)
	}
}

// A cell that will not stop takes the interpreter with it, and the next call
// says its state is gone.
func TestKernelKilledWhenACellIgnoresTheInterrupt(t *testing.T) {
	w := kernelWorker(t)
	w.kernels.grace = 300 * time.Millisecond
	cell(t, w, "s", "kept = 7")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	out, err := w.python(ctx, "r", "s", "import signal, time\nsignal.signal(signal.SIGINT, signal.SIG_IGN)\nprint('deaf', flush=True)\ntime.sleep(60)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deaf\n") || !strings.Contains(out, "error: killed") {
		t.Fatalf("killed cell %q", out)
	}
	if got := cell(t, w, "s", "print('kept' in globals())"); got != freshSession+"\nFalse\n" {
		t.Fatalf("after a kill %q", got)
	}
}

// An interpreter that dies under a cell reports it and is replaced.
func TestKernelReplacedAfterItDies(t *testing.T) {
	w := kernelWorker(t)
	cell(t, w, "s", "kept = 7")
	out := cell(t, w, "s", "import os\nprint('going', flush=True)\nos._exit(7)")
	if !strings.Contains(out, "going\n") || !strings.Contains(out, "error: the Python session ended (exit status 7)") {
		t.Fatalf("dying cell %q", out)
	}
	if got := cell(t, w, "s", "print('alive')"); got != freshSession+"\nalive\n" {
		t.Fatalf("after a death %q", got)
	}
}

// The cell's stdin is empty, never the pipe the worker talks to it on.
func TestKernelStdinIsEmpty(t *testing.T) {
	w := kernelWorker(t)
	cell(t, w, "s", "pass")
	out := cell(t, w, "s", "try:\n    input()\nexcept EOFError:\n    print('eof')")
	if out != "eof\n" {
		t.Fatalf("input() %q", out)
	}
	if got := cell(t, w, "s", "print('still here')"); got != "still here\n" {
		t.Fatalf("after input() %q", got)
	}
}

// Idle interpreters are closed, and no more than the cap are kept.
func TestKernelsAreReapedAndCapped(t *testing.T) {
	w := kernelWorker(t)
	w.kernels.max = 2
	cell(t, w, "a", "v = 'a'")
	cell(t, w, "b", "v = 'b'")
	cell(t, w, "c", "v = 'c'") // evicts a, the least recently used
	if n := w.kernels.count(); n != 2 {
		t.Fatalf("%d kernels, want 2", n)
	}
	if got := cell(t, w, "b", "print(v)"); got != "b\n" {
		t.Fatalf("b lost its state: %q", got)
	}
	if got := cell(t, w, "a", "print('v' in globals())"); got != freshSession+"\nFalse\n" {
		t.Fatalf("evicted session %q", got)
	}
	w.kernels.reap(time.Now().Add(2 * kernelIdle))
	if n := w.kernels.count(); n != 0 {
		t.Fatalf("%d kernels after the idle sweep", n)
	}
}

func TestSplitAtMarker(t *testing.T) {
	end := []byte("\x00SILO-CELL-END n1 ")
	for _, tc := range []struct{ in, out, rest string }{
		{"hello", "hello", ""},
		{"hello\n", "hello\n", ""},
		{"hello\x00", "hello", "\x00"},
		{"hello\n\x00SILO-CE", "hello\n", "\x00SILO-CE"},
		{"a\x00b\n\x00x", "a\x00b\n\x00x", ""},
		{"", "", ""},
	} {
		out, rest := holdBack([]byte(tc.in), end)
		if string(out) != tc.out || string(rest) != tc.rest {
			t.Errorf("holdBack(%q) = %q, %q; want %q, %q", tc.in, out, rest, tc.out, tc.rest)
		}
	}
}
