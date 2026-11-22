package caddy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func render(site Site) string {
	raw, _ := json.Marshal(Config(site))
	return string(raw)
}

func TestConfig(t *testing.T) {
	if len(Config(Site{})) != 0 {
		t.Error("no host, yet something to serve")
	}
	base := Site{Host: "silo.example.com", Issuer: IssuerACME, Upstream: "127.0.0.1:8080", Ask: "http://127.0.0.1:8080/caddy/ask"}
	has := func(what, got string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %s missing from %s", what, w, got)
			}
		}
	}
	hasnt := func(what, got string, not ...string) {
		t.Helper()
		for _, w := range not {
			if strings.Contains(got, w) {
				t.Errorf("%s: %s in %s", what, w, got)
			}
		}
	}

	s := render(base)
	has("host only", s, `"host":["silo.example.com"]`, `"dial":"127.0.0.1:8080"`, `"module":"acme"`)
	hasnt("host only", s, "on_demand", `"module":"internal"`, "pki")

	// Let's Encrypt over HTTP has no wildcards: content names on demand.
	web := base
	web.ContentHost = "c.example.com"
	s = render(web)
	has("over HTTP", s, `"host":["*.c.example.com"]`, `"on_demand":true`, `"endpoint":"http://127.0.0.1:8080/caddy/ask"`, `"skip_certificates":["*.c.example.com"]`)

	// Through a DNS provider: one wildcard, no asking, the provider's own
	// field names.
	dns := web
	dns.Email = "ops@example.com"
	dns.DNS = &DNS{Provider: "cloudflare", Fields: map[string]string{"api_token": "tok", "zone_token": ""}}
	s = render(dns)
	has("DNS", s, `"subjects":["*.c.example.com"]`, `"dns":{"provider":{"api_token":"tok","name":"cloudflare"}}`, `"email":"ops@example.com"`)
	hasnt("DNS", s, "on_demand", "skip_certificates", "zone_token")

	// Caddy's own authority: wildcard from it, trust not installed in the
	// container.
	own := web
	own.Issuer = IssuerInternal
	s = render(own)
	has("internal", s, `"issuers":[{"module":"internal"}],"subjects":["*.c.example.com"]`, `"install_trust":false`)
	hasnt("internal", s, "on_demand", `"module":"acme"`)

	// The machine's own names: always the authority, and an address answers
	// a browser that names no server.
	lan := Site{Local: []string{"silo-box", "192.168.1.20"}, Upstream: "127.0.0.1:8080"}
	s = render(lan)
	has("local", s, `"host":["silo-box","192.168.1.20"]`, `"issuers":[{"module":"internal"}],"subjects":["silo-box","192.168.1.20"]`, `"default_sni":"192.168.1.20"`)
	both := base
	both.Local = lan.Local
	s = render(both)
	has("domain and local", s, `"host":["silo.example.com","silo-box","192.168.1.20"]`, `"module":"acme"`, `"module":"internal"`)
}

// Every provider Admin offers is compiled into silo-caddy, and nothing
// compiled in is missing from Admin.
func TestDNSProvidersAreBuilt(t *testing.T) {
	raw, err := os.ReadFile("../../images/caddy/Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	built := map[string]bool{}
	for _, m := range regexp.MustCompile(`--with github\.com/caddy-dns/([a-z0-9]+)(/v[0-9]+)?@`).FindAllStringSubmatch(string(raw), -1) {
		built[m[1]] = true
	}
	for _, p := range DNSProviders() {
		if !built[p.ID] {
			t.Errorf("%s is offered but not built into silo-caddy", p.ID)
		}
		delete(built, p.ID)
		if len(p.Settings) == 0 || p.Name == "" || p.Description == "" {
			t.Errorf("%s: name, description and settings", p.ID)
		}
	}
	for id := range built {
		t.Errorf("%s is built into silo-caddy but not offered", id)
	}
}

// TestSyncLoadsOnlyWhenChanged runs against a stand-in for Caddy's admin API
// on a unix socket, which checks the Host as Caddy does.
func TestSyncLoadsOnlyWhenChanged(t *testing.T) {
	dir, err := os.MkdirTemp("", "caddy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "admin.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var running []byte
	loads := 0
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1" {
			http.Error(w, `{"error":"host not allowed"}`, http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			if running == nil {
				w.Write([]byte("null\n"))
				return
			}
			w.Write(running)
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			running, _ = io.ReadAll(r.Body)
			loads++
		default:
			http.NotFound(w, r)
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	a, err := NewAdmin("unix/" + sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if changed, err := a.Sync(ctx, Config(Site{})); err != nil || changed {
		t.Fatalf("empty over empty: changed=%v err=%v", changed, err)
	}
	site := Config(Site{Host: "silo.example.com", ContentHost: "c.example.com", Upstream: "127.0.0.1:8080", Ask: "http://x/caddy/ask"})
	if changed, err := a.Sync(ctx, site); err != nil || !changed {
		t.Fatalf("first push: changed=%v err=%v", changed, err)
	}
	if changed, err := a.Sync(ctx, site); err != nil || changed || loads != 1 {
		t.Fatalf("again: changed=%v err=%v loads=%d", changed, err, loads)
	}
	if _, err := NewAdmin("localhost:2019"); err == nil {
		t.Error("an address with no scheme was taken")
	}
}
