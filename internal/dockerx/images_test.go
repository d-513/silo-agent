package dockerx

import (
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"

	"silo.agent/internal/config"
)

func TestSpecOfFollowsTheBotsLimits(t *testing.T) {
	cfg := config.Config{BotLimits: config.BotLimits{MemoryMB: 2048, Pids: 4096}}
	a := specOf("bot", cfg)
	cfg.BotLimits.MemoryMB = 4096
	if b := specOf("bot", cfg); a == b || a == "" {
		t.Fatalf("a new memory limit left the box's spec at %q", b)
	}
	cfg.BotLimits.MemoryMB = 1
	if specOf("egress", cfg) != specOf("egress", config.Config{}) {
		t.Fatal("the gateway's spec moved with the Bots' limits")
	}
	if specOf("drive-helper", cfg) != "" || specOf("", cfg) != "" {
		t.Fatal("a role with no generation carries a spec")
	}
}

// What dials the control plane itself is made again when where it dials
// moves: the network and the URL are fixed at create.
func TestSpecOfFollowsTheControlPlanesNetwork(t *testing.T) {
	host := config.Config{CPURL: "http://host.containers.internal:8080"}
	net := config.Config{CPURL: "http://silo:8080", CPNetwork: "siloback"}
	for _, role := range []string{"egress", "drive"} {
		if specOf(role, host) != specs[role] {
			t.Fatalf("%s: a control plane on the host changed the spec to %q", role, specOf(role, host))
		}
		if specOf(role, net) == specOf(role, host) {
			t.Fatalf("%s: joining the control plane's network left the spec alone", role)
		}
	}
	if specOf("bot", net) != specOf("bot", host) {
		t.Fatal("a Bot's box, which dials the gateway, moved with the control plane's network")
	}
}

// reachCP puts a container on the control plane's network when it has one,
// and gives it the host's name otherwise.
func TestReachCP(t *testing.T) {
	at := func(network string) *container.HostConfig {
		t.Setenv("SILO_CP_NETWORK", network)
		store, err := config.LoadPath(filepath.Join(t.TempDir(), "silo.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		h := &container.HostConfig{}
		(&Engine{store: store}).reachCP(h)
		return h
	}
	if h := at(""); h.NetworkMode != "" || len(h.ExtraHosts) != 1 {
		t.Fatalf("on the host: network %q hosts %v", h.NetworkMode, h.ExtraHosts)
	}
	if h := at("siloback"); h.NetworkMode != "siloback" || len(h.ExtraHosts) != 0 {
		t.Fatalf("on its network: network %q hosts %v", h.NetworkMode, h.ExtraHosts)
	}
}

func TestImageIDIgnoresTheAlgorithm(t *testing.T) {
	if imageID("sha256:abc") != imageID("abc") || imageID(" sha256:abc ") != "abc" {
		t.Fatal("ids differ only by their algorithm prefix")
	}
	if !local("localhost/silo-bot:dev") || local("ghcr.io/d-513/silo-bot:v0.4.2") {
		t.Fatal("local image test")
	}
}
