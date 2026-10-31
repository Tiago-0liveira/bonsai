//go:build !windows

package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestTerminateActiveKillsDescendants(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	if _, err := RunContext(context.Background(), dir, "init"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		// The shell reaps the terminated sleep even on hosts whose PID 1
		// does not reap orphaned zombies.
		_, err := RunContext(ctx, dir, "-c", "alias.slow=!trap 'wait; exit 0' TERM; sleep 30 & child=$!; printf '%s' \"$child\" > child.pid; wait", "slow")
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		TerminateActive()
	})
	var pid int
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for pid == 0 {
		data, _ := os.ReadFile(filepath.Join(dir, "child.pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("git ended before starting descendant: %v", err)
		case <-ctx.Done():
			t.Fatal("descendant did not start")
		case <-ticker.C:
		}
	}
	TerminateActive()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("terminated git returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("git did not return promptly after termination")
	}
	deadline := time.After(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("descendant %d still exists: %v", pid, err)
		case <-ticker.C:
		}
	}
	activeGit.Lock()
	defer activeGit.Unlock()
	if len(activeGit.pids) != 0 {
		t.Fatalf("registry after Wait: %v", activeGit.pids)
	}
}

func TestActiveRegistryConcurrentRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			// Termination may win the race with a successful command.
			_, _ = RunContext(context.Background(), dir, "--version")
		})
		wg.Go(TerminateActive)
	}
	wg.Wait()
	activeGit.Lock()
	defer activeGit.Unlock()
	if len(activeGit.pids) != 0 {
		t.Fatalf("registry after concurrent Waits: %v", activeGit.pids)
	}
}
