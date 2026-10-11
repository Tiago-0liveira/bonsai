//go:build !windows

package codex

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether pid is a running process; a zombie that nobody
// reaped yet counts as gone.
func processAlive(pid int) bool {
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		if i := strings.LastIndexByte(string(data), ')'); i >= 0 && i+2 < len(data) {
			return data[i+2] != 'Z'
		}
	}
	return syscall.Kill(pid, 0) == nil
}

// assertNoLeftovers fails when any pid the fake recorded is still running.
func assertNoLeftovers(t *testing.T, pidfile string) {
	t.Helper()
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("fake never started: %v", err)
	}
	pids := strings.Fields(string(data))
	if len(pids) == 0 {
		t.Fatal("no pids recorded")
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, field := range pids {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if processAlive(pid) {
			t.Errorf("pid %d still running after the call returned", pid)
		}
	}
}
