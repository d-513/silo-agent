// Package drivehost is the guest side of Drives: it runs inside the silo-drive
// sidecar, keeps one `rclone mount` per desired drive under the mount root
// (whose mounts propagate into the Bot), and answers one-shot listings. It
// gets fully rendered specs from the control plane and knows nothing about
// providers or templates.
package drivehost

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// Mount states reported to the CP.
const (
	StateMounting = "mounting"
	StateMounted  = "mounted"
	StateError    = "error"
	StateStopped  = "stopped"
)

// Proc is one running rclone process.
type Proc interface {
	// Wait blocks until the process exits.
	Wait() error
	// Stop asks the process to exit (SIGTERM, then SIGKILL after a grace).
	Stop()
	// Tail is the last stderr lines, for error reporting.
	Tail() string
}

// Runner is the OS boundary: processes and mount points. The real one execs
// rclone and reads /proc/self/mountinfo; tests use a fake.
type Runner interface {
	Start(args, env []string) (Proc, error)
	Output(ctx context.Context, args, env []string) ([]byte, error)
	Mounted(path string) bool
	Unmount(path string) error
}

// Config is the sidecar's fixed layout.
type Config struct {
	MountRoot string // e.g. /mnt/drives; shared with the Bot via propagation
	CacheRoot string // e.g. /cache; durable per-Bot VFS cache
	RunDir    string // private tmpfs for token config files
	UID, GID  string // owner the Bot sees (silo = 1000)
	// Poll is how often mount state and token files are checked.
	Poll time.Duration
	// MountWait bounds how long a starting mount may take to appear.
	MountWait time.Duration
	// Backoff bounds for restarting a crashed mount.
	BackoffMin, BackoffMax time.Duration
	// ListTimeout bounds a one-shot listing.
	ListTimeout time.Duration
}

func (c *Config) defaults() {
	if c.UID == "" {
		c.UID = "1000"
	}
	if c.GID == "" {
		c.GID = "1000"
	}
	if c.Poll == 0 {
		c.Poll = 2 * time.Second
	}
	if c.MountWait == 0 {
		c.MountWait = 60 * time.Second
	}
	if c.BackoffMin == 0 {
		c.BackoffMin = 2 * time.Second
	}
	if c.BackoffMax == 0 {
		c.BackoffMax = time.Minute
	}
	if c.ListTimeout == 0 {
		c.ListTimeout = 45 * time.Second
	}
}

// Supervisor owns the running mounts. It outlives any one CP session: a CP
// restart must not unmount the Bot's drives.
type Supervisor struct {
	cfg  Config
	run  Runner
	emit func(*v1.DriveUp)

	mu       sync.Mutex
	mounts   map[string]*mount
	cacheMax string
}

type mount struct {
	spec   *v1.DriveSpec
	key    string
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status *v1.DriveStatus
	token  string // last token seen (sent or received)
}

// New makes a supervisor. emit receives status and token frames; it must not
// block for long (the session drops frames while disconnected and re-sends a
// snapshot on reconnect).
func New(cfg Config, run Runner, emit func(*v1.DriveUp)) *Supervisor {
	cfg.defaults()
	return &Supervisor{cfg: cfg, run: run, emit: emit, mounts: map[string]*mount{}}
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var dirRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var remoteRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
var flagRe = regexp.MustCompile(`^--[a-z0-9][a-z0-9-]*(=.*)?$`)

// reserved flags are ones the supervisor sets itself or that would let a spec
// escape its box (another config, the remote-control API, a daemon).
var reserved = []string{
	"--config", "--rc", "--rc-addr", "--daemon", "--allow-root", "--cache-dir",
	"--uid", "--gid", "--allow-other", "--log-file", "--password-command",
}

// Validate rejects a spec the supervisor must not run. The CP renders specs
// from reviewed templates, but the sidecar still refuses anything that would
// reach outside its drive.
func Validate(s *v1.DriveSpec) error {
	if !idRe.MatchString(s.GetId()) {
		return fmt.Errorf("bad drive id %q", s.GetId())
	}
	if !dirRe.MatchString(s.GetDir()) || strings.Contains(s.GetDir(), "..") {
		return fmt.Errorf("bad mount dir %q", s.GetDir())
	}
	if !remoteRe.MatchString(s.GetRemote()) {
		return fmt.Errorf("bad remote name %q", s.GetRemote())
	}
	prefix := envPrefix(s.GetRemote())
	for k := range s.GetEnv() {
		if !strings.HasPrefix(k, prefix) {
			return fmt.Errorf("env %q is not for remote %s", k, s.GetRemote())
		}
	}
	if strings.Contains(s.GetPath(), "..") {
		return errors.New("path must not contain ..")
	}
	for _, f := range s.GetFlags() {
		if !flagRe.MatchString(f) {
			return fmt.Errorf("bad flag %q", f)
		}
		name, _, _ := strings.Cut(f, "=")
		for _, r := range reserved {
			if name == r {
				return fmt.Errorf("flag %s is reserved", name)
			}
		}
	}
	return nil
}

func envPrefix(remote string) string { return "RCLONE_CONFIG_" + strings.ToUpper(remote) + "_" }

// specKey changes whenever anything that affects the running process changes.
// The token is left out: rclone refreshes it itself, and a CP echo of the
// refreshed token must not restart the mount.
func specKey(s *v1.DriveSpec, cacheMax string) string {
	h := sha256.New()
	env := make([]string, 0, len(s.GetEnv()))
	for k, v := range s.GetEnv() {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	_ = json.NewEncoder(h).Encode([]any{s.GetDir(), s.GetRemote(), env, s.GetPath(), s.GetFlags(), s.GetReadOnly(), s.GetToken() != "", cacheMax})
	return hex.EncodeToString(h.Sum(nil))
}

// Apply makes the running set match a. Unchanged drives keep running.
func (s *Supervisor) Apply(a *v1.DriveApply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheMax = a.GetCacheMaxSize()
	want := map[string]*v1.DriveSpec{}
	for _, d := range a.GetDrives() {
		if err := Validate(d); err != nil {
			s.emitStatus(&v1.DriveStatus{Id: d.GetId(), State: StateError, Detail: err.Error()})
			continue
		}
		want[d.GetId()] = d
	}
	for id, m := range s.mounts {
		d, ok := want[id]
		if ok && specKey(d, s.cacheMax) == m.key {
			// A token the mount has not seen is a fresh sign-in (Reconnect);
			// the CP echoing rclone's own refresh back matches lastToken.
			if d.GetToken() != "" && d.GetToken() != m.lastToken() {
				m.stop()
				delete(s.mounts, id)
				continue
			}
			delete(want, id)
			continue
		}
		m.stop()
		delete(s.mounts, id)
	}
	for id, d := range want {
		ctx, cancel := context.WithCancel(context.Background())
		m := &mount{spec: d, key: specKey(d, s.cacheMax), cancel: cancel, done: make(chan struct{}), token: d.GetToken()}
		s.mounts[id] = m
		go s.loop(ctx, m, s.cacheMax)
	}
}

// Snapshot is every mount's last status plus its current token, re-sent when a
// new CP session starts so nothing reported while disconnected is lost.
func (s *Supervisor) Snapshot() []*v1.DriveUp {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*v1.DriveUp
	for _, m := range s.mounts {
		m.mu.Lock()
		if m.status != nil {
			out = append(out, &v1.DriveUp{Body: &v1.DriveUp_Status{Status: m.status}})
		}
		if m.token != "" && m.token != m.spec.GetToken() {
			out = append(out, &v1.DriveUp{Body: &v1.DriveUp_Token{Token: &v1.DriveToken{Id: m.spec.GetId(), Token: m.token}}})
		}
		m.mu.Unlock()
	}
	return out
}

// StopAll unmounts everything (container shutdown).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	ms := s.mounts
	s.mounts = map[string]*mount{}
	s.mu.Unlock()
	for _, m := range ms {
		m.stop()
	}
}

// Cleanup unmounts and removes leftovers under the mount root from a previous
// run, so a restarted sidecar never leaves the Bot a dead "Transport endpoint
// is not connected" directory.
func (s *Supervisor) Cleanup() {
	entries, _ := os.ReadDir(s.cfg.MountRoot)
	for _, e := range entries {
		p := filepath.Join(s.cfg.MountRoot, e.Name())
		if s.run.Mounted(p) {
			_ = s.run.Unmount(p)
		}
		_ = os.Remove(p)
	}
	_ = os.MkdirAll(s.cfg.RunDir, 0o700)
}

func (m *mount) stop() {
	m.cancel()
	<-m.done
}

func (m *mount) lastToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token
}

func (s *Supervisor) emitStatus(st *v1.DriveStatus) {
	s.emit(&v1.DriveUp{Body: &v1.DriveUp_Status{Status: st}})
}

func (s *Supervisor) setStatus(m *mount, state, detail string) {
	st := &v1.DriveStatus{Id: m.spec.GetId(), State: state, Detail: detail}
	m.mu.Lock()
	m.status = st
	m.mu.Unlock()
	s.emitStatus(st)
}

func (s *Supervisor) mountPath(dir string) string { return filepath.Join(s.cfg.MountRoot, dir) }
func (s *Supervisor) confPath(id string) string  { return filepath.Join(s.cfg.RunDir, id+".conf") }

// MountArgs is the rclone argv for one drive.
func MountArgs(spec *v1.DriveSpec, cfg Config, conf, cacheMax string) []string {
	args := []string{
		"mount", spec.GetRemote() + ":" + spec.GetPath(), filepath.Join(cfg.MountRoot, spec.GetDir()),
		"--config", conf,
		"--allow-other",
		"--uid", cfg.UID, "--gid", cfg.GID, "--umask", "002",
		"--vfs-cache-mode", "writes",
		"--cache-dir", filepath.Join(cfg.CacheRoot, spec.GetId()),
		"--dir-cache-time", "30s",
		"--log-level", "NOTICE",
	}
	if cacheMax != "" {
		args = append(args, "--vfs-cache-max-size", cacheMax)
	}
	if spec.GetReadOnly() {
		args = append(args, "--read-only")
	}
	return append(args, spec.GetFlags()...)
}

// childEnv gives rclone only this remote's config plus a minimal base.
func childEnv(spec *v1.DriveSpec) []string {
	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root"}
	prefix := envPrefix(spec.GetRemote())
	keys := make([]string, 0, len(spec.GetEnv()))
	for k := range spec.GetEnv() {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+spec.GetEnv()[k])
	}
	return env
}

// writeConf writes the private rclone config: only the token, so rclone has a
// file to save refreshes into. Env supplies every other option.
func writeConf(file, remote, token string) error {
	body := ""
	if token != "" {
		body = "[" + remote + "]\ntoken = " + strings.ReplaceAll(token, "\n", "") + "\n"
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// readToken reads the token line rclone keeps in a config file.
func readToken(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16<<10), 1<<20)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "token = "); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", sc.Err()
}

// loop owns one drive until ctx ends. It never takes s.mu: Apply holds it while
// waiting for a stopping loop to finish.
func (s *Supervisor) loop(ctx context.Context, m *mount, cacheMax string) {
	defer close(m.done)
	id := m.spec.GetId()
	mp := s.mountPath(m.spec.GetDir())
	conf := s.confPath(id)
	wait := s.cfg.BackoffMin
	defer func() {
		if s.run.Mounted(mp) {
			_ = s.run.Unmount(mp)
		}
		_ = os.Remove(mp)
		_ = os.Remove(conf)
		s.setStatus(m, StateStopped, "")
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.run.Mounted(mp) {
			_ = s.run.Unmount(mp)
		}
		if err := os.MkdirAll(mp, 0o755); err != nil {
			s.setStatus(m, StateError, "cannot create mount point: "+err.Error())
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		if err := writeConf(conf, m.spec.GetRemote(), m.lastToken()); err != nil {
			s.setStatus(m, StateError, "cannot write config: "+err.Error())
			if !sleep(ctx, wait) {
				return
			}
			continue
		}
		s.setStatus(m, StateMounting, "")
		p, err := s.run.Start(MountArgs(m.spec, s.cfg, conf, cacheMax), childEnv(m.spec))
		if err != nil {
			s.setStatus(m, StateError, err.Error())
			if !sleep(ctx, wait) {
				return
			}
			wait = min(wait*2, s.cfg.BackoffMax)
			continue
		}
		exited := make(chan error, 1)
		go func() { exited <- p.Wait() }()
		up := s.watch(ctx, m, mp, conf, exited)
		if ctx.Err() != nil {
			p.Stop()
			<-exited
			return
		}
		detail := CleanError(p.Tail())
		if detail == "" {
			detail = "rclone stopped"
		}
		s.setStatus(m, StateError, detail)
		if up {
			wait = s.cfg.BackoffMin
		}
		if !sleep(ctx, wait) {
			return
		}
		wait = min(wait*2, s.cfg.BackoffMax)
	}
}

// watch follows one running process: it reports mounted once the mount point
// appears, forwards token refreshes, and returns when the process exits or ctx
// ends. It reports whether the mount ever came up.
func (s *Supervisor) watch(ctx context.Context, m *mount, mp, conf string, exited chan error) bool {
	up := false
	deadline := time.Now().Add(s.cfg.MountWait)
	tick := time.NewTicker(s.cfg.Poll)
	defer tick.Stop()
	check := func() {
		if !up && s.run.Mounted(mp) {
			up = true
			s.setStatus(m, StateMounted, "")
		}
		if tok, err := readToken(conf); err == nil && tok != "" && tok != m.lastToken() {
			m.mu.Lock()
			m.token = tok
			m.mu.Unlock()
			s.emit(&v1.DriveUp{Body: &v1.DriveUp_Token{Token: &v1.DriveToken{Id: m.spec.GetId(), Token: tok}}})
		}
	}
	check()
	for {
		select {
		case <-ctx.Done():
			return up
		case err := <-exited:
			exited <- err
			return up
		case <-tick.C:
			check()
			if !up && time.Now().After(deadline) {
				s.setStatus(m, StateMounting, "still connecting…")
				deadline = time.Now().Add(s.cfg.MountWait)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var logPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\s+(?:[A-Z]+\s*:\s*)?`)

// CleanError picks the most useful rclone stderr line and drops its timestamp
// and level, so the UI shows "couldn't connect: … 401" instead of a log dump.
func CleanError(tail string) string {
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	best := ""
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if best == "" {
			best = l
		}
		if strings.Contains(l, "CRITICAL") || strings.Contains(l, "ERROR") || strings.Contains(l, "Fatal") || strings.Contains(l, "Failed") {
			best = l
			break
		}
	}
	best = logPrefix.ReplaceAllString(best, "")
	best = strings.TrimPrefix(best, "Fatal error: ")
	if r := []rune(best); len(r) > 400 {
		best = string(r[:400]) + "…"
	}
	return best
}

// List answers a one-shot folder listing with its own throwaway config file,
// so it works for drives that are not mounted (the add form).
func (s *Supervisor) List(ctx context.Context, req *v1.DriveList) *v1.DriveListResult {
	res := &v1.DriveListResult{RequestId: req.GetRequestId()}
	spec := req.GetSpec()
	if err := Validate(spec); err != nil {
		res.Error = err.Error()
		return res
	}
	p := strings.Trim(path.Clean("/"+req.GetPath()), "/")
	if strings.Contains(req.GetPath(), "..") {
		res.Error = "path must not contain .."
		return res
	}
	conf := filepath.Join(s.cfg.RunDir, "list-"+req.GetRequestId()+".conf")
	if !idRe.MatchString(req.GetRequestId()) {
		res.Error = "bad request id"
		return res
	}
	if err := writeConf(conf, spec.GetRemote(), spec.GetToken()); err != nil {
		res.Error = err.Error()
		return res
	}
	defer os.Remove(conf)
	full := strings.Trim(path.Join(spec.GetPath(), p), "/")
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ListTimeout)
	defer cancel()
	out, err := s.run.Output(ctx, []string{
		"lsjson", "--dirs-only", "--no-modtime", "--no-mimetype", "--config", conf, spec.GetRemote() + ":" + full,
	}, childEnv(spec))
	if tok, _ := readToken(conf); tok != "" && tok != spec.GetToken() {
		res.Token = tok
	}
	if err != nil {
		if ctx.Err() != nil {
			res.Error = "the provider took too long to answer"
		} else {
			res.Error = CleanError(err.Error())
		}
		return res
	}
	var items []struct {
		Name  string
		Path  string
		IsDir bool
	}
	if err := json.Unmarshal(out, &items); err != nil {
		res.Error = "unexpected rclone output"
		return res
	}
	for _, it := range items {
		if !it.IsDir {
			continue
		}
		res.Dirs = append(res.Dirs, &v1.DriveDir{Name: it.Name, Path: strings.Trim(path.Join(p, it.Path), "/")})
	}
	sort.Slice(res.Dirs, func(i, j int) bool { return strings.ToLower(res.Dirs[i].Name) < strings.ToLower(res.Dirs[j].Name) })
	return res
}
