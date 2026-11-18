package app_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/toolsgen"
)

// containers lists a Bot's containers by kind, as the Containers tab does.
func containers(t *testing.T, h *apptest.H, botID string) map[string]*v1.BotContainer {
	t.Helper()
	res, err := h.Client.ListBotContainers(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: botID}))
	if err != nil {
		t.Fatalf("ListBotContainers: %v", err)
	}
	out := map[string]*v1.BotContainer{}
	for _, c := range res.Msg.Containers {
		out[c.Kind] = c
	}
	return out
}

// A new image (a release, a rebuilt tag) never pulls a running box or its
// drive sidecar out from under the Bot: the Containers tab says an update is
// waiting, and both are made again from the new image at the next start.
func TestOutdatedBoxAndDrivesWaitForTheNextStart(t *testing.T) {
	h := driveHarness(t)
	bot := h.CreateBot("Upgraded")
	startBot(t, h, bot.Id)
	draft, _ := saveDrive(h, &v1.SaveDriveRequest{BotId: bot.Id, Template: "s3", Draft: true, Options: map[string]string{"access_key_id": "A", "secret_access_key": "S"}})
	d, err := saveDrive(h, &v1.SaveDriveRequest{Id: draft.Id, Name: "files", Options: map[string]string{"access_key_id": "A"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	if got := containers(t, h, bot.Id); got["bot"].Outdated || got["drive"].Outdated {
		t.Fatalf("fresh containers read as outdated: %+v", got)
	}
	creates, driveCreates := h.Fake.Creates.Load(), h.Fake.DriveCreates.Load()
	oldToken := h.Fake.Token(bot.Id)

	h.Fake.SetImage("bot", "sha256:bot-2")
	h.Fake.SetImage("drive", "sha256:drive-2")

	// Running: left alone, and marked.
	startBot(t, h, bot.Id)
	if n := h.Fake.Creates.Load(); n != creates {
		t.Fatalf("a running box was made again (creates %d → %d)", creates, n)
	}
	got := containers(t, h, bot.Id)
	if !got["bot"].Outdated || !got["drive"].Outdated || got["bot"].State != "running" {
		t.Fatalf("outdated containers not marked: %+v", got)
	}

	// Stopped, then started: both made again, the box with a fresh token.
	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatal(err)
	}
	startBot(t, h, bot.Id)
	if n := h.Fake.Creates.Load(); n != creates+1 {
		t.Fatalf("creates = %d after the start, want %d", n, creates+1)
	}
	if h.Fake.Token(bot.Id) == oldToken {
		t.Fatal("the new box kept the old token")
	}
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	if n := h.Fake.DriveCreates.Load(); n != driveCreates+1 {
		t.Fatalf("drive sidecar creates = %d, want %d", n, driveCreates+1)
	}
	if got := containers(t, h, bot.Id); got["bot"].Outdated || got["drive"].Outdated {
		t.Fatalf("still outdated after the start: %+v", got)
	}

	// And once current, a stop and start only starts them.
	if _, err := h.Client.StopBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatal(err)
	}
	startBot(t, h, bot.Id)
	waitDrive(t, h, bot.Id, d.Id, "mounted")
	if h.Fake.Creates.Load() != creates+1 || h.Fake.DriveCreates.Load() != driveCreates+1 {
		t.Fatalf("current containers were made again: creates %d, drive %d", h.Fake.Creates.Load(), h.Fake.DriveCreates.Load())
	}
}

// The gateway holds nothing a Bot has open and keeps its address on every
// client network, so a new image replaces it at its next reconcile, running
// or not.
func TestOutdatedGatewayIsMadeAgain(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Filtered")
	startBot(t, h, bot.Id)
	net := h.Fake.BotNet(bot.Id)
	if net == nil || !h.Fake.EgressAttached(net.Network) {
		t.Fatal("no gateway on the box's network")
	}
	ups := h.Fake.EgressUps.Load()

	h.App.Egress.Reconcile(h.Ctx())
	if n := h.Fake.EgressUps.Load(); n != ups {
		t.Fatalf("a current gateway was made again (%d → %d)", ups, n)
	}

	h.Fake.SetImage("egress", "sha256:egress-2")
	h.App.Egress.Reconcile(h.Ctx())
	if n := h.Fake.EgressUps.Load(); n != ups+1 {
		t.Fatalf("gateway ups = %d, want %d", n, ups+1)
	}
	if !h.Fake.EgressAttached(net.Network) {
		t.Fatal("the new gateway is not on the box's network")
	}
	var row db.EgressHost
	h.DB.Limit(1).Find(&row)
	if id, _ := h.Fake.EgressID(h.Ctx()); row.ContainerID != id {
		t.Fatalf("gateway row %q, engine %q", row.ContainerID, id)
	}

	h.App.Egress.Reconcile(h.Ctx())
	if n := h.Fake.EgressUps.Load(); n != ups+1 {
		t.Fatalf("the new gateway was made again (ups %d)", n)
	}
}

// A STDIO sidecar holds only its MCP server, so an outdated one is made again
// the next time a session to it is made (after a control plane restart, or a
// session that ended), and the call goes through on the new one.
func TestOutdatedStdioSidecarIsMadeAgain(t *testing.T) {
	h := newStdioHarness(t)
	bot := h.CreateBot("Stdio")
	cmd, args, env := stdioChild(t)
	att, err := h.Client.CreateBotConnector(h.Ctx(), connect.NewRequest(&v1.CreateBotConnectorRequest{
		BotId: bot.GetId(), Name: "Echo", Transport: "stdio",
		StdioCommand: cmd, StdioArgs: args, Env: env, DefaultMode: "allow",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if row := h.WaitConnector(bot.GetId(), "Echo"); row.GetAuthStatus() != "authorized" {
		t.Fatalf("stdio auth %q error %q", row.GetAuthStatus(), row.GetLastError())
	}
	call := func() {
		t.Helper()
		res, err := h.WorkerClient(bot.GetId()).CallTool(h.Ctx(), connect.NewRequest(&v1.ToolReq{
			Connector: toolsgen.Slug("Echo"), Action: "echo", ArgsJson: `{"q":"hi"}`,
		}))
		if err != nil || res.Msg.GetError() != "" {
			t.Fatalf("CallTool: %v %+v", err, res.Msg)
		}
	}
	creates := h.Fake.StdioCreates.Load()

	h.Fake.SetImage("mcp-stdio", "sha256:stdio-2")
	if !containers(t, h, bot.GetId())["mcp"].Outdated {
		t.Fatal("outdated sidecar not marked")
	}
	h.App.Connectors.DropMCP(att.Msg.GetId())
	call()
	if n := h.Fake.StdioCreates.Load(); n != creates+1 {
		t.Fatalf("sidecar creates = %d, want %d", n, creates+1)
	}
	if containers(t, h, bot.GetId())["mcp"].Outdated {
		t.Fatal("still outdated after it was made again")
	}

	h.App.Connectors.DropMCP(att.Msg.GetId())
	call()
	if n := h.Fake.StdioCreates.Load(); n != creates+1 {
		t.Fatalf("a current sidecar was made again (creates %d)", n)
	}
}

// The control plane fetches the images it runs as it starts, so the first
// Bot started after an upgrade does not wait on a download.
func TestImagesArePrefetchedAtStart(t *testing.T) {
	h := apptest.New(t)
	cfg := h.Store.Config()
	want := []string{cfg.BotImage, cfg.MCPStdioImage, cfg.DriveImage(), cfg.EgressImage()}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := h.Fake.Prefetched()
		if len(got) >= len(want) {
			if !slices.Equal(got[:len(want)], want) {
				t.Fatalf("prefetched %v, want %v", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("prefetched %v, want %v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
