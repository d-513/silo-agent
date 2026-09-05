package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silo.agent/internal/masker"
)

func TestCancelJob(t *testing.T) {
	w := &worker{pending: map[string]*job{}, mask: masker.New()}
	ctx, cancel := context.WithCancel(context.Background())
	w.pending["a"] = &job{cancel: cancel}
	if !w.cancelJob("a") {
		t.Fatal("miss")
	}
	if ctx.Err() == nil {
		t.Fatal("not canceled")
	}
	if w.cancelJob("missing") {
		t.Fatal("ghost")
	}
}

func TestChildEnvRunID(t *testing.T) {
	t.Setenv("SILO_BOT_TOKEN", "secret-token-value")
	t.Setenv("SILO_CP_URL", "http://cp")
	w := &worker{mask: masker.New()}
	env := w.childEnv("run-1")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SILO_BOT_TOKEN=") || strings.Contains(joined, "SILO_CP_URL=") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "SILO_RUN_ID=run-1") {
		t.Fatal(joined)
	}
}

func TestSendSerializes(t *testing.T) {
	w := &worker{pending: map[string]*job{}}
	started := make(chan struct{})
	done := make(chan struct{})
	w.sendMu.Lock()
	go func() {
		close(started)
		w.sendMu.Lock()
		w.sendMu.Unlock()
		close(done)
	}()
	<-started
	select {
	case <-done:
		t.Fatal("send was not serialized")
	default:
	}
	w.sendMu.Unlock()
	<-done
}

func TestListDirAndBrowse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := &worker{workspace: dir}
	raw, err := w.listDir("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"name":"hello.txt"`) || !strings.Contains(raw, `"name":"sub"`) {
		t.Fatal(raw)
	}
	if _, err := w.listDir("../etc"); err == nil {
		t.Fatal("escaped")
	}
	view, err := w.browseFile("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view, `"content":"hi"`) {
		t.Fatal(view)
	}
}
