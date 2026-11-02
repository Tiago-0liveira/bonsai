package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const testConversationID = "e99ea510-5375-4dca-ac8b-5840331bac29"

type resumeEnv struct {
	provider *Provider
	manager  CredentialManager
	account  agents.Account
	sessions *agents.FileSessionStore
	accounts *agents.FileAccountStore
}

func newResumeEnv(t *testing.T) resumeEnv {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{
		ID: "acct_resume", Provider: ProviderID, Name: "resume",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	p := New(accounts, sessions)
	p.binaryResolver = fixedBinaryResolver("agy-test")
	return resumeEnv{provider: p, manager: p.credentialMgr, account: account, sessions: sessions, accounts: accounts}
}

// seedVault stores a login in the vault through the setup path.
func (e resumeEnv) seedVault(t *testing.T, access, refresh string, expiry time.Time) {
	t.Helper()
	setup, err := e.sessions.Create(e.account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeSessionAuth(t, setup.HomeDir, access, refresh, "user@example.com", expiry)
	if err := e.manager.CaptureSetup(context.Background(), e.account, setup); err != nil {
		t.Fatal(err)
	}
}

func writeConversation(t *testing.T, home, id string) {
	t.Helper()
	dir := filepath.Join(antigravityDir(home), "conversations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResumeArgsAntigravity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target agents.ResumeTarget
		want   []string
	}{
		{"with id", agents.ResumeTarget{ID: testConversationID}, []string{"--conversation", testConversationID}},
		{"by context", agents.ResumeTarget{}, []string{"--continue"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newResumeEnv(t)
			e.seedVault(t, "a", "r", time.Now().Add(time.Hour))
			session, err := e.sessions.Create(e.account, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := e.provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
				Account: e.account, Session: session,
				Launch: agents.LaunchOptions{Prompt: "do not replay"},
				Resume: &tc.target,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(prepared.Args, tc.want) {
				t.Fatalf("args = %v, want %v", prepared.Args, tc.want)
			}
			if prepared.Dir != session.WorkDir {
				t.Fatalf("dir = %q, want %q", prepared.Dir, session.WorkDir)
			}
		})
	}
}

// TestResumeReconcilesNewerTokenFirst: a crashed session's home may hold a
// token agy refreshed after the vault was last written; resume must keep it.
func TestResumeReconcilesNewerTokenFirst(t *testing.T) {
	e := newResumeEnv(t)
	ctx := context.Background()
	base := time.Now().Add(time.Hour).UTC()
	e.seedVault(t, "old-access", "old-refresh", base)

	session, err := e.sessions.Create(e.account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.manager.Materialize(ctx, e.account, session); err != nil {
		t.Fatal(err)
	}
	writeSessionAuth(t, session.HomeDir, "new-access", "new-refresh", "user@example.com", base.Add(time.Hour))

	if _, err := e.provider.PrepareSession(ctx, agents.PrepareSessionRequest{
		Account: e.account, Session: session, Resume: &agents.ResumeTarget{},
	}); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"vault": vaultPath(e.accounts, e.account),
		"home":  oauthPath(session.HomeDir),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(data, "new-access") || contains(data, "old-access") {
			t.Fatalf("%s does not hold the refreshed token", name)
		}
	}
}

func contains(data []byte, s string) bool {
	var out any
	if json.Unmarshal(data, &out) != nil {
		return false
	}
	return stringIn(out, s)
}

func stringIn(v any, s string) bool {
	switch x := v.(type) {
	case string:
		return x == s
	case map[string]any:
		for _, e := range x {
			if stringIn(e, s) {
				return true
			}
		}
	}
	return false
}

func TestResumeWithoutHomeCredentialsStillMaterializes(t *testing.T) {
	e := newResumeEnv(t)
	e.seedVault(t, "a", "r", time.Now().Add(time.Hour))
	session, err := e.sessions.Create(e.account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A retained home has no credential files: nothing to reconcile.
	if _, err := e.provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
		Account: e.account, Session: session, Resume: &agents.ResumeTarget{ID: testConversationID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oauthPath(session.HomeDir)); err != nil {
		t.Fatalf("home was not re-materialized: %v", err)
	}
}

func TestRetainRuntimeScrubsCredentials(t *testing.T) {
	e := newResumeEnv(t)
	ctx := context.Background()
	e.seedVault(t, "a", "r", time.Now().Add(time.Hour))
	session, err := e.sessions.Create(e.account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.manager.Materialize(ctx, e.account, session); err != nil {
		t.Fatal(err)
	}
	writeConversation(t, session.HomeDir, testConversationID)

	kept, err := e.provider.RetainRuntime(ctx, e.account, session)
	if err != nil || !kept {
		t.Fatalf("RetainRuntime = %v, %v", kept, err)
	}
	for _, path := range []string{oauthPath(session.HomeDir), providerSettingsPath(session.HomeDir), generationPath(session)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived: %v", path, err)
		}
	}
	conv := filepath.Join(antigravityDir(session.HomeDir), "conversations", testConversationID+".db")
	if _, err := os.Stat(conv); err != nil {
		t.Fatalf("conversation state removed: %v", err)
	}
	// Idempotent when the files are already gone.
	if kept, err := e.provider.RetainRuntime(ctx, e.account, session); err != nil || !kept {
		t.Fatalf("second RetainRuntime = %v, %v", kept, err)
	}
}

func TestResumeTarget(t *testing.T) {
	e := newResumeEnv(t)
	ctx := context.Background()
	notResumable := func(err error, reason string) {
		t.Helper()
		var nr *agents.NotResumableError
		if !errors.As(err, &nr) || nr.Reason != reason || !errors.Is(err, agents.ErrNotResumable) {
			t.Fatalf("err = %v, want not resumable: %s", err, reason)
		}
	}

	t.Run("no home", func(t *testing.T) {
		_, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{
			Account: e.account, Session: agents.Session{HomeDir: filepath.Join(t.TempDir(), "missing")},
		})
		notResumable(err, agents.ReasonNoConversation)
	})
	t.Run("home without conversation", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(antigravityDir(home), "conversations"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{Account: e.account, Session: agents.Session{HomeDir: home}})
		notResumable(err, agents.ReasonNoConversation)
	})
	t.Run("single conversation", func(t *testing.T) {
		home := t.TempDir()
		writeConversation(t, home, testConversationID)
		got, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{Account: e.account, Session: agents.Session{HomeDir: home}})
		if err != nil || got.ID != testConversationID {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("several conversations use the work dir pointer", func(t *testing.T) {
		home := t.TempDir()
		other := "11111111-2222-3333-4444-555555555555"
		writeConversation(t, home, other)
		writeConversation(t, home, testConversationID)
		cache := filepath.Join(antigravityDir(home), "cache")
		if err := os.MkdirAll(cache, 0o700); err != nil {
			t.Fatal(err)
		}
		pointer, _ := json.Marshal(map[string]string{"/work": testConversationID})
		if err := os.WriteFile(filepath.Join(cache, "last_conversations.json"), pointer, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{Account: e.account, Session: agents.Session{HomeDir: home, WorkDir: "/work"}})
		if err != nil || got.ID != testConversationID {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("ambiguous falls back to continue", func(t *testing.T) {
		home := t.TempDir()
		writeConversation(t, home, "11111111-2222-3333-4444-555555555555")
		writeConversation(t, home, testConversationID)
		got, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{Account: e.account, Session: agents.Session{HomeDir: home}})
		if err != nil || got.ID != "" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("unsafe file names are ignored", func(t *testing.T) {
		home := t.TempDir()
		writeConversation(t, home, "--dangerously-skip-permissions")
		_, err := e.provider.ResumeTarget(ctx, agents.ResumeRequest{Account: e.account, Session: agents.Session{HomeDir: home}})
		notResumable(err, agents.ReasonNoConversation)
	})
}
