package claude

import (
	"errors"
	"slices"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestReadOnlyLaunchRestrictsToolsAndIgnoresProfile(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("planner")
	// A profile that defaults to bypassPermissions must not widen a read-only run.
	account.Settings = []byte(`{"permission_mode":"bypassPermissions","model":"opus"}`)
	prepared, _ := e.prepare(account, agents.LaunchOptions{ReadOnly: true, Prompt: "plan it"})
	args := prepared.Args
	for _, want := range [][]string{{"--restricted"}, {"--tools", "Read,Grep,Glob"}, {"--strict-mcp-config"}, {"--permission-mode", "plan"}} {
		if !containsRun(args, want) {
			t.Errorf("args %v lack %v", args, want)
		}
	}
	for _, bad := range []string{"bypassPermissions", "--dangerously-skip-permissions", "--allowedTools", "--allow-dangerously-skip-permissions"} {
		if slices.Contains(args, bad) {
			t.Errorf("args %v carry %s", args, bad)
		}
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "plan it" {
		t.Errorf("prompt must come last after --: %v", args)
	}
}

func TestReadOnlyLaunchRefusesWideningInputs(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("planner")
	if err := e.provider.ValidateLaunch(account, agents.LaunchOptions{ReadOnly: true, PermissionMode: "acceptEdits"}); err == nil {
		t.Fatal("a read-only launch accepted a permission mode")
	}
	session, err := e.sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	_, err = e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: session, Args: []string{"--allowedTools", "Bash"}, Launch: agents.LaunchOptions{ReadOnly: true}})
	if !errors.Is(err, agents.ErrInvalidLaunch) {
		t.Fatalf("explicit args on a read-only launch: %v", err)
	}
}

func TestReadOnlyVerdict(t *testing.T) {
	e := newTestEnv(t)
	if s := agents.ReadOnlyOf(e.provider); !s.Supported || s.Mechanism == "" || len(s.Allows) == 0 {
		t.Fatalf("verdict = %+v", s)
	}
	if err := agents.RequireReadOnly(e.provider); err != nil {
		t.Fatal(err)
	}
}

func containsRun(args, run []string) bool {
	for i := 0; i+len(run) <= len(args); i++ {
		if slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}
