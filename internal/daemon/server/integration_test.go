package server_test

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/server"
)

// shortRuntimeDir returns a short-pathed dir for the daemon socket. Unix socket
// paths are capped (~104 bytes on macOS) and t.TempDir() is far too deep, so we
// anchor under /tmp on Unix, or default temp dir on Windows.
func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "bsrt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// newDaemon starts an isolated in-process daemon for a fresh repo root and
// returns a connected client. XDG dirs are redirected to temp locations so the
// socket and the global index never touch the developer's real config.
func newDaemon(t *testing.T) (*client.Client, string) {
	t.Helper()
	// Isolate the global index. os.UserConfigDir honors XDG_CONFIG_HOME on Linux
	// but $HOME/Library on macOS, so redirect HOME too (and clear XDG so Linux
	// also falls back to the temp HOME).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	root := t.TempDir()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(root)
	}()
	c := client.For(root)
	waitAlive(t, c)
	t.Cleanup(func() {
		_ = c.Shutdown(true)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return c, root
}

func newDaemonWithLogCap(t *testing.T, logLimit int64) (*client.Client, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	root := t.TempDir()

	srv, err := server.NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	if logLimit > 0 {
		srv.SetLogCap(logLimit)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run()
	}()
	c := client.For(root)
	waitAlive(t, c)
	t.Cleanup(func() {
		_ = c.Shutdown(true)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return c, root
}

func waitAlive(t *testing.T, c *client.Client) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Ping(); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("daemon never came up")
}

// readLog returns the full captured log of process id.
func readLog(t *testing.T, c *client.Client, id int) string {
	t.Helper()
	var b strings.Builder
	if err := c.Logs(id, false, 0, "", false, func(chunk string) error {
		b.WriteString(chunk)
		return nil
	}); err != nil {
		t.Fatalf("logs #%d: %v", id, err)
	}
	return b.String()
}

// waitFor polls cond until true or the timeout elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

// recByID finds a record in a fresh listing.
func recByID(t *testing.T, c *client.Client, id int) *procstore.Record {
	t.Helper()
	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func TestSpawnListLogsKill(t *testing.T) {
	c, root := newDaemon(t)

	rec, err := c.Spawn(root, "", "greet",
		"printf 'listening on http://localhost:3000\\n'; sleep 30", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != procstore.StatusRunning || rec.PID == 0 {
		t.Fatalf("bad spawn record: %+v", rec)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		return strings.Contains(readLog(t, c, rec.ID), "listening on http://localhost:3000")
	}) {
		t.Fatalf("log missing output: %q", readLog(t, c, rec.ID))
	}

	// Grep filters out non-matching lines.
	var grepped string
	_ = c.Logs(rec.ID, false, 0, "nomatchxyz", false, func(chunk string) error { grepped += chunk; return nil })
	if grepped != "" {
		t.Fatalf("grep should have filtered everything, got %q", grepped)
	}

	if killed, err := c.Kill(rec.ID, false, ""); err != nil || len(killed) != 1 {
		t.Fatalf("kill = %v, %v", killed, err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusStopped
	}) {
		t.Fatalf("expected stopped, got %+v", recByID(t, c, rec.ID))
	}
}

func TestMultipleProcessesPerWorktree(t *testing.T) {
	c, root := newDaemon(t)

	var ids []int
	for i := 0; i < 3; i++ {
		rec, err := c.Spawn(root, "", "svc", "sleep 30", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ID)
	}
	recs, _ := c.List()
	if len(recs) != 3 {
		t.Fatalf("want 3 processes, got %d", len(recs))
	}
	// Each has a distinct id and log file; all in the same worktree.
	for _, r := range recs {
		if r.Worktree != root {
			t.Fatalf("proc #%d in wrong worktree %q", r.ID, r.Worktree)
		}
	}

	// Kill the middle one; the others keep running.
	if _, err := c.Kill(ids[1], false, ""); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, ids[1]).Status == procstore.StatusStopped
	}) {
		t.Fatal("middle proc not stopped")
	}
	if recByID(t, c, ids[0]).Status != procstore.StatusRunning ||
		recByID(t, c, ids[2]).Status != procstore.StatusRunning {
		t.Fatal("sibling processes should still be running")
	}
}

func TestMultipleWorktrees(t *testing.T) {
	c, root := newDaemon(t)
	wtA := root + "/a"
	wtB := root + "/b"
	if err := os.MkdirAll(wtA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wtB, 0o755); err != nil {
		t.Fatal(err)
	}

	a, err := c.Spawn(wtA, "feat-a", "dev", "sleep 30", nil)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := c.Spawn(wtB, "feat-b", "dev", "sleep 30", nil)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := c.Spawn(wtB, "feat-b", "api", "sleep 30", nil)
	if err != nil {
		t.Fatal(err)
	}

	recs, _ := c.List()
	byWT := map[string]int{}
	for _, r := range recs {
		byWT[r.Worktree]++
	}
	if byWT[wtA] != 1 || byWT[wtB] != 2 {
		t.Fatalf("worktree grouping wrong: %v", byWT)
	}

	// Kill all of worktree B; worktree A untouched.
	killed, err := c.Kill(0, false, wtB)
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 2 {
		t.Fatalf("want 2 killed in B, got %v", killed)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, b1.ID).Status == procstore.StatusStopped &&
			recByID(t, c, b2.ID).Status == procstore.StatusStopped
	}) {
		t.Fatal("worktree B procs not stopped")
	}
	if recByID(t, c, a.ID).Status != procstore.StatusRunning {
		t.Fatal("worktree A proc should still run")
	}
}

func TestPolicyOnFailure(t *testing.T) {
	c, root := newDaemon(t)
	pol := &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 3}

	// Success: exits 0 -> done, no restart.
	ok, _ := c.Spawn(root, "", "ok", "true", pol)
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, ok.ID).Status == procstore.StatusDone
	}) {
		t.Fatalf("success proc should be done, got %+v", recByID(t, c, ok.ID))
	}
	if r := recByID(t, c, ok.ID); r.Restarts != 0 {
		t.Fatalf("success proc should not restart, restarts=%d", r.Restarts)
	}

	// Failure: exits non-zero -> restarts.
	bad, _ := c.Spawn(root, "", "bad", "false", pol)
	if !waitFor(t, 6*time.Second, func() bool {
		return recByID(t, c, bad.ID).Restarts >= 2
	}) {
		t.Fatalf("failing proc should restart, restarts=%d", recByID(t, c, bad.ID).Restarts)
	}
}

func TestPolicyNo(t *testing.T) {
	c, root := newDaemon(t)
	pol := &procstore.Policy{Mode: procstore.PolicyNo}

	bad, _ := c.Spawn(root, "", "bad", "false", pol)
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, bad.ID).Status == procstore.StatusFailed
	}) {
		t.Fatalf("proc should be failed, got %+v", recByID(t, c, bad.ID))
	}
	// Give it a moment to (wrongly) restart; it must not.
	time.Sleep(500 * time.Millisecond)
	if r := recByID(t, c, bad.ID); r.Restarts != 0 {
		t.Fatalf("policy=no must not restart, restarts=%d", r.Restarts)
	}
}

func TestRemoveTerminal(t *testing.T) {
	c, root := newDaemon(t)

	rec, _ := c.Spawn(root, "", "svc", "sleep 30", nil)

	// Removing a running process is refused.
	if err := c.Remove(rec.ID); err == nil {
		t.Fatal("removing a running process should error")
	}

	// Kill, then remove.
	_, _ = c.Kill(rec.ID, false, "")
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, rec.ID).Status == procstore.StatusStopped
	}) {
		t.Fatal("proc not stopped")
	}
	if err := c.Remove(rec.ID); err != nil {
		t.Fatal(err)
	}
	if recByID(t, c, rec.ID) != nil {
		t.Fatal("removed proc still listed")
	}
}

func TestSetPolicy(t *testing.T) {
	c, root := newDaemon(t)

	rec, _ := c.Spawn(root, "", "svc", "sleep 30", nil)
	if rec.Policy.Mode == procstore.PolicyAlways {
		t.Fatal("precondition: default should not be always")
	}
	updated, err := c.SetPolicy(rec.ID, procstore.Policy{Mode: procstore.PolicyAlways, MaxRestarts: 7})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Policy.Mode != procstore.PolicyAlways || updated.Policy.MaxRestarts != 7 {
		t.Fatalf("policy not updated: %+v", updated.Policy)
	}
	if r := recByID(t, c, rec.ID); r.Policy.Mode != procstore.PolicyAlways {
		t.Fatalf("policy not persisted in list: %+v", r.Policy)
	}
}

func TestRestartRevives(t *testing.T) {
	c, root := newDaemon(t)

	rec, _ := c.Spawn(root, "", "svc", "sleep 30", nil)
	_, _ = c.Kill(rec.ID, false, "")
	if !waitFor(t, 2*time.Second, func() bool {
		return recByID(t, c, rec.ID).Status == procstore.StatusStopped
	}) {
		t.Fatal("proc not stopped")
	}

	revived, err := c.Restart(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revived.Status != procstore.StatusRunning || revived.PID == 0 {
		t.Fatalf("restart did not revive: %+v", revived)
	}
}

func TestSingletonLock(t *testing.T) {
	_, root := newDaemon(t)

	// While the daemon holds the repo lock, a second acquisition must fail.
	store := procstore.New(root)
	if _, err := procstore.TryLock(store.LockPath()); err != procstore.ErrLocked {
		t.Fatalf("expected ErrLocked while daemon runs, got %v", err)
	}
}

func TestShutdownGuardsRunning(t *testing.T) {
	c, root := newDaemon(t)

	_, _ = c.Spawn(root, "", "svc", "sleep 30", nil)
	// Non-forced shutdown must be refused while a process runs.
	if err := c.Shutdown(false); err == nil {
		t.Fatal("shutdown should be refused with a running process")
	}
}

// markersIn returns every bonsai delimiter parsed out of a process's log.
func markersIn(t *testing.T, c *client.Client, id int) []procstore.Marker {
	t.Helper()
	var out []procstore.Marker
	for _, line := range strings.Split(readLog(t, c, id), "\n") {
		if mk, ok := procstore.ParseMarker(line); ok {
			out = append(out, mk)
		}
	}
	return out
}

func TestLogMarkersDelimitRuns(t *testing.T) {
	c, root := newDaemon(t)

	// A failing command with restarts disabled: start marker, then exit marker
	// carrying the process's own exit code.
	rec, err := c.Spawn(root, "", "boom", "echo working; exit 3",
		&procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusFailed
	}) {
		t.Fatalf("expected failed, got %+v", recByID(t, c, rec.ID))
	}

	marks := markersIn(t, c, rec.ID)
	if len(marks) != 2 {
		t.Fatalf("markers = %+v, want start + exit", marks)
	}
	if marks[0].Kind != procstore.MarkerStart {
		t.Fatalf("first marker = %+v, want a start marker", marks[0])
	}
	if marks[1].Kind != procstore.MarkerExit || marks[1].Code != 3 {
		t.Fatalf("exit marker = %+v, want kind=exit code=3", marks[1])
	}
	// The delimiters must not swallow or corrupt the process's own output.
	if !strings.Contains(readLog(t, c, rec.ID), "working") {
		t.Fatalf("process output lost: %q", readLog(t, c, rec.ID))
	}
}

func TestLogMarkersRecordUserStopAndSuccess(t *testing.T) {
	c, root := newDaemon(t)

	stopped, err := c.Spawn(root, "", "sleeper", "sleep 30", &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Kill(stopped.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		for _, mk := range markersIn(t, c, stopped.ID) {
			if mk.Kind == procstore.MarkerStopped {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no stopped marker: %+v", markersIn(t, c, stopped.ID))
	}

	done, err := c.Spawn(root, "", "ok", "true", &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		for _, mk := range markersIn(t, c, done.ID) {
			if mk.Kind == procstore.MarkerExit && mk.Code == 0 {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no success marker: %+v", markersIn(t, c, done.ID))
	}
}

func TestRestartRace(t *testing.T) {
	c, root := newDaemon(t)

	// Command fails immediately. Policy is on-failure.
	rec, err := c.Spawn(root, "", "flapping", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until it enters backoff
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusBackoff
	}) {
		t.Fatalf("expected backoff, got %+v", recByID(t, c, rec.ID))
	}

	// Manual restart occurs while in backoff (before timer fires).
	restarted, err := c.Restart(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.ID != rec.ID {
		t.Fatalf("expected ID %d, got %d", rec.ID, restarted.ID)
	}

	// Wait long enough for the old backoff timer (1s) to have fired.
	time.Sleep(1500 * time.Millisecond)

	// Verify exactly one process exists in the daemon listing
	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 process, got %d", len(recs))
	}
}

func TestKillDuringBackoff(t *testing.T) {
	c, root := newDaemon(t)

	rec, err := c.Spawn(root, "", "flapping", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until it enters backoff
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusBackoff
	}) {
		t.Fatalf("expected backoff, got %+v", recByID(t, c, rec.ID))
	}

	// Kill during backoff
	if _, err := c.Kill(rec.ID, false, ""); err != nil {
		t.Fatal(err)
	}

	r := recByID(t, c, rec.ID)
	if r.Status != procstore.StatusStopped {
		t.Fatalf("expected stopped immediately, got %s", r.Status)
	}

	// Wait past the restart delay (1s)
	time.Sleep(1500 * time.Millisecond)

	// Assert process did NOT restart
	r = recByID(t, c, rec.ID)
	if r.Status != procstore.StatusStopped {
		t.Fatalf("expected stopped after delay, got %s", r.Status)
	}
}

func TestRemoveDuringBackoff(t *testing.T) {
	c, root := newDaemon(t)

	rec, err := c.Spawn(root, "", "flapping", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusBackoff
	}) {
		t.Fatalf("expected backoff, got %+v", recByID(t, c, rec.ID))
	}

	// Remove during backoff
	if err := c.Remove(rec.ID); err != nil {
		t.Fatalf("remove during backoff failed: %v", err)
	}

	if recByID(t, c, rec.ID) != nil {
		t.Fatal("expected removed record to be gone")
	}

	// Wait past timer
	time.Sleep(1500 * time.Millisecond)

	// Assert process never started / no new records created
	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 processes, got %d", len(recs))
	}
}

func TestAutomaticRestartFails(t *testing.T) {
	c, root := newDaemon(t)

	// Create a subdirectory as the worktree dir
	sub := filepath.Join(root, "subworktree")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rec, err := c.Spawn(sub, "", "flapping", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 3})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until it enters backoff
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusBackoff
	}) {
		t.Fatalf("expected backoff, got %+v", recByID(t, c, rec.ID))
	}

	// Delete the worktree directory so the next start attempt fails in chdir
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}

	// Restart fires, fails to start, transitions to StatusFailed
	if !waitFor(t, 3*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusFailed && r.ExitError != ""
	}) {
		t.Fatalf("expected status failed with exit error, got %+v", recByID(t, c, rec.ID))
	}

	// INVARIANT: daemon must treat process as terminal
	ping, err := c.Ping()
	if err != nil {
		t.Fatal(err)
	}
	if ping.ProcCount != 0 {
		t.Fatalf("expected 0 running processes in ping, got %d", ping.ProcCount)
	}

	r := recByID(t, c, rec.ID)
	if r.Status == procstore.StatusRunning {
		t.Fatalf("invariant violated: failed process still marked running: %+v", r)
	}
}

func TestControlledShutdown(t *testing.T) {
	c, root := newDaemon(t)

	rec, err := c.Spawn(root, "", "sleeper", "sleep 30", nil)
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID
	if !procstore.PidAlive(pid) {
		t.Fatalf("process %d not alive after spawn", pid)
	}

	// Forced shutdown
	if err := c.Shutdown(true); err != nil {
		t.Fatalf("forced shutdown failed: %v", err)
	}

	// Child process must be gone
	if procstore.PidAlive(pid) {
		t.Fatalf("child process %d still alive after shutdown", pid)
	}

	// Final state must be persisted
	store := procstore.New(root)
	persisted, err := store.ReadRecord(rec.ID)
	if err != nil {
		t.Fatalf("reading persisted record: %v", err)
	}
	if persisted.Status != procstore.StatusStopped {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, procstore.StatusStopped)
	}

	// Socket must be cleaned up
	if _, err := os.Stat(store.SockPath()); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after shutdown: %v", err)
	}
}

func TestCrashRecoveryMarksLost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	root := t.TempDir()

	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Simulate stale active records from a crashed daemon
	r1 := &procstore.Record{
		ID:        1,
		Label:     "stale-running",
		Command:   "some command 1",
		Worktree:  root,
		PID:       9999999, // Non-existent PID
		Status:    procstore.StatusRunning,
		StartedAt: time.Now().Add(-10 * time.Minute),
	}
	r2 := &procstore.Record{
		ID:        2,
		Label:     "stale-backoff",
		Command:   "some command 2",
		Worktree:  root,
		PID:       0,
		Status:    procstore.StatusBackoff,
		StartedAt: time.Now().Add(-5 * time.Minute),
	}
	if err := store.WriteRecord(r1); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteRecord(r2); err != nil {
		t.Fatal(err)
	}

	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		_ = server.Serve(root)
	}()
	c := client.For(root)
	waitAlive(t, c)
	t.Cleanup(func() {
		_ = c.Shutdown(true)
		<-srvDone
	})

	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	for _, r := range recs {
		if r.Status != procstore.StatusLost {
			t.Errorf("proc #%d status = %q, want %q", r.ID, r.Status, procstore.StatusLost)
		}
	}
}

func TestLogRotationFollower(t *testing.T) {
	c, root := newDaemonWithLogCap(t, 80)

	cmd := "printf 'line 111111111111111111\\n'; sleep 0.1; printf 'line 222222222222222222\\n'; sleep 0.1; printf 'line 333333333333333333\\n'; sleep 0.1; printf 'line 444444444444444444\\n'"
	rec, err := c.Spawn(root, "", "rotator", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}

	var collected strings.Builder
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = c.Logs(rec.ID, true, 0, "", false, func(chunk string) error {
			mu.Lock()
			collected.WriteString(chunk)
			mu.Unlock()
			return nil
		})
	}()

	if !waitFor(t, 4*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusDone
	}) {
		t.Fatalf("expected done, got %+v", recByID(t, c, rec.ID))
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("follower did not complete after process exited")
	}

	mu.Lock()
	out := collected.String()
	mu.Unlock()

	for _, expected := range []string{"line 111111111111111111", "line 222222222222222222", "line 333333333333333333", "line 444444444444444444"} {
		if !strings.Contains(out, expected) {
			t.Errorf("follower missing %q in output: %q", expected, out)
		}
	}
}

func TestProtocolCompatibility(t *testing.T) {
	t.Run("matching version succeeds", func(t *testing.T) {
		c, _ := newDaemon(t)
		ping, err := c.Ping()
		if err != nil {
			t.Fatal(err)
		}
		if ping.Version != protocol.Version {
			t.Fatalf("version = %d, want %d", ping.Version, protocol.Version)
		}
		if err := c.CheckCompatibility(); err != nil {
			t.Fatalf("CheckCompatibility failed: %v", err)
		}
	})

	t.Run("incompatible busy daemon refuses and preserves processes", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
		root := t.TempDir()

		store := procstore.New(root)
		if err := store.EnsureDirs(); err != nil {
			t.Fatal(err)
		}

		ln, err := net.Listen("unix", store.SockPath())
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()

		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					dec := protocol.NewDecoder(c)
					enc := protocol.NewEncoder(c)
					req, err := dec.ReadRequest()
					if err != nil {
						return
					}
					if req.Kind == protocol.KindPing {
						_ = enc.WriteResponse(&protocol.Response{
							OK:        true,
							Version:   999, // incompatible!
							ProcCount: 2,   // has 2 running processes!
							EOF:       true,
						})
					}
				}(conn)
			}
		}()

		c := client.For(root)
		err = c.CheckCompatibility()
		if err == nil {
			t.Fatal("expected error on incompatible busy daemon, got nil")
		}
		if !strings.Contains(err.Error(), "daemon protocol version mismatch") && !strings.Contains(err.Error(), "version 999") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("incompatible idle daemon is safely replaced", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
		root := t.TempDir()

		store := procstore.New(root)
		if err := store.EnsureDirs(); err != nil {
			t.Fatal(err)
		}

		ln, err := net.Listen("unix", store.SockPath())
		if err != nil {
			t.Fatal(err)
		}

		shutdownReceived := make(chan struct{})
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					dec := protocol.NewDecoder(c)
					enc := protocol.NewEncoder(c)
					req, err := dec.ReadRequest()
					if err != nil {
						return
					}
					switch req.Kind {
					case protocol.KindPing:
						_ = enc.WriteResponse(&protocol.Response{
							OK:        true,
							Version:   999, // incompatible!
							ProcCount: 0,   // idle!
							EOF:       true,
						})
					case protocol.KindShutdown:
						_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
						_ = ln.Close()
						_ = os.Remove(store.SockPath())
						close(shutdownReceived)
					}
				}(conn)
			}
		}()

		c := client.For(root)
		_ = c.CheckCompatibility()
		select {
		case <-shutdownReceived:
			// Success: shutdown was requested for the idle incompatible daemon
		case <-time.After(2 * time.Second):
			t.Fatal("expected shutdown to be sent to idle incompatible daemon")
		}
	})
}

func TestOfflineLogs(t *testing.T) {
	root := t.TempDir()
	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	content := "line 1: INFO startup\nline 2: WARNING low memory\nline 3: ERROR disk full\nline 4: info heartbeat\nline 5: error timeout\n"
	if err := os.WriteFile(store.LogPath(1), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c := client.For(root)

	// Tail
	var tailOut string
	if err := c.Logs(1, false, 2, "", false, func(s string) error { tailOut += s; return nil }); err != nil {
		t.Fatal(err)
	}
	wantTail := "line 4: info heartbeat\nline 5: error timeout\n"
	if tailOut != wantTail {
		t.Errorf("offline tail = %q, want %q", tailOut, wantTail)
	}

	// Grep case-sensitive
	var grepOut string
	if err := c.Logs(1, false, 0, "ERROR", false, func(s string) error { grepOut += s; return nil }); err != nil {
		t.Fatal(err)
	}
	wantGrep := "line 3: ERROR disk full\n"
	if grepOut != wantGrep {
		t.Errorf("offline grep = %q, want %q", grepOut, wantGrep)
	}

	// Grep case-insensitive
	var grepIOut string
	if err := c.Logs(1, false, 0, "error", true, func(s string) error { grepIOut += s; return nil }); err != nil {
		t.Fatal(err)
	}
	wantGrepI := "line 3: ERROR disk full\nline 5: error timeout\n"
	if grepIOut != wantGrepI {
		t.Errorf("offline grep -i = %q, want %q", grepIOut, wantGrepI)
	}
}
