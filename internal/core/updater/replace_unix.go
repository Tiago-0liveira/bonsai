//go:build !windows

package updater

import "os"

func swap(tmp, target string) error { return os.Rename(tmp, target) }
