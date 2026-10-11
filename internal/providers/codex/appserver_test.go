//go:build !windows

package codex

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func shortAppServerTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := appServerTimeout
	appServerTimeout = d
	t.Cleanup(func() { appServerTimeout = old })
}

func TestAppServerHonoursTimeoutAndLeavesNoProcess(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_CODEX_PIDFILE", pidfile)
	t.Setenv("FAKE_CODEX_APPSERVER", "hang")
	shortAppServerTimeout(t, 400*time.Millisecond)

	start := time.Now()
	_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v, the timeout was not honoured", elapsed)
	}
	assertNoLeftovers(t, pidfile)
}

func TestAppServerEarlyExit(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_CODEX_PIDFILE", pidfile)
	for _, mode := range []string{"exit", "exit-after-init"} {
		t.Setenv("FAKE_CODEX_APPSERVER", mode)
		start := time.Now()
		_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
		if err == nil || !strings.Contains(err.Error(), "exited early") {
			t.Fatalf("%s: err = %v", mode, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("%s: waited for the timeout instead of the exit", mode)
		}
	}
	assertNoLeftovers(t, pidfile)
}

func TestAppServerResponseIsSizeCapped(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_CODEX_PIDFILE", pidfile)
	t.Setenv("FAKE_CODEX_APPSERVER", "huge")
	_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err == nil || !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("err = %v", err)
	}
	assertNoLeftovers(t, pidfile)
}

func TestAppServerSkipsNotificationsAndServerRequests(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	t.Setenv("FAKE_CODEX_APPSERVER", "noisy")
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 2 {
		t.Fatalf("snapshot = %+v, err = %v", snapshot, err)
	}
}

func TestAppServerRunsInProfileEnvironmentAndExitsAfterSuccess(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv("FAKE_CODEX_PIDFILE", pidfile)
	t.Setenv("OPENAI_API_KEY", "sk-parent")
	if _, err := e.provider.Usage(t.Context(), work, agents.UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	log := e.calls()
	if !strings.Contains(log, "app-server home="+e.profileHome(work)+` store="file"`) {
		t.Fatalf("calls = %q", log)
	}
	assertNoLeftovers(t, pidfile)
}

func TestConcurrentUsageShareOneProcess(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	t.Setenv("FAKE_CODEX_DELAY", "0.6") // every caller arrives while the first read runs
	done := make(chan error, 8)
	for i := 0; i < cap(done); i++ {
		go func() {
			_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
			done <- err
		}()
	}
	for i := 0; i < cap(done); i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := e.spawns(); n != 1 {
		t.Fatalf("spawns = %d, want 1", n)
	}
}
