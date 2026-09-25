//go:build !windows

package gym

import (
	"os"

	"golang.org/x/sys/unix"
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
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
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
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	err := l.file.Close()
	l.file = nil
	return err
}
