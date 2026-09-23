// Package version holds metadata injected by GoReleaser.
package version

import (
	"fmt"
	"strings"
)

var (
	Version        = "dev"
	Commit         = "unknown"
	Date           = "unknown"
	CurrentVersion = "dev"
	BuildDate      = "unknown"
	GitCommit      = "unknown"
)

func init() {
	if Version != "dev" {
		CurrentVersion = Version
	}
	if Commit != "unknown" {
		GitCommit = Commit
	}
	if Date != "unknown" {
		BuildDate = Date
	}
}

// Current returns the raw version string.
func Current() string {
	if CurrentVersion != "dev" && Version == "dev" {
		return CurrentVersion
	}
	return Version
}

// String returns the formatted version tag (e.g. "v1.2.3" or "dev").
func String() string {
	v := Current()
	if v == "dev" {
		return v
	}
	return "v" + strings.TrimPrefix(v, "v")
}

// Info returns the version string with build metadata if available.
func Info() string {
	s := String()
	commit := GitCommit
	if commit == "unknown" {
		commit = Commit
	}
	date := BuildDate
	if date == "unknown" {
		date = Date
	}
	if commit != "unknown" && date != "unknown" {
		return fmt.Sprintf("%s (%s, %s)", s, commit, date)
	}
	if commit != "unknown" {
		return fmt.Sprintf("%s (%s)", s, commit)
	}
	return s
}
