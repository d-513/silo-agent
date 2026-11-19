//go:build !webui

package webui

import "io/fs"

// A build without the webui tag carries no console.
var bundle fs.FS
