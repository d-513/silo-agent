package app_test

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/config"
	"silo.agent/internal/dockerx"
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
models:
  - dummy/echo
providers:
  dummy:
    api_key: test
search:
  engine: duckduckgo_scraper
`, port, dataDir, dockerHost, port, apptest.BotImage())
}

func newContainerHarness(t *testing.T) *apptest.H {
	t.Helper()
	dataDir := t.TempDir()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	return apptest.New(t,
		apptest.WithListener(ln),
		apptest.WithYAML(containerYAML(t, dataDir, port)),
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
