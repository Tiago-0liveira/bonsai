//go:build !windows

package procstore

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// PidAlive reports whether pid names a live process owned by the current user (signal 0 probe).
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

// ProcessMatches reports whether pid is alive and still plausibly refers to the
// process bonsai started. On Linux, procfs metadata is used to guard against PID
// reuse when available. The current working directory is deliberately not part
// of identity: a perfectly valid managed command may chdir after launch.
func ProcessMatches(pid int, startedAt time.Time, _ string) bool {
	if !PidAlive(pid) {
		return false
	}
	procDir := fmt.Sprintf("/proc/%d", pid)
	if fi, err := os.Stat(procDir); err == nil {
		if !startedAt.IsZero() {
			mtime := fi.ModTime()
			diff := mtime.Sub(startedAt)
			if diff < -1*time.Minute || diff > 1*time.Minute {
				return false
			}
		}
		return true
	}
	return true
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
