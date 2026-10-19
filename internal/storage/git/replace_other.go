//go:build !windows

package git

import "os"

// ReplaceFile replaces newpath with oldpath.
func ReplaceFile(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
