package agentruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// RunHook finds the sessions root and the providers on its own, the way the
// hook command does in a bare process.
func TestRunHookAppendsClaudeSignal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	root, err := agents.DefaultSessionsRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "sess_a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dir, agents.ActivitySpoolName)
	if err := os.WriteFile(spool, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agents.ActivityEnv, spool)

	var out bytes.Buffer
	if code := RunHook([]string{"claude", "PermissionRequest"}, strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`), &out); code != 0 || out.Len() != 0 {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
	var got []agentactivity.Signal
	stop, err := agentactivity.Tail(t.Context(), spool, func(s agentactivity.Signal) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if len(got) != 1 || got[0].Kind != agentactivity.PermissionRequested || got[0].Detail != "Bash" || got[0].Source != agentactivity.SourceHook || got[0].At.IsZero() {
		t.Fatalf("signals %+v", got)
	}

	// An unknown provider does nothing, successfully.
	if code := RunHook([]string{"nope", "Stop"}, strings.NewReader(`{}`), &out); code != 0 || out.Len() != 0 {
		t.Errorf("nope: code = %d out=%q", code, out.String())
	}
	if data, _ := os.ReadFile(spool); bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("spool changed: %q", data)
	}

	// Codex is registered like Claude: its Stop hook ends the turn.
	if code := RunHook([]string{"codex", "Stop"}, strings.NewReader(`{"last_assistant_message":"SECRET"}`), &out); code != 0 || out.Len() != 0 {
		t.Errorf("codex: code = %d out=%q", code, out.String())
	}
	data, _ := os.ReadFile(spool)
	if bytes.Count(data, []byte("\n")) != 2 || !bytes.Contains(data, []byte(`"kind":"turn_completed"`)) || bytes.Contains(data, []byte("SECRET")) {
		t.Fatalf("spool after codex Stop: %q", data)
	}
}

// agy runs with HOME set to the session home, so the hook computes another
// default sessions root there. The root named on the command line is what lets
// it find its spool, and only that exact layout.
func TestRunHookAntigravityUnderOverriddenHome(t *testing.T) {
	sessionHome := t.TempDir()
	t.Setenv("HOME", sessionHome)
	t.Setenv("XDG_CACHE_HOME", "")
	if err := os.Unsetenv("XDG_CACHE_HOME"); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "sessions")
	dir := filepath.Join(root, "sess_a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(dir, agents.ActivitySpoolName)
	if err := os.WriteFile(spool, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agents.ActivityEnv, spool)
	payload := `{"toolCall":{"name":"run_command","args":{"CommandLine":"SECRET"}}}`

	var out bytes.Buffer
	if code := RunHook([]string{"antigravity", "PostToolUse"}, strings.NewReader(payload), &out); code != 0 || out.String() != "{}" {
		t.Fatalf("without the root: code=%d out=%q", code, out.String())
	}
	if data, _ := os.ReadFile(spool); len(data) != 0 {
		t.Fatalf("the hook trusted the default root of the overridden HOME: %q", data)
	}

	out.Reset()
	if code := RunHook([]string{"antigravity", "PostToolUse", "--sessions-root=" + root}, strings.NewReader(payload), &out); code != 0 || out.String() != "{}" {
		t.Fatalf("with the root: code=%d out=%q", code, out.String())
	}
	var got []agentactivity.Signal
	stop, err := agentactivity.Tail(t.Context(), spool, func(s agentactivity.Signal) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if len(got) != 1 || got[0].Kind != agentactivity.ToolFinished || got[0].Detail != "run_command" || got[0].Source != agentactivity.SourceHook {
		t.Fatalf("signals %+v", got)
	}

	// The root does not widen what a spool may be: another directory is rejected.
	other := filepath.Join(t.TempDir(), "elsewhere", agents.ActivitySpoolName)
	if err := os.MkdirAll(filepath.Dir(other), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agents.ActivityEnv, other)
	RunHook([]string{"antigravity", "PostToolUse", "--sessions-root=" + root}, strings.NewReader(payload), &out)
	if data, _ := os.ReadFile(other); len(data) != 0 {
		t.Fatalf("a spool outside the root was written: %q", data)
	}
}
