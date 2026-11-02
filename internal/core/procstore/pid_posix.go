//go:build !windows && !darwin

package procstore

import (
	"syscall"
	"time"
)

const processStartTolerance = 5 * time.Second

// PidAlive reports whether pid names a live process owned by the current user (signal 0 probe).
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
