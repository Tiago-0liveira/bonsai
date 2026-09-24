//go:build windows

package procstore

import (
	"golang.org/x/sys/windows"
)

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

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
