//go:build embedui

// Package web exposes the production web UI bundle to the Go binary.
//
// Release builds compile with `-tags embedui` after `pnpm -C web build`, which
// embeds web/dist; the build fails if the bundle is missing. Builds without the
// tag (plain `go build ./...`) need no Node toolchain and serve a placeholder
// page instead. The e2e build writes to web/dist-e2e, never web/dist, so test
// hooks cannot reach a release binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// Dist returns the UI bundle rooted at web/dist, or nil when the binary was
// built without it.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
