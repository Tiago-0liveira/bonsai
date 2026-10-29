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

// ProcessMatches reports whether pid is alive and was started when bonsai
// recorded it. It compares the kernel's process start time (starttime in
// /proc/<pid>/stat, fixed for the life of the process) with startedAt, so PID
// reuse is detected without depending on when something first looked at
// /proc/<pid>, which is what the directory's mtime reflects. The working
// directory is deliberately not part of identity: a valid managed command may
// chdir after launch. Without readable metadata it fails closed.
func ProcessMatches(pid int, startedAt time.Time, _ string) bool {
	if !PidAlive(pid) || startedAt.IsZero() {
		return false
	}
	started, err := processStartTime(pid)
	if err != nil {
		return false
	}
	diff := started.Sub(startedAt)
	return diff >= -processStartTolerance && diff <= processStartTolerance
}

func processStartTime(pid int) (time.Time, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, err
	}
	ticks, err := parseStatStartTicks(string(stat))
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
