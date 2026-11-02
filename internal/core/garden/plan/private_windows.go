//go:build windows

package plan

import "os"

// checkPrivate has no unix owner or mode bits to read on Windows; the user profile's ACLs apply.
func checkPrivate(string) error { return nil }

func openPlanFile(path string) (*os.File, error) { return os.Open(path) }
