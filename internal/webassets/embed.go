package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded web/dist rooted at its top-level.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
