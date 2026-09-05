package app

import (
	"path/filepath"
	"strings"
)

// relWorkspace strips a redundant /workspace or workspace/ prefix so present(" /workspace/foo.md")
// does not become /workspace/workspace/foo.md on the Bot.
func relWorkspace(p string) string {
	p = strings.TrimSpace(filepath.ToSlash(p))
	switch {
	case p == "" || p == "." || p == "/workspace" || p == "workspace":
		return ""
	case strings.HasPrefix(p, "/workspace/"):
		p = strings.TrimPrefix(p, "/workspace/")
	case strings.HasPrefix(p, "workspace/"):
		p = strings.TrimPrefix(p, "workspace/")
	}
	return strings.TrimPrefix(p, "/")
}
