//go:build windows

package procstore

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File, blocking bool) error {
	h := windows.Handle(f.Fd())
	var flags uint32 = windows.LOCKFILE_EXCLUSIVE_LOCK
	if !blocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	var ol windows.Overlapped
	err := windows.LockFileEx(h, flags, 0, 1, 0, &ol)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || err == windows.Errno(33) {
			return ErrLocked
		}
		return err
	}
	return nil
}

func unlockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var ol windows.Overlapped
	return windows.UnlockFileEx(h, 0, 1, 0, &ol)
}
