//go:build !embedweb

package web

import "io/fs"

// Files returns nil in development builds: the Vite dev server serves the
// front end and forwards /api to the Go server.
func Files() fs.FS { return nil }
