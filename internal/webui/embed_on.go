//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

// dist is web/'s production build, put here by `make web-build`.
//
//go:embed all:dist
var dist embed.FS

var bundle = func() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()
