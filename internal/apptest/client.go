package apptest

import (
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/gen/silo/v1/silov1connect"
	"silo.agent/internal/auth"
	"silo.agent/internal/db"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
	"silo.agent/internal/rpcx"
)

// --- worker/bridge subprocess ----------------------------------------------

// StartBridge launches the real bridge process with the exact env the CP put
// on the sidecar spec. It returns a stop function and is also registered with
// t.Cleanup. Safe to call from the App's goroutine: it reports with Errorf,
// never Fatalf.
func StartBridge(t *testing.T, spec dockerx.StdioSpec) func() {
	if specEnvValue(spec, "SILO_CP_URL") == "" || specEnvValue(spec, "SILO_MCP_CMD") == "" {
		return func() {}
	}
	bin, err := bridgeBin.build()
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

func specEnvValue(spec dockerx.StdioSpec, key string) string {
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}

// --- clients and waiters ---------------------------------------------------

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
	return silov1connect.NewBotWorkerClient(&http.Client{}, h.URL, connect.WithInterceptors(rpcx.Bearer(tok)))
}

// SignedInUser creates a non-admin user and signs them in. It returns their UI
// client and the HTTP client that holds their session cookie, for tests of what
// one user must not reach in another's Bot.
func (h *H) SignedInUser(email string) (silov1connect.UIClient, *http.Client) {
	h.T.Helper()
	hash, err := auth.HashPassword("pw")
	if err != nil {
		h.T.Fatal(err)
	}
	h.DB.Create(&db.User{ID: ids.New(), Email: email, PasswordHash: hash})
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	cl := silov1connect.NewUIClient(hc, h.URL)
	if _, err := cl.SignIn(h.Ctx(), connect.NewRequest(&v1.SignInRequest{Email: email, Password: "pw"})); err != nil {
		h.T.Fatalf("sign in %s: %v", email, err)
	}
	return cl, hc
}

// Browser is someone who has not signed in: a UI client and the HTTP client
// whose cookie jar it shares.
func (h *H) Browser() (silov1connect.UIClient, *http.Client) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	return silov1connect.NewUIClient(hc, h.URL), hc
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
