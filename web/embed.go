//go:build embedweb

// Package web holds the SvelteKit front end. Production builds (-tags
// embedweb) embed its compiled output; see embed_dev.go for development.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// Files returns the built front end.
func Files() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
