package git

import "sync"

var activeGit = struct {
	sync.Mutex
	pids map[int]struct{}
}{pids: make(map[int]struct{})}

// TerminateActive terminates all currently running bounded Git process trees.
// The executable owns signal policy; core callers do not install handlers.
func TerminateActive() {
	activeGit.Lock()
	defer activeGit.Unlock()
	for pid := range activeGit.pids {
		terminateTree(pid)
	}
}
