package portowner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func find(port int) (Owner, bool) {
	want := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, inode := range parseProcNetTCP(string(raw), port) {
			want["socket:["+inode+"]"] = true
		}
	}
	if len(want) == 0 {
		return Owner{}, false
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return Owner{}, false
	}
	for _, entry := range procs {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // another user's process; not visible
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil && want[target] {
				name, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
				return Owner{PID: pid, Name: strings.TrimSpace(string(name))}, true
			}
		}
	}
	return Owner{}, false
}
