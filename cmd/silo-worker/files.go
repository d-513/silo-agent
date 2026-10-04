package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	v1 "silo.agent/gen/silo/v1"
)

func (w *worker) resolve(p string) (string, error) {
	p = strings.TrimSpace(filepath.ToSlash(p))
	ws := filepath.ToSlash(filepath.Clean(w.workspace))
	switch {
	case p == "" || p == ".":
		p = "."
	case p == ws || p == "/workspace" || p == "workspace":
		p = "."
	case strings.HasPrefix(p, ws+"/"):
		p = strings.TrimPrefix(p, ws+"/")
	case strings.HasPrefix(p, "/workspace/"):
		p = strings.TrimPrefix(p, "/workspace/")
	case strings.HasPrefix(p, "workspace/"):
		p = strings.TrimPrefix(p, "workspace/")
	}
	if p == "" {
		p = "."
	}
	full := filepath.Join(w.workspace, p)
	rel, err := filepath.Rel(w.workspace, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.New("path escapes workspace")
	}
	return full, nil
}

const (
	browseLimit  = 2 << 20  // Files preview / default browse budget
	presentLimit = 32 << 20 // explicit high budget the CP asks for on present
	putLimit     = 50 << 20 // uploads via PutFile
)

// Read slicing happens on the Worker so a huge file never crosses the RPC
// whole; the CP only adds line numbers.
const (
	readDefaultLimit = 2000
	readMaxBytes     = 2 << 20
)

// readView is the JSON the CP parses for the `read` tool. next_offset/total_lines
// are omitted when unknown (the slice stopped before EOF).
type readView struct {
	Content    string `json:"content"`
	NextOffset int    `json:"next_offset,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// scanLinesKeepCR is bufio.ScanLines without dropping a trailing carriage
// return, so a CRLF file round-trips through read -> patch unchanged.
func scanLinesKeepCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// readFileSlice returns a slice of a workspace file. offset is 1-based; a
// missing/zero limit defaults to readDefaultLimit lines. The slice is capped at
// readMaxBytes so a minified or binary file cannot exhaust memory.
func (w *worker) readFileSlice(p string, offset, limit int) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", errors.New("is a directory")
	}
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 {
		limit = readDefaultLimit
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), readMaxBytes)
	// Keep a trailing \r so a CRLF file read back through `patch` still matches.
	sc.Split(scanLinesKeepCR)
	var b strings.Builder
	line, kept := 0, 0
	stopped := false
	for sc.Scan() {
		text := sc.Text()
		line++
		if line < offset {
			continue
		}
		if strings.IndexByte(text, 0) >= 0 {
			return "", errors.New("binary file; use terminal to inspect it")
		}
		if kept >= limit || b.Len()+len(text)+1 > readMaxBytes {
			stopped = true
			break
		}
		b.WriteString(text)
		b.WriteByte('\n')
		kept++
	}
	scanErr := sc.Err()
	if errors.Is(scanErr, bufio.ErrTooLong) {
		return "", errors.New("a line is too long to read (over 2 MB); use terminal to inspect it")
	}
	if scanErr != nil {
		return "", scanErr
	}
	view := readView{Content: b.String()}
	if stopped {
		view.Truncated = true
		view.NextOffset = offset + kept
	} else {
		view.TotalLines = line
	}
	out, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

const grepDefaultMax = 80

// grepPruneDirs are heavyweight trees rg should not descend into. Hidden dirs
// (.git, .cache) are already skipped by rg's default ignore.
var grepPruneDirs = []string{
	"node_modules", ".venv", "venv", "__pycache__", "chrome-profile",
	"dist", "build", "target",
}

// grepArgs builds the rg argv. No shell: the pattern and path are single argv
// entries, so metacharacters are data, not commands. --sort path makes output
// deterministic and grouped by file.
func grepArgs(pattern, path, include string, maxHits int) []string {
	if path == "" {
		path = "."
	}
	args := []string{
		"--line-number", "--no-heading", "--with-filename",
		"--color=never", "--sort", "path",
		"--max-columns", "2000", "--max-columns-preview",
	}
	for _, d := range grepPruneDirs {
		args = append(args, "--glob", "!**/"+d+"/**")
	}
	// Drives are remote storage: a workspace-wide search must not crawl a
	// whole Google Drive over the network. Searching inside one still works.
	if !inDrives(path) {
		args = append(args, "--glob", "!/drives/**")
	}
	if include != "" {
		args = append(args, "--glob", include)
	}
	return append(args, "-e", pattern, "--", path)
}

// grep runs ripgrep and reads at most maxHits lines, killing rg once the cap is
// reached so a broad scan cannot flood the run. It does not go through the shell
// path, so no tool_chunk events are emitted for it.
func (w *worker) grep(ctx context.Context, g *v1.GrepCmd) (string, error) {
	if strings.TrimSpace(g.GetPattern()) == "" {
		return "", errors.New("pattern required")
	}
	max := int(g.GetMaxHits())
	if max <= 0 {
		max = grepDefaultMax
	}
	c := exec.CommandContext(ctx, "rg", grepArgs(g.GetPattern(), g.GetPath(), g.GetInclude(), max)...)
	c.Dir = w.workspace
	stdout, err := c.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		return "", fmt.Errorf("search unavailable: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var b strings.Builder
	hits := 0
	early := false
	for sc.Scan() {
		if hits >= max {
			early = true
			break
		}
		b.WriteString(sc.Text())
		b.WriteByte('\n')
		hits++
	}
	if early && c.Process != nil {
		_ = c.Process.Kill()
	}
	waitErr := c.Wait()
	if early {
		return b.String(), nil
	}
	if sc.Err() != nil {
		return "", sc.Err()
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		if ee.ExitCode() == 1 { // no matches is not an error
			return "", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = ee.Error()
		}
		return "", fmt.Errorf("search failed: %s", msg)
	}
	if waitErr != nil {
		return "", waitErr
	}
	return b.String(), nil
}

type dirEnt struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type fileView struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	Data      string `json:"data"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
}

func (w *worker) relPath(full string) string {
	rel, err := filepath.Rel(w.workspace, full)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (w *worker) listDir(p string) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return "", err
	}
	out := make([]dirEnt, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		child := filepath.Join(full, e.Name())
		out = append(out, dirEnt{
			Name:     e.Name(),
			Path:     w.relPath(child),
			Dir:      e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (w *worker) browseFile(p string, limit int64) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", errors.New("is a directory")
	}
	if limit <= 0 || limit > presentLimit {
		limit = browseLimit
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, limit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	raw := buf[:n]
	trunc := st.Size() > int64(len(raw)) || n > int(limit)
	if n > int(limit) {
		raw = raw[:limit]
	}
	view := fileView{
		Name:      filepath.Base(full),
		Size:      st.Size(),
		Truncated: trunc,
		Data:      base64.StdEncoding.EncodeToString(raw),
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		view.Binary = true
	} else {
		view.Content = string(raw)
	}
	b, err := json.Marshal(view)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (w *worker) mkdir(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path required")
	}
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	return "ok", os.MkdirAll(full, 0o755)
}

// inDrives is true for a workspace path at or under drives/.
func inDrives(p string) bool {
	p = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "/workspace/")
	p = strings.TrimPrefix(p, "./")
	return p == "drives" || strings.HasPrefix(p, "drives/")
}

// driveRoot is true for /workspace/drives and each /workspace/drives/<name>:
// mount points of remote storage, which only the Drives tab adds or removes.
func (w *worker) driveRoot(full string) bool {
	rel, err := filepath.Rel(w.workspace, full)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "drives" || (strings.HasPrefix(rel, "drives/") && !strings.Contains(strings.TrimPrefix(rel, "drives/"), "/"))
}

func (w *worker) remove(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("cannot remove workspace root")
	}
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	if full == w.workspace {
		return "", errors.New("cannot remove workspace root")
	}
	if w.driveRoot(full) {
		return "", errors.New("cannot remove a drive: it is remote storage mounted by the owner (Drives tab). Delete files inside it instead")
	}
	return "ok", os.RemoveAll(full)
}

func (w *worker) putFile(p string, data []byte) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path required")
	}
	if len(data) > putLimit {
		return "", fmt.Errorf("file too large (%d bytes, max %d)", len(data), putLimit)
	}
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	return "ok", os.WriteFile(full, data, 0o644)
}
