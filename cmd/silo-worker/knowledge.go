package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The knowledge index reads folders through two worker commands: walk lists
// files (path, size, mtime — no reads) and extract returns one file's plain
// text plus the sha256 of its bytes.
const (
	walkDefaultMax = 5000
	extractTextMax = 2 << 20 // default cap on returned text
	sniffBytes     = 8192
)

// extractFileMax is the largest file extract will read at all; a bigger one is
// reported as skipped, unhashed, so it is not downloaded again each sweep.
var extractFileMax int64 = 50 << 20

type walkFile struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"` // unix nanoseconds
}

// walk lists every regular file under p, recursive and sorted. Hidden entries,
// heavy trees (grepPruneDirs), symlinks and — when p is not itself inside
// drives/ — the drives mounts are skipped. An unreadable root is an error:
// the caller must never mistake a flaky mount for an empty folder.
func (w *worker) walk(p string, max int) (string, error) {
	full, err := w.resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("not a folder")
	}
	if _, err := os.ReadDir(full); err != nil {
		return "", err
	}
	if max <= 0 || max > walkDefaultMax {
		max = walkDefaultMax
	}
	skipDrives := !inDrives(w.relPath(full))
	prune := map[string]bool{}
	for _, d := range grepPruneDirs {
		prune[d] = true
	}
	out := struct {
		Files     []walkFile `json:"files"`
		Truncated bool       `json:"truncated"`
	}{Files: []walkFile{}}
	err = filepath.WalkDir(full, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == full {
				return err
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil // one unreadable entry must not abort the sweep
		}
		name := d.Name()
		if d.IsDir() {
			if path == full {
				return nil
			}
			if strings.HasPrefix(name, ".") || prune[name] || (skipDrives && w.relPath(path) == "drives") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if len(out.Files) >= max {
			out.Truncated = true
			return fs.SkipAll
		}
		out.Files = append(out.Files, walkFile{Path: w.relPath(path), Size: info.Size(), Mtime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(out)
	return string(b), err
}

type extractResult struct {
	Text      string `json:"text"`
	Sha256    string `json:"sha256"`
	Kind      string `json:"kind"` // text | pdf | document | skipped
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Detail    string `json:"detail,omitempty"`
}

// extract reads one file and returns its plain text and the sha256 of all its
// bytes. Text files are read as is, PDFs go through pdftotext (form feeds mark
// pages), word-processor files through pandoc, legacy .doc through catdoc;
// anything else is sniffed and skipped when binary. With ocr, scanned PDF
// pages and image files are read with tesseract (ocr.go); without it they are
// reported, not read. A missing extractor is an error so the owner sees why a
// file was not indexed.
func (w *worker) extract(ctx context.Context, p string, max int64, ocr bool) (string, error) {
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
	if max <= 0 || max > extractTextMax {
		max = extractTextMax
	}
	res := extractResult{Size: st.Size(), Kind: "skipped"}
	if st.Size() > extractFileMax {
		res.Detail = fmt.Sprintf("file too large (%d bytes, max %d)", st.Size(), extractFileMax)
		return marshalExtract(res)
	}
	ext := strings.ToLower(filepath.Ext(full))
	if imageExts[ext] {
		if !ocr {
			res.Detail = "image (" + ocrOffNote + ")"
			return marshalExtract(res)
		}
		if st.Size() < ocrMinImageBytes {
			res.Detail = "image too small to hold text"
			return marshalExtract(res)
		}
	}
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	var text string
	switch {
	case imageExts[ext]:
		res.Kind = "image"
		text, err = hashThenRunOCR(ctx, f, h, "tesseract", full, "stdout", "-l", ocrLangs())
		if err == nil && !looksLikeText(text) {
			text, res.Detail = "", "no text found in the image"
		}
	case ext == ".pdf":
		res.Kind = "pdf"
		text, err = hashThenRun(ctx, f, h, "pdftotext", "-layout", full, "-")
		if err == nil {
			text, res.Detail = ocrPDF(ctx, full, text, ocr)
		}
	case ext == ".docx" || ext == ".odt" || ext == ".rtf" || ext == ".epub" || ext == ".html" || ext == ".htm":
		res.Kind = "document"
		text, err = hashThenRun(ctx, f, h, "pandoc", "-t", "gfm", "--wrap=none", full)
	case ext == ".doc":
		res.Kind = "document"
		text, err = hashThenRun(ctx, f, h, "catdoc", full)
	default:
		var data []byte
		data, err = io.ReadAll(io.TeeReader(io.LimitReader(f, extractFileMax+1), h))
		if err != nil {
			return "", err
		}
		if looksBinary(data) {
			res.Detail = "binary file"
			res.Sha256 = hex.EncodeToString(h.Sum(nil))
			return marshalExtract(res)
		}
		res.Kind = "text"
		text = string(data)
	}
	if err != nil {
		return "", err
	}
	res.Sha256 = hex.EncodeToString(h.Sum(nil))
	text = strings.ToValidUTF8(text, "�")
	if int64(len(text)) > max {
		cut := int(max)
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, res.Truncated = text[:cut], true
	}
	res.Text = text
	return marshalExtract(res)
}

func marshalExtract(r extractResult) (string, error) {
	b, err := json.Marshal(r)
	return string(b), err
}

// looksBinary sniffs the head of a file: a NUL, or a head that is mostly
// invalid UTF-8, is not text worth indexing.
func looksBinary(data []byte) bool {
	head := data
	if len(head) > sniffBytes {
		head = head[:sniffBytes]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	bad := 0
	for i := 0; i < len(head); {
		r, n := utf8.DecodeRune(head[i:])
		if r == utf8.RuneError && n == 1 && i+4 <= len(head) {
			bad++
		}
		i += n
	}
	return bad*100 > len(head) // over 1% of bytes
}

// hashThenRunOCR is hashThenRun for tesseract, which gets a single thread per
// process: pages run in parallel instead, which scales better than OpenMP.
func hashThenRunOCR(ctx context.Context, f io.Reader, h io.Writer, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", fmt.Errorf("%s is not installed in this Bot's image", name)
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return runOCRTool(ctx, name, args...)
}

// hashThenRun hashes the file, then converts it. The converter is looked up
// first: on a drive, reading the whole file only to find the tool missing
// would be a wasted download on every sweep.
func hashThenRun(ctx context.Context, f io.Reader, h io.Writer, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", fmt.Errorf("%s is not installed in this Bot's image", name)
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return runExtractor(ctx, name, args...)
}

// runExtractor runs one converter without a shell and returns its stdout.
func runExtractor(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s is not installed in this Bot's image", name)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s failed: %s", name, msg)
	}
	return string(out), nil
}
