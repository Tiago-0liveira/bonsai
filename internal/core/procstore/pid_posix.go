//go:build !windows && !darwin

package procstore

import (
	"fmt"
	"os"
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

// ProcessMatches reports whether pid is alive and still plausibly refers to the
// process bonsai started. On Linux, procfs metadata is used to guard against PID
// reuse when available. The current working directory is deliberately not part
// of identity: a perfectly valid managed command may chdir after launch.
func ProcessMatches(pid int, startedAt time.Time, _ string) bool {
	if !PidAlive(pid) || startedAt.IsZero() {
		return false
	}
	procDir := fmt.Sprintf("/proc/%d", pid)
	fi, err := os.Stat(procDir)
	if err != nil {
		// Without trustworthy process metadata we cannot safely distinguish the
		// original child from a reused PID. Fail closed rather than risk killing
		// an unrelated process during orphan recovery.
		return false
	}
	mtime := fi.ModTime()
	diff := mtime.Sub(startedAt)
	return diff >= -processStartTolerance && diff <= processStartTolerance
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
