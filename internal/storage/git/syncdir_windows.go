//go:build windows

package git

// Go's os.File.Sync does not support directory handles on Windows and returns
// access denied. The replacement file itself is synced before the atomic rename.
func syncDir(string) error { return nil }
