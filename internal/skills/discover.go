package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var skillContainers = []string{
	"skills",
	"skills/.curated",
	"skills/.experimental",
	"skills/.system",
	".agents/skills",
	".claude/skills",
	".cursor/skills",
	".codex/skills",
	".github/skills",
}

// Discover finds SKILL.md trees under root. A SKILL.md at a shallower path
// shadows anything nested below it. prefix, if set, restricts to that subtree.
func Discover(root, prefix string) ([]string, error) {
	root = filepath.Clean(root)
	base := root
	if prefix != "" {
		base = filepath.Join(root, filepath.FromSlash(strings.Trim(prefix, "/")))
	}
	st, err := os.Stat(base)
	if err != nil {
		return nil, err
	}
	var found []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		seen[dir] = true
		found = append(found, dir)
	}
	if !st.IsDir() {
		if strings.EqualFold(filepath.Base(base), "SKILL.md") {
			add(filepath.Dir(base))
		}
		return found, nil
	}
	if skillAt(base) {
		add(base)
		return found, nil
	}
	for _, c := range skillContainers {
		dir := filepath.Join(base, filepath.FromSlash(c))
		walkSkills(dir, 3, add)
	}
	if len(found) == 0 {
		walkSkills(base, 4, add)
	}
	return found, nil
}

func walkSkills(dir string, depth int, add func(string)) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		if rel != "." && strings.Count(filepath.ToSlash(rel), "/") >= depth {
			return fs.SkipDir
		}
		if skillAt(path) {
			add(path)
			return fs.SkipDir
		}
		return nil
	})
}

func skillAt(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}
