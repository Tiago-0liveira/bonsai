//go:build !windows

package review

import (
	"errors"
	"os"
	"syscall"
)

// checkPrivate refuses a directory another user owns or that group or others can write: they could swap the
// directory or file under the reviewer. Symlinks are not followed.
func checkPrivate(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o022 != 0 {
		return errors.New("report directory is not private to this user")
	}
	return nil
}

// openReportFile never follows a symlink and never blocks on a swapped-in FIFO or device.
func openReportFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
