//go:build windows

package git

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// ReplaceFile replaces newpath with oldpath, retrying transient Windows sharing errors.
func ReplaceFile(oldpath, newpath string) error {
	return retryRename(oldpath, newpath, os.Rename, func(err error) bool {
		return errors.Is(err, fs.ErrPermission) ||
			errors.Is(err, syscall.Errno(5)) || // ERROR_ACCESS_DENIED
			errors.Is(err, syscall.Errno(32)) || // ERROR_SHARING_VIOLATION
			errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
	}, time.Now, time.Sleep)
}
