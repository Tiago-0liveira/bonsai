package server_test

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
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
	// Isolate the global index across platforms (Linux, macOS, Windows).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
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
		case <-time.After(5 * time.Second):
		}
	})
	return c, root
}

func newDaemonWithLogCap(t *testing.T, logLimit int64) (*client.Client, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
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
		case <-time.After(5 * time.Second):
		}
	})
	return c, root
}

func newDaemonWithServer(t *testing.T) (*client.Client, string, *server.Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	root := t.TempDir()

	srv, err := server.NewServer(root)
	if err != nil {
		t.Fatal(err)
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
		case <-time.After(5 * time.Second):
		}
	})
	return c, root, srv
}

// newExternalDaemonClient returns an isolated client whose daemon, when needed,
// is started as a real child process. Recovery tests use this instead of an
// in-process Server so killing the daemon also removes its supervisor goroutines.
func newExternalDaemonClient(t *testing.T) (*client.Client, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	t.Setenv("BONSAI_DAEMON_BIN", getTestBonsaiBin(t))
	root := t.TempDir()
	c := client.For(root)
	t.Cleanup(func() { _ = c.Shutdown(true) })
	return c, root
}

// crashExternalDaemon terminates the actual daemon process without asking it to
// stop its children, then waits until both the PID and socket are unusable.
func crashExternalDaemon(t *testing.T, c *client.Client) {
	t.Helper()
	ping, err := c.Ping()
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.FindProcess(ping.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	// Reap when possible. Signal-0/PidAlive is not a useful crash boundary here:
	// a killed detached child may remain as a zombie until somebody waits for it.
	_, _ = p.Wait()
	if !waitFor(t, 3*time.Second, func() bool {
		_, err := c.Ping()
		return err != nil
	}) {
		t.Fatal("daemon socket remained reachable after crash")
	}
}

var (
	testBonsaiBin     string
	testBonsaiBinOnce sync.Once
	realHome, _       = os.UserHomeDir()
	realGOPATH        = os.Getenv("GOPATH")
	realGOCACHE       = os.Getenv("GOCACHE")
)

func getTestBonsaiBin(t *testing.T) string {
	t.Helper()
	testBonsaiBinOnce.Do(func() {
		binDir, err := os.MkdirTemp("", "bonsai-test-bin-*")
		if err != nil {
			t.Fatal(err)
		}
		name := "bonsai"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		testBonsaiBin = filepath.Join(binDir, name)
		cmd := exec.Command("go", "build", "-o", testBonsaiBin, "github.com/Tiago-0liveira/bonsai")
		env := os.Environ()
		if realHome != "" {
			env = append(env, "HOME="+realHome)
		}
		if realGOPATH != "" {
			env = append(env, "GOPATH="+realGOPATH)
		}
		if realGOCACHE != "" {
			env = append(env, "GOCACHE="+realGOCACHE)
		}
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building bonsai binary for tests: %v: %s", err, string(out))
		}
	})
	return testBonsaiBin
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

func TestClientStructuredExecHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+2 >= len(os.Args) {
		return
	}
	out := os.Args[sep+1]
	payload := os.Args[sep+2]
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(out, []byte(cwd+"\n"+payload), 0o644); err != nil {
		os.Exit(3)
	}
}

func TestClientSpawnExecPreservesStructuredInvocation(t *testing.T) {
	c, root := newDaemon(t)
	working := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "spawn-exec-result.txt")
	marker := filepath.Join(root, "MUST_NOT_EXIST")
	literal := "arg with spaces; touch " + marker
	args := []string{"-test.run=^TestClientStructuredExecHelper$", "--", out, literal}

	rec, err := c.SpawnExec(root, "feat/structured", working, "structured", exe, args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Worktree != root || rec.WorkingDir != working || rec.Program != exe || len(rec.Args) != len(args) {
		t.Fatalf("structured spawn record = %+v", rec)
	}

	if !waitFor(t, 3*time.Second, func() bool {
		data, readErr := os.ReadFile(out)
		if readErr != nil {
			return false
		}
		parts := strings.SplitN(string(data), "\n", 2)
		return len(parts) == 2 && pathsEquivalent(parts[0], working) && parts[1] == literal
	}) {
		data, _ := os.ReadFile(out)
		t.Fatalf("structured helper output = %q", string(data))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("structured argv was interpreted by a shell: %v", err)
	}

	listed := recByID(t, c, rec.ID)
	if listed == nil || listed.Program != exe || listed.WorkingDir != working || len(listed.Args) != len(args) {
		t.Fatalf("listed structured record = %+v", listed)
	}
}

func pathsEquivalent(a, b string) bool {
	canonical := func(path string) string {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		}
		return filepath.Clean(path)
	}
	return canonical(a) == canonical(b)
}

func TestSpawnListLogsKill(t *testing.T) {
	c, root := newDaemon(t)

	greetCmd := "printf 'listening on http://localhost:3000\\n'; sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		greetCmd = "echo listening on http://localhost:3000 & ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "greet", greetCmd, nil)
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
	boomCmd := "echo working; exit 3"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		boomCmd = "echo working & exit /b 3"
	}
	rec, err := c.Spawn(root, "", "boom", boomCmd,
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

func TestRestartRace_KillDuringStartingBarrier(t *testing.T) {
	c, root, srv := newDaemonWithServer(t)

	barrierHit := make(chan struct{})
	killDone := make(chan struct{})
	srv.SetStartBarrier(func(id int) {
		close(barrierHit)
		// During the starting barrier, issue kill
		_, err := c.Kill(id, false, "")
		if err != nil {
			t.Errorf("kill during barrier failed: %v", err)
		}
		close(killDone)
	})

	rec, err := c.Spawn(root, "", "flapper", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-barrierHit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for start barrier")
	}

	<-killDone
	time.Sleep(500 * time.Millisecond)

	r := recByID(t, c, rec.ID)
	if r.Status != procstore.StatusStopped {
		t.Fatalf("expected status stopped, got %s", r.Status)
	}

	// Verify it does NOT restart later
	time.Sleep(1500 * time.Millisecond)
	r = recByID(t, c, rec.ID)
	if r.Status != procstore.StatusStopped {
		t.Fatalf("expected status to remain stopped, got %s", r.Status)
	}
}

func TestRestartRace_RestartDuringStartingBarrier(t *testing.T) {
	c, root, srv := newDaemonWithServer(t)

	var once sync.Once
	barrierHit := make(chan struct{})
	restartDone := make(chan struct{})
	srv.SetStartBarrier(func(id int) {
		once.Do(func() {
			close(barrierHit)
			// During the starting barrier, issue manual restart
			restarted, err := c.Restart(id)
			if err != nil {
				t.Errorf("restart during barrier failed: %v", err)
			}
			if restarted == nil {
				t.Errorf("restart returned nil record")
			}
			close(restartDone)
		})
	})

	_, err := c.Spawn(root, "", "flapper", "exit 1", &procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-barrierHit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for start barrier")
	}

	<-restartDone
	time.Sleep(1500 * time.Millisecond)

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
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
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

func TestCrashRecovery_OrphanAliveAndCleanup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	root := t.TempDir()

	srv1, err := server.NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	srv1Done := make(chan struct{})
	go func() {
		defer close(srv1Done)
		_ = srv1.Run()
	}()
	c := client.For(root)
	waitAlive(t, c)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 30 127.0.0.1 >nul"
	}

	rec, err := c.Spawn(root, "", "long-job", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID
	if !procstore.PidAlive(pid) {
		t.Fatalf("spawned process PID %d is not alive", pid)
	}

	// Stop daemon 1 WITHOUT killing children (simulating crash leaving orphan alive)
	srv1.Stop(false)
	<-srv1Done

	// Child must still be alive
	if !procstore.PidAlive(pid) {
		t.Fatalf("expected child PID %d to still be alive after ungraceful shutdown", pid)
	}

	// Start daemon 2 for the same root
	srv2, err := server.NewServer(root)
	if err != nil {
		t.Fatal(err)
	}
	srv2Done := make(chan struct{})
	go func() {
		defer close(srv2Done)
		_ = srv2.Run()
	}()
	waitAlive(t, c)
	t.Cleanup(func() {
		_ = c.Shutdown(true)
		<-srv2Done
	})

	// Daemon 2 must reconcile live orphan as StatusOrphan
	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].Status != procstore.StatusOrphan {
		t.Fatalf("expected status %q, got %q", procstore.StatusOrphan, recs[0].Status)
	}

	// Bonsai kill must succeed in killing the orphan
	killed, err := c.Kill(recs[0].ID, false, "")
	if err != nil {
		t.Fatalf("c.Kill failed: %v", err)
	}
	if len(killed) != 1 || killed[0] != recs[0].ID {
		t.Fatalf("expected killed IDs [%d], got %v", recs[0].ID, killed)
	}

	// Verify status is stopped
	r := recByID(t, c, recs[0].ID)
	if r.Status != procstore.StatusStopped {
		t.Fatalf("expected status %q, got %q", procstore.StatusStopped, r.Status)
	}

	// Verify child process was actually terminated
	if !waitFor(t, 2*time.Second, func() bool {
		return !procstore.PidAlive(pid)
	}) {
		t.Fatalf("expected child PID %d to be dead after kill", pid)
	}
}

func TestKillRecoversAfterDaemonCrash(t *testing.T) {
	c, root := newExternalDaemonClient(t)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "orphan-kill", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID

	// Simulate a daemon crash while the child survives.
	crashExternalDaemon(t, c)
	if !procstore.PidAlive(pid) {
		t.Fatalf("expected child PID %d to survive daemon crash", pid)
	}

	// Kill must revive supervision, reconcile the orphan, then terminate it.
	killed, err := c.Kill(rec.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != rec.ID {
		t.Fatalf("killed = %v, want [%d]", killed, rec.ID)
	}
	if procstore.PidAlive(pid) {
		t.Fatalf("kill returned before orphan PID %d was confirmed dead", pid)
	}
	r := recByID(t, c, rec.ID)
	if r == nil || r.Status != procstore.StatusStopped {
		t.Fatalf("record = %+v, want stopped", r)
	}
}

func TestRemoveRefusesLiveOrphanAfterDaemonCrash(t *testing.T) {
	c, root := newExternalDaemonClient(t)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "orphan-remove", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID

	crashExternalDaemon(t, c)

	if err := c.Remove(rec.ID); err == nil {
		t.Fatal("expected remove to refuse a live orphan")
	}
	if !procstore.PidAlive(pid) {
		t.Fatalf("remove unexpectedly killed orphan PID %d", pid)
	}
	if _, err := os.Stat(procstore.New(root).RecordPath(rec.ID)); err != nil {
		t.Fatalf("remove deleted orphan metadata: %v", err)
	}

	// Cleanup through the normal recovery-aware kill path.
	if _, err := c.Kill(rec.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return !procstore.PidAlive(pid) }) {
		t.Fatalf("orphan PID %d survived cleanup", pid)
	}
}

func TestForceShutdownRecoversAndKillsOrphan(t *testing.T) {
	c, root := newExternalDaemonClient(t)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "orphan-shutdown", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID

	crashExternalDaemon(t, c)
	if !procstore.PidAlive(pid) {
		t.Fatalf("expected child PID %d to survive daemon crash", pid)
	}

	// --force is a cleanup operation even when the original daemon is gone.
	if err := c.Shutdown(true); err != nil {
		t.Fatal(err)
	}
	if procstore.PidAlive(pid) {
		t.Fatalf("forced shutdown returned before orphan PID %d was confirmed dead", pid)
	}

	persisted, err := procstore.New(root).ReadRecord(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != procstore.StatusStopped {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, procstore.StatusStopped)
	}
}

func TestManualRestartLogOpenFailureBecomesFailed(t *testing.T) {
	c, root := newDaemon(t)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "restart-log-failure", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Kill(rec.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusStopped
	}) {
		t.Fatalf("process did not stop: %+v", recByID(t, c, rec.ID))
	}

	// Make newLogWriter fail before cmd.Start. A directory at the log path is
	// portable and does not depend on filesystem permission behavior.
	store := procstore.New(root)
	if err := os.Remove(store.LogPath(rec.ID)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.LogPath(rec.ID), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Restart(rec.ID); err == nil {
		t.Fatal("expected restart to fail when log path is a directory")
	}
	r := recByID(t, c, rec.ID)
	if r == nil || r.Status != procstore.StatusFailed || r.ExitError == "" {
		t.Fatalf("record = %+v, want failed with exit error", r)
	}
	ping, err := c.Ping()
	if err != nil {
		t.Fatal(err)
	}
	if ping.ProcCount != 0 {
		t.Fatalf("active process count = %d, want 0", ping.ProcCount)
	}
}

func TestCrashRecoveryAllowsProcessChangedCWD(t *testing.T) {
	c, root := newExternalDaemonClient(t)

	sub := filepath.Join(root, "child")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := "cd child && sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "cd child && ping -n 31 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "changed-cwd", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	pid := rec.PID
	time.Sleep(100 * time.Millisecond)

	crashExternalDaemon(t, c)

	recs, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Status != procstore.StatusOrphan {
		t.Fatalf("records = %+v, want one orphan after cwd change", recs)
	}
	if !procstore.PidAlive(pid) {
		t.Fatalf("PID %d unexpectedly exited", pid)
	}

	if _, err := c.Kill(rec.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return !procstore.PidAlive(pid) }) {
		t.Fatalf("PID %d survived cleanup", pid)
	}
}

func TestLogRotationFollower(t *testing.T) {
	c, root := newDaemonWithLogCap(t, 80)

	cmd := "printf 'line 111111111111111111\\n'; sleep 0.1; printf 'line 222222222222222222\\n'; sleep 0.1; printf 'line 333333333333333333\\n'; sleep 0.1; printf 'line 444444444444444444\\n'"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "echo line 111111111111111111& ping -n 1 127.0.0.1 >nul& echo line 222222222222222222& ping -n 1 127.0.0.1 >nul& echo line 333333333333333333& ping -n 1 127.0.0.1 >nul& echo line 444444444444444444"
	}
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

func TestShutdownErrorsWhenDaemonSurvivesDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
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
				if req.Kind == protocol.KindShutdown {
					// Acknowledge the request but deliberately remain reachable.
					_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
				}
			}(conn)
		}
	}()

	err = client.For(root).Shutdown(true)
	if err == nil {
		t.Fatal("expected shutdown to fail when daemon survives the deadline")
	}
	if !strings.Contains(err.Error(), "survived shutdown deadline") {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}

func TestScheduledRestartHonorsPolicyChangeDuringBackoff(t *testing.T) {
	c, root := newDaemon(t)

	rec, err := c.Spawn(root, "", "policy-change", "exit 1",
		&procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 5})
	if err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusBackoff
	}) {
		t.Fatalf("expected backoff, got %+v", recByID(t, c, rec.ID))
	}
	if _, err := c.SetPolicy(rec.ID, procstore.Policy{Mode: procstore.PolicyNo}); err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.Status == procstore.StatusFailed
	}) {
		t.Fatalf("scheduled restart ignored policy change: %+v", recByID(t, c, rec.ID))
	}
	r := recByID(t, c, rec.ID)
	if r.Restarts != 1 {
		t.Fatalf("restarts = %d, want 1", r.Restarts)
	}
}

func TestActiveCountReconcilesExitedOrphan(t *testing.T) {
	c, root := newExternalDaemonClient(t)

	cmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "ping -n 31 127.0.0.1 >nul"
	}
	orphan, err := c.Spawn(root, "", "orphan-count", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	crashExternalDaemon(t, c)

	// Starting another process revives the daemon, which adopts the survivor as
	// an orphan without otherwise touching its status.
	keeper, err := c.Spawn(root, "", "keeper", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}
	ping, err := c.Ping()
	if err != nil {
		t.Fatal(err)
	}
	if ping.ProcCount != 2 {
		t.Fatalf("active count before orphan exit = %d, want 2", ping.ProcCount)
	}

	coreexec.KillPID(orphan.PID)
	if !waitFor(t, 3*time.Second, func() bool {
		return !procstore.ProcessMatches(orphan.PID, orphan.StartedAt, orphan.Worktree)
	}) {
		t.Fatalf("orphan PID %d did not exit", orphan.PID)
	}

	// Do not call List here: Ping itself must reconcile the dead orphan while
	// computing activeCountLocked.
	ping, err = c.Ping()
	if err != nil {
		t.Fatal(err)
	}
	if ping.ProcCount != 1 {
		t.Fatalf("active count after orphan exit = %d, want 1", ping.ProcCount)
	}
	persisted, err := procstore.New(root).ReadRecord(orphan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != procstore.StatusLost {
		t.Fatalf("orphan status = %q, want %q", persisted.Status, procstore.StatusLost)
	}

	if _, err := c.Kill(keeper.ID, false, ""); err != nil {
		t.Fatal(err)
	}
}

func TestNonFollowLogsIncludeRotatedHistory(t *testing.T) {
	c, root := newDaemon(t)
	store := procstore.New(root)
	id := 4242
	if err := os.WriteFile(store.LogPath(id)+".1", []byte("old-1\nold-target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.LogPath(id), []byte("new-1\nnew-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read := func(tail int, grep string) string {
		t.Helper()
		var out strings.Builder
		if err := c.Logs(id, false, tail, grep, false, func(chunk string) error {
			out.WriteString(chunk)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if got, want := read(0, ""), "old-1\nold-target\nnew-1\nnew-2\n"; got != want {
		t.Fatalf("online full log = %q, want %q", got, want)
	}
	if got, want := read(3, ""), "old-target\nnew-1\nnew-2\n"; got != want {
		t.Fatalf("online tail = %q, want %q", got, want)
	}
	if got, want := read(0, "old-target"), "old-target\n"; got != want {
		t.Fatalf("online grep = %q, want %q", got, want)
	}

	if err := c.Shutdown(false); err != nil {
		t.Fatal(err)
	}
	if got, want := read(0, ""), "old-1\nold-target\nnew-1\nnew-2\n"; got != want {
		t.Fatalf("offline full log = %q, want %q", got, want)
	}
	if got, want := read(0, "old-target"), "old-target\n"; got != want {
		t.Fatalf("offline grep = %q, want %q", got, want)
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
		appData := t.TempDir()
		t.Setenv("AppData", appData)
		t.Setenv("APPDATA", appData)
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

	t.Run("incompatible daemon survives shutdown returns ErrIncompatibleDaemon", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
		appData := t.TempDir()
		t.Setenv("AppData", appData)
		t.Setenv("APPDATA", appData)
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
					switch req.Kind {
					case protocol.KindPing:
						_ = enc.WriteResponse(&protocol.Response{
							OK:        true,
							Version:   999, // incompatible
							ProcCount: 0,
							EOF:       true,
						})
					case protocol.KindShutdown:
						// Acknowledge shutdown but keep listening to simulate daemon surviving deadline
						_ = enc.WriteResponse(&protocol.Response{OK: true, EOF: true})
					}
				}(conn)
			}
		}()

		c := client.For(root)
		err = c.CheckCompatibility()
		if err == nil {
			t.Fatal("expected error when daemon survives shutdown deadline, got nil")
		}
		if !errors.Is(err, client.ErrIncompatibleDaemon) {
			t.Fatalf("expected ErrIncompatibleDaemon, got: %v", err)
		}
		if !strings.Contains(err.Error(), "survived shutdown deadline") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("incompatible daemon shutdown error returns ErrIncompatibleDaemon", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
		appData := t.TempDir()
		t.Setenv("AppData", appData)
		t.Setenv("APPDATA", appData)
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
					switch req.Kind {
					case protocol.KindPing:
						_ = enc.WriteResponse(&protocol.Response{
							OK:        true,
							Version:   999,
							ProcCount: 0,
							EOF:       true,
						})
					case protocol.KindShutdown:
						// Abruptly close to cause shutdown error
						c.Close()
					}
				}(conn)
			}
		}()

		c := client.For(root)
		err = c.CheckCompatibility()
		if err == nil {
			t.Fatal("expected error on shutdown failure, got nil")
		}
		if !errors.Is(err, client.ErrIncompatibleDaemon) {
			t.Fatalf("expected ErrIncompatibleDaemon, got: %v", err)
		}
	})

	t.Run("incompatible idle daemon is safely replaced and verified", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")
		appData := t.TempDir()
		t.Setenv("AppData", appData)
		t.Setenv("APPDATA", appData)
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

		binPath := getTestBonsaiBin(t)
		t.Setenv("BONSAI_DAEMON_BIN", binPath)

		c := client.For(root)
		if err := c.CheckCompatibility(); err != nil {
			t.Fatalf("CheckCompatibility failed: %v", err)
		}

		select {
		case <-shutdownReceived:
		default:
			t.Fatal("expected shutdown to be received")
		}

		// Ping must confirm that the replacement daemon is running the current protocol version
		ping, err := c.Ping()
		if err != nil {
			t.Fatalf("pinging replacement daemon: %v", err)
		}
		if ping.Version != protocol.Version {
			t.Fatalf("replacement version = %d, want %d", ping.Version, protocol.Version)
		}
		_ = c.Shutdown(true)
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

func TestLastURL_LiveCapture(t *testing.T) {
	c, root := newDaemon(t)

	cmd := "printf 'Starting dev server on http://localhost:4321\\n'; sleep 0.5"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "echo Starting dev server on http://localhost:4321 & ping -n 2 127.0.0.1 >nul"
	}
	rec, err := c.Spawn(root, "", "web", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}

	if !waitFor(t, 3*time.Second, func() bool {
		r := recByID(t, c, rec.ID)
		return r != nil && r.LastURL == "http://localhost:4321"
	}) {
		t.Fatalf("expected LastURL to be captured as http://localhost:4321, got %+v", recByID(t, c, rec.ID))
	}
}

func TestLogStreamingGrepAcrossChunkBoundary(t *testing.T) {
	c, root := newDaemon(t)

	// Create a line where the match target straddles the 32 KiB chunk boundary (32768 bytes).
	// 32765 'a's + "MATCH_TARGET" + " remainder\n"
	pad := strings.Repeat("a", 32765)
	payload := pad + "MATCH_TARGET remainder\n"
	logPath := filepath.Join(root, "source.log")
	if err := os.WriteFile(logPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := "cat source.log"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		cmd = "type source.log"
	}

	rec, err := c.Spawn(root, "", "grepper", cmd, &procstore.Policy{Mode: procstore.PolicyNo})
	if err != nil {
		t.Fatal(err)
	}

	var collected strings.Builder
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = c.Logs(rec.ID, true, 0, "MATCH_TARGET", false, func(chunk string) error {
			mu.Lock()
			collected.WriteString(chunk)
			mu.Unlock()
			return nil
		})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for log streaming")
	}

	mu.Lock()
	got := strings.ReplaceAll(collected.String(), "\r\n", "\n")
	mu.Unlock()

	if !strings.Contains(got, "MATCH_TARGET") {
		t.Fatalf("expected streamed grep to match string crossing 32 KiB boundary, got %d bytes: %q", len(got), got)
	}
	if got != payload {
		t.Fatalf("expected complete intact line of length %d, got %d", len(payload), len(got))
	}
}
