package siloosd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "silo.agent/gen/silo/v1"
)

// fakeRun answers commands by the longest prefix of "name arg arg …" it was
// scripted with, and records every call.
type fakeRun struct {
	mu    sync.Mutex
	out   map[string]string
	fail  map[string]string
	calls []string
}

func newFake() *fakeRun { return &fakeRun{out: map[string]string{}, fail: map[string]string{}} }

func (f *fakeRun) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, line)
	best, bestOut, isFail := -1, "", false
	for k, v := range f.out {
		if strings.HasPrefix(line, k) && len(k) > best {
			best, bestOut, isFail = len(k), v, false
		}
	}
	for k, v := range f.fail {
		if strings.HasPrefix(line, k) && len(k) > best {
			best, bestOut, isFail = len(k), v, true
		}
	}
	if best < 0 {
		return nil, errors.New("unscripted: " + line)
	}
	if isFail {
		return nil, errors.New(bestOut)
	}
	return []byte(bestOut), nil
}

func (f *fakeRun) ran(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

const (
	wired = "11111111-2222-3333-4444-555555555555"
	wifi  = "99999999-2222-3333-4444-555555555555"
)

// network scripts one wired device on DHCP and a Wi-Fi profile.
func network(f *fakeRun) {
	f.out["nmcli -t -f DEVICE,TYPE,STATE,CON-UUID device status"] = "enp1s0:ethernet:connected:" + wired + "\nlo:loopback:connected (externally):\nwlp2s0:wifi:disconnected:\n"
	f.out["nmcli -t -e no -f GENERAL.HWADDR,IP4.ADDRESS,IP4.GATEWAY,IP4.DNS device show enp1s0"] = "GENERAL.HWADDR:52:54:00:12:34:56\nIP4.ADDRESS[1]:192.168.1.20/24\nIP4.GATEWAY:192.168.1.1\nIP4.DNS[1]:192.168.1.1\nIP4.DNS[2]:1.1.1.1\n"
	f.out["nmcli -t -e no -f GENERAL.HWADDR,IP4.ADDRESS,IP4.GATEWAY,IP4.DNS device show wlp2s0"] = "GENERAL.HWADDR:AA:BB:CC:DD:EE:FF\nIP4.GATEWAY:--\n"
	f.out["nmcli -t -e no -f connection.id,connection.uuid,connection.type,ipv4.method,ipv4.addresses,ipv4.gateway,ipv4.dns,ipv4.ignore-auto-dns connection show "+wired] =
		"connection.id:Wired connection 1\nconnection.uuid:" + wired + "\nconnection.type:802-3-ethernet\nipv4.method:auto\nipv4.addresses:\nipv4.gateway:--\nipv4.dns:\nipv4.ignore-auto-dns:no\n"
	f.out["nmcli -t -e no -f connection.id,connection.uuid,connection.type,ipv4.method,ipv4.addresses,ipv4.gateway,ipv4.dns,ipv4.ignore-auto-dns connection show "+wifi] =
		"connection.id:Home\nconnection.uuid:" + wifi + "\nconnection.type:802-11-wireless\nipv4.method:auto\n"
	f.out["busctl --json=short get-property org.freedesktop.NetworkManager /org/freedesktop/NetworkManager org.freedesktop.NetworkManager Checkpoints"] = `{"type":"ao","data":[]}`
}

func newServer(t *testing.T, f *fakeRun) *Server {
	t.Helper()
	return New(Config{Run: f, StateDir: t.TempDir(), Root: t.TempDir(), After: func(time.Duration, func()) {}})
}

func TestNetworkIsRead(t *testing.T) {
	f := newFake()
	network(f)
	s := newServer(t, f)
	n, err := s.network(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Devices) != 2 {
		t.Fatalf("devices = %v, want the wired and the Wi-Fi one (no loopback)", n.Devices)
	}
	d := n.Devices[0]
	if d.Name != "enp1s0" || d.Mac != "52:54:00:12:34:56" || strings.Join(d.Addresses, " ") != "192.168.1.20/24" || d.Gateway != "192.168.1.1" || strings.Join(d.Dns, " ") != "192.168.1.1 1.1.1.1" {
		t.Errorf("device read as %v", d)
	}
	if d.Connection.GetUuid() != wired || d.Connection.GetMethod() != "auto" || d.Connection.GetGateway() != "" {
		t.Errorf("connection read as %v", d.Connection)
	}
	if n.Devices[1].Connection != nil {
		t.Errorf("a device with no profile got one: %v", n.Devices[1].Connection)
	}
}

func TestChangeRunsUnderACheckpoint(t *testing.T) {
	f := newFake()
	network(f)
	f.out["busctl --json=short call org.freedesktop.NetworkManager /org/freedesktop/NetworkManager org.freedesktop.NetworkManager CheckpointCreate aouu 0 120 1"] = `{"type":"o","data":["/org/freedesktop/NetworkManager/Checkpoint/7"]}`
	f.out["nmcli connection modify "+wired] = ""
	f.out["nmcli --wait 30 connection up "+wired] = ""
	s := newServer(t, f)
	ctx := context.Background()
	held := func(paths string) {
		f.out["busctl --json=short get-property "+nmService+" "+nmPath+" "+nmIface+" Checkpoints"] = `{"type":"ao","data":[` + paths + `]}`
	}
	held(`"/org/freedesktop/NetworkManager/Checkpoint/7"`)
	if _, err := s.ApplyConnection(ctx, connect.NewRequest(&v1.ApplyConnectionRequest{
		Uuid: wired, Method: "manual", Addresses: []string{"192.168.1.50/24"}, Gateway: "192.168.1.1", Dns: []string{"9.9.9.9"},
	})); err != nil {
		t.Fatal(err)
	}
	create, modify, up := f.ran("busctl --json=short call "+nmService+" "+nmPath+" "+nmIface+" CheckpointCreate"), f.ran("nmcli connection modify"), f.ran("nmcli --wait 30 connection up")
	if create < 0 || modify < create || up < modify {
		t.Fatalf("checkpoint %d, modify %d, up %d: the change must come after the checkpoint\n%s", create, modify, up, strings.Join(f.calls, "\n"))
	}
	want := "nmcli connection modify " + wired + " ipv4.method manual ipv4.addresses 192.168.1.50/24 ipv4.gateway 192.168.1.1 ipv4.dns 9.9.9.9 ipv4.ignore-auto-dns no"
	if f.calls[modify] != want {
		t.Errorf("modified with\n%s\nwant\n%s", f.calls[modify], want)
	}

	// NetworkManager holds it: the page shows it waiting.
	n, _ := s.network(ctx)
	if n.Pending.GetConnection() != wired {
		t.Fatalf("pending = %v", n.Pending)
	}
	// A restarted siloosd still knows it.
	if again := New(Config{Run: f, StateDir: s.state, Root: s.root}); again.pending == nil || again.pending.Checkpoint != "/org/freedesktop/NetworkManager/Checkpoint/7" {
		t.Errorf("pending after a restart = %v", again.pending)
	}
	if _, err := s.ApplyConnection(ctx, connect.NewRequest(&v1.ApplyConnectionRequest{Uuid: wired, Method: "auto"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("a second change while one waits: %v", err)
	}

	f.out["busctl call org.freedesktop.NetworkManager /org/freedesktop/NetworkManager org.freedesktop.NetworkManager CheckpointDestroy o /org/freedesktop/NetworkManager/Checkpoint/7"] = ""
	if _, err := s.ConfirmNetwork(ctx, connect.NewRequest(&v1.ConfirmNetworkRequest{})); err != nil {
		t.Fatal(err)
	}
	if s.pending != nil {
		t.Error("kept, yet still pending")
	}
	// Once NetworkManager no longer holds a checkpoint (it rolled back by
	// itself), nothing is pending.
	s.pending = &pendingNet{Checkpoint: "/org/freedesktop/NetworkManager/Checkpoint/8", Connection: wired, Expires: time.Now().Add(time.Minute)}
	held("")
	if n, _ := s.network(ctx); n.Pending != nil {
		t.Errorf("a rolled back change still pending: %v", n.Pending)
	}
	if _, err := os.Stat(filepath.Join(s.state, pendingFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the pending file stays: %v", err)
	}
}

func TestRefusedChangeIsRolledBack(t *testing.T) {
	f := newFake()
	network(f)
	f.out["busctl --json=short call "+nmService+" "+nmPath+" "+nmIface+" CheckpointCreate"] = `{"type":"o","data":["/org/freedesktop/NetworkManager/Checkpoint/2"]}`
	f.out["nmcli connection modify "+wired] = ""
	f.fail["nmcli --wait 30 connection up "+wired] = "Error: Connection activation failed"
	f.out["busctl call "+nmService+" "+nmPath+" "+nmIface+" CheckpointRollback o /org/freedesktop/NetworkManager/Checkpoint/2"] = ""
	s := newServer(t, f)
	_, err := s.ApplyConnection(context.Background(), connect.NewRequest(&v1.ApplyConnectionRequest{Uuid: wired, Method: "auto", Dns: []string{"1.1.1.1"}}))
	if err == nil || !strings.Contains(err.Error(), "undone") {
		t.Fatalf("err = %v", err)
	}
	if f.ran("busctl call "+nmService+" "+nmPath+" "+nmIface+" CheckpointRollback") < 0 {
		t.Error("not rolled back")
	}
	if s.pending != nil {
		t.Error("still pending after the roll back")
	}
}

func TestChangeArgumentsAreChecked(t *testing.T) {
	f := newFake()
	network(f)
	s := newServer(t, f)
	bad := []*v1.ApplyConnectionRequest{
		{Uuid: "--help", Method: "auto"},
		{Uuid: wired, Method: "shared"},
		{Uuid: wired, Method: "manual"},
		{Uuid: wired, Method: "manual", Addresses: []string{"192.168.1.5/24,10.0.0.1/8"}},
		{Uuid: wired, Method: "manual", Addresses: []string{"-x"}},
		{Uuid: wired, Method: "manual", Addresses: []string{"127.0.0.1/8"}},
		{Uuid: wired, Method: "manual", Addresses: []string{"192.168.1.5/24"}, Gateway: "10.0.0.1"},
		{Uuid: wired, Method: "manual", Addresses: []string{"192.168.1.5/24"}, Dns: []string{"1.1.1.1 2.2.2.2"}},
		{Uuid: wired, Method: "auto", Addresses: []string{"192.168.1.5/24"}},
		{Uuid: wired, Method: "auto", ConfirmWithin: 5},
		{Uuid: wifi, Method: "auto"},
	}
	for _, r := range bad {
		if _, err := s.ApplyConnection(context.Background(), connect.NewRequest(r)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%v: %v, want InvalidArgument", r, err)
		}
	}
	if f.ran("busctl --json=short call") >= 0 || f.ran("nmcli connection modify") >= 0 {
		t.Errorf("a refused change touched NetworkManager:\n%s", strings.Join(f.calls, "\n"))
	}
}

func TestHostnameAndZoneAreChecked(t *testing.T) {
	for _, h := range []string{"silo", "silo-1.example.com", "a"} {
		if !validHostname(h) {
			t.Errorf("%q refused", h)
		}
	}
	for _, h := range []string{"", "-silo", "silo-", "si lo", "silo_1", "a..b", strings.Repeat("a", 64)} {
		if validHostname(h) {
			t.Errorf("%q let through", h)
		}
	}
	for _, z := range []string{"UTC", "Europe/Warsaw", "America/Argentina/Buenos_Aires", "Etc/GMT+2"} {
		if !validZone(z) {
			t.Errorf("%q refused", z)
		}
	}
	for _, z := range []string{"", "-h", "../etc/passwd", "Europe/../x", "Europe Warsaw"} {
		if validZone(z) {
			t.Errorf("%q let through", z)
		}
	}

	f := newFake()
	f.out["timedatectl list-timezones"] = "Europe/Berlin\nUTC\n"
	s := newServer(t, f)
	if _, err := s.SetTime(context.Background(), connect.NewRequest(&v1.SetTimeRequest{TimeZone: "Europe/Atlantis"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("an unknown zone: %v", err)
	}
}

const bootcJSON = `{"apiVersion":"org.containers.bootc/v1","kind":"BootcHost","status":{
 "staged":null,
 "booted":{"image":{"image":{"image":"ghcr.io/d-513/silo-os:latest","transport":"registry"},"version":"v0.4.2","timestamp":"2026-11-01T10:00:00Z","imageDigest":"sha256:aaa"},
   "cachedUpdate":{"image":{"image":"ghcr.io/d-513/silo-os:latest","transport":"registry"},"version":"v0.4.3","timestamp":"2026-11-20T10:00:00Z","imageDigest":"sha256:bbb"}},
 "rollback":{"image":{"image":{"image":"ghcr.io/d-513/silo-os:latest","transport":"registry"},"version":"v0.4.1","imageDigest":"sha256:000"}},
 "rollbackQueued":false}}`

func TestUpdatesReadBootc(t *testing.T) {
	f := newFake()
	f.out["bootc status --format=json"] = bootcJSON
	f.out["timedatectl show -p Timezone --value"] = "UTC\n"
	s := newServer(t, f)
	u, err := s.GetUpdates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := u.Msg
	if m.Booted.GetVersion() != "v0.4.2" || m.Available.GetVersion() != "v0.4.3" || m.Rollback.GetVersion() != "v0.4.1" || m.Staged != nil {
		t.Errorf("updates read as %v", m)
	}
	if m.Policy.GetMode() != modeDownload || m.Policy.GetHour() != 4 {
		t.Errorf("default policy = %v", m.Policy)
	}
	if _, err := s.RunUpdate(context.Background(), connect.NewRequest(&v1.RunUpdateRequest{Action: "switch"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("an unknown job: %v", err)
	}
}

func TestPolicyRunsOnceADayAtItsHour(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 21, h, m, 0, 0, time.UTC) }
	p := policyFile{Mode: modeDownload, Hour: 4}
	if got := nextRun(p, at(2, 0)); !got.Equal(at(4, 0)) {
		t.Errorf("before the hour: %v", got)
	}
	if got := nextRun(p, at(4, 30)); !got.Equal(at(4, 0)) {
		t.Errorf("within the hour: %v", got)
	}
	if got := nextRun(p, at(6, 0)); !got.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Errorf("after the hour: %v", got)
	}
	p.LastRun = at(4, 1)
	if got := nextRun(p, at(4, 30)); !got.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Errorf("after running today: %v", got)
	}
	if !nextRun(policyFile{Mode: modeOff, Hour: 4}, at(4, 0)).IsZero() {
		t.Error("off runs")
	}
}

func TestScheduledRebootOnlyWhenStaged(t *testing.T) {
	for _, staged := range []bool{false, true} {
		f := newFake()
		f.out["timedatectl show -p Timezone --value"] = "UTC\n"
		f.out["bootc upgrade"] = "Queued for next boot"
		f.out["systemctl reboot"] = ""
		st := bootcJSON
		if staged {
			st = strings.Replace(bootcJSON, `"staged":null`, `"staged":{"image":{"image":{"image":"x"},"version":"v0.4.3","imageDigest":"sha256:bbb"}}`, 1)
		}
		f.out["bootc status --format=json"] = st
		now := time.Date(2026, 11, 21, 4, 5, 0, 0, time.UTC)
		s := New(Config{Run: f, StateDir: t.TempDir(), Now: func() time.Time { return now }})
		s.policy = policyFile{Mode: modeReboot, Hour: 4}
		s.scheduled(context.Background())
		s.jobs.Lock() // the job's goroutine is done once it lets go
		s.jobs.Unlock()
		if got := f.ran("systemctl reboot") >= 0; got != staged {
			t.Errorf("staged=%v: rebooted=%v\n%s", staged, got, strings.Join(f.calls, "\n"))
		}
		if !sameDay(s.policy.LastRun, now) {
			t.Error("the run was not recorded")
		}
	}
}

func TestJournalLines(t *testing.T) {
	raw := `{"__REALTIME_TIMESTAMP":"1763700000000000","PRIORITY":"3","MESSAGE":"boom","CONTAINER_NAME":"silo"}
{"__REALTIME_TIMESTAMP":"1763700001000000","MESSAGE":[104,105,255],"_SYSTEMD_UNIT":"siloosd.service"}
not json
`
	lines := journalLines([]byte(raw))
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0].Unit != "silo" || lines[0].Priority != 3 || lines[0].Message != "boom" || !strings.HasPrefix(lines[0].Time, "2025-") {
		t.Errorf("first = %v", lines[0])
	}
	if lines[1].Message != "hi�" || lines[1].Unit != "siloosd.service" || lines[1].Priority != 6 {
		t.Errorf("second = %v", lines[1])
	}
}

func TestSocketAnswersOnlyAllowedUIDs(t *testing.T) {
	dir, err := os.MkdirTemp("", "siloosd") // short: a unix socket path is limited to 108 bytes
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	f := newFake()
	f.out["hostnamectl hostname"] = "silo-box\n"
	for i, allow := range [][]int{{os.Getuid()}, {os.Getuid() + 4242}} {
		path := filepath.Join(dir, "s.sock")
		ln, err := Listen(path, -1)
		if err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o660 {
			t.Errorf("socket mode %v", st.Mode().Perm())
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { Serve(ctx, ln, newServer(t, f), allow); close(done) }()
		res, err := Dial(path).GetSystem(context.Background(), connect.NewRequest(&v1.GetSystemRequest{}))
		if i == 0 && (err != nil || res.Msg.Hostname != "silo-box") {
			t.Errorf("own uid: %v %v", res, err)
		}
		if i == 1 && err == nil {
			t.Error("a uid not allowed was answered")
		}
		cancel()
		<-done
	}
}

func TestChannelOfTheBootedImage(t *testing.T) {
	const repo = "ghcr.io/d-513/silo-os"
	for _, tc := range []struct{ ref, transport, channel, tag string }{
		{"ghcr.io/d-513/silo-os:latest", "registry", "latest", "latest"},
		{"ghcr.io/d-513/silo-os", "registry", "latest", "latest"},
		{"ghcr.io/d-513/silo-os:master", "registry", "master", "master"},
		{"ghcr.io/d-513/silo-os:v0.4.2", "registry", "pinned", "v0.4.2"},
		{"ghcr.io/d-513/silo-os:master-abc1234", "registry", "pinned", "master-abc1234"},
		{"localhost/silo-os:dev", "registry", "local", "dev"},
		{"localhost/silo-os:dev", "containers-storage", "local", ""},
		{"ghcr.io/someone/fork-os:latest", "registry", "custom", "latest"},
		{"localhost:5000/silo-os:latest", "registry", "custom", "latest"},
	} {
		ch, tag := channelOf(tc.ref, tc.transport, repo)
		if ch != tc.channel || tag != tc.tag {
			t.Errorf("%s (%s) = %s %s, want %s %s", tc.ref, tc.transport, ch, tag, tc.channel, tc.tag)
		}
	}
	if ch, _ := channelOf("ghcr.io/d-513/silo-os:latest", "registry", ""); ch != "custom" {
		t.Errorf("an image that names no repository = %s", ch)
	}
}

func TestChannelSwitchStaysInTheRepository(t *testing.T) {
	f := newFake()
	f.out["bootc status --format=json"] = bootcJSON
	f.out["timedatectl show -p Timezone --value"] = "UTC\n"
	f.out["bootc switch ghcr.io/d-513/silo-os:master"] = ""
	f.out["skopeo inspect --no-tags docker://ghcr.io/d-513/silo-os:master"] = remote("master-abc1234", "2026-11-20T10:00:00Z")
	f.out["skopeo inspect --no-tags docker://ghcr.io/d-513/silo-os:v0.4.1"] = remote("v0.4.1", "2026-10-01T10:00:00Z")
	s := newServer(t, f)
	if err := os.MkdirAll(filepath.Join(s.root, "usr/lib/siloos"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(s.root, releaseFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ask := func(ch string) error {
		_, err := s.SetUpdateChannel(context.Background(), connect.NewRequest(&v1.SetUpdateChannelRequest{Channel: ch}))
		return err
	}

	// No release file: nowhere to switch to.
	if err := ask("master"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("no repository: %v", err)
	}
	write("SILOOS_VERSION=v0.4.2\nSILOOS_REPOSITORY=ghcr.io/d-513/silo-os\n")
	for _, bad := range []string{"", "evil.example.com/os:latest", "--help", "latest@sha256:00", "../x", "nightly"} {
		if err := ask(bad); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := ask("v0.4.1"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("an older version: %v", err)
	}
	if err := ask("master"); err != nil {
		t.Fatal(err)
	}
	s.jobs.Lock()
	s.jobs.Unlock()
	if f.ran("bootc switch ghcr.io/d-513/silo-os:master") < 0 {
		t.Errorf("not switched:\n%s", strings.Join(f.calls, "\n"))
	}
	u, _ := s.GetUpdates(context.Background(), nil)
	if u.Msg.Channel != "latest" || u.Msg.Version != "v0.4.2" || u.Msg.Repository != "ghcr.io/d-513/silo-os" || u.Msg.Job.GetAction() != "switch" {
		t.Errorf("updates = %v", u.Msg)
	}
	if !older("v0.4.1", "v0.4.2") || older("v0.5.0", "v0.4.9") || older("master", "v0.4.2") || older("v0.4.2-rc1", "v0.4.2") {
		t.Error("older")
	}
}

// remote is what skopeo inspect says of an image.
func remote(version, created string) string {
	return `{"Name":"ghcr.io/d-513/silo-os","Created":"` + created + `","Labels":{"org.opencontainers.image.version":"` + version + `"}}`
}

// The machine runs v0.4.2, built 2026-11-01: nothing built before it, and no
// lower version, is a place it may move to, whatever the tag is called.
func TestNoMoveToAnOlderImage(t *testing.T) {
	f := newFake()
	f.out["bootc status --format=json"] = bootcJSON
	f.out["timedatectl show -p Timezone --value"] = "UTC\n"
	f.out["bootc switch"] = ""
	const repo = "docker://ghcr.io/d-513/silo-os:"
	f.out["skopeo list-tags docker://ghcr.io/d-513/silo-os"] = `{"Repository":"ghcr.io/d-513/silo-os","Tags":["v0.3.0","v0.4.1","v0.4.2","v0.4.3","v0.5.0-rc1","v0.5.0","latest","master","master-0000001","master-abc1234","dev","sha256-0123"]}`
	f.out["skopeo inspect --no-tags "+repo+"latest"] = remote("v0.4.2", "2026-11-01T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"master"] = remote("master-abc1234", "2026-11-20T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"v0.3.0"] = remote("v0.3.0", "2026-09-01T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"v0.4.1"] = remote("v0.4.1", "2026-10-01T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"master-0000001"] = remote("master-0000001", "2026-10-20T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"master-abc1234"] = remote("master-abc1234", "2026-11-20T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"v0.4.2"] = remote("v0.4.2", "2026-11-01T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"v0.4.3"] = remote("v0.4.3", "2026-11-10T10:00:00Z")
	// Rebuilt from an older commit, or mislabelled: its label is what counts.
	f.out["skopeo inspect --no-tags "+repo+"v0.5.0-rc1"] = remote("v0.4.0", "2026-11-22T10:00:00Z")
	f.out["skopeo inspect --no-tags "+repo+"v0.5.0"] = remote("v0.5.0", "2026-11-21T10:00:00Z")
	f.fail["skopeo inspect --no-tags "+repo+"v9.9.9"] = "manifest unknown"
	s := newServer(t, f)
	if err := os.MkdirAll(filepath.Join(s.root, "usr/lib/siloos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, releaseFile), []byte("SILOOS_VERSION=v0.4.2\nSILOOS_REPOSITORY=ghcr.io/d-513/silo-os\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ask := func(ch string) error {
		_, err := s.SetUpdateChannel(context.Background(), connect.NewRequest(&v1.SetUpdateChannelRequest{Channel: ch}))
		s.jobs.Lock()
		s.jobs.Unlock()
		return err
	}
	for _, old := range []string{"v0.3.0", "v0.4.1", "master-0000001", "v0.5.0-rc1", "v9.9.9"} {
		if err := ask(old); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("%s: %v", old, err)
		}
	}
	for _, ok := range []string{"latest", "v0.4.2", "v0.4.3", "v0.5.0", "master", "master-abc1234"} {
		if err := ask(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	if f.ran("bootc switch ghcr.io/d-513/silo-os:v0.4.1") >= 0 || f.ran("bootc switch ghcr.io/d-513/silo-os:master-0000001") >= 0 {
		t.Error("switched to an older image")
	}

	res, err := s.ListUpdateChannels(context.Background(), connect.NewRequest(&v1.ListUpdateChannelsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range res.Msg.Channels {
		line := c.Tag
		if c.Older {
			line += " older"
			if c.Reason == "" {
				t.Errorf("%s: older with no reason", c.Tag)
			}
		}
		got = append(got, line)
	}
	want := "latest master v0.5.0 v0.5.0-rc1 older v0.4.3 v0.4.2"
	if strings.Join(got, " ") != want {
		t.Errorf("channels = %s, want %s", strings.Join(got, " "), want)
	}
	if c := res.Msg.Channels[1]; c.Version != "master-abc1234" || c.Created != "2026-11-20T10:00:00Z" {
		t.Errorf("master = %v", c)
	}

	// Running a master build newer than every release: latest is behind it.
	f.mu.Lock()
	f.out["bootc status --format=json"] = strings.ReplaceAll(strings.ReplaceAll(bootcJSON, "silo-os:latest", "silo-os:master"), `"timestamp":"2026-11-01T10:00:00Z"`, `"timestamp":"2026-11-23T10:00:00Z"`)
	f.mu.Unlock()
	if err := os.WriteFile(filepath.Join(s.root, releaseFile), []byte("SILOOS_VERSION=master-fff0000\nSILOOS_REPOSITORY=ghcr.io/d-513/silo-os\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"latest", "v0.5.0", "master-abc1234"} {
		if err := ask(old); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("from a newer master build, %s: %v", old, err)
		}
	}

	// A local build is no release: its build time says nothing, so any
	// channel takes it back to the published ones.
	f.mu.Lock()
	f.out["bootc status --format=json"] = strings.ReplaceAll(bootcJSON, "ghcr.io/d-513/silo-os:latest", "localhost/silo-os:dev")
	f.mu.Unlock()
	if err := os.WriteFile(filepath.Join(s.root, releaseFile), []byte("SILOOS_VERSION=dev\nSILOOS_REPOSITORY=ghcr.io/d-513/silo-os\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ask("v0.4.1"); err != nil {
		t.Errorf("from a local build: %v", err)
	}

	// Credentials bootc uses are handed to skopeo too.
	if err := os.MkdirAll(filepath.Join(s.root, "etc/ostree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "etc/ostree/auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.out["skopeo inspect --authfile /etc/ostree/auth.json"] = remote("v0.5.0", "2026-11-21T10:00:00Z")
	if err := ask("v0.5.0"); err != nil || f.ran("skopeo inspect --authfile /etc/ostree/auth.json --no-tags "+repo+"v0.5.0") < 0 {
		t.Errorf("auth file: %v", err)
	}
}

func TestLocalBuildHasNothingToFetch(t *testing.T) {
	f := newFake()
	f.out["bootc status --format=json"] = strings.ReplaceAll(bootcJSON, "ghcr.io/d-513/silo-os:latest", "localhost/silo-os:dev")
	f.out["timedatectl show -p Timezone --value"] = "UTC\n"
	s := newServer(t, f)
	for _, a := range []string{"check", "download"} {
		if _, err := s.RunUpdate(context.Background(), connect.NewRequest(&v1.RunUpdateRequest{Action: a})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Errorf("%s on a local build: %v", a, err)
		}
	}
	u, _ := s.GetUpdates(context.Background(), nil)
	if u.Msg.Channel != "local" || u.Msg.NextRunAt != "" {
		t.Errorf("updates = %v", u.Msg)
	}
	now := time.Date(2026, 11, 21, 4, 5, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.scheduled(context.Background())
	if f.ran("bootc upgrade") >= 0 {
		t.Error("the policy ran on a local build")
	}
}
