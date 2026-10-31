//go:build linux

package procstore

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// userHZ is the unit of /proc/<pid>/stat times. The kernel exports them in
// USER_HZ, which is 100 on every Linux ABI; sysconf(_SC_CLK_TCK) is not
// reachable without cgo.
const userHZ = 100

// ProcessMatches reports whether pid is alive and still names the managed child.
// Kernel start ticks in /proc/<pid>/stat are fixed for the process's lifetime,
// so they detect PID reuse without wall-clock steps changing its identity.
// Older records lack ticks and fall back to a wall-clock start comparison.
// The working directory is deliberately not part of identity: a valid managed
// command may chdir after launch. Without readable metadata it fails closed.
func ProcessMatches(pid int, startedAt time.Time, startTicks uint64, _ string) bool {
	if !PidAlive(pid) || startedAt.IsZero() {
		return false
	}
	if startTicks != 0 {
		ticks, err := processStartTicks(pid)
		return err == nil && ticks == startTicks
	}
	started, err := processStartTime(pid)
	if err != nil {
		return false
	}
	diff := started.Sub(startedAt)
	return diff >= -processStartTolerance && diff <= processStartTolerance
}

// StartTicks returns the kernel's process start ticks, or 0 if unavailable.
func StartTicks(pid int) uint64 {
	ticks, _ := processStartTicks(pid)
	return ticks
}

func processStartTicks(pid int) (uint64, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	return parseStatStartTicks(string(stat))
}

func processStartTime(pid int) (time.Time, error) {
	ticks, err := processStartTicks(pid)
	if err != nil {
		return time.Time{}, err
	}
	sys, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	boot, err := parseBootTime(sys)
	if err != nil {
		return time.Time{}, err
	}
	return boot.Add(time.Duration(ticks) * time.Second / userHZ), nil
}

// parseStatStartTicks extracts field 22 (starttime, in ticks since boot) from a
// /proc/<pid>/stat line. The command name (field 2) is parenthesized and may
// itself contain spaces and parentheses, so fields are counted after the last
// ")".
func parseStatStartTicks(stat string) (uint64, error) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("malformed stat: no command field")
	}
	// Fields after the command start at field 3 (state); starttime is field 22.
	fields := strings.Fields(stat[end+1:])
	const idx = 22 - 3
	if len(fields) <= idx {
		return 0, fmt.Errorf("malformed stat: %d fields after command", len(fields))
	}
	return strconv.ParseUint(fields[idx], 10, 64)
}

// parseBootTime reads the "btime" line (seconds since the epoch) of /proc/stat.
func parseBootTime(stat []byte) (time.Time, error) {
	for _, line := range bytes.Split(stat, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(line, []byte("btime ")); ok {
			secs, err := strconv.ParseInt(strings.TrimSpace(string(rest)), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(secs, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("btime not found")
}
