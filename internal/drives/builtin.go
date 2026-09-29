package drives

import (
	"embed"
	"io/fs"
	"sync"
)

//go:embed catalog
var catalogFS embed.FS

var builtin = sync.OnceValue(func() *Registry {
	sub, err := fs.Sub(catalogFS, "catalog")
	if err != nil {
		panic(err)
	}
	return MustLoad(sub)
})

// Builtin is the embedded template library. Adding a provider is a new
// catalog/<key>/ directory; nothing else changes.
func Builtin() *Registry { return builtin() }
