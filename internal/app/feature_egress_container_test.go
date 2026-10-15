package app_test

import (
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
)

// TestContainerEgressBoxIsIsolated proves the wiring against a real engine: a
// Bot's box is on its own internal network, its resolver is the gateway, and
// DNS answers with the gateway (so every connection is intercepted).
func TestContainerEgressBoxIsIsolated(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("Egress")
	h.StartSeededBot(id)

	var row db.EgressClient
	if err := h.DB.Where("bot_id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("no egress client row: %v", err)
	}
	gid, err := h.Host.EgressID(h.Ctx())
	if err != nil || gid == "" {
		t.Fatalf("no gateway: %v", err)
	}
	addr, err := h.Host.NetworkAddr(h.Ctx(), gid, row.Network)
	if err != nil || addr == "" {
		t.Fatalf("the gateway is not on %s: %v", row.Network, err)
	}

	cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{
		Command: "cat /etc/resolv.conf; echo ---; getent hosts example.com; echo ---; ip -o addr show scope global",
	}}}
	out, err := h.App.Hub.Exec(h.Ctx(), id, cmd)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(out, "nameserver "+addr) {
		t.Fatalf("the box's resolver is not the gateway %s:\n%s", addr, out)
	}
	// Dialing the gateway is the only route out, so the name resolves to it.
	if !strings.Contains(out, addr) || !strings.Contains(out, "example.com") {
		t.Fatalf("example.com did not resolve through the gateway:\n%s", out)
	}
	// The box has no other network: only the gateway's subnet is on it.
	global := out[strings.LastIndex(out, "---")+3:]
	if strings.Contains(global, "host.containers.internal") {
		t.Fatalf("the box sees a host route):\n%s", global)
	}
}
