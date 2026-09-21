package dockerx

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestNamePrefix(t *testing.T) {
	if Name("abc") != "silo-abc" {
		t.Fatalf("Name %q", Name("abc"))
	}
}

func TestIsNotFoundWrapped(t *testing.T) {
	wrapped := fmt.Errorf("inspect: %w: id", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Fatal("wrapped ErrNotFound not detected")
	}
	if IsNotFound(fmt.Errorf("some other error")) {
		t.Fatal("unrelated error misclassified")
	}
}

func TestRemoveEmptyIDIsNoop(t *testing.T) {
	e := &Engine{}
	if err := e.Remove(context.Background(), ""); err != nil {
		t.Fatalf("Remove empty: %v", err)
	}
}

func TestCpuPctNoDelta(t *testing.T) {
	s := container.StatsResponse{
		CPUStats:    container.CPUStats{CPUUsage: container.CPUUsage{TotalUsage: 100}, SystemUsage: 200, OnlineCPUs: 1},
		PreCPUStats: container.CPUStats{CPUUsage: container.CPUUsage{TotalUsage: 100}, SystemUsage: 200},
	}
	if p := cpuPct(s); p != 0 {
		t.Fatalf("zero delta cpu %v", p)
	}
}
