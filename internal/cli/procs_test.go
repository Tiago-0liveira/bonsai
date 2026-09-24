package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/server"
)

// shortRuntimeDir returns a short-pathed dir for the daemon socket (macOS caps
// Unix socket paths at ~104 bytes; t.TempDir() is far too deep). On Windows,
// defaults to standard temp dir.
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

// setupCLIRepo creates a temp git repo, chdirs into it, isolates XDG dirs, and
// starts an in-process daemon so CLI commands connect without exec-autostarting.
func setupCLIRepo(t *testing.T) string {
	t.Helper()
	// Isolate the global index across platforms (Linux, macOS, Windows).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	repo := t.TempDir()
	// HOME is redirected for index isolation, so git has no global identity;
	// supply one per invocation.
	ident := []string{"-c", "user.email=test@example.com", "-c", "user.name=test"}
	run := func(args ...string) {
		cmd := exec.Command("git", append(ident, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")
	t.Chdir(repo)

	root, err := git.MainRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(root) }()

	c := client.For(root)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Ping(); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { shutdownAndWait(t, root) })
	return root
}

// runCLI invokes a subcommand and returns combined stdout.
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := Run(args, &out, &errOut); err != nil {
		t.Fatalf("cli %v: %v (stderr: %s)", args, err, errOut.String())
	}
	return out.String()
}

// runCLIExpectErr invokes a subcommand expecting an error.
func runCLIExpectErr(t *testing.T, args ...string) error {
	t.Helper()
	var out, errOut bytes.Buffer
	return Run(args, &out, &errOut)
}

func TestCLISpawnPsLogsGrepKill(t *testing.T) {
	setupCLIRepo(t)

	devCmd := "printf 'listening on http://localhost:5173\\n'; sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		devCmd = "echo listening on http://localhost:5173 & ping -n 31 127.0.0.1 >nul"
	}
	out := runCLI(t, "spawn", "--label", "dev", devCmd)
	if !strings.Contains(out, "started #1") {
		t.Fatalf("spawn output: %q", out)
	}

	// ps eventually shows the process running with its detected URL.
	var ps string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ps = runCLI(t, "ps")
		if strings.Contains(ps, "running") && strings.Contains(ps, "5173") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(ps, "dev") || !strings.Contains(ps, "running") || !strings.Contains(ps, "http://localhost:5173") {
		t.Fatalf("ps output: %q", ps)
	}

	// logs shows captured output.
	logs := runCLI(t, "logs", "1")
	if !strings.Contains(logs, "listening on http://localhost:5173") {
		t.Fatalf("logs output: %q", logs)
	}

	// grep across all logs prefixes with the id.
	grep := runCLI(t, "grep", "listening")
	if !strings.Contains(grep, "1: listening on http://localhost:5173") {
		t.Fatalf("grep output: %q", grep)
	}
	// grep with a non-match yields nothing.
	if g := runCLI(t, "grep", "zzznotfound"); strings.TrimSpace(g) != "" {
		t.Fatalf("grep non-match should be empty, got %q", g)
	}

	// kill, then ps shows stopped.
	if k := runCLI(t, "kill", "1"); !strings.Contains(k, "killed") {
		t.Fatalf("kill output: %q", k)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(runCLI(t, "ps"), "stopped") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(runCLI(t, "ps"), "stopped") {
		t.Fatalf("ps after kill: %q", runCLI(t, "ps"))
	}
}

// startRepo creates and inits a temp git repo, starts its in-process daemon,
// and returns its main root. Unlike setupCLIRepo it does not touch HOME/XDG
// env vars, so callers can share one global index across multiple repos.
func startRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	ident := []string{"-c", "user.email=test@example.com", "-c", "user.name=test"}
	run := func(args ...string) {
		cmd := exec.Command("git", append(ident, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")

	root, err := git.MainRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(root) }()

	c := client.For(root)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Ping(); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { shutdownAndWait(t, root) })
	return root
}

// shutdownAndWait stops root's daemon and blocks until it has deregistered
// from the global index (Deregister runs asynchronously after the Shutdown
// RPC responds), so callers can safely tear down shared HOME/XDG dirs right
// after — otherwise a late Deregister write can race a concurrent
// RemoveAll(HOME) and fail with "directory not empty".
func shutdownAndWait(t *testing.T, root string) {
	t.Helper()
	_ = client.For(root).Shutdown(true)
	clean := filepath.Clean(root)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		daemons, err := procstore.ListDaemons()
		if err != nil {
			return
		}
		found := false
		for _, d := range daemons {
			if d.Root == clean {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCLIKillCrossRepo covers killing a process in another repo's daemon via
// --repo, since process ids are local to each repo (regression: bare `kill
// <id>` from repo A silently no-ops on an id that only exists in repo B).
func TestCLIKillCrossRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))

	rootA := startRepo(t)
	rootB := startRepo(t)

	t.Chdir(rootA)
	sleepCmd := "sleep 30"
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		sleepCmd = "ping -n 31 127.0.0.1 >nul"
	}
	runCLI(t, "spawn", "--label", "in-a", sleepCmd)

	// Repo B's daemon starts its own id counter at 1 too, but has spawned
	// nothing: from there, id 1 only exists in repo A's daemon. A bare kill
	// no-ops with a hint, but targeting repo A via --repo (by directory
	// name) succeeds.
	t.Chdir(rootB)
	if out := runCLI(t, "kill", "1"); !strings.Contains(out, "no matching") {
		t.Fatalf("bare kill across repos should no-op, got: %q", out)
	}
	if out := runCLI(t, "kill", "--repo", filepath.Base(rootA), "1"); !strings.Contains(out, "killed") {
		t.Fatalf("kill --repo output: %q", out)
	}

	deadline := time.Now().Add(2 * time.Second)
	var ps string
	for time.Now().Before(deadline) {
		ps = runCLI(t, "ps", "--all")
		if strings.Contains(ps, "stopped") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(ps, "stopped") {
		t.Fatalf("ps --all after cross-repo kill: %q", ps)
	}
}

func TestCLIMultipleProcesses(t *testing.T) {
	setupCLIRepo(t)

	runCLI(t, "spawn", "--label", "one", "sleep 30")
	runCLI(t, "spawn", "--label", "two", "sleep 30")
	runCLI(t, "spawn", "--label", "three", "sleep 30")

	ps := runCLI(t, "ps")
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(ps, want) {
			t.Fatalf("ps missing %q: %s", want, ps)
		}
	}
	if n := strings.Count(ps, "running"); n != 3 {
		t.Fatalf("want 3 running, got %d:\n%s", n, ps)
	}

	// kill --all stops every process.
	runCLI(t, "kill", "--all")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(runCLI(t, "ps"), "running") == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if strings.Count(runCLI(t, "ps"), "running") != 0 {
		t.Fatalf("processes still running after kill --all:\n%s", runCLI(t, "ps"))
	}
}

func TestCLIRestartAndDaemonStatus(t *testing.T) {
	setupCLIRepo(t)

	runCLI(t, "spawn", "--label", "svc", "sleep 30")

	// daemon status reports the running count.
	if s := runCLI(t, "daemon", "status"); !strings.Contains(s, "running process") {
		t.Fatalf("daemon status: %q", s)
	}

	// kill then restart revives it.
	runCLI(t, "kill", "1")
	time.Sleep(300 * time.Millisecond)
	if r := runCLI(t, "restart", "1"); !strings.Contains(r, "restarted #1") {
		t.Fatalf("restart output: %q", r)
	}
	if !strings.Contains(runCLI(t, "ps"), "running") {
		t.Fatalf("process not running after restart:\n%s", runCLI(t, "ps"))
	}
}

func TestCLISpawnRestartPolicyValidation(t *testing.T) {
	setupCLIRepo(t)

	// An invalid --restart value is rejected before spawning.
	if err := runCLIExpectErr(t, "spawn", "--restart", "bogus", "sleep 1"); err == nil {
		t.Fatal("expected error for invalid --restart")
	}
	// A valid policy spawns.
	if out := runCLI(t, "spawn", "--restart", "always", "sleep 30"); !strings.Contains(out, "started") {
		t.Fatalf("spawn with policy: %q", out)
	}
}

func TestCLIOfflineLogs(t *testing.T) {
	// Setup repo without starting any daemon
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_RUNTIME_DIR", shortRuntimeDir(t))
	repo := t.TempDir()

	ident := []string{"-c", "user.email=test@example.com", "-c", "user.name=test"}
	cmd := exec.Command("git", append(ident, "init")...)
	cmd.Dir = repo
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	store := procstore.New(repo)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	content := "line 1: INFO init\nline 2: WARNING alert\nline 3: ERROR critical failure\nline 4: info ping\nline 5: error timeout\n"
	if err := os.WriteFile(store.LogPath(1), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tail
	tailOut := runCLI(t, "logs", "1", "-n", "2")
	if !strings.Contains(tailOut, "line 4: info ping") || !strings.Contains(tailOut, "line 5: error timeout") || strings.Contains(tailOut, "line 1") {
		t.Fatalf("unexpected offline tail output: %q", tailOut)
	}

	// Grep case-sensitive
	grepOut := runCLI(t, "logs", "1", "--grep", "ERROR")
	if !strings.Contains(grepOut, "line 3: ERROR critical failure") || strings.Contains(grepOut, "line 5") {
		t.Fatalf("unexpected offline grep output: %q", grepOut)
	}

	// Grep case-insensitive
	grepIOut := runCLI(t, "logs", "1", "--grep", "error", "-i")
	if !strings.Contains(grepIOut, "line 3: ERROR critical failure") || !strings.Contains(grepIOut, "line 5: error timeout") || strings.Contains(grepIOut, "line 1") {
		t.Fatalf("unexpected offline grep -i output: %q", grepIOut)
	}
}
