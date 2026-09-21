package app_test

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/toolsgen"
)

// newStdioHarness binds the CP to a known loopback port and points cp_url at
// it, then runs the real bridge process for every STDIO sidecar the CP creates.
func newStdioHarness(t *testing.T) *apptest.H {
	t.Helper()
	dataDir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	raw := fmt.Sprintf(`http_addr: "127.0.0.1:%d"
data_dir: %q
cp_url: http://127.0.0.1:%d
model: dummy/echo
model_title: dummy/echo
models:
  - dummy/echo
providers:
  dummy:
    api_key: test
`, port, dataDir, port)
	return apptest.New(t, apptest.WithListener(ln), apptest.WithYAML(raw), apptest.WithStdioBridge())
}

// stdioChild makes the connector run this test binary as an MCP echo server
// over STDIO — the same binary the test is executing.
func stdioChild(t *testing.T) (string, []string, []*v1.EnvInput) {
	t.Helper()
	return os.Args[0], []string{"-test.run=^$"}, []*v1.EnvInput{{Name: apptest.MCPHelperEnv, Value: "echo"}}
}

func TestStdioConnectorEndToEnd(t *testing.T) {
	h := newStdioHarness(t)
	bot := h.CreateBot("Stdio")
	cmd, args, env := stdioChild(t)

	att, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args, Env: env, DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatalf("CreateBotConnector: %v", err)
	}
	row := h.WaitConnector(bot.GetId(), "Echo")
	if row.GetAuthStatus() != "authorized" {
		t.Fatalf("stdio auth %q error %q", row.GetAuthStatus(), row.GetLastError())
	}
	if h.Fake.StdioCreates.Load() < 1 {
		t.Fatal("sidecar was not created")
	}

	wc := h.WorkerClient(bot.GetId())
	res, err := wc.CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
		Connector: toolsgen.Slug("Echo"), Action: "echo", ArgsJson: `{"q":"hello-stdio"}`,
	}))
	if err != nil || res.Msg.GetError() != "" {
		t.Fatalf("CallTool: %v %+v", err, res.Msg)
	}
	if res.Msg.GetResultJson() != "hello-stdio" {
		t.Fatalf("result %q", res.Msg.GetResultJson())
	}

	if _, err := h.Client.DetachConnector(h.Ctx(), connect.NewRequest(&v1.DetachConnectorRequest{
		BotId: bot.GetId(), Id: att.Msg.GetId(),
	})); err != nil {
		t.Fatal(err)
	}
	if h.Fake.StdioDrops.Load() < 1 {
		t.Fatal("sidecar was not dropped")
	}
}

func TestStdioSidecarsArePerAttachment(t *testing.T) {
	h := newStdioHarness(t)
	bot := h.CreateBot("PerAttach")
	cmd, args, env := stdioChild(t)

	first, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Files MCP", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args, Env: env, DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Files MCP", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args, Env: env, DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitConnectorID(bot.GetId(), first.Msg.GetId())
	h.WaitConnectorID(bot.GetId(), second.Msg.GetId())

	var r1, r2 db.BotConnector
	h.DB.First(&r1, "id = ?", first.Msg.GetId())
	h.DB.First(&r2, "id = ?", second.Msg.GetId())
	if r1.ContainerID == "" || r1.ContainerID == r2.ContainerID {
		t.Fatalf("shared sidecar %q %q", r1.ContainerID, r2.ContainerID)
	}
	if h.Fake.StdioCreates.Load() < 2 {
		t.Fatalf("sidecar creates %d", h.Fake.StdioCreates.Load())
	}
}

func TestStdioMissingSecretFails(t *testing.T) {
	h := newStdioHarness(t)
	bot := h.CreateBot("MissingSecret")
	cmd, args, _ := stdioChild(t)

	att, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Sec", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args,
		Env: []*v1.EnvInput{{Name: "TOKEN", Secret: "missing"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	row := h.WaitConnector(bot.GetId(), "Sec")
	if row.GetAuthStatus() != "error" || !strings.Contains(row.GetLastError(), "secret") {
		t.Fatalf("expected missing-secret error, got %q %q", row.GetAuthStatus(), row.GetLastError())
	}
	_ = att
}

func TestStdioEnvMasked(t *testing.T) {
	h := newStdioHarness(t)
	bot := h.CreateBot("Masked")
	cmd, args, _ := stdioChild(t)

	att, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "GH", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args, DefaultMode: "allow",
		Env: []*v1.EnvInput{{Name: "TOKEN", Value: "plaintextsecretvalue"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	h.WaitConnector(bot.GetId(), "GH")
	if got := h.App.Mask(bot.GetId()).Apply("token=plaintextsecretvalue"); !strings.Contains(got, "***") {
		t.Fatalf("secret not masked: %q", got)
	}
	_ = att
}
