package app_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/config"
	"silo.agent/internal/dockerx"
	"silo.agent/internal/ids"
	"silo.agent/internal/llm"
	"silo.agent/internal/llm/dummy"
)

func botReq(id string) *connect.Request[v1.GetBotRequest] {
	return connect.NewRequest(&v1.GetBotRequest{Id: id})
}

// TestMain sweeps any zztest container left behind by an interrupted run, so a
// crashed test never leaves a Bot box consuming resources. It also lets the
// test binary act as a STDIO MCP child when the real bridge spawns it.
func TestMain(m *testing.M) {
	if apptest.RunMCPHelper() {
		os.Exit(0)
	}
	apptest.SweepTestContainers()
	code := m.Run()
	apptest.SweepTestContainers()
	os.Exit(code)
}

// containerYAML binds the CP on all interfaces so the Bot can reach it through
// host.containers.internal, and points data_dir at a temp dir.
func containerYAML(t *testing.T, dataDir string, port int) string {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///run/user/1000/podman/podman.sock"
	}
	return fmt.Sprintf(`http_addr: "0.0.0.0:%d"
data_dir: %q
docker_host: %s
cp_url: http://host.containers.internal:%d
bot_image: %s
model: dummy/echo
model_title: dummy/echo
embedding_model: dummy/embed
models:
  - dummy/echo
providers:
  dummy:
    api_key: test
search:
  engine: duckduckgo_scraper
`, port, dataDir, dockerHost, port, apptest.BotImage())
}

func newContainerHarness(t *testing.T, extraYAML ...string) *apptest.H {
	t.Helper()
	dataDir := t.TempDir()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return apptest.New(t,
		apptest.WithListener(ln),
		apptest.WithYAML(containerYAML(t, dataDir, port)+strings.Join(extraYAML, "")),
		apptest.WithHostFactory(func(store *config.Store) (dockerx.Host, error) {
			return dockerx.New(store)
		}),
	)
}

// TestContainerWorkerFileTool boots a real Bot image, waits for its worker to
// dial the CP, and round-trips a shell command through the tunnel.
func TestContainerWorkerFileTool(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	dummy.Reset()
	dummy.Script("Test_40",
		dummy.Turn{ToolCalls: []llm.ToolCall{{Name: "terminal", Arguments: `{"command":"echo container-hello"}`}}},
		dummy.Turn{Text: "Test_40_Output"},
	)
	h := newContainerHarness(t)
	id := h.SeedBot("Container")
	h.StartSeededBot(id)

	chat := h.FirstChat(id)
	runID, _ := h.Send(id, chat, "Test_40_Input")
	h.WaitRun(runID)

	var result string
	for _, ev := range h.Events(runID) {
		if ev.Kind == "tool_result" {
			result = ev.Body
		}
	}
	if !strings.Contains(result, "container-hello") {
		t.Fatalf("tool result %q", result)
	}
	if !strings.Contains(h.RunBody(runID), "Test_40_Output") {
		t.Fatalf("body %q", h.RunBody(runID))
	}
}

// TestContainerLifecycle checks stop/start/reset semantics against the real
// engine: stop keeps the box, reset removes it.
func TestContainerLifecycle(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("Lifecycle")
	h.StartSeededBot(id)

	if _, err := h.Client.StopBot(h.Ctx(), botReq(id)); err != nil {
		t.Fatalf("StopBot: %v", err)
	}
	h.WaitBotStatus(id, "stopped")

	// The container still exists after stop, just stopped.
	if _, err := h.Client.StartBot(h.Ctx(), botReq(id)); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
	h.WaitWorker(id, 60*time.Second)

	if _, err := h.Client.ResetContainer(h.Ctx(), botReq(id)); err != nil {
		t.Fatalf("ResetContainer: %v", err)
	}
	// Inspect by name: after reset the stored id is cleared and the box gone.
	if _, err := h.Host.Inspect(h.Ctx(), dockerx.Name(id)); err == nil {
		t.Fatal("container still exists after reset")
	}
}

// TestContainerTunnel is the whole feature against a real box: a web server
// started inside the Bot's container is fetched through its tunnel address.
func TestContainerTunnel(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t, "tunnels:\n  host: tunnels.test\n")
	id := h.SeedBot("Tunneled")
	h.StartSeededBot(id)

	// A service on the box's own localhost, in the background.
	start := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{
		Command: "mkdir -p /tmp/site && echo tunnel-works > /tmp/site/hello.txt && cd /tmp/site && (setsid python3 -m http.server 8123 --bind 127.0.0.1 >/dev/null 2>&1 &) ; sleep 1; echo started",
	}}}
	if out, err := h.App.Hub.Exec(h.Ctx(), id, start); err != nil || !strings.Contains(out, "started") {
		t.Fatalf("start service: %q, %v", out, err)
	}

	tun, err := h.Client.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 8123, Public: true}))
	if err != nil {
		t.Fatal(err)
	}
	cp := strings.TrimPrefix(h.URL, "http://")
	hc := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, cp)
		},
	}}
	deadline := time.Now().Add(20 * time.Second)
	var body string
	for {
		res, err := hc.Get("http://" + tun.Msg.GetName() + ".tunnels.test/hello.txt")
		if err == nil {
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode == 200 {
				body = string(b)
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("tunnel never served the file (last err %v)", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if strings.TrimSpace(body) != "tunnel-works" {
		t.Fatalf("body %q", body)
	}

	// Nothing listens on 8124: a clear 502, and the tunnel row is untouched.
	closed, err := h.Client.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 8124, Public: true}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := hc.Get("http://" + closed.Msg.GetName() + ".tunnels.test/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatalf("closed port status %d, want 502", res.StatusCode)
	}
}
