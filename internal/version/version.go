// Package version is the release a binary was built as, and so the images it
// runs. A plain `go build` (and every Makefile target but `release` and
// `nightly`) is a dev build: its images are the local ones `make images`
// tags, and nothing is pulled. A prod build sets the three variables with
// -ldflags -X, which bakes in the registry its own images were pushed to, so
// a control plane always asks for the images of the commit it was built from.
package version

import (
	"runtime/debug"
	"strings"
	"sync"
)

// Set by -ldflags -X silo.agent/internal/version.<Name>=… (see the Makefile).
var (
	// Version is the release tag (v0.4.2), master-<sha> for a nightly, or dev.
	Version = "dev"
	// Commit is the short commit the binary was built from; empty in a build
	// that did not say, in which case Go's own VCS stamp is read.
	Commit = ""
	// Registry is where a prod build's images live (ghcr.io/d-513). Empty is
	// a dev build: images are localhost/… and are built, never pulled.
	Registry = ""
)

// LocalRegistry is the repository part of a dev build's images: podman's
// name for an image built on this host.
const LocalRegistry = "localhost"

// Dev reports whether this is a dev build (no registry baked in).
func Dev() bool { return strings.TrimSpace(Registry) == "" }

// Image is the reference of one of Silo's images (silo-bot, silo-mcp-stdio,
// silo-drive, silo-egress) for this build: <registry>/<name>:<version>.
func Image(name string) string {
	reg := strings.TrimSuffix(strings.TrimSpace(Registry), "/")
	if reg == "" {
		reg = LocalRegistry
	}
	return reg + "/" + name + ":" + tag()
}

// tag is the version as an image tag: an empty or odd one reads as dev.
func tag() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "dev"
	}
	return v
}

var vcs = sync.OnceValue(func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev != "" && dirty {
		rev += "-dirty"
	}
	return rev
})

// Revision is Commit, else what Go stamped into the binary.
func Revision() string {
	if c := strings.TrimSpace(Commit); c != "" {
		return c
	}
	return vcs()
}

// String is the version for a log line: "v0.4.2 (abc1234)".
func String() string {
	if r := Revision(); r != "" {
		return tag() + " (" + r + ")"
	}
	return tag()
}
