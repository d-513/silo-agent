// Package workspace is the Bot's files as the control plane sees them: path
// rules, the worker round trip, reading a file off the box, and the Files tab
// RPCs.
package workspace

import (
	"path"
	"path/filepath"
	"strings"
	"unicode"
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

// IsUpload is /workspace/tmp: what people sent in, wiped on every start. The
// Bot looks at these; they are not something it made for anyone.
func IsUpload(p string) bool {
	p = Rel(p)
	return p == "tmp" || strings.HasPrefix(p, "tmp/")
}

// nameRunes caps a file name CleanName keeps.
const nameRunes = 80

// CleanName is a file name someone else chose (a channel's sender, another
// Bot) made safe to be a path and to be read by a model: letters, digits and
// a little punctuation, no way out of the folder.
func CleanName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.TrimLeft(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name), ".")
	if runes := []rune(name); len(runes) > nameRunes {
		name = string(runes[len(runes)-nameRunes:]) // the end holds the extension
	}
	if strings.Trim(name, "_") == "" {
		return "file"
	}
	return name
}
