// Package workspace is the Bot's files as the control plane sees them: path
// rules, the worker round trip, reading a file off the box, and the Files tab
// RPCs.
package workspace

import (
	"path/filepath"
	"strings"
)

// Rel strips a redundant /workspace or workspace/ prefix so present(" /workspace/foo.md")
// does not become /workspace/workspace/foo.md on the Bot.
func Rel(p string) string {
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

// IsScratch is /workspace/bot — the Bot's scratch. Not a user-facing present.
func IsScratch(p string) bool {
	p = Rel(p)
	return p == "bot" || strings.HasPrefix(p, "bot/")
}
