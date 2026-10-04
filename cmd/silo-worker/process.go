package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"silo.agent/internal/desktop"
)

func (w *worker) childEnv(runID string) []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "SILO_BOT_TOKEN=") || strings.HasPrefix(e, "SILO_CP_URL=") || strings.HasPrefix(e, "SILO_RUN_ID=") || strings.HasPrefix(e, "PYTHONPATH=") {
			continue
		}
		out = append(out, e)
	}
	sock := os.Getenv("SILO_WORKER_SOCK")
	if sock == "" {
		sock = "/var/run/silo/worker.sock"
	}
	out = append(out, "SILO_WORKER_SOCK="+sock, "PYTHONPATH=/opt/silo")
	if runID != "" {
		out = append(out, "SILO_RUN_ID="+runID)
	}
	return out
}

func (w *worker) shell(ctx context.Context, runID, command string, chunk func(string)) (string, error) {
	return w.runCmd(ctx, runID, chunk, "/bin/sh", "-lc", command)
}

func (w *worker) python(ctx context.Context, runID, code string, chunk func(string)) (string, error) {
	if desktop.WantsChromium(code) {
		if _, err := w.ensureChrome(ctx); err != nil {
			return "", err
		}
	}
	return w.runCmd(ctx, runID, chunk, "python3", "-c", code)
}

func (w *worker) runCmd(ctx context.Context, runID string, chunk func(string), name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = w.workspace
	c.Env = w.childEnv(runID)
	var buf bytes.Buffer
	c.Stdout = &tee{w: &buf, f: chunk}
	c.Stderr = c.Stdout
	err := c.Run()
	// A nonzero exit is a result, not a transport failure: the traceback in
	// the buffer is what the model needs; err.Error() alone is "exit status 1".
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		fmt.Fprintf(&buf, "\nerror: %v", err)
		if ctx.Err() != nil {
			fmt.Fprintf(&buf, " (%v)", ctx.Err())
		}
		return buf.String(), nil
	}
	return buf.String(), err
}

type tee struct {
	w io.Writer
	f func(string)
}

func (t *tee) Write(p []byte) (int, error) {
	if t.f != nil {
		t.f(string(p))
	}
	return t.w.Write(p)
}
