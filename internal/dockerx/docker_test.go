package dockerx

import (
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
