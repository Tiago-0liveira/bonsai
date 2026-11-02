package claude

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const resumeID = "123e4567-e89b-42d3-a456-426614174000"

func TestResumeArgs(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	prepare := func(launch agents.LaunchOptions) agents.PreparedSession {
		session, err := e.sessions.Create(account, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
		prepared, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{
			Account: account, Session: session, Launch: launch, Resume: &agents.ResumeTarget{ID: resumeID},
		})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	pair := func(args []string, flag, value string) bool {
		i := slices.Index(args, flag)
		return i >= 0 && i+1 < len(args) && args[i+1] == value
	}

	got := prepare(agents.LaunchOptions{Model: "opus", Effort: "high", PermissionMode: "acceptEdits", DisplayName: "n", Prompt: "do it"})
	for flag, value := range map[string]string{"--resume": resumeID, "--model": "opus", "--effort": "high", "--permission-mode": "acceptEdits", "--name": "n"} {
		if !pair(got.Args, flag, value) {
			t.Errorf("args %v lack %s %s", got.Args, flag, value)
		}
	}
	if slices.Contains(got.Args, "--session-id") || slices.Contains(got.Args, "--") || slices.Contains(got.Args, "do it") {
		t.Errorf("resume must not carry --session-id or a prompt: %v", got.Args)
	}
	if got.ProviderSessionID != resumeID {
		t.Errorf("ProviderSessionID = %q", got.ProviderSessionID)
	}

	ro := prepare(agents.LaunchOptions{ReadOnly: true, Prompt: "x"})
	if !pair(ro.Args, "--resume", resumeID) || !slices.Contains(ro.Args, "--restricted") || !pair(ro.Args, "--tools", "Read,Grep,Glob") {
		t.Errorf("read-only resume lost restrictions: %v", ro.Args)
	}
}

func TestResumeTarget(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	dir := filepath.Join(configDir(e.accounts, account), "projects", "-tmp-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, resumeID+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := "223e4567-e89b-42d3-a456-426614174000"
	cases := []struct {
		name, id, reason string
	}{
		{"present", resumeID, ""},
		{"absent", other, agents.ReasonNoConversation},
		{"empty", "", agents.ReasonNotFound},
		{"traversal", "../x", agents.ReasonNotFound},
		{"glob", "*", agents.ReasonNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, err := e.provider.ResumeTarget(t.Context(), agents.ResumeRequest{Account: account, ProviderSessionID: tc.id})
			if tc.reason == "" {
				if err != nil || target.ID != tc.id {
					t.Fatalf("got %+v, %v", target, err)
				}
				return
			}
			var nr *agents.NotResumableError
			if !errors.Is(err, agents.ErrNotResumable) || !errors.As(err, &nr) || nr.Reason != tc.reason {
				t.Fatalf("err = %v, want reason %q", err, tc.reason)
			}
		})
	}
}
