package catalog

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"silo.agent/internal/skills"
)

//go:embed skills
var skillFiles embed.FS

const DefaultSkill = skills.DefaultName

// SeededName is true when name is one of the embed’d catalog skills.
func SeededName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	st, err := fs.Stat(skillFiles, filepath.ToSlash(filepath.Join("skills", name)))
	return err == nil && st.IsDir()
}

// SeedSkills copies missing catalog skill dirs into data/skills/library.
func SeedSkills(dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	dstRoot := skills.LibraryDir(dataDir)
	if err := os.MkdirAll(dstRoot, 0o700); err != nil {
		return err
	}
	ents, err := fs.ReadDir(skillFiles, "skills")
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if skills.Exists(dstRoot, name) {
			continue
		}
		src := filepath.Join("skills", name)
		dst := filepath.Join(dstRoot, name)
		if err := copyFS(skillFiles, src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyFS(fsys fs.FS, src, dst string) error {
	return fs.WalkDir(fsys, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
