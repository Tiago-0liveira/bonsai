//go:build !windows

package agenthost

import (
	"os"
	"syscall"
)

// lockFile takes path's exclusive lock without waiting. The holder keeps it
// for its lifetime, so the lock is held exactly while the holder runs.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// held reports whether a running holder keeps path's lock.
func held(path string) bool {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false
	}
	return err == syscall.EWOULDBLOCK || err == syscall.EAGAIN
}

// terminate sends SIGTERM to a holder, which stops its session and exits.
func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
