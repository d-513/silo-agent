package llm

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Silo does not ship Claude Code: it is Anthropic's program under Anthropic's
// terms, not open source and not Silo's to pass on. The operator has it
// fetched (Admin → the provider's Install), and what is fetched is the same
// file `make claude-bin` and Anthropic's Agent SDK take: the platform package
// @anthropic-ai/claude-agent-sdk-<os>-<arch> on npm, whose one program is
// `claude`. It lands in <providers.claude.dir>/program, which the provider
// reads before the machine's PATH.

const (
	claudeProgramName  = "Claude Code"
	claudeProgramAbout = "Silo does not include Claude Code: it is Anthropic's program, under Anthropic's own terms. Installing downloads the newest release from Anthropic's package on npm (a few hundred MB) into Silo's data folder, and checks it against the checksum npm publishes."
	// claudeProgramMax bounds the download and the program taken out of it.
	claudeProgramMax = 2 << 30
	// claudeInstallWait is how long one install may take.
	claudeInstallWait = 30 * time.Minute
)

// claudeNpm is where the package is asked for; a test points it at its
// own server.
var claudeNpm = "https://registry.npmjs.org"

// programDir is where an installed copy lives; installed the program itself.
func (c claudeConf) programDir() string { return filepath.Join(c.dir, "program") }
func (c claudeConf) installed() string  { return filepath.Join(c.programDir(), "claude") }

// claudePlatform is this machine as the packages are named: linux-x64,
// linux-arm64-musl, darwin-arm64.
func claudePlatform() (string, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	osName := map[string]string{"linux": "linux", "darwin": "darwin"}[runtime.GOOS]
	if arch == "" || osName == "" {
		return "", fmt.Errorf("Claude Code has no release for %s/%s that Silo can install", runtime.GOOS, runtime.GOARCH)
	}
	p := osName + "-" + arch
	if runtime.GOOS == "linux" {
		if m, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(m) > 0 {
			if g, _ := filepath.Glob("/lib*/ld-linux-*.so.*"); len(g) == 0 {
				p += "-musl"
			}
		}
	}
	return p, nil
}

// claudeInstall is one install's progress, per program folder.
type claudeInstall struct {
	running     atomic.Bool
	done, total atomic.Int64
	mu          sync.Mutex
	problem     string
}

var claudeInstalls sync.Map // programDir -> *claudeInstall

func (c claudeConf) install() *claudeInstall {
	v, _ := claudeInstalls.LoadOrStore(c.programDir(), &claudeInstall{})
	return v.(*claudeInstall)
}

// claudeVersions remembers what a program said its version is, by the file
// it was (path, size, time): the page asks while it is open.
var claudeVersions sync.Map

func (c claudeConf) version(ctx context.Context, bin string) (string, error) {
	fi, err := os.Stat(bin)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", bin, fi.Size(), fi.ModTime().UnixNano())
	if v, ok := claudeVersions.Load(key); ok {
		return v.(string), nil
	}
	if err := c.mkdirs(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.Command(bin, "--version")
	cmd.Dir = c.workDir()
	cmd.Env = c.env()
	out, err := runClaude(ctx, cmd)
	v := strings.TrimSpace(string(out))
	if err != nil || v == "" {
		if err == nil {
			err = errors.New("it printed nothing")
		}
		return "", fmt.Errorf("the program at %s does not run here: %w", bin, err)
	}
	// "2.1.90 (Claude Code)": the number is the version.
	v, _, _ = strings.Cut(v, " ")
	claudeVersions.Store(key, v)
	return v, nil
}

// ProgramStatus is the Claude Code program on this machine.
func (c *claudeClient) ProgramStatus(ctx context.Context) ProgramStatus {
	st := ProgramStatus{Name: claudeProgramName, About: claudeProgramAbout, CanInstall: c.conf.binary == ""}
	in := c.conf.install()
	st.Installing = in.running.Load()
	st.Done, st.Total = in.done.Load(), in.total.Load()
	in.mu.Lock()
	st.Problem = in.problem
	in.mu.Unlock()
	bin, err := c.conf.path()
	if err != nil {
		return st
	}
	st.Installed, st.Path, st.Managed = true, bin, bin == c.conf.installed()
	if v, err := c.conf.version(ctx, bin); err != nil {
		if st.Problem == "" {
			st.Problem = err.Error()
		}
	} else {
		st.Version = v
	}
	return st
}

// InstallProgram starts fetching the newest Claude Code.
func (c *claudeClient) InstallProgram(context.Context) error {
	if c.conf.binary != "" {
		return errors.New("providers.claude.binary names the program to run; empty that setting to have Silo install its own")
	}
	if _, err := claudePlatform(); err != nil {
		return err
	}
	in := c.conf.install()
	if !in.running.CompareAndSwap(false, true) {
		return nil
	}
	in.done.Store(0)
	in.total.Store(0)
	in.mu.Lock()
	in.problem = ""
	in.mu.Unlock()
	conf := c.conf
	go func() {
		defer in.running.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), claudeInstallWait)
		defer cancel()
		if err := conf.fetch(ctx, in); err != nil {
			in.mu.Lock()
			in.problem = "Installing Claude Code failed: " + err.Error()
			in.mu.Unlock()
			return
		}
		// A new program may list other models and effort levels.
		claudeModelCache.Lock()
		clear(claudeModelCache.m)
		claudeModelCache.Unlock()
	}()
	return nil
}

// fetch downloads the platform package, checks it, and puts its program in
// place of the installed one. Processes running the old file keep it.
func (c claudeConf) fetch(ctx context.Context, in *claudeInstall) error {
	platform, err := claudePlatform()
	if err != nil {
		return err
	}
	dir := c.programDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// What an install that was cut off left behind.
	if old, _ := filepath.Glob(filepath.Join(dir, ".new-*")); len(old) > 0 {
		for _, f := range old {
			_ = os.Remove(f)
		}
	}

	var meta struct {
		Version string `json:"version"`
		Dist    struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	pkg := "@anthropic-ai/claude-agent-sdk-" + platform
	body, err := claudeGet(ctx, claudeNpm+"/"+pkg+"/latest")
	if err != nil {
		return fmt.Errorf("ask npm for %s: %w", pkg, err)
	}
	err = json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(&meta)
	body.Close()
	if err != nil {
		return fmt.Errorf("read npm's answer for %s: %w", pkg, err)
	}
	want, ok := strings.CutPrefix(meta.Dist.Integrity, "sha512-")
	if !ok || meta.Dist.Tarball == "" {
		return fmt.Errorf("npm's answer for %s names no sha512 checksum", pkg)
	}
	// The package is the registry's to serve: an answer that sends the
	// download elsewhere is not followed.
	if a, b := hostOf(meta.Dist.Tarball), hostOf(claudeNpm); a == "" || a != b {
		return fmt.Errorf("npm's answer for %s points at another server", pkg)
	}

	resp, err := claudeGetResp(ctx, meta.Dist.Tarball)
	if err != nil {
		return fmt.Errorf("download %s %s: %w", pkg, meta.Version, err)
	}
	defer resp.Body.Close()
	if resp.ContentLength > 0 {
		in.total.Store(resp.ContentLength)
	}
	// The archive is hashed whole while the program is taken out of it, and
	// the program is kept only if the hash is the published one.
	sum := sha512.New()
	counted := &countReader{r: io.LimitReader(resp.Body, claudeProgramMax), n: &in.done}
	tmp, err := os.CreateTemp(dir, ".new-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	found, err := untarProgram(io.TeeReader(counted, sum), tmp)
	if err == nil {
		// Whatever follows the last entry is part of what was hashed.
		_, err = io.Copy(io.Discard, io.TeeReader(counted, sum))
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s %s: %w", pkg, meta.Version, err)
	}
	if got := base64.StdEncoding.EncodeToString(sum.Sum(nil)); got != want {
		return fmt.Errorf("the download of %s %s does not match npm's checksum; nothing was installed", pkg, meta.Version)
	}
	if !found {
		return fmt.Errorf("%s %s holds no claude program", pkg, meta.Version)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	if _, err := c.version(ctx, tmp.Name()); err != nil {
		return fmt.Errorf("the downloaded program does not run on this server: %w", err)
	}
	return os.Rename(tmp.Name(), c.installed())
}

// untarProgram copies package/claude out of the gzipped archive r into w,
// reading the archive to its end.
func untarProgram(r io.Reader, w io.Writer) (bool, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return false, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return false, err
		}
		if found || h.Typeflag != tar.TypeReg || h.Name != "package/claude" {
			continue
		}
		if h.Size > claudeProgramMax {
			return false, errors.New("the program in it is too large")
		}
		if _, err := io.Copy(w, tr); err != nil {
			return false, err
		}
		found = true
	}
}

type countReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func claudeGet(ctx context.Context, addr string) (io.ReadCloser, error) {
	resp, err := claudeGetResp(ctx, addr)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func claudeGetResp(ctx context.Context, addr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("the server answered %s", resp.Status)
	}
	return resp, nil
}
