//go:build windows

package review

import "os"

// checkPrivate has no unix owner or mode bits to read on Windows; the user profile's ACLs apply.
func checkPrivate(string) error { return nil }

func openReportFile(path string) (*os.File, error) { return os.Open(path) }
