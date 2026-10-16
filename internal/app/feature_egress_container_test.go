package app_test

import (
	"strings"
	"testing"
	"time"

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

// TestContainerEgressBoxTrustsTheGateway proves the CA install against a real
// engine: the gateway decrypts HTTPS, and the system store (Python's ssl, as
// OpenSSL, git and Go read it), Python requests and Node all accept what it
// signs, while Chromium's NSS store holds the same CA. It needs
// the gateway to reach the internet.
func TestContainerEgressBoxTrustsTheGateway(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("Trust")
	h.StartSeededBot(id)

	script := strings.Join([]string{
		`python3 -c 'import socket, ssl; s = ssl.create_default_context().wrap_socket(socket.create_connection(("example.com", 443), 20), server_hostname="example.com"); print("issuer", s.getpeercert()["issuer"])'`,
		`python3 -c 'import requests; print("requests", requests.get("https://example.com/", timeout=20).status_code)'`,
		`node -e 'fetch("https://example.com/").then(r => console.log("node", r.status)).catch(e => { console.log("node error", e.cause || e); })'`,
		`certutil -L -d sql:$HOME/.pki/nssdb`,
	}, "; echo ---; ")
	cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: script}}}
	out, err := h.App.Hub.Exec(h.Ctx(), id, cmd)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	for _, want := range []string{"requests 200", "node 200"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: HTTPS through the gateway is not trusted:\n%s", want, out)
		}
	}
	// The certificate the box saw is the gateway's, not the site's: the
	// connection really was decrypted.
	if !strings.Contains(out, "('commonName', 'Silo network filter')") {
		t.Fatalf("the box did not see the gateway's certificate:\n%s", out)
	}
	if !strings.Contains(out, "Silo network filter                                          C,,") {
		t.Fatalf("the CA is not in Chromium's NSS store:\n%s", out)
	}
}

// TestContainerEgressOtherProtocols proves the SOCKS5 door end to end: ssh and
// proxychains4 in the box point at the gateway, a port that is not web traffic
// is refused while the Bot's Other protocols switch is off, and the same
// command reaches it once the owner turns the switch on (no restart). It needs
// the gateway to reach github.com:22.
func TestContainerEgressOtherProtocols(t *testing.T) {
	if !apptest.ContainersEnabled(t) {
		return
	}
	h := newContainerHarness(t)
	id := h.SeedBot("Socks")
	h.StartSeededBot(id)

	exec := func(command string) string {
		t.Helper()
		cmd := &v1.Cmd{Id: ids.New(), Body: &v1.Cmd_Terminal{Terminal: &v1.TerminalCmd{Command: command}}}
		out, err := h.App.Hub.Exec(h.Ctx(), id, cmd)
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		return out
	}

	setup := exec(`echo "proxy=$SILO_SOCKS_PROXY"; ssh -G github.com | grep -i '^proxycommand'`)
	if !strings.Contains(setup, "proxy=socks5h://") || !strings.Contains(setup, "nc -X 5 -x") {
		t.Fatalf("the box is not pointed at the SOCKS door:\n%s", setup)
	}

	// An ssh banner through proxychains: python is dynamically linked, so the
	// connect is the proxy's, and the name goes to the gateway unresolved.
	probe := `proxychains4 python3 -c 'import socket; s = socket.create_connection(("github.com", 22), 15); print("banner", s.recv(64).decode(errors="replace").strip())' 2>&1`

	off := exec(probe)
	t.Logf("with the switch off the Bot reads:\n%s", off)
	if strings.Contains(off, "banner SSH-") {
		t.Fatalf("port 22 connected with Other protocols off:\n%s", off)
	}

	if err := h.DB.Model(&db.Bot{}).Where("id = ?", id).Update("other_protocols", true).Error; err != nil {
		t.Fatal(err)
	}
	h.App.Egress.Apply()
	var out string
	for i := 0; i < 10; i++ {
		if out = exec(probe); strings.Contains(out, "banner SSH-") {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("port 22 did not connect with Other protocols on:\n%s", out)
}
