package antigravity

import (
	"context"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type providerTestBinaryResolver struct {
	path  string
	calls int
}

func (r *providerTestBinaryResolver) Resolve() (string, error) {
	r.calls++
	return r.path, nil
}

type providerTestCredentialManager struct {
	materialize int
	reconcile   int
	capture     int
}

func (m *providerTestCredentialManager) Materialize(context.Context, agents.Account, agents.Session) error {
	m.materialize++
	return nil
}

func (m *providerTestCredentialManager) Reconcile(context.Context, agents.Account, agents.Session) error {
	m.reconcile++
	return nil
}

func (m *providerTestCredentialManager) CaptureSetup(context.Context, agents.Account, agents.Session) error {
	m.capture++
	return nil
}

type providerTestLauncher struct {
	calls    int
	prepared agents.PreparedSession
}

func (l *providerTestLauncher) RunForeground(_ context.Context, prepared agents.PreparedSession) error {
	l.calls++
	l.prepared = prepared
	return nil
}

func TestProviderLifecycleWiresIsolatedSessionState(t *testing.T) {
	resolver := &providerTestBinaryResolver{path: "agy-test"}
	credentials := &providerTestCredentialManager{}
	launcher := &providerTestLauncher{}
	provider := &Provider{
		binaryResolver: resolver,
		credentialMgr:  credentials,
		launcher:       launcher,
	}

	account := agents.Account{
		ID: "acct_provider", Provider: ProviderID, Name: "personal",
		Settings: []byte(`{"model":"gemini-test"}`),
	}
	runtimeDir := t.TempDir()
	homeDir := t.TempDir()
	if _, err := provider.SetupAccount(context.Background(), agents.SetupRequest{
		Account: account, RuntimeDir: runtimeDir, HomeDir: homeDir,
	}); err != nil {
		t.Fatal(err)
	}
	if launcher.calls != 1 || credentials.capture != 1 {
		t.Fatalf("setup calls = launcher %d capture %d", launcher.calls, credentials.capture)
	}
	if launcher.prepared.Executable != resolver.path || launcher.prepared.EnvSet["HOME"] != homeDir {
		t.Fatalf("setup prepared session = %+v", launcher.prepared)
	}

	session := agents.Session{
		ID: "sess_provider", Provider: ProviderID, AccountID: account.ID,
		WorkDir: t.TempDir(), RuntimeDir: t.TempDir(), HomeDir: t.TempDir(),
	}
	prepared, err := provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
		Account: account, Session: session, Args: []string{"--verbose"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if credentials.materialize != 1 {
		t.Fatalf("materialize calls = %d, want 1", credentials.materialize)
	}
	if prepared.Executable != resolver.path || prepared.Dir != session.WorkDir || prepared.EnvSet["HOME"] != session.HomeDir {
		t.Fatalf("prepared session = %+v", prepared)
	}
	wantArgs := []string{"--verbose", "--model", "gemini-test"}
	if len(prepared.Args) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", prepared.Args, wantArgs)
	}
	for i := range wantArgs {
		if prepared.Args[i] != wantArgs[i] {
			t.Fatalf("args = %v, want %v", prepared.Args, wantArgs)
		}
	}

	if err := provider.FinalizeSession(context.Background(), agents.FinalizeSessionRequest{
		Account: account, Session: session,
	}); err != nil {
		t.Fatal(err)
	}
	if credentials.reconcile != 1 {
		t.Fatalf("reconcile calls = %d, want 1", credentials.reconcile)
	}
	if resolver.calls != 2 {
		t.Fatalf("binary resolver calls = %d, want 2", resolver.calls)
	}
}
