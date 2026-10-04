package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	v1 "silo.agent/gen/silo/v1"
	"silo.agent/internal/toolsgen"
)

func toolsDir() string {
	if d := os.Getenv("SILO_TOOLS_DIR"); d != "" {
		return d
	}
	return "/opt/silo/tools"
}

func ensureToolsDir() {
	dir := toolsDir()
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "__init__.py")
	if _, err := os.Stat(p); err != nil {
		_ = os.WriteFile(p, []byte(toolsgen.RootInit), 0o644)
	}
}

func (w *worker) syncTools(stubs []*v1.ToolStub) (string, error) {
	if err := toolsgen.Write(toolsDir(), stubs); err != nil {
		return "", err
	}
	return "ok", nil
}

func skillsDir() string {
	if d := os.Getenv("SILO_SKILLS_DIR"); d != "" {
		return d
	}
	return "/opt/silo/skills"
}

func (w *worker) syncSkills(files []*v1.SkillFile) (string, error) {
	dir := skillsDir()
	if err := toolsgen.ResetDir(dir); err != nil {
		return "", err
	}
	for _, f := range files {
		if f == nil {
			continue
		}
		rel := filepath.ToSlash(strings.TrimSpace(f.GetPath()))
		if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
			return "", errors.New("invalid skill path")
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		got, err := filepath.Rel(dir, full)
		if err != nil || strings.HasPrefix(got, "..") {
			return "", errors.New("skill path escapes")
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, f.GetData(), 0o644); err != nil {
			return "", err
		}
	}
	return "ok", nil
}
