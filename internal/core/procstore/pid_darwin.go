//go:build darwin

package procstore

import (
	"time"

	"golang.org/x/sys/unix"
)

const processStartTolerance = 5 * time.Second

// PidAlive reports whether pid names a live process.
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || err == unix.EPERM
}

// ProcessMatches reports whether pid still refers to the process bonsai started.
// Darwin has no procfs, so use kern.proc.pid and compare the kernel-reported
// process creation time. If that metadata cannot be read, fail closed rather
// than treating a reused PID as the original managed child.
func ProcessMatches(pid int, startedAt time.Time, _ string) bool {
	if pid <= 0 || startedAt.IsZero() {
		return false
	}
	kproc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kproc == nil || int(kproc.Proc.P_pid) != pid {
		return false
	}
	started := time.Unix(
		int64(kproc.Proc.P_starttime.Sec),
		int64(kproc.Proc.P_starttime.Usec)*int64(time.Microsecond),
	)
	diff := started.Sub(startedAt)
	return diff >= -processStartTolerance && diff <= processStartTolerance
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
