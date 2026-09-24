package exec

import (
	"os"
	"runtime"
	"testing"
	"time"
)

var (
	testSleepCmd = "sleep 30"
	testTrueCmd  = "true"
	testFalseCmd = "false"
	testURLCmd   = "printf 'ready at http://localhost:4321\\n'"
)

func init() {
	if runtime.GOOS == "windows" && os.Getenv("SHELL") == "" {
		testSleepCmd = "ping -n 31 127.0.0.1 >nul"
		testTrueCmd = "exit /b 0"
		testFalseCmd = "exit /b 1"
		testURLCmd = "echo ready at http://localhost:4321"
	}
}

// waitDone polls until p reports Done or the deadline passes.
func waitDone(p *Process, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if p.Done() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return p.Done()
}

func TestKillByIDLeavesSiblings(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	long, err := m.Spawn(dir, "long", testSleepCmd)
	if err != nil {
		t.Fatal(err)
	}
	short, err := m.Spawn(dir, "short", testTrueCmd)
	if err != nil {
		t.Fatal(err)
	}

	m.KillByID(dir, long.ID)
	if !waitDone(long, time.Second) {
		t.Fatal("killed process still running")
	}
	// The sibling must remain listed.
	if _, ok := m.GetByID(dir, short.ID); !ok {
		t.Fatal("sibling process was removed")
	}
	if len(m.List(dir)) != 2 {
		t.Fatalf("List len = %d, want 2", len(m.List(dir)))
	}
}

func TestRestartRespawns(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	p, err := m.Spawn(dir, "job", testSleepCmd)
	if err != nil {
		t.Fatal(err)
	}
	np, err := m.Restart(dir, p.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if np.ID == p.ID {
		t.Fatal("restart reused the old ID")
	}
	if np.Label != "job" || np.Command != testSleepCmd {
		t.Fatalf("restart lost label/command: %q %q", np.Label, np.Command)
	}
	if !waitDone(p, time.Second) {
		t.Fatal("original process not killed by restart")
	}
	m.KillByID(dir, np.ID) // cleanup
}

func TestRemoveDropsFromList(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	p, err := m.Spawn(dir, "job", testTrueCmd)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(p, time.Second)
	m.Remove(dir, p.ID)
	if _, ok := m.GetByID(dir, p.ID); ok {
		t.Fatal("removed process still present")
	}
	if len(m.List(dir)) != 0 {
		t.Fatalf("List len = %d, want 0", len(m.List(dir)))
	}
}

func TestKilledProcessReportsStopped(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	p, err := m.Spawn(dir, "srv", testSleepCmd)
	if err != nil {
		t.Fatal(err)
	}
	m.KillByID(dir, p.ID)
	if !waitDone(p, time.Second) {
		t.Fatal("killed process still running")
	}
	if got := p.Status(); got != "stopped" {
		t.Errorf("killed status = %q, want stopped", got)
	}

	// A natural non-zero exit still reads as failed.
	bad, err := m.Spawn(dir, "bad", testFalseCmd)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(bad, time.Second)
	if got := bad.Status(); got != "failed" {
		t.Errorf("failed status = %q, want failed", got)
	}
}

func TestEditorResolution(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	want := "vi"
	if runtime.GOOS == "windows" {
		want = "notepad"
	}
	if name, args := editor(""); name != want || args != nil {
		t.Errorf("fallback = %q %v, want %s", name, args, want)
	}

	t.Setenv("EDITOR", "nano")
	if name, args := editor(""); name != "nano" || len(args) != 0 {
		t.Errorf("$EDITOR only = %q %v, want nano", name, args)
	}

	t.Setenv("EDITOR", "vim -u NONE")
	if name, args := editor(""); name != "vim" || len(args) != 2 || args[0] != "-u" || args[1] != "NONE" {
		t.Errorf("$EDITOR with args = %q %v", name, args)
	}

	// $VISUAL wins over $EDITOR.
	t.Setenv("VISUAL", "code -w")
	if name, args := editor(""); name != "code" || len(args) != 1 || args[0] != "-w" {
		t.Errorf("$VISUAL priority = %q %v, want code -w", name, args)
	}

	// An explicit override wins over $VISUAL/$EDITOR.
	if name, args := editor("hx"); name != "hx" || len(args) != 0 {
		t.Errorf("override = %q %v, want hx", name, args)
	}
}

func TestEditorCmdRootedAtDir(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano")
	dir := t.TempDir()
	cmd := EditorCmd(dir, "")
	if cmd.Dir != dir {
		t.Errorf("EditorCmd dir = %q, want %q", cmd.Dir, dir)
	}
	if got := cmd.Args[0]; got != "nano" {
		t.Errorf("EditorCmd argv0 = %q, want nano", got)
	}

	cmd = EditorCmd(dir, "hx")
	if got := cmd.Args[0]; got != "hx" {
		t.Errorf("EditorCmd override argv0 = %q, want hx", got)
	}
}

func TestLastLocalURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"none", "no urls here", ""},
		{"vite banner", "  ➜  Local:   http://localhost:5173/\n", "http://localhost:5173/"},
		{"ipv4", "listening on http://127.0.0.1:3000", "http://127.0.0.1:3000"},
		{"wildcard", "http://0.0.0.0:8080/x", "http://0.0.0.0:8080/x"},
		{"ipv6", "http://[::1]:4000", "http://[::1]:4000"},
		{"last wins", "http://localhost:1 then http://localhost:2", "http://localhost:2"},
		{"ansi wrapped", "\x1b[32m➜ http://localhost:9999\x1b[0m", "http://localhost:9999"},
		{"trailing punctuation", "served at http://localhost:3000.", "http://localhost:3000"},
		{"ignores remote", "see https://example.com/page", ""},
		{"ignores lookalike host", "http://localhost.evil.com/x", ""},
		{"https local", "https://localhost:8443", "https://localhost:8443"},
	}
	for _, c := range cases {
		if got := LastLocalURL(c.in); got != c.want {
			t.Errorf("%s: LastLocalURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestProcessLastURL(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()
	p, err := m.Spawn(dir, "srv", testURLCmd)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(p, time.Second)
	if got := p.LastURL(); got != "http://localhost:4321" {
		t.Errorf("LastURL = %q", got)
	}
}
