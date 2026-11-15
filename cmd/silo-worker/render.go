package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

// The page reader's two jobs that need the box: running a page's scripts, and
// opening a document. The Control Plane fetches and extracts; a stranger's
// JavaScript and a stranger's PDF are handled here, where the Bot's own
// browser already handles them.

const (
	// renderWait is how long a page gets to settle; renderKill when its
	// scripts are stopped whatever they are doing; renderTimeout the
	// process's own deadline.
	renderWait    = "8000"
	renderKill    = "25000"
	renderTimeout = 40 * time.Second
	// renderMax caps the document handed back.
	renderMax = "5242880"
	// docMax is the largest document read.
	docMax = 32 << 20
)

// lightpanda is the headless browser's binary; SILO_LIGHTPANDA names another
// (tests).
func lightpanda() string {
	if p := os.Getenv("SILO_LIGHTPANDA"); p != "" {
		return p
	}
	return "lightpanda"
}

// render loads a page in Lightpanda, one process per page, and returns the
// DOM after its scripts ran.
func render(ctx context.Context, c *v1.RenderCmd) (string, error) {
	u, err := url.Parse(c.GetUrl())
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", errors.New("only http and https pages can be rendered")
	}
	args := []string{
		"fetch", u.String(), "--json", "--dump", "html", "--with-base",
		"--wait-until", "networkalmostidle", "--wait-ms", renderWait, "--terminate-ms", renderKill,
		"--http-timeout", "15000", "--dump-max-bytes", renderMax, "--log-level", "error",
	}
	if c.GetBlockPrivate() {
		args = append(args, "--block-private-networks")
	}
	if l := strings.TrimSpace(c.GetLocale()); l != "" {
		args = append(args, "--locale", l)
	}
	// Behind the egress gateway every site's certificate is the gateway's:
	// the bundle start.sh wrote holds its CA beside the system's roots.
	if ca := os.Getenv("SSL_CERT_FILE"); ca != "" {
		if _, err := os.Stat(ca); err == nil {
			args = append(args, "--ca-cert", ca)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, lightpanda(), args...)
	cmd.Env = append(os.Environ(), "LIGHTPANDA_DISABLE_TELEMETRY=true")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, os.ErrNotExist) {
		return "", errors.New("lightpanda is not installed in this Bot's image (reset the container to get the current one)")
	}
	// A failed page still prints its status as JSON and exits 1.
	var res struct {
		URL     string  `json:"url"`
		Status  int     `json:"http_status"`
		Content string  `json:"content"`
		Error   *string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &res) != nil {
		if ctx.Err() != nil {
			return "", errors.New("the page did not finish loading in time")
		}
		msg := lastLine(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("lightpanda failed: %s", msg)
	}
	if res.Error != nil && *res.Error != "" {
		return "", fmt.Errorf("the page could not be loaded (%s)", *res.Error)
	}
	b, err := json.Marshal(map[string]any{"url": res.URL, "status": res.Status, "html": validUTF8(res.Content)})
	return string(b), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := lines[len(lines)-1]
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}

// readDoc writes a fetched document to a scratch file, reads its text like a
// workspace file's, and removes it.
func readDoc(ctx context.Context, c *v1.ReadDocCmd) (string, error) {
	if len(c.GetData()) > docMax {
		return "", fmt.Errorf("document too large (%d bytes, max %d)", len(c.GetData()), docMax)
	}
	dir, err := os.MkdirTemp("", "silo-doc")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	// Only the extension of the name is used: it is a stranger's text.
	ext := strings.ToLower(filepath.Ext(filepath.Base(c.GetName())))
	if len(ext) > 6 || strings.ContainsAny(ext, "/\\ ") {
		ext = ""
	}
	full := filepath.Join(dir, "document"+ext)
	if err := os.WriteFile(full, c.GetData(), 0o600); err != nil {
		return "", err
	}
	return extractAt(ctx, full, 0, c.GetOcr())
}
