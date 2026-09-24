//go:build !windows

package procstore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// ProcessMatches reports whether pid is alive and matches expected process metadata.
// It guards against PID reuse by checking ownership, procfs cwd, and creation time if available.
func ProcessMatches(pid int, startedAt time.Time, worktree string) bool {
	if !PidAlive(pid) {
		return false
	}
	procDir := fmt.Sprintf("/proc/%d", pid)
	if fi, err := os.Stat(procDir); err == nil {
		if !startedAt.IsZero() {
			mtime := fi.ModTime()
			diff := mtime.Sub(startedAt)
			if diff < -2*time.Minute || diff > 2*time.Minute {
				return false
			}
		}
		if worktree != "" {
			if cwd, err := os.Readlink(procDir + "/cwd"); err == nil {
				if filepath.Clean(cwd) != filepath.Clean(worktree) {
					return false
				}
			}
		}
		return true
	}
	// Fallback for systems without /proc (e.g. macOS / Darwin):
	// Validate process start time using ps if startedAt is provided.
	if !startedAt.IsZero() {
		out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "lstart=").Output()
		if err != nil {
			return false
		}
		raw := strings.TrimSpace(string(out))
		if raw != "" {
			// lstart format: "Mon Jan _2 15:04:05 2006"
			if t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", raw, time.Local); err == nil {
				diff := t.Sub(startedAt)
				if diff < -2*time.Minute || diff > 2*time.Minute {
					return false
				}
			}
		}
	}
	return true
}

func pidAlive(pid int) bool {
	return PidAlive(pid)
}
