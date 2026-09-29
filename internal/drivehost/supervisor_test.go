package drivehost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

type fakeProc struct {
	f    *fakeRun
	mp   string
	done chan struct{}
	once sync.Once
	tail string
}

func (p *fakeProc) Wait() error { <-p.done; return nil }
func (p *fakeProc) Stop()       { p.exit("") }
func (p *fakeProc) Tail() string {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return p.tail
}

// exit ends the process as a crash (with stderr) or a stop; the kernel drops
// the FUSE mount with it.
func (p *fakeProc) exit(stderr string) {
	p.once.Do(func() {
		p.f.mu.Lock()
		p.tail = stderr
		delete(p.f.mounted, p.mp)
		p.f.mu.Unlock()
		close(p.done)
	})
}

type fakeRun struct {
	mu      sync.Mutex
	starts  [][]string
	envs    [][]string
	procs   []*fakeProc
	mounted map[string]bool
	unmount []string
	noMount bool // processes start but never mount
	output  func(args []string) ([]byte, error)
}

func newFake() *fakeRun { return &fakeRun{mounted: map[string]bool{}} }

func (f *fakeRun) Start(args, env []string) (Proc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mp := args[2]
	p := &fakeProc{f: f, mp: mp, done: make(chan struct{})}
	f.starts = append(f.starts, args)
	f.envs = append(f.envs, env)
	f.procs = append(f.procs, p)
	if !f.noMount {
		f.mounted[mp] = true
	}
	return p, nil
}

func (f *fakeRun) Output(_ context.Context, args, _ []string) ([]byte, error) {
	return f.output(args)
}

func (f *fakeRun) Mounted(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mounted[p]
}

func (f *fakeRun) Unmount(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmount = append(f.unmount, p)
	delete(f.mounted, p)
	return nil
}

func (f *fakeRun) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func (f *fakeRun) proc(i int) *fakeProc {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.procs[i]
}

type recorder struct {
	mu     sync.Mutex
	frames []*v1.DriveUp
}

func (r *recorder) emit(m *v1.DriveUp) {
	r.mu.Lock()
	r.frames = append(r.frames, m)
	r.mu.Unlock()
}

func (r *recorder) states(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, f := range r.frames {
		if st := f.GetStatus(); st != nil && st.GetId() == id {
			out = append(out, st.GetState())
		}
	}
	return out
}

func (r *recorder) last(id string) *v1.DriveStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.frames) - 1; i >= 0; i-- {
		if st := r.frames[i].GetStatus(); st != nil && st.GetId() == id {
			return st
		}
	}
	return nil
}

func (r *recorder) tokens() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, f := range r.frames {
		if t := f.GetToken(); t != nil {
			out = append(out, t.GetToken())
		}
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func setup(t *testing.T) (*Supervisor, *fakeRun, *recorder, Config) {
	dir := t.TempDir()
	cfg := Config{
		MountRoot: filepath.Join(dir, "mnt"), CacheRoot: filepath.Join(dir, "cache"), RunDir: filepath.Join(dir, "run"),
		Poll: 5 * time.Millisecond, MountWait: time.Second, BackoffMin: 10 * time.Millisecond, BackoffMax: 40 * time.Millisecond,
	}
	_ = os.MkdirAll(cfg.RunDir, 0o700)
	f := newFake()
	r := &recorder{}
	s := New(cfg, f, r.emit)
	t.Cleanup(s.StopAll)
	return s, f, r, cfg
}

func spec(id string) *v1.DriveSpec {
	return &v1.DriveSpec{
		Id: id, Dir: id + "-dir", Remote: "r_" + id, Path: "Projects",
		Env: map[string]string{"RCLONE_CONFIG_R_" + strings.ToUpper(id) + "_TYPE": "s3"},
	}
}

func hasState(r *recorder, id, state string) func() bool {
	return func() bool { st := r.last(id); return st != nil && st.GetState() == state }
}

func TestApplyMountsAndReports(t *testing.T) {
	s, f, r, cfg := setup(t)
	d := spec("abc")
	d.ReadOnly = true
	d.Flags = []string{"--fast-list"}
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{d}, CacheMaxSize: "5G"})
	eventually(t, "mounted", hasState(r, "abc", StateMounted))

	if got := r.states("abc"); got[0] != StateMounting {
		t.Fatalf("first state = %v", got)
	}
	args := f.starts[0]
	mp := filepath.Join(cfg.MountRoot, "abc-dir")
	for _, want := range [][]string{
		{"mount", "r_abc:Projects", mp},
		{"--allow-other"}, {"--read-only"}, {"--fast-list"},
		{"--vfs-cache-max-size", "5G"},
		{"--cache-dir", filepath.Join(cfg.CacheRoot, "abc")},
		{"--uid", "1000", "--gid", "1000"},
	} {
		if !containsRun(args, want) {
			t.Errorf("args %v missing %v", args, want)
		}
	}
	env := f.envs[0]
	if !slices.Contains(env, "RCLONE_CONFIG_R_ABC_TYPE=s3") || slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "SILO_") }) {
		t.Fatalf("env = %v", env)
	}

	// A changed spec restarts the mount; wait for each one to come up.
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{spec("abc")}, CacheMaxSize: "5G"})
	eventually(t, "restart on change", func() bool { return f.startCount() == 2 && hasState(r, "abc", StateMounted)() })
	d2 := spec("abc")
	d2.ReadOnly = true
	d2.Flags = []string{"--fast-list"}
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{d2}, CacheMaxSize: "5G"})
	eventually(t, "restart on change back", func() bool { return f.startCount() == 3 && hasState(r, "abc", StateMounted)() })

	// The same set again: nothing restarts.
	same := spec("abc")
	same.ReadOnly = true
	same.Flags = []string{"--fast-list"}
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{same}, CacheMaxSize: "5G"})
	time.Sleep(30 * time.Millisecond)
	if n := f.startCount(); n != 3 {
		t.Fatalf("unchanged apply restarted: %d starts", n)
	}

	// Removed: stopped, unmounted, directory gone.
	s.Apply(&v1.DriveApply{})
	eventually(t, "stopped", hasState(r, "abc", StateStopped))
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Fatalf("mount point left behind: %v", err)
	}
}

func containsRun(hay, needle []string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if slices.Equal(hay[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func TestCrashReportsAndRestarts(t *testing.T) {
	s, f, r, _ := setup(t)
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{spec("x")}})
	eventually(t, "mounted", hasState(r, "x", StateMounted))
	f.proc(0).exit("2026/09/29 12:18:28 NOTICE: something\n2026/09/29 12:18:29 CRITICAL: Failed to create file system for \"r_x:\": couldn't connect: 403 Forbidden\n")
	eventually(t, "error", hasState(r, "x", StateError))
	if got := r.last("x").GetDetail(); got != `Failed to create file system for "r_x:": couldn't connect: 403 Forbidden` {
		t.Fatalf("detail = %q", got)
	}
	eventually(t, "restart", func() bool { return f.startCount() >= 2 })
	eventually(t, "mounted again", hasState(r, "x", StateMounted))
}

func TestTokenRefreshRoundTrip(t *testing.T) {
	s, f, r, cfg := setup(t)
	d := spec("tok")
	d.Token = `{"access_token":"a1"}`
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{d}})
	eventually(t, "mounted", hasState(r, "tok", StateMounted))
	conf := filepath.Join(cfg.RunDir, "tok.conf")
	b, _ := os.ReadFile(conf)
	if string(b) != "[r_tok]\ntoken = {\"access_token\":\"a1\"}\n" {
		t.Fatalf("conf = %q", b)
	}
	if !containsRun(f.starts[0], []string{"--config", conf}) {
		t.Fatalf("mount not pointed at its conf: %v", f.starts[0])
	}

	// rclone refreshes the token in place.
	_ = os.WriteFile(conf, []byte("[r_tok]\ntype = drive\ntoken = {\"access_token\":\"a2\"}\n\n"), 0o600)
	eventually(t, "token frame", func() bool { return slices.Contains(r.tokens(), `{"access_token":"a2"}`) })

	// The CP stores it and echoes it in the next Apply: no restart.
	echo := spec("tok")
	echo.Token = `{"access_token":"a2"}`
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{echo}})
	time.Sleep(30 * time.Millisecond)
	if n := f.startCount(); n != 1 {
		t.Fatalf("echoed token restarted the mount (%d starts)", n)
	}
	// A reconnect in the UI hands over a different token: restart with it.
	fresh := spec("tok")
	fresh.Token = `{"access_token":"b1"}`
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{fresh}})
	eventually(t, "restart for new sign-in", func() bool { return f.startCount() == 2 })
	eventually(t, "conf rewritten", func() bool {
		b, _ := os.ReadFile(conf)
		return strings.Contains(string(b), "b1")
	})

	// Snapshot re-sends status for the next session.
	snap := s.Snapshot()
	if len(snap) == 0 || snap[0].GetStatus() == nil {
		t.Fatalf("snapshot = %v", snap)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(*v1.DriveSpec){
		"foreign env":   func(d *v1.DriveSpec) { d.Env["RCLONE_CONFIG_OTHER_TYPE"] = "local" },
		"reserved flag": func(d *v1.DriveSpec) { d.Flags = []string{"--rc"} },
		"config flag":   func(d *v1.DriveSpec) { d.Flags = []string{"--config=/etc/x"} },
		"not a flag":    func(d *v1.DriveSpec) { d.Flags = []string{"; rm -rf /"} },
		"bad id":        func(d *v1.DriveSpec) { d.Id = "../etc" },
		"bad dir":       func(d *v1.DriveSpec) { d.Dir = "../../etc" },
		"upper dir":     func(d *v1.DriveSpec) { d.Dir = "Work" },
		"bad remote":    func(d *v1.DriveSpec) { d.Remote = "a:b" },
		"dotdot path":   func(d *v1.DriveSpec) { d.Path = "a/../../b" },
	}
	if err := Validate(spec("ok")); err != nil {
		t.Fatalf("good spec: %v", err)
	}
	for name, mut := range cases {
		d := spec("ok")
		mut(d)
		if Validate(d) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// An invalid spec in an Apply reports an error and never starts.
	s, f, r, _ := setup(t)
	bad := spec("bad")
	bad.Flags = []string{"--rc"}
	s.Apply(&v1.DriveApply{Drives: []*v1.DriveSpec{bad}})
	if st := r.last("bad"); st == nil || st.GetState() != StateError || f.startCount() != 0 {
		t.Fatalf("bad spec: %v, %d starts", st, f.startCount())
	}
}

func TestList(t *testing.T) {
	s, f, _, cfg := setup(t)
	var gotArgs []string
	f.output = func(args []string) ([]byte, error) {
		gotArgs = args
		conf := args[slices.Index(args, "--config")+1]
		// rclone refreshed the token while listing.
		_ = os.WriteFile(conf, []byte("[r_l]\ntoken = new\n"), 0o600)
		return []byte(`[{"Path":"b","Name":"b","IsDir":true},{"Path":"A","Name":"A","IsDir":true},{"Path":"f.txt","Name":"f.txt","IsDir":false}]`), nil
	}
	d := spec("l")
	d.Token = "old"
	res := s.List(context.Background(), &v1.DriveList{RequestId: "req1", Spec: d, Path: "/Clients/"})
	if res.GetError() != "" {
		t.Fatal(res.GetError())
	}
	if !slices.Contains(gotArgs, "r_l:Projects/Clients") {
		t.Fatalf("listed %v", gotArgs)
	}
	if len(res.Dirs) != 2 || res.Dirs[0].Name != "A" || res.Dirs[0].Path != "Clients/A" {
		t.Fatalf("dirs = %v", res.Dirs)
	}
	if res.GetToken() != "new" {
		t.Fatalf("refreshed token not returned: %q", res.GetToken())
	}
	if left, _ := filepath.Glob(filepath.Join(cfg.RunDir, "list-*")); len(left) != 0 {
		t.Fatalf("list conf left behind: %v", left)
	}

	f.output = func([]string) ([]byte, error) {
		return nil, errors.New("2026/09/29 10:00:00 ERROR : : error listing: couldn't list buckets: InvalidAccessKeyId")
	}
	res = s.List(context.Background(), &v1.DriveList{RequestId: "req2", Spec: spec("l")})
	if !strings.Contains(res.GetError(), "InvalidAccessKeyId") || strings.Contains(res.GetError(), "2026/") {
		t.Fatalf("error = %q", res.GetError())
	}
	res = s.List(context.Background(), &v1.DriveList{RequestId: "req3", Spec: spec("l"), Path: "../x"})
	if res.GetError() == "" {
		t.Fatal("dotdot listing accepted")
	}
}

func TestCleanupRemovesStaleMounts(t *testing.T) {
	s, f, _, cfg := setup(t)
	stale := filepath.Join(cfg.MountRoot, "old")
	_ = os.MkdirAll(stale, 0o755)
	f.mounted[stale] = true
	s.Cleanup()
	if !slices.Contains(f.unmount, stale) {
		t.Fatalf("stale mount not unmounted: %v", f.unmount)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale dir left")
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/mnt/drives/my\040drive`); got != "/mnt/drives/my drive" {
		t.Fatalf("got %q", got)
	}
}
