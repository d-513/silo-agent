package dockerx

import (
	"testing"

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

func TestImageIDIgnoresTheAlgorithm(t *testing.T) {
	if imageID("sha256:abc") != imageID("abc") || imageID(" sha256:abc ") != "abc" {
		t.Fatal("ids differ only by their algorithm prefix")
	}
	if !local("localhost/silo-bot:dev") || local("ghcr.io/d-513/silo-bot:v0.4.2") {
		t.Fatal("local image test")
	}
}
