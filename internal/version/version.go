// Package version holds metadata injected by GoReleaser.
package version

import "strings"

var Version = "dev"
var Commit = "unknown"
var Date = "unknown"

func String() string {
	if Version == "dev" {
		return Version
	}
	return "v" + strings.TrimPrefix(Version, "v")
}
