package dockerx

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestCpuMemStats(t *testing.T) {
	s := container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 200, PercpuUsage: []uint64{1, 1}},
			SystemUsage: 400,
			OnlineCPUs:  2,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 100},
			SystemUsage: 200,
		},
		MemoryStats: container.MemoryStats{
			Usage: 300,
			Stats: map[string]uint64{"inactive_file": 50},
			Limit: 1000,
		},
	}
	if p := cpuPct(s); p != 100 {
		t.Fatalf("cpu %v", p)
	}
	if m := memUsed(s); m != 250 {
		t.Fatalf("mem %d", m)
	}
}

func TestEnvTokenHashEmpty(t *testing.T) {
	e := &Engine{}
	if _, err := e.EnvTokenHash(t.Context(), ""); !IsNotFound(err) {
		t.Fatalf("empty id: %v", err)
	}
}

func TestIsNotFoundEmpty(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Fatal("ErrNotFound")
	}
	if IsNotFound(nil) {
		t.Fatal("nil")
	}
}

func TestStdioName(t *testing.T) {
	if StdioName("abc") != "silo-mcp-abc" {
		t.Fatal(StdioName("abc"))
	}
}

// A Bot's machine runs on UTC whatever the host is set to: the human's zone is
// the Bot's (it follows them), never the box's.
func TestBotMachineIsUTC(t *testing.T) {
	env := botEnv("http://cp", "bot1", "tok", "")
	if !slices.Contains(env, "TZ=UTC") {
		t.Fatalf("env %v has no TZ=UTC", env)
	}
	if !slices.Contains(env, "SILO_BOT_TOKEN=tok") {
		t.Fatalf("env %v lost the bot token", env)
	}
	if slices.Contains(env, "SILO_EGRESS_DNS=") {
		t.Fatalf("env %v sets an egress resolver with the filter off", env)
	}
	on := botEnv("http://cp", "bot1", "tok", "10.0.0.2")
	if !slices.Contains(on, "SILO_EGRESS_DNS=10.0.0.2") {
		t.Fatalf("env %v has no egress resolver", on)
	}
}

// One Bot's sidecars must never share an npm directory with another's: npx
// runs what is in it.
func TestStdioCacheDirIsPerBot(t *testing.T) {
	a, b := stdioCacheDir("/data", "bot-a"), stdioCacheDir("/data", "bot-b")
	if a == "" || a == b {
		t.Fatalf("cache dirs %q %q", a, b)
	}
	if want := filepath.Join("/data", "bots", "bot-a", "mcp-npm"); a != want {
		t.Fatalf("cache dir %q, want %q", a, want)
	}
	for _, id := range []string{"", "..", "a/b", "../x", ".hidden"} {
		if got := stdioCacheDir("/data", id); got != "" {
			t.Fatalf("bot id %q got a cache dir %q", id, got)
		}
	}
	if got := stdioCacheDir("", "bot-a"); got != "" {
		t.Fatalf("no data dir, yet %q", got)
	}
}
