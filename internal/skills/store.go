package skills

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Info struct {
	Name        string
	Description string
	Kind        string
	Source      string
	Seed        string
	Dir         string
}

type File struct {
	Path string
	Data []byte
}

type DirEnt struct {
	Name     string
	Path     string
	Dir      bool
	Size     int64
	Modified time.Time
}

func LibraryDir(dataDir string) string {
	return filepath.Join(dataDir, "skills", "library")
}

func PersonalDir(dataDir, userID string) string {
	return filepath.Join(dataDir, "skills", "users", userID)
}

func RootFor(dataDir, kind, userID string) string {
	if kind == KindPersonal {
		return PersonalDir(dataDir, userID)
	}
	return LibraryDir(dataDir)
}

func List(root, kind string) ([]Info, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		info, err := Load(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		info.Kind = kind
		out = append(out, info)
	}
	return out, nil
}

func Load(dir string) (Info, error) {
	name := filepath.Base(dir)
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Info{}, err
	}
	m, err := Parse(string(raw))
	if err != nil {
		return Info{}, err
	}
	if err := MatchDir(m.Name, name); err != nil {
		return Info{}, err
	}
	src, seed := "", ""
	if m.Metadata != nil {
		src = m.Metadata["silo_source"]
		seed = m.Metadata["silo_seed"]
	}
	return Info{Name: m.Name, Description: m.Description, Source: src, Seed: seed, Dir: dir}, nil
}

func Exists(root, name string) bool {
	if ValidName(name) != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(root, name, "SKILL.md"))
	return err == nil
}

func Delete(root, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return fmt.Errorf("skill %s not found", name)
	}
	return os.RemoveAll(dir)
}

func resolveRel(root, name, rel string) (base, full string, err error) {
	if err := ValidName(name); err != nil {
		return "", "", err
	}
	base = filepath.Join(root, name)
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "." {
		rel = ""
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", "", fmt.Errorf("path escapes skill")
	}
	full = base
	if rel != "" {
		full = filepath.Join(base, filepath.FromSlash(rel))
	}
	got, err := filepath.Rel(base, full)
	if err != nil || strings.HasPrefix(got, "..") {
		return "", "", fmt.Errorf("path escapes skill")
	}
	return base, full, nil
}

func ListRel(root, name, rel string) ([]DirEnt, error) {
	base, full, err := resolveRel(root, name, rel)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	var out []DirEnt
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		child := filepath.Join(full, e.Name())
		p, err := filepath.Rel(base, child)
		if err != nil {
			continue
		}
		out = append(out, DirEnt{
			Name:     e.Name(),
			Path:     filepath.ToSlash(p),
			Dir:      e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func ReadRel(root, name, rel string) ([]byte, error) {
	if rel == "" || rel == "." {
		rel = "SKILL.md"
	}
	_, full, err := resolveRel(root, name, rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func StatRel(root, name, rel string) (os.FileInfo, string, error) {
	if rel == "" || rel == "." {
		rel = "SKILL.md"
	}
	_, full, err := resolveRel(root, name, rel)
	if err != nil {
		return nil, "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return nil, "", err
	}
	if st.IsDir() {
		return nil, "", fmt.Errorf("is a directory")
	}
	return st, full, nil
}

func WalkFiles(dir string) ([]File, error) {
	var out []File
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(filepath.Base(rel), ".") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, File{Path: rel, Data: b})
		return nil
	})
	return out, err
}

func CopyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(rel)
		if d.IsDir() && (base == ".git" || base == "node_modules") {
			return fs.SkipDir
		}
		if strings.HasPrefix(base, ".") && base != ".curated" && base != ".experimental" && base != ".system" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func PatchSource(dir, source string) error {
	if source == "" {
		return nil
	}
	p := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	m, err := Parse(string(raw))
	if err != nil {
		return err
	}
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}
	if m.Metadata["silo_source"] == source {
		return nil
	}
	m.Metadata["silo_source"] = source
	return os.WriteFile(p, []byte(encode(m)), 0o644)
}

func encode(m Meta) string {
	type fm struct {
		Name          string            `yaml:"name"`
		Description   string            `yaml:"description"`
		License       string            `yaml:"license,omitempty"`
		Compatibility string            `yaml:"compatibility,omitempty"`
		Metadata      map[string]string `yaml:"metadata,omitempty"`
	}
	b, err := yaml.Marshal(fm{
		Name: m.Name, Description: m.Description, License: m.License,
		Compatibility: m.Compatibility, Metadata: m.Metadata,
	})
	if err != nil {
		return m.Raw
	}
	return "---\n" + string(b) + "---\n" + strings.TrimPrefix(m.Body, "\n")
}
