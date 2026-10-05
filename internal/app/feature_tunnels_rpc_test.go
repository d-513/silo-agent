package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/llm/dummy"
)

const tunnelTestHost = "tunnels.test"

// tunnelHarness is a CP whose tunnels live under tunnels.test (plus any extra
// YAML), so a request with Host <name>.tunnels.test reaches the proxy.
func tunnelHarness(t *testing.T, extraYAML string) *apptest.H {
	t.Helper()
	dummy.Reset()
	dir := t.TempDir()
	return apptest.New(t, apptest.WithDataDir(dir),
		apptest.WithYAML(apptest.DefaultYAML(dir)+"tunnels:\n  host: "+tunnelTestHost+"\n"+extraYAML))
}

var nameRe = regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+$`)

func createTunnel(t *testing.T, h *apptest.H, botID string, port int32, public bool) *v1.Tunnel {
	t.Helper()
	res, err := h.Client.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: botID, Port: port, Public: public}))
	if err != nil {
		t.Fatalf("CreateTunnel(%d): %v", port, err)
	}
	return res.Msg
}

func tunnelList(t *testing.T, h *apptest.H, botID string) *v1.ListTunnelsResponse {
	t.Helper()
	res, err := h.Client.ListTunnels(h.Ctx(), connect.NewRequest(&v1.ListTunnelsRequest{BotId: botID}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg
}

func TestTunnelsListReportsStateAndDomain(t *testing.T) {
	// No tunnels.host and no public_url to derive one from.
	dummy.Reset()
	bare := apptest.New(t)
	id := bare.CreateBot("Bare").GetId()
	got := tunnelList(t, bare, id)
	if got.GetState() != "no_host" || got.GetHost() != "" {
		t.Fatalf("state=%q host=%q, want no_host", got.GetState(), got.GetHost())
	}
	if _, err := bare.Client.CreateTunnel(bare.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 8000})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("create without a domain: %v", err)
	}

	h := tunnelHarness(t, "")
	id = h.CreateBot("Domain").GetId()
	got = tunnelList(t, h, id)
	if got.GetState() != "ok" || got.GetHost() != tunnelTestHost || got.GetMax() != 20 || len(got.GetTunnels()) != 0 {
		t.Fatalf("list = %+v", got)
	}

	off := tunnelHarness(t, "  enabled: false\n")
	id = off.CreateBot("Off").GetId()
	if got := tunnelList(t, off, id); got.GetState() != "off" {
		t.Fatalf("state = %q, want off", got.GetState())
	}
	if _, err := off.Client.CreateTunnel(off.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 8000})); code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("create while off: %v", err)
	}
}

func TestTunnelCreateNamesAndUrls(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Namer").GetId()
	tun := createTunnel(t, h, id, 8000, false)
	if !nameRe.MatchString(tun.GetName()) {
		t.Fatalf("name %q is not adjective-colour-animal", tun.GetName())
	}
	if tun.GetPort() != 8000 || tun.GetPublic() || tun.GetCreatedBy() != "owner" || tun.GetId() == "" {
		t.Fatalf("tunnel = %+v", tun)
	}
	if want := "http://" + tun.GetName() + "." + tunnelTestHost; tun.GetUrl() != want {
		t.Fatalf("url %q, want %q", tun.GetUrl(), want)
	}
	other := createTunnel(t, h, id, 8001, true)
	if other.GetName() == tun.GetName() || !other.GetPublic() {
		t.Fatalf("second tunnel = %+v", other)
	}
	list := tunnelList(t, h, id).GetTunnels()
	if len(list) != 2 {
		t.Fatalf("list has %d", len(list))
	}
}

func TestTunnelCreateRefusals(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Refuser").GetId()
	createTunnel(t, h, id, 8000, false)
	for _, tc := range []struct {
		port int32
		want connect.Code
	}{
		{8000, connect.CodeAlreadyExists}, // one tunnel per port
		{0, connect.CodeInvalidArgument},
		{70000, connect.CodeInvalidArgument},
		{-5, connect.CodeInvalidArgument},
		{5900, connect.CodeInvalidArgument}, // x11vnc
		{9222, connect.CodeInvalidArgument}, // Chromium debugging
	} {
		_, err := h.Client.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: tc.port}))
		if code(err) != tc.want {
			t.Errorf("port %d: %v, want %v", tc.port, err, tc.want)
		}
	}
}

func TestTunnelCap(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Capped").GetId()
	for p := int32(8000); p < 8020; p++ {
		createTunnel(t, h, id, p, false)
	}
	if _, err := h.Client.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 9000})); code(err) != connect.CodeResourceExhausted {
		t.Fatalf("21st tunnel: %v", err)
	}
	// The cap is per Bot.
	other := h.CreateBot("Other").GetId()
	createTunnel(t, h, other, 8000, false)
}

func TestTunnelUpdateAndDelete(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Editor").GetId()
	tun := createTunnel(t, h, id, 8000, false)

	res, err := h.Client.UpdateTunnel(h.Ctx(), connect.NewRequest(&v1.UpdateTunnelRequest{BotId: id, Id: tun.GetId(), Public: true}))
	if err != nil || !res.Msg.GetPublic() || res.Msg.GetName() != tun.GetName() {
		t.Fatalf("UpdateTunnel = %+v, %v", res.Msg, err)
	}
	var row db.Tunnel
	h.DB.First(&row, "id = ?", tun.GetId())
	if !row.Public {
		t.Fatal("public was not stored")
	}

	if _, err := h.Client.DeleteTunnel(h.Ctx(), connect.NewRequest(&v1.DeleteTunnelRequest{BotId: id, Id: tun.GetId()})); err != nil {
		t.Fatal(err)
	}
	if n := len(tunnelList(t, h, id).GetTunnels()); n != 0 {
		t.Fatalf("%d tunnels after delete", n)
	}
	if _, err := h.Client.DeleteTunnel(h.Ctx(), connect.NewRequest(&v1.DeleteTunnelRequest{BotId: id, Id: tun.GetId()})); code(err) != connect.CodeNotFound {
		t.Fatalf("second delete: %v", err)
	}
	// The port is free to use again.
	createTunnel(t, h, id, 8000, false)
}

func TestTunnelRPCsAreOwnerOnly(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Mine").GetId()
	tun := createTunnel(t, h, id, 8000, false)
	eve, _ := h.SignedInUser("eve@test.local")

	if _, err := eve.ListTunnels(h.Ctx(), connect.NewRequest(&v1.ListTunnelsRequest{BotId: id})); code(err) != connect.CodeNotFound {
		t.Errorf("ListTunnels: %v", err)
	}
	if _, err := eve.CreateTunnel(h.Ctx(), connect.NewRequest(&v1.CreateTunnelRequest{BotId: id, Port: 8001})); code(err) != connect.CodeNotFound {
		t.Errorf("CreateTunnel: %v", err)
	}
	if _, err := eve.UpdateTunnel(h.Ctx(), connect.NewRequest(&v1.UpdateTunnelRequest{BotId: id, Id: tun.GetId(), Public: true})); code(err) != connect.CodeNotFound {
		t.Errorf("UpdateTunnel: %v", err)
	}
	if _, err := eve.DeleteTunnel(h.Ctx(), connect.NewRequest(&v1.DeleteTunnelRequest{BotId: id, Id: tun.GetId()})); code(err) != connect.CodeNotFound {
		t.Errorf("DeleteTunnel: %v", err)
	}
	// And a tunnel id from one Bot cannot be edited through another Bot's id.
	mine2 := h.CreateBot("Mine2").GetId()
	if _, err := h.Client.DeleteTunnel(h.Ctx(), connect.NewRequest(&v1.DeleteTunnelRequest{BotId: mine2, Id: tun.GetId()})); code(err) != connect.CodeNotFound {
		t.Errorf("cross-bot delete: %v", err)
	}
}

// Rows keep only the name: the URL follows the configured domain, so changing
// tunnels.host re-points every tunnel at once, with no restart.
func TestTunnelURLFollowsTheConfiguredDomain(t *testing.T) {
	// Settings edits need a silo.yaml to write.
	dummy.Reset()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "silo.yaml")
	if err := os.WriteFile(cfgPath, []byte(apptest.DefaultYAML(dir)+"tunnels:\n  host: "+tunnelTestHost+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := apptest.New(t, apptest.WithDataDir(dir), apptest.WithConfigPath(cfgPath))
	id := h.CreateBot("Mover").GetId()
	tun := createTunnel(t, h, id, 8000, false)

	if _, err := h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{
		Fields: map[string]string{"tunnels.host": "Other.Example.com", "tunnels.scheme": "https"},
	})); err != nil {
		t.Fatal(err)
	}
	got := tunnelList(t, h, id)
	if got.GetHost() != "other.example.com" || got.GetTunnels()[0].GetUrl() != "https://"+tun.GetName()+".other.example.com" {
		t.Fatalf("after PutSettings: host=%q url=%q", got.GetHost(), got.GetTunnels()[0].GetUrl())
	}
	settings, err := h.Client.GetSettings(h.Ctx(), connect.NewRequest(&v1.GetSettingsRequest{}))
	if err != nil || settings.Msg.GetTunnelsHost() != "other.example.com" {
		t.Fatalf("Settings.tunnels_host = %q, %v", settings.Msg.GetTunnelsHost(), err)
	}
	// A bad suffix is refused and leaves the good one in place.
	_, err = h.Client.PutSettings(h.Ctx(), connect.NewRequest(&v1.PutSettingsRequest{Fields: map[string]string{"tunnels.host": "https://x.example.com"}}))
	if code(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "tunnels.host") {
		t.Fatalf("bad host: %v", err)
	}
	if tunnelList(t, h, id).GetHost() != "other.example.com" {
		t.Fatal("a rejected value changed the host")
	}
}

func TestTunnelsGoWithTheirBot(t *testing.T) {
	h := tunnelHarness(t, "")
	id := h.CreateBot("Doomed").GetId()
	tun := createTunnel(t, h, id, 8000, false)
	h.DB.Create(&db.TunnelGrant{ID: "g1", TunnelID: tun.GetId(), UserID: "u"})
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
	var n int64
	h.DB.Model(&db.Tunnel{}).Where("bot_id = ?", id).Count(&n)
	var g int64
	h.DB.Model(&db.TunnelGrant{}).Where("tunnel_id = ?", tun.GetId()).Count(&g)
	if n != 0 || g != 0 {
		t.Fatalf("%d tunnels and %d grants survived DeleteBot", n, g)
	}
}
