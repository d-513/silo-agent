package app_test

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
)

// startBot starts a bot through the public API (no worker needed to inspect the
// box Docker was asked to create).
func startBot(t *testing.T, h *apptest.H, id string) {
	t.Helper()
	if _, err := h.Client.StartBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: id})); err != nil {
		t.Fatalf("StartBot: %v", err)
	}
}

// A Bot's box is created on its own internal network, with the gateway the only
// way out: its DNS and its control-plane link both point at the gateway.
func TestEgressBoxGetsItsOwnNetwork(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Filtered")
	startBot(t, h, bot.Id)

	net := h.Fake.BotNet(bot.Id)
	if net == nil {
		t.Fatal("box was created without an egress network")
	}
	if want := "silo-egress-bot-" + bot.Id; net.Network != want {
		t.Fatalf("network = %q, want %q", net.Network, want)
	}
	if net.DNS == "" {
		t.Fatal("no gateway address on the client network")
	}
	if !strings.HasPrefix(net.CPURL, "http://"+net.DNS+":") {
		t.Fatalf("CPURL = %q, want the gateway at %s", net.CPURL, net.DNS)
	}
	if !h.Fake.EgressAttached(net.Network) {
		t.Fatal("the gateway is not attached to the client network")
	}
	var row db.EgressClient
	if err := h.DB.Where("kind = ? AND bot_id = ?", "bot", bot.Id).First(&row).Error; err != nil {
		t.Fatalf("no egress client row: %v", err)
	}
	if row.Subnet == "" || row.Network != net.Network {
		t.Fatalf("row = %+v", row)
	}
	if subnet := h.Fake.NetworkSubnet(net.Network); subnet != row.Subnet {
		t.Fatalf("network subnet = %q, row subnet = %q", subnet, row.Subnet)
	}
	// The gateway is the one container, created lazily for the first client.
	if h.Fake.EgressUps.Load() == 0 {
		t.Fatal("the gateway was never started")
	}
}

// Each Bot gets a subnet of its own from the pool.
func TestEgressSubnetsAreDistinct(t *testing.T) {
	h := apptest.New(t)
	a := h.CreateBot("One")
	b := h.CreateBot("Two")
	startBot(t, h, a.Id)
	startBot(t, h, b.Id)

	na, nb := h.Fake.BotNet(a.Id), h.Fake.BotNet(b.Id)
	if na == nil || nb == nil {
		t.Fatalf("missing networks: %+v %+v", na, nb)
	}
	if na.Network == nb.Network {
		t.Fatal("two bots share a network")
	}
	if h.Fake.NetworkSubnet(na.Network) == h.Fake.NetworkSubnet(nb.Network) {
		t.Fatalf("two bots share a subnet: %s", h.Fake.NetworkSubnet(na.Network))
	}
	if h.Fake.EgressUps.Load() != 1 {
		t.Fatalf("gateway started %d times, want once", h.Fake.EgressUps.Load())
	}
}

// With the filter off a box is created the old way: no network spec, no
// gateway.
func TestEgressOffMeansNoGateway(t *testing.T) {
	yaml := apptest.DefaultYAML(t.TempDir()) + "\negress:\n  enabled: false\n"
	h := apptest.New(t, apptest.WithYAML(yaml))
	bot := h.CreateBot("Unfiltered")
	startBot(t, h, bot.Id)

	if net := h.Fake.BotNet(bot.Id); net != nil {
		t.Fatalf("box got a network with the filter off: %+v", net)
	}
	if h.Fake.EgressUps.Load() != 0 {
		t.Fatal("the gateway was started with the filter off")
	}
}

func strptr(s string) *string { return &s }

// A surrogate secret: the box only ever sees the stand-in, and the stand-in is
// stable. Switching to plain hands the box the real value.
func TestEgressSurrogateSecretIsWhatTheBoxSees(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Surrogates")
	const real = "ghp_REALVALUE11111111111111"

	add, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "GITHUB_TOKEN", Value: real,
		Mode: "surrogate", Hosts: []string{"api.github.com", "*.githubusercontent.com"},
	}))
	if err != nil {
		t.Fatalf("AddSecret: %v", err)
	}
	if add.Msg.GetMode() != "surrogate" || !add.Msg.GetSurrogate() {
		t.Fatalf("secret meta = %+v", add.Msg)
	}
	if len(add.Msg.GetHosts()) != 2 {
		t.Fatalf("hosts = %v", add.Msg.GetHosts())
	}

	// Reading a secret is Ask by default; allow it so nothing waits on a vote.
	h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "secrets", Action: "GITHUB_TOKEN", Decision: "allow",
	}))

	wc := h.WorkerClient(bot.GetId())
	got, err := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "GITHUB_TOKEN"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetValue() == real {
		t.Fatal("the box got the real value of a surrogate secret")
	}
	if !strings.HasPrefix(got.Msg.GetValue(), "ghp_") || len(got.Msg.GetValue()) != len(real) {
		t.Fatalf("stand-in = %q, want a ghp_ token of the same length", got.Msg.GetValue())
	}
	again, _ := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "GITHUB_TOKEN"}))
	if again.Msg.GetValue() != got.Msg.GetValue() {
		t.Fatalf("the stand-in changed between reads: %q then %q", got.Msg.GetValue(), again.Msg.GetValue())
	}

	if _, err := h.Client.UpdateSecret(h.Ctx(), connect.NewRequest(&v1.UpdateSecretRequest{
		BotId: bot.GetId(), Id: add.Msg.GetId(), Mode: strptr("plain"),
	})); err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	plain, _ := wc.GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "GITHUB_TOKEN"}))
	if plain.Msg.GetValue() != real {
		t.Fatalf("plain secret = %q, want the real value", plain.Msg.GetValue())
	}
}

// A surrogate needs hosts and a value long enough to stand in for.
func TestEgressSurrogateNeedsHosts(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Strict")
	_, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "NO_HOSTS", Value: "ghp_REALVALUE11111111111111", Mode: "surrogate",
	}))
	if err == nil {
		t.Fatal("a surrogate secret with no hosts was accepted")
	}
	_, err = h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "SHORT", Value: "abc", Mode: "surrogate", Hosts: []string{"x.example"},
	}))
	if err == nil {
		t.Fatal("a too-short surrogate was accepted")
	}
}

// ${secrets.NAME} in a connector resolves to the Bot's own secret on the
// connection path, so a connector never needs its value written into it.
func TestEgressConnectorSecretReference(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Connectors")
	// No hosts: a plain secret, so the real value is what resolves.
	if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "API_KEY", Value: "s3cr3t-value",
	})); err != nil {
		t.Fatal(err)
	}
	c := &db.Connector{
		BotID:       bot.GetId(),
		HTTPURL:     "https://api.example.com/mcp",
		HeadersJSON: `{"Authorization":"Bearer ${secrets.API_KEY}"}`,
	}
	got := h.App.Connectors.ResolveConnector(c)
	var hdr map[string]string
	if err := json.Unmarshal([]byte(got.HeadersJSON), &hdr); err != nil {
		t.Fatal(err)
	}
	if hdr["Authorization"] != "Bearer s3cr3t-value" {
		t.Fatalf("header = %q, want the secret resolved", hdr["Authorization"])
	}
	// The stored row keeps the placeholder.
	if !strings.Contains(c.HeadersJSON, "${secrets.API_KEY}") {
		t.Fatalf("the row was rewritten: %q", c.HeadersJSON)
	}
}

// Deleting a Bot takes its client network with it.
func TestEgressDeleteBotDropsNetwork(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Temporary")
	startBot(t, h, bot.Id)
	net := h.Fake.BotNet(bot.Id)
	if net == nil {
		t.Fatal("no network")
	}
	if _, err := h.Client.DeleteBot(h.Ctx(), connect.NewRequest(&v1.GetBotRequest{Id: bot.Id})); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	if h.Fake.EgressAttached(net.Network) {
		t.Fatal("the gateway is still attached to a deleted Bot's network")
	}
	var n int64
	h.DB.Model(&db.EgressClient{}).Where("bot_id = ?", bot.Id).Count(&n)
	if n != 0 {
		t.Fatalf("%d egress client rows survive the Bot", n)
	}
}
