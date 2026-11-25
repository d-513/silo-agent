package llm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeNpm serves one platform package whose program is a script that says
// its version, as the registry does: metadata at /<pkg>/latest, then the
// archive. tamper changes the archive after its checksum was published.
func fakeNpm(t *testing.T, version string, tamper bool) {
	t.Helper()
	platform, err := claudePlatform()
	if err != nil {
		t.Skip(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"package/claude":       "#!/bin/sh\necho '" + version + " (Claude Code)'\n",
		"package/LICENSE.md":   "theirs",
		"package/../../escape": "no",
	} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	sum := sha512.Sum512(buf.Bytes())
	archive := buf.Bytes()
	if tamper {
		archive = append(bytes.Clone(archive), 0)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/@anthropic-ai/claude-agent-sdk-" + platform + "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.0.1", "dist": map[string]string{
				"tarball":   srv.URL + "/pkg.tgz",
				"integrity": "sha512-" + base64.StdEncoding.EncodeToString(sum[:]),
			}})
		case "/pkg.tgz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prev := claudeNpm
	claudeNpm = srv.URL
	t.Cleanup(func() { claudeNpm = prev })
}

func installAndWait(t *testing.T, c *claudeClient) ProgramStatus {
	t.Helper()
	if err := c.InstallProgram(context.Background()); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if st := c.ProgramStatus(context.Background()); !st.Installing {
			return st
		}
	}
	t.Fatal("the install never ended")
	return ProgramStatus{}
}

// Claude Code is not part of Silo: with none on the machine the provider says
// how to get it, the install fetches the publisher's package into the
// provider's own folder, and that copy is the one that runs from then on.
func TestClaudeProgramIsInstalledOnDemand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	c := &claudeClient{conf: claudeConf{dir: dir}}
	if _, err := c.conf.path(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("with no program: %v", err)
	}
	if st := c.ProgramStatus(context.Background()); st.Installed || !st.CanInstall || st.Problem != "" {
		t.Fatalf("before: %+v", st)
	}

	fakeNpm(t, "9.9.9", false)
	st := installAndWait(t, c)
	want := filepath.Join(dir, "program", "claude")
	if !st.Installed || !st.Managed || st.Version != "9.9.9" || st.Path != want || st.Problem != "" {
		t.Fatalf("after: %+v", st)
	}
	if st.Done == 0 {
		t.Fatal("no progress was counted")
	}
	if p, err := c.conf.path(); err != nil || p != want {
		t.Fatalf("path %q %v", p, err)
	}
	left, _ := os.ReadDir(filepath.Join(dir, "program"))
	if len(left) != 1 {
		t.Fatalf("the program folder holds %d files", len(left))
	}

	// A newer release replaces it.
	fakeNpm(t, "10.0.0", false)
	if st := installAndWait(t, c); st.Version != "10.0.0" {
		t.Fatalf("update: %+v", st)
	}
}

// A download that is not what npm published is never installed, and the one
// already there stays.
func TestClaudeProgramChecksTheDownload(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := &claudeClient{conf: claudeConf{dir: t.TempDir()}}
	fakeNpm(t, "9.9.9", false)
	installAndWait(t, c)

	fakeNpm(t, "6.6.6", true)
	st := installAndWait(t, c)
	if !strings.Contains(st.Problem, "checksum") || st.Version != "9.9.9" {
		t.Fatalf("a tampered download: %+v", st)
	}
}

// A program the operator named is theirs: it runs, and Silo installs nothing
// over it.
func TestClaudeProgramLeavesTheOperatorsOwn(t *testing.T) {
	own := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(own, []byte("#!/bin/sh\necho '1.2.3 (Claude Code)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &claudeClient{conf: claudeConf{dir: t.TempDir(), binary: own}}
	st := c.ProgramStatus(context.Background())
	if !st.Installed || st.Managed || st.CanInstall || st.Version != "1.2.3" {
		t.Fatalf("%+v", st)
	}
	if err := c.InstallProgram(context.Background()); err == nil {
		t.Fatal("installed over the operator's own program")
	}
}
