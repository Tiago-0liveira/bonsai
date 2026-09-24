//go:build !windows

package procstore

import (
	"errors"
	"syscall"
)

// PidAlive reports whether pid names a live process (signal 0 probe).
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
