package app_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/app/egress"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
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
	// The gateway decrypts HTTPS: the box is given its CA to trust.
	var ca db.EgressCA
	h.DB.First(&ca)
	if net.CA == "" || net.CA != string(ca.CertPEM) {
		t.Fatal("the box was not given the gateway's certificate")
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
	startBot(t, h, bot.Id)
	const real = "ghp_REALVALUE11111111111111"

	add, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "GITHUB_TOKEN", Value: real,
		Mode: "surrogate", Hosts: []string{"api.github.com", "raw.githubusercontent.com"},
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

// A surrogate is only a stand-in where the gateway swaps it back. A box the
// filter does not stand in front of gets the real value: with the filter off,
// or a box created before it was turned on.
func TestEgressUnfilteredBoxGetsTheRealValue(t *testing.T) {
	const real = "ghp_REALVALUE11111111111111"
	for _, tc := range []struct {
		name string
		yaml string
	}{
		{"filter off", "\negress:\n  enabled: false\n"},
		{"box not on its network", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.New(t, apptest.WithYAML(apptest.DefaultYAML(t.TempDir())+tc.yaml))
			bot := h.CreateBot("Plainly")
			startBot(t, h, bot.Id)
			if tc.yaml == "" {
				// A box made the old way: running, but on no client network.
				h.Fake.SetBoxNetworks(bot.Id, nil)
			}
			if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
				BotId: bot.GetId(), Name: "GITHUB_TOKEN", Value: real,
				Mode: "surrogate", Hosts: []string{"api.github.com"},
			})); err != nil {
				t.Fatal(err)
			}
			h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
				BotId: bot.GetId(), Connector: "secrets", Action: "GITHUB_TOKEN", Decision: "allow",
			}))
			got, err := h.WorkerClient(bot.GetId()).GetSecret(h.Ctx(), connect.NewRequest(&v1.SecretReq{Name: "GITHUB_TOKEN"}))
			if err != nil {
				t.Fatal(err)
			}
			if got.Msg.GetValue() != real {
				t.Fatalf("value = %q, want the real one: nothing would swap a stand-in back", got.Msg.GetValue())
			}
		})
	}
}

// An allowed host must be a host pattern, and never a public suffix: the real
// value would go to every site below it.
func TestEgressSurrogateHostsAreChecked(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Hosts")
	for i, host := range []string{"com", "co.uk", "*.github.io", "github.io", "*.githubusercontent.com", "not a host", "*.com"} {
		_, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
			BotId: bot.GetId(), Name: "BAD_" + string(rune('A'+i)), Value: "ghp_REALVALUE11111111111111",
			Mode: "surrogate", Hosts: []string{"api.github.com", host},
		}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("host %q: err = %v, want refused", host, err)
		}
	}
	for i, host := range []string{"api.github.com", "*.api.example.com", "someone.github.io", "10.0.0.5"} {
		if _, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
			BotId: bot.GetId(), Name: "GOOD_" + string(rune('A'+i)), Value: "ghp_REALVALUE11111111111111",
			Mode: "surrogate", Hosts: []string{host},
		})); err != nil {
			t.Errorf("host %q refused: %v", host, err)
		}
	}
}

// Renaming a secret moves its rule with it.
func TestEgressRenameSecretMovesItsRule(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Rename")
	add, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "OLD", Value: "value",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.SetRule(h.Ctx(), connect.NewRequest(&v1.SetRuleRequest{
		BotId: bot.GetId(), Connector: "secrets", Action: "OLD", Decision: "allow",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Client.UpdateSecret(h.Ctx(), connect.NewRequest(&v1.UpdateSecretRequest{
		BotId: bot.GetId(), Id: add.Msg.GetId(), Name: strptr("NEW"),
	})); err != nil {
		t.Fatal(err)
	}
	var rules []db.Rule
	h.DB.Where("bot_id = ? AND connector = ?", bot.GetId(), "secrets").Find(&rules)
	if len(rules) != 1 || rules[0].Action != "NEW" || rules[0].Decision != "allow" {
		t.Fatalf("rules = %+v, want one allow rule on NEW", rules)
	}
}

// A gateway container left behind with no row (a fresh database) holds a
// token nobody knows: it is replaced, never reused.
func TestEgressLeftoverGatewayIsReplaced(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Leftover")
	startBot(t, h, bot.Id)
	h.DB.Where("1 = 1").Delete(&db.EgressHost{})

	if err := h.App.Egress.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	var row db.EgressHost
	if err := h.DB.First(&row).Error; err != nil {
		t.Fatalf("no gateway row: %v", err)
	}
	token := ""
	for _, e := range h.Fake.EgressEnv() {
		if v, ok := strings.CutPrefix(e, "SILO_EGRESS_TOKEN="); ok {
			token = v
		}
	}
	if token == "" || ids.Hash(token) != row.TokenHash {
		t.Fatal("the gateway runs with a token the row does not know")
	}
	if h.Fake.EgressUps.Load() != 2 {
		t.Fatalf("gateway started %d times, want a second, fresh one", h.Fake.EgressUps.Load())
	}
	if !h.Fake.EgressAttached(h.Fake.BotNet(bot.Id).Network) {
		t.Fatal("the new gateway is not attached to the client network")
	}
}

// Clients starting at once make one gateway, and a reconcile running
// alongside never drops a network that is being set up.
func TestEgressConcurrentClientsShareOneGateway(t *testing.T) {
	h := apptest.New(t)
	var bots []string
	for i := 0; i < 6; i++ {
		bots = append(bots, h.CreateBot("Racer").Id)
	}
	var wg sync.WaitGroup
	for _, id := range bots {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := h.App.Egress.ClientNetwork(context.Background(), "bot", id, "", "Racer"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			h.App.Egress.Reconcile(context.Background())
		}()
	}
	wg.Wait()
	if n := h.Fake.EgressUps.Load(); n != 1 {
		t.Fatalf("gateway started %d times, want once", n)
	}
	for _, id := range bots {
		if !h.Fake.EgressAttached("silo-egress-bot-" + id) {
			t.Fatalf("bot %s lost its network", id)
		}
	}
}

// Swapping into bodies is the owner's per-secret choice: off unless asked for,
// and an unrelated edit keeps it.
func TestEgressSwapBodyIsPerSecret(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Bodies")
	add, err := h.Client.AddSecret(h.Ctx(), connect.NewRequest(&v1.AddSecretRequest{
		BotId: bot.GetId(), Name: "TOKEN", Value: "ghp_REALVALUE11111111111111", Hosts: []string{"api.github.com"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if add.Msg.GetSwapBody() {
		t.Fatal("a new secret swaps into bodies without being asked")
	}
	on := true
	up, err := h.Client.UpdateSecret(h.Ctx(), connect.NewRequest(&v1.UpdateSecretRequest{
		BotId: bot.GetId(), Id: add.Msg.GetId(), SwapBody: &on,
	}))
	if err != nil || !up.Msg.GetSwapBody() {
		t.Fatalf("turning it on: %v %+v", err, up.Msg)
	}
	up, err = h.Client.UpdateSecret(h.Ctx(), connect.NewRequest(&v1.UpdateSecretRequest{
		BotId: bot.GetId(), Id: add.Msg.GetId(), Name: strptr("TOKEN2"),
	}))
	if err != nil || !up.Msg.GetSwapBody() {
		t.Fatalf("a rename dropped the switch: %v %+v", err, up.Msg)
	}
}

// Rotating the CA strands a box that trusts the old one (the gateway signs
// with the new one at once), so the next start makes the box again with the
// new certificate, as a Container reset would; the token is fresh too.
func TestEgressRotateRecreatesTheBoxOnStart(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Rotated")
	startBot(t, h, bot.Id)
	before := h.Fake.BotNet(bot.Id)
	if before == nil || before.CA == "" {
		t.Fatal("box was created without the gateway's certificate")
	}
	oldToken := h.Fake.Token(bot.Id)

	// An unchanged CA leaves the running box alone.
	startBot(t, h, bot.Id)
	if n := h.Fake.Creates.Load(); n != 1 {
		t.Fatalf("creates = %d before rotation, want 1", n)
	}

	if _, err := h.Client.RotateEgressCA(h.Ctx(), connect.NewRequest(&v1.RotateEgressCARequest{})); err != nil {
		t.Fatalf("RotateEgressCA: %v", err)
	}
	startBot(t, h, bot.Id)

	if n := h.Fake.Creates.Load(); n != 2 {
		t.Fatalf("creates = %d after rotation, want 2", n)
	}
	after := h.Fake.BotNet(bot.Id)
	var ca db.EgressCA
	h.DB.First(&ca)
	if after == nil || after.CA != string(ca.CertPEM) || after.CA == before.CA {
		t.Fatal("the recreated box does not trust the rotated certificate")
	}
	if h.Fake.Token(bot.Id) == oldToken {
		t.Fatal("the recreated box kept the old token")
	}
	var row db.Bot
	h.DB.First(&row, "id = ?", bot.Id)
	if row.TokenHash != ids.Hash(h.Fake.Token(bot.Id)) || row.ContainerID == "" {
		t.Fatalf("bot row = token %q container %q", row.TokenHash, row.ContainerID)
	}

	// And once made again, it is not made a third time.
	startBot(t, h, bot.Id)
	if n := h.Fake.Creates.Load(); n != 2 {
		t.Fatalf("creates = %d after a second start, want 2", n)
	}
}

// A box is told the gateway's address once, at create. A gateway made again
// (removed by hand, a cleanup, a lost row) must come back at that address on
// every client network, or every existing box loses DNS and its way out.
func TestEgressGatewayKeepsItsAddressWhenRecreated(t *testing.T) {
	h := apptest.New(t)
	bot := h.CreateBot("Pinned")
	startBot(t, h, bot.Id)
	net := h.Fake.BotNet(bot.Id)
	var row db.EgressClient
	h.DB.Where("kind = ? AND bot_id = ?", "bot", bot.Id).First(&row)
	if want := egress.GatewayAddr(row.Subnet); net.DNS != want {
		t.Fatalf("gateway at %q, want the fixed %q", net.DNS, want)
	}

	var host db.EgressHost
	h.DB.First(&host)
	h.Fake.DropEgress(context.Background(), host.ContainerID)
	if err := h.App.Egress.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	var gw db.EgressHost
	h.DB.First(&gw)
	addr, err := h.Fake.NetworkAddr(context.Background(), gw.ContainerID, net.Network)
	if err != nil || addr != net.DNS {
		t.Fatalf("recreated gateway at %q (%v), the box still uses %q", addr, err, net.DNS)
	}
}
