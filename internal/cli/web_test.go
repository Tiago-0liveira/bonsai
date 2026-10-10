package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/portowner"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

func TestWebFailureBlockFormat(t *testing.T) {
	var errOut bytes.Buffer
	w := &webCLI{errOut: &errOut}
	err := w.fail(webFailure{
		process: "api",
		detail:  "exited with code 1 — port 7001 is used by another program (pid 4242, node)",
		fix:     "bonsai web --port 7011",
		logs:    true,
	})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("fail returned %v, want ExitError{1}", err)
	}
	want := "✗ bonsai web could not start\n" +
		"  api   exited with code 1 — port 7001 is used by another program (pid 4242, node)\n" +
		"  fix   bonsai web --port 7011\n" +
		"  logs  bonsai web logs api\n"
	if errOut.String() != want {
		t.Fatalf("failure block:\n%s\nwant:\n%s", errOut.String(), want)
	}
}

func TestPortFailureNamesTheOwner(t *testing.T) {
	foreign := portFailure(7001, portUse{owner: portowner.Owner{PID: 4242, Name: "node"}, known: true})
	if foreign.process != "api" || foreign.detail != "port 7001 is used by another program (pid 4242, node)" ||
		!strings.HasPrefix(foreign.fix, "bonsai web --port 7011") {
		t.Fatalf("foreign = %+v", foreign)
	}
	if hidden := portFailure(7001, portUse{}); hidden.detail != "port 7001 is used by another program" {
		t.Fatalf("hidden owner = %+v", hidden)
	}
	if b := portFailure(7001, portUse{bonsai: true, owner: portowner.Owner{PID: 7}, known: true}); b.detail != "port 7001 is used by another bonsai local API (pid 7)" {
		t.Fatalf("bonsai = %+v", b)
	}
	dev := portFailure(7001, portUse{legacy: &legacyServeGroup{workspace: "/src/bonsai", mode: procstore.ServeModeDevelopment}})
	if !strings.Contains(dev.detail, "development stack of /src/bonsai") || !strings.Contains(dev.fix, "__serve-dev-stack stop") {
		t.Fatalf("dev stack = %+v", dev)
	}
	for _, f := range []webFailure{foreign, dev} {
		if strings.Contains(f.fix, "--force") {
			t.Fatalf("fix suggests a destructive command: %q", f.fix)
		}
	}
}

func TestInspectPortClassifiesListeners(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty daemon index
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	foreign := describePortOwner(port, inspectPort(port, t.TempDir()))
	if foreign.free || foreign.bonsai || foreign.legacy != nil {
		t.Fatalf("raw listener = %+v", foreign)
	}

	// Something answering bonsai's public /version probe.
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"dev","api_version":3}`)
			return
		}
		http.NotFound(w, r)
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(ln) }()
	defer server.Close()
	if use := describePortOwner(port, inspectPort(port, t.TempDir())); !use.bonsai {
		t.Fatalf("bonsai API not recognised: %+v", use)
	}
	server.Close()
	if !waitPortFree(port, 2*time.Second) {
		t.Fatal("port stayed busy after close")
	}
	if use := inspectPort(port, t.TempDir()); !use.free {
		t.Fatalf("closed port = %+v", use)
	}
}

func TestStartFailureFromDaemonError(t *testing.T) {
	w := &webCLI{home: "/state/bonsai/web"}
	crash := errors.New("api: failed (exit 1): exit status 1\n" +
		procstore.Marker{Kind: procstore.MarkerExit, Code: 1, Text: "exited 1 · failed"}.Encode() +
		"bonsai: read project roots /x/project-roots.json: unexpected end of JSON input\n" +
		procstore.Marker{Kind: procstore.MarkerExit, Code: 1, Text: "exited 1 · failed · retries exhausted"}.Encode())
	f := w.startFailure(freePort(t), crash)
	if f.process != "api" || f.detail != "exited with code 1 — read project roots /x/project-roots.json: unexpected end of JSON input" || !f.logs {
		t.Fatalf("crash = %+v", f)
	}
	timeout := w.startFailure(17001, errors.New("api: readiness timed out after 30s"))
	if timeout.detail != "did not start listening on 127.0.0.1:17001 in time" {
		t.Fatalf("timeout = %+v", timeout)
	}
	daemon := w.startFailure(17001, errors.New("daemon did not start in time"))
	if daemon.process != "daemon" || !strings.Contains(daemon.fix, "/state/bonsai/web") {
		t.Fatalf("daemon = %+v", daemon)
	}
}

func TestWebProcessNamesAndInterval(t *testing.T) {
	for _, name := range []string{"", "api", "tunnel"} {
		if got, err := webProcessName(name); err != nil || got != name {
			t.Fatalf("webProcessName(%q) = %q, %v", name, got, err)
		}
	}
	if _, err := webProcessName("web"); err == nil {
		t.Fatal("unknown process accepted")
	}
	if got := humanInterval(2 * time.Minute); got != "2 min" {
		t.Fatalf("humanInterval(2m) = %q", got)
	}
	if got := humanInterval(30 * time.Second); got != "30 s" {
		t.Fatalf("humanInterval(30s) = %q", got)
	}
}

func TestWebRejectsBadArguments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"--port", "0"}, {"--port", "70000"}, {"extra"}, {"status", "x"}, {"restart", "web"}, {"logs", "api", "extra"}} {
		if err := cmdWeb(args, strings.NewReader(""), &out, &errOut); err == nil {
			t.Fatalf("bonsai web %v accepted", args)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestWantsSetup(t *testing.T) {
	current := config.WebSetupVersion
	cases := []struct {
		tty, noSetup, exists bool
		version              int
		want                 bool
	}{
		{true, false, false, 0, true},           // first run in a terminal
		{true, false, true, 0, true},            // defaults written by an older bonsai web
		{true, false, true, current, false},     // already set up
		{true, true, false, 0, false},           // --no-setup
		{false, false, false, 0, false},         // script or pipe
		{false, false, true, 0, false},          // script, older settings
		{true, false, true, current + 1, false}, // settings from a newer bonsai
	}
	for _, tc := range cases {
		if got := wantsSetup(tc.tty, tc.noSetup, tc.exists, tc.version); got != tc.want {
			t.Errorf("wantsSetup(tty=%v, noSetup=%v, exists=%v, version=%d) = %v", tc.tty, tc.noSetup, tc.exists, tc.version, got)
		}
	}
}

func TestSetupWithoutATerminalPrintsTheSettings(t *testing.T) {
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "web.json")
	w := &webCLI{out: &out, errOut: &out, configPath: path}
	if err := w.setup(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Saved default settings to " + path, `"setup_version": 0`, "Run bonsai web setup in a terminal for the guided setup"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, out.String())
		}
	}
	if _, exists, err := config.ReadWebConfig(path); err != nil || !exists {
		t.Fatalf("defaults not written: %v", err)
	}
}

func TestPrintDoctor(t *testing.T) {
	healthy := []checks.Check{
		{ID: checks.IDGit, Title: "git", State: checks.OK, Detail: "2.43.0"},
		{ID: checks.IDTunnelNgrok, Title: "ngrok", State: checks.Skip, Detail: "not installed", Fix: &checks.Fix{Command: "brew install ngrok"}},
	}
	var out bytes.Buffer
	w := &webCLI{out: &out}
	if err := w.printDoctor(healthy); err != nil {
		t.Fatalf("healthy doctor: %v", err)
	}
	want := "bonsai web doctor\n" +
		"  ✓ git    2.43.0\n" +
		"  – ngrok  not installed\n" +
		"\n✓ Everything bonsai web needs is in place.\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	warned := append(healthy, checks.Check{ID: checks.IDGHAuth, Title: "gh logged in", State: checks.Warn, Detail: "not logged in to github.com", Fix: &checks.Fix{Command: "gh auth login --hostname github.com", Inline: true}})
	if err := w.printDoctor(warned); err != nil {
		t.Fatalf("warnings must not fail doctor: %v", err)
	}
	if !strings.Contains(out.String(), "               fix  gh auth login --hostname github.com\n") || !strings.HasSuffix(out.String(), "! 1 warning\n") {
		t.Fatalf("warning output:\n%s", out.String())
	}

	out.Reset()
	failed := append(warned, checks.Check{ID: checks.IDPort, Title: "port 7001", State: checks.Fail, Detail: "port 7001 is used by another program (pid 4242, node)", Fix: &checks.Fix{Command: "bonsai web --port 7011"}})
	err := w.printDoctor(failed)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("failure must exit 1, got %v", err)
	}
	if !strings.Contains(out.String(), "  ✗ port 7001     port 7001 is used by another program (pid 4242, node)\n") || !strings.HasSuffix(out.String(), "✗ 1 problem, 1 warning\n") {
		t.Fatalf("failure output:\n%s", out.String())
	}
}
