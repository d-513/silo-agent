package main

// The artifact store keeps what a Bot handed the human as it was at that
// moment, so a card never changes under them. It lives outside the workspace
// (/var/lib/silo/artifacts, bound from the host so a container reset keeps it)
// where the Bot's file tools, grep, Knowledge and the history never reach.
//
// Layout:
//
//	blobs/<aa>/<sha256>   content, written once (temp file, fsync, rename)
//	pins/<id>             one per pinned version: "file" or "tree"
//	tmp/                  half-written files; swept when the store opens
//
// A file's pin id is the sha256 of its bytes; a folder's is the sha256 of its
// manifest (a blob listing path, sha256, size and mode of every file). The
// control plane hashes whatever it reads against the id it asked for, so an
// altered blob reads as altered, never as other content.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	v1 "silo.agent/gen/silo/v1"
)

const (
	defaultArtifactFile  = 32 << 20
	defaultArtifactFiles = 1000
	defaultArtifactTree  = 25 << 20
	defaultArtifactStore = 2 << 30
	// previewText is how much of a text file the card shows.
	previewText  = 900
	previewLines = 14
	// previewImage caps the card's JPEG (base64 in the event).
	previewImage = 96 << 10
)

func artifactsDir() string {
	if d := os.Getenv("SILO_ARTIFACTS_DIR"); d != "" {
		return d
	}
	return "/var/lib/silo/artifacts"
}

// manifestEntry is one file of a pinned folder.
type manifestEntry struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

type manifest struct {
	V       int             `json:"v"`
	Entries []manifestEntry `json:"entries"`
}

type artifactPreview struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
	// More: the text goes on past the excerpt.
	More bool `json:"more,omitempty"`
}

type pinResult struct {
	ID      string           `json:"id,omitempty"`
	Kind    string           `json:"kind"`
	Name    string           `json:"name"`
	Size    int64            `json:"size"`
	Entries []manifestEntry  `json:"entries,omitempty"`
	Preview *artifactPreview `json:"preview,omitempty"`
	// TooLarge says why nothing was pinned; the card then shows the live file.
	TooLarge string   `json:"too_large,omitempty"`
	Evicted  []string `json:"evicted,omitempty"`
}

type artifactStore struct {
	root string
}

func (w *worker) artifactStore() (*artifactStore, error) {
	if w.artifacts == nil {
		st, err := openArtifactStore(artifactsDir())
		if err != nil {
			return nil, err
		}
		w.artifacts = st
	}
	return w.artifacts, nil
}

func openArtifactStore(root string) (*artifactStore, error) {
	st := &artifactStore{root: root}
	for _, d := range []string{"blobs", "pins", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, err
		}
	}
	// Whatever is in tmp/ was being written when the worker last stopped.
	if ents, err := os.ReadDir(filepath.Join(root, "tmp")); err == nil {
		for _, e := range ents {
			_ = os.RemoveAll(filepath.Join(root, "tmp", e.Name()))
		}
	}
	return st, nil
}

func validSHA(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

func (st *artifactStore) blobPath(sha string) string {
	return filepath.Join(st.root, "blobs", sha[:2], sha)
}

// put copies r into the store and returns its sha256 and size. More than max
// bytes is an error and leaves nothing behind.
func (st *artifactStore) put(r io.Reader, max int64) (string, int64, error) {
	tmp, err := os.CreateTemp(filepath.Join(st.root, "tmp"), "blob-")
	if err != nil {
		return "", 0, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, max+1))
	if err == nil && n > max {
		err = errTooLarge
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	dst := st.blobPath(sha)
	if _, err := os.Stat(dst); err == nil {
		return sha, n, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", 0, err
	}
	if err := os.Rename(name, dst); err != nil {
		return "", 0, err
	}
	syncDir(filepath.Dir(dst))
	return sha, n, nil
}

var errTooLarge = errors.New("too large")

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// writePin records a pinned version last, after every blob it needs is in.
func (st *artifactStore) writePin(id, kind string) error {
	tmp, err := os.CreateTemp(filepath.Join(st.root, "tmp"), "pin-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = tmp.WriteString(kind)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(st.root, "pins", id)); err != nil {
		return err
	}
	syncDir(filepath.Join(st.root, "pins"))
	return nil
}

func (w *worker) artifactPin(ctx context.Context, c *v1.ArtifactPinCmd) (string, error) {
	w.artMu.Lock()
	defer w.artMu.Unlock()
	st, err := w.artifactStore()
	if err != nil {
		return "", err
	}
	full, err := w.resolve(c.GetPath())
	if err != nil {
		return "", err
	}
	if full == filepath.Clean(w.workspace) {
		return "", errors.New("the workspace root is not an artifact: name a file or a folder in it")
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	maxFile := orDefault(c.GetMaxFileBytes(), defaultArtifactFile)
	maxFiles := int(c.GetMaxTreeFiles())
	if maxFiles <= 0 {
		maxFiles = defaultArtifactFiles
	}
	maxTree := orDefault(c.GetMaxTreeBytes(), defaultArtifactTree)
	res := pinResult{Name: filepath.Base(full)}
	switch {
	case info.IsDir():
		res.Kind = "tree"
		if err := requireAny(full, c.GetTreeRequires()); err != nil {
			return "", err
		}
		entries, err := treeFiles(full, maxFiles, maxTree)
		if errors.Is(err, errTooLarge) {
			res.TooLarge = fmt.Sprintf("the folder is over %d files or %d MB", maxFiles, maxTree>>20)
			return jsonString(res)
		}
		if err != nil {
			return "", err
		}
		if err := st.pinTree(ctx, &res, full, entries, maxFile); err != nil {
			if errors.Is(err, errTooLarge) {
				res.TooLarge = fmt.Sprintf("a file in the folder is over %d MB", maxFile>>20)
				res.Entries = nil
				return jsonString(res)
			}
			return "", err
		}
	case !info.Mode().IsRegular():
		return "", errors.New("not a regular file")
	case c.GetAsTree():
		res.Kind = "tree"
		if info.Size() > maxFile {
			res.TooLarge = fmt.Sprintf("the file is over %d MB", maxFile>>20)
			return jsonString(res)
		}
		one := []treeFile{{rel: filepath.Base(full), full: full, size: info.Size(), mode: info.Mode()}}
		if err := st.pinTree(ctx, &res, full, one, maxFile); err != nil {
			if errors.Is(err, errTooLarge) {
				res.TooLarge = fmt.Sprintf("the file is over %d MB", maxFile>>20)
				return jsonString(res)
			}
			return "", err
		}
	default:
		res.Kind = "file"
		res.Size = info.Size()
		if info.Size() > maxFile {
			res.TooLarge = fmt.Sprintf("the file is over %d MB", maxFile>>20)
			if c.GetPreview() {
				res.Preview = makePreview(ctx, full)
			}
			return jsonString(res)
		}
		f, err := os.Open(full)
		if err != nil {
			return "", err
		}
		sha, n, err := st.put(f, maxFile)
		f.Close()
		if errors.Is(err, errTooLarge) {
			res.TooLarge = fmt.Sprintf("the file is over %d MB", maxFile>>20)
			return jsonString(res)
		}
		if err != nil {
			return "", err
		}
		if err := st.writePin(sha, "file"); err != nil {
			return "", err
		}
		res.ID, res.Size = sha, n
		if c.GetPreview() {
			res.Preview = makePreview(ctx, st.blobPath(sha), filepath.Base(full))
		}
	}
	res.Evicted = st.enforce(orDefault(c.GetMaxStoreBytes(), defaultArtifactStore), res.ID)
	return jsonString(res)
}

func orDefault(v, d int64) int64 {
	if v <= 0 {
		return d
	}
	return v
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func requireAny(dir string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	for _, n := range names {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.Mode().IsRegular() {
			return nil
		}
	}
	return fmt.Errorf("a folder artifact must hold %s at its top", strings.Join(names, " or "))
}

type treeFile struct {
	rel  string
	full string
	size int64
	mode fs.FileMode
}

// artifactSkipDirs are never copied into a folder artifact.
var artifactSkipDirs = map[string]bool{".git": true, "node_modules": true, "__pycache__": true, ".venv": true}

// treeFiles lists a folder's regular files (links are not followed), sorted.
func treeFiles(root string, maxFiles int, maxBytes int64) ([]treeFile, error) {
	var out []treeFile
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && artifactSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		total += fi.Size()
		out = append(out, treeFile{rel: filepath.ToSlash(rel), full: p, size: fi.Size(), mode: fi.Mode()})
		if len(out) > maxFiles || total > maxBytes {
			return errTooLarge
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

func (st *artifactStore) pinTree(ctx context.Context, res *pinResult, full string, files []treeFile, maxFile int64) error {
	m := manifest{V: 1, Entries: make([]manifestEntry, 0, len(files))}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		fh, err := os.Open(f.full)
		if err != nil {
			return err
		}
		sha, n, err := st.put(fh, maxFile)
		fh.Close()
		if err != nil {
			return err
		}
		mode := uint32(0o644)
		if f.mode&0o111 != 0 {
			mode = 0o755
		}
		m.Entries = append(m.Entries, manifestEntry{Path: f.rel, SHA: sha, Size: n, Mode: mode})
		res.Size += n
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	id, _, err := st.put(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	if err := st.writePin(id, "tree"); err != nil {
		return err
	}
	res.ID, res.Entries = id, m.Entries
	return nil
}

func (w *worker) artifactRead(c *v1.ArtifactReadCmd) (string, error) {
	w.artMu.Lock()
	defer w.artMu.Unlock()
	st, err := w.artifactStore()
	if err != nil {
		return "", err
	}
	id := c.GetId()
	if !validSHA(id) {
		return "", errors.New("bad artifact id")
	}
	raw, err := os.ReadFile(st.blobPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("artifact not kept")
	}
	if err != nil {
		return "", err
	}
	return jsonString(struct {
		Size int64  `json:"size"`
		Data string `json:"data"`
	}{int64(len(raw)), base64.StdEncoding.EncodeToString(raw)})
}

func (w *worker) artifactForget(c *v1.ArtifactForgetCmd) (string, error) {
	w.artMu.Lock()
	defer w.artMu.Unlock()
	st, err := w.artifactStore()
	if err != nil {
		return "", err
	}
	n := 0
	for _, id := range c.GetIds() {
		if !validSHA(id) {
			continue
		}
		if os.Remove(filepath.Join(st.root, "pins", id)) == nil {
			n++
		}
	}
	freed := st.gc()
	return jsonString(struct {
		Forgot int   `json:"forgot"`
		Freed  int64 `json:"freed"`
	}{n, freed})
}

type pinInfo struct {
	id   string
	kind string
	at   time.Time
}

func (st *artifactStore) pins() []pinInfo {
	ents, _ := os.ReadDir(filepath.Join(st.root, "pins"))
	out := make([]pinInfo, 0, len(ents))
	for _, e := range ents {
		if !validSHA(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		kind, _ := os.ReadFile(filepath.Join(st.root, "pins", e.Name()))
		out = append(out, pinInfo{id: e.Name(), kind: strings.TrimSpace(string(kind)), at: fi.ModTime()})
	}
	return out
}

// live is every blob a pin still needs.
func (st *artifactStore) live() map[string]bool {
	keep := map[string]bool{}
	for _, p := range st.pins() {
		keep[p.id] = true
		if p.kind != "tree" {
			continue
		}
		raw, err := os.ReadFile(st.blobPath(p.id))
		if err != nil {
			continue
		}
		var m manifest
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		for _, e := range m.Entries {
			keep[e.SHA] = true
		}
	}
	return keep
}

// blobs lists every stored blob with its size.
func (st *artifactStore) blobs() map[string]int64 {
	out := map[string]int64{}
	_ = filepath.WalkDir(filepath.Join(st.root, "blobs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !validSHA(d.Name()) {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			out[d.Name()] = fi.Size()
		}
		return nil
	})
	return out
}

// gc removes the blobs no pin needs and answers the bytes freed.
func (st *artifactStore) gc() int64 {
	keep := st.live()
	var freed int64
	for sha, size := range st.blobs() {
		if keep[sha] {
			continue
		}
		if os.Remove(st.blobPath(sha)) == nil {
			freed += size
			_ = os.Remove(filepath.Dir(st.blobPath(sha))) // only when empty
		}
	}
	return freed
}

// enforce keeps the store under budget by dropping the oldest pins, never
// keep, and answers the ids it dropped.
func (st *artifactStore) enforce(budget int64, keep string) []string {
	var total int64
	for _, size := range st.blobs() {
		total += size
	}
	if total <= budget {
		return nil
	}
	pins := st.pins()
	sort.Slice(pins, func(i, j int) bool { return pins[i].at.Before(pins[j].at) })
	var evicted []string
	for _, p := range pins {
		if total <= budget {
			break
		}
		if p.id == keep {
			continue
		}
		if os.Remove(filepath.Join(st.root, "pins", p.id)) != nil {
			continue
		}
		evicted = append(evicted, p.id)
		total -= st.gc()
	}
	return evicted
}

// --- card previews ----------------------------------------------------------

var previewTextExt = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".csv": true, ".tsv": true, ".json": true, ".yaml": true, ".yml": true,
	".toml": true, ".xml": true, ".log": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".go": true,
	".rs": true, ".java": true, ".c": true, ".h": true, ".cpp": true, ".sh": true, ".sql": true, ".css": true, ".mmd": true,
	".ini": true, ".rb": true, ".php": true, ".swift": true, ".kt": true, ".r": true, ".tex": true, ".geojson": true,
}

var previewImageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".tif": true, ".tiff": true}

// makePreview is the card's look at a file: the first lines of text, or a
// small JPEG of an image or of a PDF's first page. Anything else, or a tool
// that fails, is no preview.
func makePreview(ctx context.Context, file string, name ...string) *artifactPreview {
	n := filepath.Base(file)
	if len(name) > 0 {
		n = name[0]
	}
	ext := strings.ToLower(path.Ext(n))
	switch {
	case previewTextExt[ext]:
		return textPreview(file)
	case previewImageExt[ext]:
		return imagePreview(ctx, "convert", file+"[0]", "-auto-orient", "-thumbnail", "640x400>", "-background", "white", "-flatten", "-strip", "-quality", "72", "jpeg:-")
	case ext == ".pdf":
		return pdfPreview(ctx, file)
	}
	return nil
}

func textPreview(file string) *artifactPreview {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, previewText*2)
	n, _ := io.ReadFull(f, buf)
	raw := buf[:n]
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil
	}
	more := n == len(buf)
	text := strings.ToValidUTF8(string(raw), "")
	if !utf8.ValidString(text) {
		return nil
	}
	if r := []rune(text); len(r) > previewText {
		text, more = string(r[:previewText]), true
	}
	if lines := strings.SplitAfter(text, "\n"); len(lines) > previewLines {
		text, more = strings.Join(lines[:previewLines], ""), true
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &artifactPreview{Text: text, More: more}
}

func imagePreview(ctx context.Context, name string, args ...string) *artifactPreview {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil || len(out) == 0 || len(out) > previewImage {
		return nil
	}
	return &artifactPreview{Image: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out)}
}

func pdfPreview(ctx context.Context, file string) *artifactPreview {
	dir, err := os.MkdirTemp("", "silo-preview-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out := filepath.Join(dir, "page")
	if err := exec.CommandContext(pctx, "pdftoppm", "-f", "1", "-l", "1", "-singlefile", "-scale-to", "640", "-jpeg", "-jpegopt", "quality=72", file, out).Run(); err != nil {
		return nil
	}
	raw, err := os.ReadFile(out + ".jpg")
	if err != nil || len(raw) == 0 || len(raw) > previewImage {
		return nil
	}
	return &artifactPreview{Image: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw)}
}
