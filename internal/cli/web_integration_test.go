package cli

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
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
		shutdownWebTestDaemon(t, client.ForUserHome(webHome))
		_ = os.RemoveAll(run)
	})
	return env
}

func shutdownWebTestDaemon(t *testing.T, c *client.Client) {
	t.Helper()
	if err := c.Shutdown(true); err != nil {
		t.Errorf("shutting down test daemon: %v", err)
	}
	// Shutdown returns when the socket closes, before the daemon finishes
	// updating the global index. Its lock is released only after that cleanup,
	// so wait for it before TempDir removes the isolated config directory.
	if !waitUntil(5*time.Second, func() bool {
		lock, err := procstore.TryLock(c.Store().LockPath())
		if os.IsNotExist(err) {
			return true // this test never started the daemon
		}
		if err != nil {
			return false
		}
		_ = lock.Unlock()
		return true
	}) {
		t.Errorf("test daemon cleanup did not finish: %s", c.Store().Root())
	}
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
	for _, want := range []string{"Saved default settings", "✓ bonsai web is running", "UI        http://127.0.0.1:" + port + "/app", "hosted    https://app.bonsai.dev/app", "updates   standard", "bonsai web stop"} {
		if !strings.Contains(out, want) {
			t.Fatalf("first start output lacks %q:\n%s", want, out)
		}
	}
	if !portListening(t, port) {
		t.Fatal("API is not listening after bonsai web returned")
	}
	// The API serves the UI on both loopback names. This test binary is built
	// without -tags embedui, so /app is the placeholder page.
	for _, host := range []string{"127.0.0.1", "localhost"} {
		code, body := httpGet(t, "http://"+host+":"+port+"/app/settings")
		if code != http.StatusOK || !strings.Contains(body, "UI not built") {
			t.Fatalf("GET %s /app/settings = %d\n%s", host, code, body)
		}
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

func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func sessionStatus(t *testing.T, port, origin string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/api/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestWebAppliesTheHostedInterfaceSetting(t *testing.T) {
	e := newWebEnv(t)
	port := strconv.Itoa(freePort(t))
	e.mustRun(t, "web", "--no-open", "--port", port)
	if got := sessionStatus(t, port, "https://app.bonsai.dev"); got != http.StatusCreated {
		t.Fatalf("hosted origin with the interface enabled: %d", got)
	}

	path, err := config.WebConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := config.ReadWebConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.UpdateWebConfig(path, current.Revision, func(c *config.WebConfig) error {
		c.Interfaces.Hosted = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out := e.mustRun(t, "web", "--no-open", "--port", port)
	if !strings.Contains(out, "Restarting bonsai web to apply changed settings") || strings.Contains(out, "hosted    https") {
		t.Fatalf("hosted interface change not applied:\n%s", out)
	}
	if got := sessionStatus(t, port, "https://app.bonsai.dev"); got != http.StatusForbidden {
		t.Fatalf("hosted origin with the interface disabled: %d", got)
	}
	for _, origin := range []string{"http://127.0.0.1:" + port, "http://localhost:" + port} {
		if got := sessionStatus(t, port, origin); got != http.StatusCreated {
			t.Fatalf("own origin %s: %d", origin, got)
		}
	}
	if got := sessionStatus(t, port, "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", got)
	}
	if out := e.mustRun(t, "web", "--no-open", "--port", port); !strings.Contains(out, "already running") {
		t.Fatalf("unchanged settings did not reuse:\n%s", out)
	}
}

// An API left by another bonsai version would serve that version's UI; bonsai
// web replaces it so the browser always gets this binary's UI.
func TestWebReplacesAnAPIFromAnotherVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the bonsai binary")
	}
	// Build before newWebEnv moves HOME (and with it the module cache).
	old := filepath.Join(t.TempDir(), filepath.Base(webTestBinary(t)))
	build := exec.Command("go", "build", "-o", old, "-ldflags", "-X github.com/Tiago-0liveira/bonsai/internal/version.Version=v0.0.1-old", "github.com/Tiago-0liveira/bonsai")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the old binary: %v\n%s", err, out)
	}
	e := newWebEnv(t)
	port := strconv.Itoa(freePort(t))
	oldEnv := *e
	oldEnv.bin = old
	oldEnv.mustRun(t, "web", "--no-open", "--port", port)
	if _, body := httpGet(t, "http://127.0.0.1:"+port+"/version"); !strings.Contains(body, "v0.0.1-old") {
		t.Fatalf("old API version: %s", body)
	}
	out := e.mustRun(t, "web", "--no-open", "--port", port)
	if !strings.Contains(out, "Restarting bonsai web v0.0.1-old to run") || !strings.Contains(out, "✓ bonsai web is running") {
		t.Fatalf("old API was not replaced:\n%s", out)
	}
	if _, body := httpGet(t, "http://127.0.0.1:"+port+"/version"); strings.Contains(body, "v0.0.1-old") {
		t.Fatalf("old API still serving: %s", body)
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
	repo := canonicalRepo(t) // daemons key by the canonical root
	port := freePort(t)

	// A per-repo production group, as started by bonsai serve before
	// bonsai web existed, holds the port.
	legacy := startLegacyServe(t, e, repo, port)

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

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, out)
	}
}

// startLegacyServe starts a per-repo production group the way bonsai serve
// did before bonsai web existed.
func startLegacyServe(t *testing.T, e *webEnv, repo string, port int) *client.Client {
	t.Helper()
	legacy := client.For(repo)
	t.Cleanup(func() { shutdownWebTestDaemon(t, legacy) })
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
	return legacy
}

func canonicalRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(initRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// A dotfiles repository in the home directory must not capture the web
// daemon: its home is used verbatim, never canonicalized to ~.
func TestWebStartsWhenHomeIsAGitRepo(t *testing.T) {
	e := newWebEnv(t)
	gitInit(t, os.Getenv("HOME"))
	port := strconv.Itoa(freePort(t))
	out := e.mustRun(t, "web", "--no-open", "--port", port)
	if !strings.Contains(out, "✓ bonsai web is running") {
		t.Fatalf("start:\n%s", out)
	}
	if _, err := client.ForUserHome(e.home).Ping(); err != nil {
		t.Fatalf("web daemon is not listening for its own home: %v", err)
	}
	if _, err := os.Stat(procstore.New(os.Getenv("HOME")).PidPath()); err == nil {
		t.Fatal("a daemon was started for the enclosing home repository")
	}
	e.mustRun(t, "web", "stop")
}

func TestServeStopAlsoStopsPerRepoAPIOnAnotherPort(t *testing.T) {
	e := newWebEnv(t)
	repo := canonicalRepo(t)
	legacy := startLegacyServe(t, e, repo, freePort(t))
	e.cwd = repo
	out, errOut, code := e.run(t, "serve", "stop")
	if code != 0 || !strings.Contains(out, "✓ stopped the per-repo local API of "+repo) {
		t.Fatalf("serve stop: code %d\n%s\n%s", code, out, errOut)
	}
	if group, err := legacy.ServeStatus(serveWorkspaceID(repo)); err != nil || group != nil {
		t.Fatalf("legacy group survived bonsai serve stop: %+v, %v", group, err)
	}
}

func TestServeAliasYAMLPortNeverBreaksTheSharedStack(t *testing.T) {
	e := newWebEnv(t)
	port := strconv.Itoa(freePort(t))
	e.mustRun(t, "web", "--no-open", "--port", port)

	repo := canonicalRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".bonsai.yaml"), []byte("serve:\n  api_port: "+strconv.Itoa(freePort(t))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.cwd = repo
	out, errOut, code := e.run(t, "serve", "-d")
	if code != 0 || !strings.Contains(out, "already running") || !strings.Contains(errOut, "from .bonsai.yaml is ignored") {
		t.Fatalf("code %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestStaleLegacySpecIsNotBlamedOrStopped(t *testing.T) {
	e := newWebEnv(t)
	repo := canonicalRepo(t)
	livePort := freePort(t)
	legacy := startLegacyServe(t, e, repo, livePort)

	// A foreign program holds another port, and the repo daemon's directory
	// carries a stale spec claiming it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	stale := `{"spec":{"mode":"production","workspace_id":"stale","workspace_path":"/stale","api_port":` + strconv.Itoa(busy) + `}}`
	if err := os.WriteFile(filepath.Join(procstore.New(repo).Dir(), "serve", "stale.json"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errOut, code := e.run(t, "web", "--no-open", "--port", strconv.Itoa(busy))
	if code != 1 || !strings.Contains(errOut, "is used by another program") || strings.Contains(errOut, "per-repo") {
		t.Fatalf("code %d\n%s", code, errOut)
	}
	if group, err := legacy.ServeStatus(serveWorkspaceID(repo)); err != nil || group == nil || group.State != "ready" {
		t.Fatalf("unrelated legacy group was disturbed: %+v, %v", group, err)
	}
}
