package apptest

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/dockerx"
)

// --- worker/bridge subprocess ----------------------------------------------

var (
	bridgeOnce sync.Once
	bridgePath string
	bridgeErr  error
)

// BridgeBinary builds cmd/silo-mcp-bridge once per test binary.
func BridgeBinary(t *testing.T) string {
	t.Helper()
	bin, err := bridgeBinaryOnce()
	if err != nil {
		t.Fatalf("bridge binary: %v", err)
	}
	return bin
}

// StartBridge launches the real bridge process with the exact env the CP put
// on the sidecar spec. It returns a stop function and is also registered with
// t.Cleanup. Safe to call from the App's goroutine: it reports with Errorf,
// never Fatalf.
func StartBridge(t *testing.T, spec dockerx.StdioSpec) func() {
	if specEnvValue(spec, "SILO_CP_URL") == "" || specEnvValue(spec, "SILO_MCP_CMD") == "" {
		return func() {}
	}
	bin, err := bridgeBinaryOnce()
	if err != nil {
		t.Errorf("bridge binary: %v", err)
		return func() {}
	}
	cmd := exec.Command(bin)
	cmd.Env = append(cleanEnv(), spec.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Errorf("start bridge: %v", err)
		return func() {}
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				_, _ = cmd.Process.Wait()
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func bridgeBinaryOnce() (string, error) {
	bridgeOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			bridgeErr = err
			return
		}
		out := filepath.Join(os.TempDir(), fmt.Sprintf("silo-bridge-test-%d", os.Getpid()))
		cmd := exec.Command("go", "build", "-o", out, "silo.agent/cmd/silo-mcp-bridge")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			bridgeErr = fmt.Errorf("build bridge: %v\n%s", err, b)
			return
		}
		bridgePath = out
	})
	return bridgePath, bridgeErr
}

func specEnvValue(spec dockerx.StdioSpec, key string) string {
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}

// --- clients and waiters ---------------------------------------------------

// TokenAuth sets a bearer token on every request. Used for the worker and
// bridge clients, mirroring the container worker.
type TokenAuth struct{ Token string }

func (t TokenAuth) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+t.Token)
		return next(ctx, req)
	}
}

func (t TokenAuth) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+t.Token)
		return conn
	}
}

func (t TokenAuth) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// WorkerClient returns a BotWorker client authenticated as the bot's worker.
// It needs the fake host to have captured the container token at Create.
func (h *H) WorkerClient(botID string) silov1connect.BotWorkerClient {
	h.T.Helper()
	if h.Fake == nil {
		h.T.Fatal("WorkerClient needs the fake host to recover the container token")
	}
	tok := h.Fake.Token(botID)
	if tok == "" {
		h.T.Fatalf("no container token recorded for bot %s (create the bot via the API first)", botID)
	}
	return silov1connect.NewBotWorkerClient(&http.Client{}, h.URL, connect.WithInterceptors(TokenAuth{Token: tok}))
}

// BridgeClient returns an MCPHost client authenticated as a sidecar.
func (h *H) BridgeClient(token string) silov1connect.MCPHostClient {
	return silov1connect.NewMCPHostClient(&http.Client{}, h.URL, connect.WithInterceptors(TokenAuth{Token: token}))
}

// WaitConnector blocks until the named bot connector leaves the initializing
// state and returns the row. The name must match exactly, so "Docs" and
// "Docs 2" do not collide.
func (h *H) WaitConnector(botID, name string) *v1.BotConnector {
	return h.waitConnector(botID, func(c *v1.BotConnector) bool {
		return c.GetConnector().GetName() == name
	})
}

// WaitConnectorID blocks until the attachment with the given id is ready.
func (h *H) WaitConnectorID(botID, id string) *v1.BotConnector {
	return h.waitConnector(botID, func(c *v1.BotConnector) bool { return c.GetId() == id })
}

func (h *H) waitConnector(botID string, match func(*v1.BotConnector) bool) *v1.BotConnector {
	h.T.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last *v1.BotConnector
	for {
		res, err := h.Client.ListBotConnectors(h.Ctx(), connect.NewRequest(&v1.ListBotConnectorsRequest{BotId: botID}))
		if err == nil {
			for _, c := range res.Msg.GetConnectors() {
				if !match(c) {
					continue
				}
				last = c
				if st := c.GetAuthStatus(); st != "initializing" && st != "" {
					return c
				}
			}
		}
		if time.Now().After(deadline) {
			h.T.Fatalf("connector for bot %s stuck: %+v", botID, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
