package egress

import (
	"testing"

	"silo.agent/internal/config"
	"silo.agent/internal/db"
	"silo.agent/internal/db/dbtest"
)

func withPool(pool string) func() config.Config {
	return func() config.Config { return config.Config{Egress: config.Egress{SubnetPool: pool}} }
}

func TestCarveTakesTheLowestFreeSubnet(t *testing.T) {
	s := New(nil, nil, nil, withPool("10.213.0.0/16"))
	got, err := s.carve(map[string]bool{"10.213.0.0/24": true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.213.1.0/24" {
		t.Fatalf("carve = %s, want 10.213.1.0/24", got)
	}
}

func TestCarveExhaustsThePool(t *testing.T) {
	s := New(nil, nil, nil, withPool("10.213.0.0/23"))
	_, err := s.carve(map[string]bool{"10.213.0.0/24": true, "10.213.1.0/24": true})
	if err == nil {
		t.Fatal("want a pool-exhausted error")
	}
}

func TestRenderApplyCarriesClientsListsAndCA(t *testing.T) {
	gdb := dbtest.New(t)
	cfg := func() config.Config {
		return config.Config{
			CPURL: "http://silo.example:8080",
			Egress: config.Egress{
				Block: []string{"evil.example"},
				Trust: []string{"internal.example"},
			},
		}
	}
	s := New(gdb, nil, nil, cfg)
	gdb.Create(&db.EgressClient{ID: "c1", Kind: "bot", BotID: "b1", Name: "One", Subnet: "10.213.0.0/24", Network: "n1"})
	gdb.Create(&db.Secret{ID: "s1", BotID: "b1", Name: "API", Value: "sekret"})

	apply, err := s.renderApply()
	if err != nil {
		t.Fatal(err)
	}
	if len(apply.Clients) != 1 || apply.Clients[0].Subnet != "10.213.0.0/24" || apply.Clients[0].Kind != "bot" {
		t.Fatalf("clients = %+v", apply.Clients)
	}
	if len(apply.Bots) != 1 || apply.Bots[0].BotId != "b1" {
		t.Fatalf("bots = %+v", apply.Bots)
	}
	if got := apply.Bots[0].PlainSecrets; len(got) != 1 || got[0] != "sekret" {
		t.Fatalf("plain secrets = %v", got)
	}
	if apply.CpAddr != "silo.example:8080" {
		t.Fatalf("cp_addr = %q", apply.CpAddr)
	}
	if apply.Operator.GetBlock()[0] != "evil.example" {
		t.Fatalf("operator block = %v", apply.Operator.GetBlock())
	}
	if len(apply.Feeds) != len(config.DefaultEgressFeeds) {
		t.Fatalf("feeds = %d, want the defaults", len(apply.Feeds))
	}
	if len(apply.CaCert) == 0 || len(apply.CaKey) == 0 {
		t.Fatal("no CA in the apply")
	}
	// The CA is generated once and reused.
	again, err := s.renderApply()
	if err != nil {
		t.Fatal(err)
	}
	if string(again.CaCert) != string(apply.CaCert) {
		t.Fatal("the CA changed between applies")
	}
}
