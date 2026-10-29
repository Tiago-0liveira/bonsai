//go:build !windows

package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStructuredCancelKillsProcessGroup(t *testing.T) {
	f := newStructuredFixture(t)
	fifo := filepath.Join(t.TempDir(), "pid")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no fifo support")
	}
	t.Setenv("FAKE_CLAUDE_SPAWN_FIFO", fifo)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.e.provider.RunStructured(ctx, f.request())
		done <- err
	}()
	data, err := os.ReadFile(fifo) // blocks until the fake has spawned its child
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still runs after cancel", pid)
		}
		runtime.Gosched()
	}
	if dirs := f.runDirs(t); len(dirs) != 0 {
		t.Fatalf("work dirs left: %v", dirs)
	}
}

// processGone is true once pid no longer runs; a zombie awaiting its reaper counts.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return true
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	i := strings.LastIndexByte(string(stat), ')')
	return i >= 0 && len(stat) > i+2 && stat[i+2] == 'Z'
}
