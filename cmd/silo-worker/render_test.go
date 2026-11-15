package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "silo.agent/gen/silo/v1"
)

// fakeLightpanda stands a script in for the browser: it records its
// arguments and environment and prints what the test gives it.
func fakeLightpanda(t *testing.T, stdout string, exit int) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	out := filepath.Join(dir, "out")
	if err := os.WriteFile(out, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nprintf 'telemetry=%s\\n' \"$LIGHTPANDA_DISABLE_TELEMETRY\" >> " + argsFile +
		"\ncat " + out + "\necho 'level=error msg=\"page failed\"' >&2\nexit " + string(rune('0'+exit)) + "\n"
	bin := filepath.Join(dir, "lightpanda")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SILO_LIGHTPANDA", bin)
	return argsFile
}

func TestRenderRunsLightpandaOncePerPage(t *testing.T) {
	ca := filepath.Join(t.TempDir(), "bundle.crt")
	if err := os.WriteFile(ca, []byte("PEM"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", ca)
	argsFile := fakeLightpanda(t, `{"url":"https://example.com/app","http_status":200,"headers":[],"dump":"html","content":"<html><body>drawn</body></html>","error":null}`, 0)

	raw, err := render(context.Background(), &v1.RenderCmd{Url: "https://example.com/app", BlockPrivate: true, Locale: "pl-PL"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		URL    string `json:"url"`
		Status int    `json:"status"`
		HTML   string `json:"html"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil || got.URL != "https://example.com/app" || got.Status != 200 || got.HTML != "<html><body>drawn</body></html>" {
		t.Fatalf("result %q: %v", raw, err)
	}
	b, _ := os.ReadFile(argsFile)
	args := strings.Join(strings.Split(strings.TrimSpace(string(b)), "\n"), " ")
	for _, want := range []string{
		"fetch https://example.com/app --json --dump html --with-base", "--wait-until networkalmostidle",
		"--block-private-networks", "--locale pl-PL", "--ca-cert " + ca, "telemetry=true",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("lightpanda was run without %q: %s", want, args)
		}
	}

	// The operator's switch off, and no gateway CA: neither flag is passed.
	t.Setenv("SSL_CERT_FILE", "")
	if _, err := render(context.Background(), &v1.RenderCmd{Url: "https://example.com/app"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(argsFile)
	if strings.Contains(string(b), "--block-private-networks") || strings.Contains(string(b), "--ca-cert") || strings.Contains(string(b), "--locale") {
		t.Fatalf("flags nobody asked for: %s", b)
	}
}

func TestRenderSaysWhyAPageFailed(t *testing.T) {
	fakeLightpanda(t, `{"url":"https://nope.invalid/","http_status":0,"content":"<html>Navigation failed</html>","error":"CouldntResolveHost"}`, 1)
	if _, err := render(context.Background(), &v1.RenderCmd{Url: "https://nope.invalid/"}); err == nil || !strings.Contains(err.Error(), "CouldntResolveHost") {
		t.Fatalf("failed page: %v", err)
	}
	fakeLightpanda(t, "", 1)
	if _, err := render(context.Background(), &v1.RenderCmd{Url: "https://example.com/"}); err == nil || !strings.Contains(err.Error(), `lightpanda failed: level=error msg="page failed"`) {
		t.Fatalf("crashed browser: %v", err)
	}
	t.Setenv("SILO_LIGHTPANDA", filepath.Join(t.TempDir(), "missing"))
	if _, err := render(context.Background(), &v1.RenderCmd{Url: "https://example.com/"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing browser: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "http://", "--version"} {
		if _, err := render(context.Background(), &v1.RenderCmd{Url: bad}); err == nil || !strings.Contains(err.Error(), "only http and https") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

func TestReadDocReadsAndForgetsTheFile(t *testing.T) {
	raw, err := readDoc(context.Background(), &v1.ReadDocCmd{Name: "../../etc/notes.txt", Data: []byte("words in a file\n")})
	if err != nil {
		t.Fatal(err)
	}
	var got extractResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil || got.Kind != "text" || got.Text != "words in a file\n" {
		t.Fatalf("result %q: %v", raw, err)
	}
	left, _ := filepath.Glob(filepath.Join(os.TempDir(), "silo-doc*"))
	if len(left) != 0 {
		t.Fatalf("scratch files left behind: %v", left)
	}
}
