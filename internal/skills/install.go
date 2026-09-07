package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxDownloadBytes = 10 << 20
	MaxExtractBytes  = 25 << 20
	MaxExtractFiles  = 1000
)

var HTTPClient = http.DefaultClient

type InstallResult struct {
	Installed []string
	Skipped   []string
}

func InstallURL(dest, source string) (InstallResult, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return InstallResult{}, fmt.Errorf("url required")
	}
	tmp, err := os.MkdirTemp("", "silo-skill-*")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(tmp)
	if err := fetchInto(tmp, source); err != nil {
		return InstallResult{}, err
	}
	return installDiscovered(dest, tmp, source)
}

func InstallArchive(dest string, body []byte, filename string) (InstallResult, error) {
	if len(body) == 0 {
		return InstallResult{}, fmt.Errorf("archive required")
	}
	if len(body) > MaxDownloadBytes {
		return InstallResult{}, fmt.Errorf("archive exceeds %d bytes", MaxDownloadBytes)
	}
	filename = archiveName(filename)
	tmp, err := os.MkdirTemp("", "silo-skill-*")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(tmp)
	if _, err := extractArchive(tmp, body, "", filename); err != nil {
		return InstallResult{}, err
	}
	_ = flattenZipRoot(tmp)
	return installDiscovered(dest, tmp, "upload:"+filename)
}

func archiveName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == "/" || strings.Contains(name, "..") {
		return "upload.zip"
	}
	return name
}

func installDiscovered(dest, tmp, source string) (InstallResult, error) {
	dirs, err := Discover(tmp, "")
	if err != nil {
		return InstallResult{}, err
	}
	if len(dirs) == 0 {
		return InstallResult{}, fmt.Errorf("no SKILL.md found")
	}
	var out InstallResult
	for _, dir := range dirs {
		info, err := loadInstall(dir, tmp)
		if err != nil {
			continue
		}
		if Exists(dest, info.Name) {
			out.Skipped = append(out.Skipped, info.Name)
			continue
		}
		target := filepath.Join(dest, info.Name)
		if err := CopyTree(dir, target); err != nil {
			return out, err
		}
		_ = PatchSource(target, source)
		out.Installed = append(out.Installed, info.Name)
	}
	if len(out.Installed) == 0 && len(out.Skipped) == 0 {
		return out, fmt.Errorf("no valid skills in source")
	}
	return out, nil
}

// loadInstall is Load, plus SKILL.md sitting at the extract root (zip of files, no folder).
func loadInstall(dir, extractRoot string) (Info, error) {
	info, err := Load(dir)
	if err == nil {
		return info, nil
	}
	if filepath.Clean(dir) != filepath.Clean(extractRoot) {
		return Info{}, err
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if rerr != nil {
		return Info{}, err
	}
	m, perr := Parse(string(raw))
	if perr != nil {
		return Info{}, perr
	}
	return Info{Name: m.Name, Description: m.Description, Dir: dir}, nil
}

func fetchInto(dir, source string) error {
	owner, repo, ref, sub, ok := parseGitHub(source)
	if ok {
		if ref == "" {
			ref = "HEAD"
		}
		u := fmt.Sprintf("https://codeload.github.com/%s/%s/zip/%s", owner, repo, ref)
		body, ctype, err := get(u)
		if err != nil {
			return err
		}
		extractRoot, err := extractArchive(dir, body, ctype, u)
		if err != nil {
			return err
		}
		if sub != "" {
			return hoist(extractRoot, sub)
		}
		return flattenZipRoot(extractRoot)
	}
	if !(strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")) {
		return fmt.Errorf("unsupported source %q", source)
	}
	body, ctype, err := get(source)
	if err != nil {
		return err
	}
	if looksLikeSkillMD(body, ctype, source) {
		m, err := Parse(string(body))
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, m.Name)
		if err := os.MkdirAll(dst, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, "SKILL.md"), body, 0o644)
	}
	_, err = extractArchive(dir, body, ctype, source)
	return err
}

func parseGitHub(source string) (owner, repo, ref, sub string, ok bool) {
	s := strings.TrimSpace(source)
	s = strings.TrimSuffix(s, ".git")
	if strings.HasPrefix(s, "https://github.com/") || strings.HasPrefix(s, "http://github.com/") {
		u, err := url.Parse(s)
		if err != nil {
			return "", "", "", "", false
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return "", "", "", "", false
		}
		owner, repo = parts[0], parts[1]
		if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
			ref = parts[3]
			if len(parts) > 4 {
				sub = strings.Join(parts[4:], "/")
			}
		}
		return owner, repo, ref, sub, true
	}
	if strings.Contains(s, "://") {
		return "", "", "", "", false
	}
	parts := strings.Split(s, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], "", "", true
	}
	return "", "", "", "", false
}

func get(raw string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "silo-agent")
	res, err := HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download %s: HTTP %d", raw, res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxDownloadBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > MaxDownloadBytes {
		return nil, "", fmt.Errorf("download exceeds %d bytes", MaxDownloadBytes)
	}
	return b, res.Header.Get("Content-Type"), nil
}

func looksLikeSkillMD(body []byte, ctype, src string) bool {
	if strings.Contains(strings.ToLower(src), "skill.md") {
		return true
	}
	if strings.Contains(ctype, "zip") || strings.Contains(ctype, "gzip") || strings.Contains(ctype, "tar") {
		return false
	}
	s := strings.TrimSpace(string(body))
	return strings.HasPrefix(s, "---") && strings.Contains(s, "\nname:")
}

func extractArchive(dir string, body []byte, ctype, src string) (string, error) {
	lower := strings.ToLower(ctype + " " + src)
	switch {
	case strings.Contains(lower, "zip") || bytes.HasPrefix(body, []byte("PK")):
		return dir, unzip(dir, body)
	case strings.Contains(lower, "gzip") || strings.Contains(lower, "tar") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.gz"):
		return dir, untar(dir, body)
	default:
		if bytes.HasPrefix(body, []byte("PK")) {
			return dir, unzip(dir, body)
		}
		return "", fmt.Errorf("not a skill archive")
	}
}

func unzip(dir string, body []byte) error {
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	if len(r.File) > MaxExtractFiles {
		return fmt.Errorf("archive has too many files")
	}
	var total int64
	for _, f := range r.File {
		if err := safeZipName(f.Name); err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxExtractBytes+1-total))
		rc.Close()
		if err != nil {
			return err
		}
		total += int64(len(data))
		if total > MaxExtractBytes {
			return fmt.Errorf("extracted archive exceeds %d bytes", MaxExtractBytes)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func untar(dir string, body []byte) error {
	var r io.Reader = bytes.NewReader(body)
	if gr, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
		defer gr.Close()
		r = gr
	}
	tr := tar.NewReader(r)
	var n int
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		n++
		if n > MaxExtractFiles {
			return fmt.Errorf("archive has too many files")
		}
		if err := safeZipName(h.Name); err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(tr, MaxExtractBytes+1-total))
			if err != nil {
				return err
			}
			total += int64(len(data))
			if total > MaxExtractBytes {
				return fmt.Errorf("extracted archive exceeds %d bytes", MaxExtractBytes)
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return err
			}
		}
	}
}

func safeZipName(name string) error {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("archive path escapes")
	}
	return nil
}

func flattenZipRoot(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var only string
	n := 0
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		n++
		only = e.Name()
	}
	if n != 1 {
		return nil
	}
	inner := filepath.Join(dir, only)
	st, err := os.Stat(inner)
	if err != nil || !st.IsDir() {
		return nil
	}
	if skillAt(inner) {
		return nil
	}
	return hoistDir(inner, dir)
}

func hoist(root, sub string) error {
	_ = flattenZipRoot(root)
	src := filepath.Join(root, filepath.FromSlash(sub))
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("path %s not in archive", sub)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(root), "skill-hoist-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if st.IsDir() {
		if err := CopyTree(src, tmp); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			return err
		}
		if err := copyFile(src, filepath.Join(tmp, filepath.Base(src))); err != nil {
			return err
		}
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range ents {
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
	return hoistDir(tmp, root)
}

func hoistDir(src, dst string) error {
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if err := os.Rename(from, to); err != nil {
			if e.IsDir() {
				if err := CopyTree(from, to); err != nil {
					return err
				}
			} else if err := copyFile(from, to); err != nil {
				return err
			}
		}
	}
	return nil
}
