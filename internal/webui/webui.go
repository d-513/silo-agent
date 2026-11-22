// Package webui serves the web console from inside the control plane, so a
// release is one binary and one origin: the bundle `vite build` makes is
// compiled in with the `webui` build tag (`make build-cp-ui`, the control
// plane image). Without the tag there is no bundle and Handler is nil: a dev
// build's console is Vite's dev server, which proxies the API.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves the embedded console, or nil when this build has none.
func Handler() http.Handler {
	if bundle == nil {
		return nil
	}
	return New(bundle)
}

// Embedded reports whether this build carries the console.
func Embedded() bool { return bundle != nil }

// New serves a built console from fsys, as the fallback under every route the
// control plane does not own: a file that exists is itself, and any other
// page address is index.html, where the client's router reads the path (a
// reload of /bots/x/rules must open that page).
//
// What is not a page is never answered with the app: a missing file with an
// extension, anything under an API prefix, and every method but GET and HEAD
// are 404 or 405, so a mistyped API call gets an error and not 200 HTML.
func New(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
				w.Header().Set("Cache-Control", cacheFor(name))
				// Go's type table does not know a web app manifest.
				if strings.HasSuffix(name, ".webmanifest") {
					w.Header().Set("Content-Type", "application/manifest+json")
				}
				http.ServeFileFS(w, r, fsys, name)
				return
			}
			if !page(name) {
				http.NotFound(w, r)
				return
			}
		}
		// The page itself is never cached: it names the hashed files of this
		// release, and a cached copy would go on asking for the last one's.
		w.Header().Set("Cache-Control", "no-cache")
		body, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "the web console is not in this build", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}

// apiPrefixes are the control plane's own path families. A path under one
// that reached this handler matched no route, and is an error, not a page.
var apiPrefixes = []string{"silo.v1.", "caddy/", "assets/", "vad/", "oauth/", "auth/", "voice/", "tunnels/", "mail/", "connectors/", "artifacts/"}

// page reports whether a path with no file behind it is one of the console's
// own addresses: no extension, and not under an API prefix.
func page(name string) bool {
	for _, p := range apiPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return path.Ext(path.Base(name)) == ""
}

// cacheFor is how long a file may be kept. Vite names what is under assets/
// by its content, so those never change; the rest (icons, the voice
// detector's files) keep their names across releases.
func cacheFor(name string) string {
	if strings.HasPrefix(name, "assets/") {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=3600"
}
