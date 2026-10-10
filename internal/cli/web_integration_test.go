package cli

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
)

var (
	webTestBin     string
	webTestBinErr  error
	webTestBinOnce sync.Once
)

// webTestBinary builds the real bonsai binary once: `bonsai web` spawns the
// daemon and the API from os.Executable, so a test binary cannot stand in.
func webTestBinary(t *testing.T) string {
	t.Helper()
	webTestBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "bonsai-web-bin-*")
		if err != nil {
			webTestBinErr = err
			return
		}
		name := "bonsai"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		webTestBin = filepath.Join(dir, name)
		out, err := exec.Command("go", "build", "-o", webTestBin, "github.com/Tiago-0liveira/bonsai").CombinedOutput()
		if err != nil {
			webTestBinErr = errors.New(err.Error() + ": " + string(out))
		}
	})
	if webTestBinErr != nil {
		t.Fatalf("building bonsai: %v", webTestBinErr)
	}
	return webTestBin
}

type webEnv struct {
	bin, cwd, home string
}

// newWebEnv isolates every user-level path bonsai web touches (config, state,
// sockets, daemon index) on Linux, macOS and Windows, and guarantees the web
// daemon is shut down afterwards.
func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the bonsai binary")
	}
	bin := webTestBinary(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))
	t.Setenv("LocalAppData", filepath.Join(home, "localappdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	t.Setenv("BONSAI_FRONTEND_ORIGIN", "")
	t.Setenv("BONSAI_DAEMON_BIN", bin)
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp" // Unix socket paths must stay short
	}
	run, err := os.MkdirTemp(base, "bwrt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	webHome, err := config.WebHome()
	if err != nil {
		t.Fatal(err)
	}
	env := &webEnv{bin: bin, cwd: t.TempDir(), home: webHome}
	t.Cleanup(func() {
		c := client.For(webHome)
		if _, err := c.Ping(); err == nil {
			_ = c.Shutdown(true)
		}
		_ = os.RemoveAll(run)
	})
	return env
}

func (e *webEnv) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(e.bin, args...)
	cmd.Dir = e.cwd
	cmd.Env = os.Environ()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("bonsai %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

func (e *webEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := e.run(t, args...)
	if code != 0 {
		t.Fatalf("bonsai %v exited %d\nstdout:\n%s\nstderr:\n%s", args, code, out, errOut)
	}
	return out
}

// webRecordsSettled reports whether no process the web daemon ever started
// is still alive.
func webRecordsSettled(t *testing.T, home string) bool {
	t.Helper()
	records, err := procstore.New(home).ListRecords()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		if !procstore.IsTerminal(r.Status) || procstore.ProcessMatches(r.PID, r.StartedAt, r.Worktree) {
			return false
		}
	}
	return true
}

func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

func TestWebStartReuseStatusLogsRestartStop(t *testing.T) {
	e := newWebEnv(t)
	port := strconv.Itoa(freePort(t))

	out := e.mustRun(t, "web", "--no-open", "--port", port)
	for _, want := range []string{"Saved default settings", "✓ bonsai web is running", "UI        https://app.bonsai.dev/app", "API       http://127.0.0.1:" + port, "updates   standard", "bonsai web stop"} {
		if !strings.Contains(out, want) {
			t.Fatalf("first start output lacks %q:\n%s", want, out)
		}
	}
	if !portListening(t, port) {
		t.Fatal("API is not listening after bonsai web returned")
	}

	out = e.mustRun(t, "web", "--no-open", "--port", port)
	if !strings.Contains(out, "✓ bonsai web is already running") || strings.Contains(out, "Saved default settings") {
		t.Fatalf("second start did not reuse:\n%s", out)
	}

	other := strconv.Itoa(freePort(t))
	_, errOut, code := e.run(t, "web", "--no-open", "--port", other)
	if code != 1 || !strings.Contains(errOut, "already running on port "+port) {
		t.Fatalf("different port: code %d\n%s", code, errOut)
	}

	status := e.mustRun(t, "web", "status")
	if !strings.Contains(status, "bonsai web · ready") || !strings.Contains(status, "api") || !strings.Contains(status, "127.0.0.1:"+port) {
		t.Fatalf("status:\n%s", status)
	}

	logs := e.mustRun(t, "web", "logs", "api", "-n", "20")
	if !strings.Contains(logs, "Bonsai local API listening at http://127.0.0.1:"+port) {
		t.Fatalf("logs:\n%s", logs)
	}
	if _, errOut, code := e.run(t, "web", "logs", "tunnel"); code == 0 || !strings.Contains(errOut, "not available") {
		t.Fatalf("tunnel logs: code %d %s", code, errOut)
	}

	if out := e.mustRun(t, "web", "restart", "api"); !strings.Contains(out, "✓ restarted") || !strings.Contains(out, "bonsai web · ready") {
		t.Fatalf("restart:\n%s", out)
	}
	if !portListening(t, port) {
		t.Fatal("API is not listening after restart")
	}

	if out := e.mustRun(t, "web", "stop"); !strings.Contains(out, "✓ bonsai web stopped") {
		t.Fatalf("stop:\n%s", out)
	}
	if !waitUntil(5*time.Second, func() bool { return !portListening(t, port) && webRecordsSettled(t, e.home) }) {
		t.Fatal("processes survived bonsai web stop")
	}
	if out := e.mustRun(t, "web", "status"); !strings.Contains(out, "not running") {
		t.Fatalf("status after stop:\n%s", out)
	}
	if out := e.mustRun(t, "web", "stop"); !strings.Contains(out, "not running") {
		t.Fatalf("second stop:\n%s", out)
	}
	if logs := e.mustRun(t, "web", "logs"); !strings.Contains(logs, "listening") {
		t.Fatalf("logs after stop do not show the last run:\n%s", logs)
	}
}

func TestWebPortConflictLeavesNothingRunning(t *testing.T) {
	e := newWebEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	out, errOut, code := e.run(t, "web", "--no-open", "--port", port)
	if code != 1 {
		t.Fatalf("exit code %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{"✗ bonsai web could not start", "api   port " + port + " is used by another program", "fix   bonsai web --port"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("diagnosis lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(errOut, "bonsai:") {
		t.Fatalf("failure printed a second generic error line:\n%s", errOut)
	}
	if runtime.GOOS == "linux" && !strings.Contains(errOut, "(pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("diagnosis does not name the owning pid %d:\n%s", os.Getpid(), errOut)
	}
	if status := e.mustRun(t, "web", "status"); !strings.Contains(status, "not running") {
		t.Fatalf("a half-started group survived:\n%s", status)
	}
	if !webRecordsSettled(t, e.home) {
		t.Fatal("orphan process left behind")
	}
}

func TestWebAPICrashIsReportedAndCleanedUp(t *testing.T) {
	e := newWebEnv(t)
	roots, err := config.ProjectRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(roots), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roots, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(freePort(t))
	_, errOut, code := e.run(t, "web", "--no-open", "--port", port)
	if code != 1 || !strings.Contains(errOut, "api   exited with code 1 — read project roots") || !strings.Contains(errOut, "logs  bonsai web logs api") {
		t.Fatalf("code %d\n%s", code, errOut)
	}
	if !waitUntil(5*time.Second, func() bool { return webRecordsSettled(t, e.home) }) {
		t.Fatal("crashed API left a live process")
	}
	if logs := e.mustRun(t, "web", "logs", "api"); !strings.Contains(logs, "read project roots") {
		t.Fatalf("logs hint does not show the failure:\n%s", logs)
	}
}

func TestServeAliasReplacesPerRepoAPI(t *testing.T) {
	e := newWebEnv(t)
	repo, err := filepath.EvalSymlinks(initRepo(t)) // daemons key by the canonical root
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)

	// A per-repo production group, as started by bonsai serve before
	// bonsai web existed, holds the port.
	legacy := client.For(repo)
	t.Cleanup(func() { _ = legacy.Shutdown(true) })
	if _, err := legacy.ServeStart(procstore.ServeSpec{
		Mode:          procstore.ServeModeProduction,
		WorkspaceID:   serveWorkspaceID(repo),
		WorkspacePath: repo,
		Executable:    e.bin,
		APIPort:       port,
		BrowserOrigin: hostedWebOrigin(),
	}); err != nil {
		t.Fatal(err)
	}

	e.cwd = repo
	out, errOut, code := e.run(t, "serve", "-d", "--api-port", strconv.Itoa(port))
	if code != 0 {
		t.Fatalf("serve -d exited %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "bonsai serve is deprecated: use bonsai web") {
		t.Fatalf("no deprecation notice:\n%s", errOut)
	}
	if !strings.Contains(out, "Stopped the per-repo local API of "+repo) || !strings.Contains(out, "✓ bonsai web is running") {
		t.Fatalf("legacy group not replaced:\n%s", out)
	}
	if group, err := legacy.ServeStatus(serveWorkspaceID(repo)); err != nil || group != nil {
		t.Fatalf("legacy group still present: %+v, %v", group, err)
	}
	if status := e.mustRun(t, "serve", "status"); !strings.Contains(status, "bonsai web · ready") {
		t.Fatalf("serve status:\n%s", status)
	}
	if out := e.mustRun(t, "serve", "stop"); !strings.Contains(out, "✓ bonsai web stopped") {
		t.Fatalf("serve stop:\n%s", out)
	}
}

func portListening(t *testing.T, port string) bool {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
