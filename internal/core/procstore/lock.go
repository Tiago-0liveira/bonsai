package procstore

import (
	"errors"
	"os"
	"syscall"
)

// ErrLocked is returned by TryLock when another process already holds the lock.
var ErrLocked = errors.New("procstore: lock held by another process")

// FileLock is an advisory flock held on an open file descriptor. It releases on
// Unlock or when the process exits (the kernel drops flocks on close).
type FileLock struct{ f *os.File }

// TryLock takes a non-blocking exclusive flock on path, creating it if needed.
// It returns ErrLocked if the lock is already held elsewhere.
func TryLock(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &FileLock{f: f}, nil
}

// Lock takes a blocking exclusive flock on path, waiting until it is available.
// Used to serialize read-modify-write of the shared global index.
func Lock(path string) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &FileLock{f: f}, nil
}

// Unlock releases the lock and closes the descriptor.
func (l *FileLock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// pidAlive reports whether pid names a live process (signal 0 probe).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
