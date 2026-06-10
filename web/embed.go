// Package web embeds the built React UI into the binary via go:embed.
//
// The dist directory is produced by `npm run build` in this folder. A minimal
// placeholder index.html is committed so the Go build works before/without a
// frontend build; the real bundle overwrites it.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded built UI rooted at the dist directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
