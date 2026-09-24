//go:build windows

package procstore

import (
	"time"

	"golang.org/x/sys/windows"
)

const processStartTolerance = 5 * time.Second

// PidAlive reports whether pid names a live process on Windows.
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	const stillActive = 259
	return exitCode == stillActive
}

// ProcessMatches reports whether pid is alive and matches expected process metadata.
func ProcessMatches(pid int, startedAt time.Time, worktree string) bool {
	if pid <= 0 || startedAt.IsZero() {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil || exitCode != 259 {
		return false
	}

	var creationTime, exitTime, kernelTime, userTime windows.Filetime
	if err := windows.GetProcessTimes(h, &creationTime, &exitTime, &kernelTime, &userTime); err != nil {
		return false
	}
	t := time.Unix(0, creationTime.Nanoseconds())
	diff := t.Sub(startedAt)
	return diff >= -processStartTolerance && diff <= processStartTolerance
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
