//go:build windows

package gym

import (
	"os"

	"golang.org/x/sys/windows"
)

// FileLock represents an OS advisory file lock.
type FileLock struct {
	file *os.File
}

// AcquireFileLock blocks until an exclusive advisory lock is acquired on lockPath.
func AcquireFileLock(lockPath string) (*FileLock, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	h := windows.Handle(f.Fd())
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &FileLock{file: f}, nil
}

// Release unlocks and closes the lock file.
func (l *FileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	h := windows.Handle(l.file.Fd())
	_ = windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
	err := l.file.Close()
	l.file = nil
	return err
}
