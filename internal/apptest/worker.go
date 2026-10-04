package apptest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// builtBinary is a cmd/ program built once per test binary into the OS temp dir.
type builtBinary struct {
	name string // short name, for the file and error text
	pkg  string // import path to build
	once sync.Once
	path string
	err  error
}

var (
	workerBin = &builtBinary{name: "worker", pkg: "silo.agent/cmd/silo-worker"}
	bridgeBin = &builtBinary{name: "bridge", pkg: "silo.agent/cmd/silo-mcp-bridge"}
)

func (b *builtBinary) build() (string, error) {
	b.once.Do(func() {
		root, err := repoRoot()
		if err != nil {
			b.err = err
			return
		}
		out := filepath.Join(os.TempDir(), fmt.Sprintf("silo-%s-test-%d", b.name, os.Getpid()))
		cmd := exec.Command("go", "build", "-o", out, b.pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if o, err := cmd.CombinedOutput(); err != nil {
			b.err = fmt.Errorf("build %s: %v\n%s", b.name, err, o)
			return
		}
		b.path = out
	})
	return b.path, b.err
}

// WorkerBinary builds cmd/silo-worker once per test binary and caches the
// binary in the OS temp dir. It is the fast "real worker, no container" seam.
func WorkerBinary(t *testing.T) string {
	t.Helper()
	bin, err := workerBin.build()
	if err != nil {
		t.Fatalf("worker binary: %v", err)
	}
	return bin
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate apptest source")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("go.mod not found above %s", file)
}

// Worker is a running silo-worker subprocess connected to the harness CP.
type Worker struct {
	Workspace string
	Token     string
	cmd       *exec.Cmd
}

// StartWorker launches the real worker binary against the harness CP and waits
// for it to attach. The bot must already exist (CreateBot stores its token in
// the fake host). Cleanup kills the process group so shell children die too.
func (h *H) StartWorker(botID string) *Worker {
	h.T.Helper()
	if h.Fake == nil {
		h.T.Fatal("StartWorker needs the fake host to recover the container token")
	}
	token := h.Fake.Token(botID)
	if token == "" {
		h.T.Fatalf("no container token recorded for bot %s", botID)
	}
	ws := h.T.TempDir()
	scratch := h.T.TempDir()
	sockDir := h.T.TempDir()
	sock := filepath.Join(sockDir, "worker.sock")

	cmd := exec.Command(WorkerBinary(h.T))
	cmd.Env = append(cleanEnv(), []string{
		"SILO_CP_URL=" + h.URL,
		"SILO_BOT_TOKEN=" + token,
		"SILO_WORKSPACE=" + ws,
		"SILO_WORKER_SOCK=" + sock,
		"SILO_TOOLS_DIR=" + filepath.Join(scratch, "tools"),
		"SILO_SKILLS_DIR=" + filepath.Join(scratch, "skills"),
	}...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var log strings.Builder
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		h.T.Fatalf("start worker: %v", err)
	}
	w := &Worker{Workspace: ws, Token: token, cmd: cmd}
	h.T.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
	})

	deadline := time.Now().Add(15 * time.Second)
	for !h.App.Hub.Connected(botID) {
		if time.Now().After(deadline) {
			h.T.Fatalf("worker never connected\n%s\nlogs:\n%s", h.URL, log.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	return w
}

// cleanEnv returns the process environment with Silo and Docker variables
// stripped so the subprocess cannot inherit the developer's real config.
func cleanEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SILO_") || strings.HasPrefix(kv, "DOCKER_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
