package app_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/apptest"
	"silo.agent/internal/db"
	"silo.agent/internal/ids"
	"silo.agent/internal/siloosd"
)

// osRun stands in for the machine under a real siloosd: it answers the few
// commands these tests make and records them.
type osRun struct {
	mu    sync.Mutex
	calls []string
}

func (r *osRun) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.mu.Lock()
	r.calls = append(r.calls, line)
	r.mu.Unlock()
	switch {
	case line == "hostnamectl hostname":
		return []byte("silo-box\n"), nil
	case strings.HasPrefix(line, "hostnamectl set-hostname "), strings.HasPrefix(line, "systemctl "):
		return nil, nil
	case strings.HasPrefix(line, "timedatectl show"):
		return []byte("Timezone=UTC\nNTP=yes\nNTPSynchronized=yes\n"), nil
	case line == "bootc status --format=json":
		return []byte(`{"status":{"booted":{"image":{"image":{"image":"ghcr.io/d-513/silo-os:v1.0.0","transport":"registry"},"version":"v1.0.0","timestamp":"2026-11-01T00:00:00Z","imageDigest":"sha256:a"}}}}`), nil
	case line == "skopeo list-tags docker://ghcr.io/d-513/silo-os":
		return []byte(`{"Tags":["v0.9.0","v1.0.0","v1.1.0","latest","master"]}`), nil
	case strings.HasPrefix(line, "skopeo inspect --no-tags docker://ghcr.io/d-513/silo-os:"):
		tag := strings.TrimPrefix(line, "skopeo inspect --no-tags docker://ghcr.io/d-513/silo-os:")
		version, built := map[string]string{"latest": "v1.1.0", "master": "master-abc1234"}[tag], "2026-11-20T00:00:00Z"
		if version == "" {
			version = tag
		}
		if tag == "v0.9.0" {
			built = "2026-10-01T00:00:00Z"
		}
		return []byte(`{"Created":"` + built + `","Labels":{"org.opencontainers.image.version":"` + version + `"}}`), nil
	case strings.HasPrefix(line, "bootc switch "):
		return nil, nil
	}
	return nil, errors.New("unscripted: " + line)
}

func (r *osRun) ran(line string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c == line {
			return true
		}
	}
	return false
}

// siloOSD runs a real siloosd on a unix socket in a short temporary folder
// and answers its path.
func siloOSD(t *testing.T, run *osRun) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "siloos")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "siloosd.sock")
	ln, err := siloosd.Listen(path, -1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr/lib/siloos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr/lib/siloos/release"), []byte("SILOOS_VERSION=v1.0.0\nSILOOS_REPOSITORY=ghcr.io/d-513/silo-os\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := siloosd.New(siloosd.Config{Run: run, StateDir: t.TempDir(), Root: root, After: func(_ time.Duration, fn func()) { fn() }})
	done := make(chan struct{})
	go func() { siloosd.Serve(ctx, ln, srv, []int{os.Getuid()}); close(done) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	return path
}

// Off SiloOS the page is not there: GetSiloOS says so, and every other
// SiloOS call is refused without reaching anything.
func TestSiloOSIsOffElsewhere(t *testing.T) {
	h := apptest.New(t)
	st, err := h.Client.GetSiloOS(h.Ctx(), connect.NewRequest(&v1.GetSiloOSRequest{}))
	if err != nil || st.Msg.Available {
		t.Fatalf("GetSiloOS = %v, %v; want not available", st, err)
	}
	if _, err := h.Client.SiloOSPower(h.Ctx(), connect.NewRequest(&v1.PowerRequest{Action: "reboot"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("power off SiloOS: %v", err)
	}
}

// On SiloOS an admin reaches siloosd through the console; nobody else does.
func TestSiloOSIsTheAdminsOnly(t *testing.T) {
	run := &osRun{}
	sock := siloOSD(t, run)
	h := apptest.New(t, apptest.WithYAML("siloos:\n  socket: "+sock+"\n"))

	st, err := h.Client.GetSiloOS(h.Ctx(), connect.NewRequest(&v1.GetSiloOSRequest{}))
	if err != nil || !st.Msg.Available || st.Msg.Error != "" || st.Msg.System.GetHostname() != "silo-box" || st.Msg.System.GetImageVersion() != "v1.0.0" {
		t.Fatalf("GetSiloOS = %v, %v", st.Msg, err)
	}
	if run.ran("bootc status --format=json") {
		t.Error("the summary ran bootc (it audits a denied chcon each time)")
	}
	if _, err := h.Client.SetSiloOSHostname(h.Ctx(), connect.NewRequest(&v1.SetHostnameRequest{Hostname: "Silo-2"})); err != nil {
		t.Fatal(err)
	}
	if !run.ran("hostnamectl set-hostname silo-2") {
		t.Errorf("the host name was not set: %v", run.calls)
	}
	// siloosd's own refusal reaches the admin as it is.
	if _, err := h.Client.SetSiloOSHostname(h.Ctx(), connect.NewRequest(&v1.SetHostnameRequest{Hostname: "-x"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a bad host name: %v", err)
	}

	user, _, _ := h.Person("someone@test.local")
	if _, err := user.GetSiloOS(h.Ctx(), connect.NewRequest(&v1.GetSiloOSRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("a user read the machine: %v", err)
	}
	if _, err := user.SiloOSPower(h.Ctx(), connect.NewRequest(&v1.PowerRequest{Action: "reboot"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("a user rebooted the machine: %v", err)
	}
	if run.ran("systemctl reboot") {
		t.Fatal("the machine rebooted for a user")
	}
	if _, err := h.Client.SiloOSPower(h.Ctx(), connect.NewRequest(&v1.PowerRequest{Action: "reboot"})); err != nil || !run.ran("systemctl reboot") {
		t.Errorf("the admin's reboot: %v", err)
	}
}

// An admin moves the machine between the channels the registry lists, never
// back to an older image; a user does not see them.
func TestSiloOSChannelsMoveOnlyForward(t *testing.T) {
	run := &osRun{}
	sock := siloOSD(t, run)
	h := apptest.New(t, apptest.WithYAML("siloos:\n  socket: "+sock+"\n"))

	res, err := h.Client.ListSiloOSUpdateChannels(h.Ctx(), connect.NewRequest(&v1.ListUpdateChannelsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, c := range res.Msg.Channels {
		tags = append(tags, c.Tag)
	}
	if strings.Join(tags, " ") != "latest master v1.1.0 v1.0.0" {
		t.Errorf("channels = %v", tags)
	}
	if _, err := h.Client.SetSiloOSUpdateChannel(h.Ctx(), connect.NewRequest(&v1.SetUpdateChannelRequest{Channel: "v0.9.0"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("an older version: %v", err)
	}
	if run.ran("bootc switch ghcr.io/d-513/silo-os:v0.9.0") {
		t.Fatal("moved to an older version")
	}
	if _, err := h.Client.SetSiloOSUpdateChannel(h.Ctx(), connect.NewRequest(&v1.SetUpdateChannelRequest{Channel: "v1.1.0"})); err != nil {
		t.Fatal(err)
	}

	user, _, _ := h.Person("someone@test.local")
	if _, err := user.ListSiloOSUpdateChannels(h.Ctx(), connect.NewRequest(&v1.ListUpdateChannelsRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("a user listed the channels: %v", err)
	}
}

// A siloosd that is gone is the page's error, never Unavailable (which the
// console reads as the control plane being down).
func TestSiloOSDownIsNotUnavailable(t *testing.T) {
	h := apptest.New(t, apptest.WithYAML("siloos:\n  socket: /nonexistent/siloosd.sock\n"))
	st, err := h.Client.GetSiloOS(h.Ctx(), connect.NewRequest(&v1.GetSiloOSRequest{}))
	if err != nil || !st.Msg.Available || st.Msg.Error == "" {
		t.Fatalf("GetSiloOS = %v, %v", st.Msg, err)
	}
	if _, err := h.Client.GetSiloOSNetwork(h.Ctx(), connect.NewRequest(&v1.GetNetworkRequest{})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("network with siloosd down: %v", err)
	}
}

// The control plane gives Caddy its configuration, and Caddy may get a
// certificate on demand only for names the control plane knows.
func TestCaddyIsConfiguredAndAsks(t *testing.T) {
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
	var mu sync.Mutex
	var loaded string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/config/":
			if loaded == "" {
				w.Write([]byte("null"))
				return
			}
			w.Write([]byte(loaded))
		case "/load":
			b, _ := io.ReadAll(r.Body)
			loaded = string(b)
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	h := apptest.New(t, apptest.WithYAML("public_url: https://silo.example.com\ncontent:\n  host: c.example.com\ncaddy:\n  admin: unix/"+sock+"\n"))
	h.App.SiloOS.SyncProxy(h.Ctx())
	p := h.App.SiloOS.ProxyStatus()
	if p.State != "ok" || p.Host != "silo.example.com" || p.ContentHost != "c.example.com" {
		t.Fatalf("proxy = %v", p)
	}
	mu.Lock()
	got := loaded
	mu.Unlock()
	if !strings.Contains(got, `"silo.example.com"`) || !strings.Contains(got, `"*.c.example.com"`) || !strings.Contains(got, "/caddy/ask") {
		t.Errorf("Caddy was given %s", got)
	}

	bot := h.CreateBot("Tunneller")
	if err := h.DB.Create(&db.Tunnel{ID: ids.New(), BotID: bot.GetId(), Port: 3000, Name: "brave-blue-fox", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	ask := func(domain string) int {
		resp, err := http.Get(h.URL + "/caddy/ask?domain=" + domain)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for domain, want := range map[string]int{
		"silo.example.com":                    200,
		"tunnel-brave-blue-fox.c.example.com": 200,
		"tunnel-no-such-one.c.example.com":    404,
		"app-nothing.c.example.com":           404,
		"evil.example.com":                    404,
		"":                                    404,
	} {
		if got := ask(domain); got != want {
			t.Errorf("ask %q = %d, want %d", domain, got, want)
		}
	}
}
