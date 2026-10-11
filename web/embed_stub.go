//go:build !embedui

package web

import "io/fs"

// Dist returns nil: this binary was built without `-tags embedui`, so the
// local API serves a placeholder page that explains how to build the UI.
func Dist() fs.FS { return nil }
