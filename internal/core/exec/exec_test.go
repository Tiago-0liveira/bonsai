package exec

import (
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	// Run before TempDir cleanup, including when a spawn or assertion fails.
	t.Cleanup(func() {
		m.KillAll()
		for _, p := range m.All() {
			if !waitDone(p, 10*time.Second) {
				t.Errorf("process %q still running during cleanup", p.Label)
			}
		}
	})

	long, err := m.Spawn(dir, "long", testSleepCmd)
	if err != nil {
		t.Fatal(err)
	}
	short, err := m.Spawn(dir, "short", testTrueCmd)
	if err != nil {
		t.Fatal(err)
	}

	m.KillByID(dir, long.ID)
	if !waitDone(long, 10*time.Second) {
		t.Fatal("killed process still running")
	}
	// The sibling must remain listed.
	if _, ok := m.GetByID(dir, short.ID); !ok {
		t.Fatal("sibling process was removed")
	}
	if len(m.List(dir)) != 2 {
		t.Fatalf("List len = %d, want 2", len(m.List(dir)))
	}

	// The sibling command is intentionally short-lived. Wait for cmd.Wait to
	// finish before TempDir cleanup: on Windows a still-exiting cmd.exe can keep
	// its working directory open long enough for RemoveAll to fail.
	if !waitDone(short, 10*time.Second) {
		short.Kill()
		if !waitDone(short, 10*time.Second) {
			t.Fatal("short sibling did not exit")
		}
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

func TestExecCommandPreservesArgvAndDir(t *testing.T) {
	dir := t.TempDir()
	args := []string{"value with spaces", "semi;colon", "quote\"value", "$HOME", "&&"}
	cmd := ExecCommand(dir, "tool", args...)
	if cmd.Dir != dir {
		t.Fatalf("ExecCommand dir = %q, want %q", cmd.Dir, dir)
	}
	want := append([]string{"tool"}, args...)
	if len(cmd.Args) != len(want) {
		t.Fatalf("ExecCommand argv = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("ExecCommand argv[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}
}

func TestShellCmdEnv(t *testing.T) {
	cache := t.TempDir()
	prev := userCacheDir
	userCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { userCacheDir = prev })
	t.Setenv("BONSAI_DEPTH", "2")
	t.Setenv("BONSAI_PROMPT_PREFIX", "stale")
	cmd := ShellCmd(t.TempDir(), ShellOptions{Glyph: "B"})
	var depth, prefix []string
	for _, kv := range cmd.Env {
		switch {
		case strings.HasPrefix(kv, "BONSAI_DEPTH="):
			depth = append(depth, kv)
		case strings.HasPrefix(kv, "BONSAI_PROMPT_PREFIX="):
			prefix = append(prefix, kv)
		}
	}
	if len(depth) != 1 || depth[0] != "BONSAI_DEPTH=3" {
		t.Errorf("depth env = %v", depth)
	}
	if len(prefix) != 1 || prefix[0] != "BONSAI_PROMPT_PREFIX=B3 " {
		t.Errorf("prefix env = %v", prefix)
	}
	if got := os.Getenv("BONSAI_DEPTH"); got != "2" {
		t.Errorf("own env mutated: %q", got)
	}
}

func TestShellCmdPrompt(t *testing.T) {
	cache := t.TempDir()
	prev := userCacheDir
	userCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { userCacheDir = prev })
	t.Setenv("SHELL", "/bin/bash")

	auto := ShellCmd(t.TempDir(), ShellOptions{})
	if len(auto.Args) != 3 || auto.Args[1] != "--rcfile" {
		t.Errorf("auto args = %v", auto.Args)
	}
	off := ShellCmd(t.TempDir(), ShellOptions{Prompt: "off"})
	if len(off.Args) != 1 {
		t.Errorf("off args = %v", off.Args)
	}

	// Cache failure must still give a usable plain shell.
	userCacheDir = func() (string, error) { return "", os.ErrNotExist }
	bad := ShellCmd(t.TempDir(), ShellOptions{})
	if len(bad.Args) != 1 || len(bad.Env) == 0 {
		t.Errorf("fallback args=%v env=%d", bad.Args, len(bad.Env))
	}
}

func TestShellCmdMemoizesShims(t *testing.T) {
	cache := t.TempDir()
	prev := userCacheDir
	userCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { userCacheDir = prev })
	t.Setenv("SHELL", "/bin/bash")

	first := ShellCmd(t.TempDir(), ShellOptions{})
	if len(first.Args) != 3 {
		t.Fatalf("args = %v", first.Args)
	}
	rc := first.Args[2]
	if err := os.Remove(rc); err != nil {
		t.Fatal(err)
	}
	ShellCmd(t.TempDir(), ShellOptions{})
	if _, err := os.Stat(rc); !os.IsNotExist(err) {
		t.Errorf("shims rewritten for the same cache dir: %v", err)
	}

	// A stale mtime is refreshed on a memo hit so pruning never takes a live dir.
	dir := filepath.Dir(rc)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	ShellCmd(t.TempDir(), ShellOptions{})
	if fi, err := os.Stat(dir); err != nil || time.Since(fi.ModTime()) > time.Hour {
		t.Errorf("shim dir mtime not refreshed: %v %v", fi, err)
	}

	other := t.TempDir()
	userCacheDir = func() (string, error) { return other, nil }
	second := ShellCmd(t.TempDir(), ShellOptions{})
	if len(second.Args) != 3 {
		t.Fatalf("args = %v", second.Args)
	}
	if _, err := os.Stat(second.Args[2]); err != nil {
		t.Errorf("new cache dir not set up: %v", err)
	}
}

func TestShellExitErr(t *testing.T) {
	plain := errors.New("start failed")
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"plain error unchanged", plain, plain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShellExitErr(tc.in); got != tc.want {
				t.Fatalf("ShellExitErr(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	wrapped := fmt.Errorf("launch: %w", plain)
	if got := ShellExitErr(wrapped); got != wrapped {
		t.Fatalf("wrapped non-exit error changed: %v", got)
	}

	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	err := osexec.Command("sh", "-c", "exit 3").Run()
	var ee *osexec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("setup: want ExitError, got %v", err)
	}
	if got := ShellExitErr(err); got != nil {
		t.Fatalf("ShellExitErr(exit 3) = %v, want nil", got)
	}
	if got := ShellExitErr(fmt.Errorf("shell: %w", err)); got != nil {
		t.Fatalf("wrapped exit error = %v, want nil", got)
	}
}
