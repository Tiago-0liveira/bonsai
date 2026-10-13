package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// signInLauncher stands in for agy: it writes a sign-in into the setup home,
// as agy does before the user exits it.
type signInLauncher struct {
	t       *testing.T
	access  string
	expiry  time.Time
	calls   int
	lastEnv map[string]string
}

func (l *signInLauncher) RunForeground(_ context.Context, prepared agents.PreparedSession) error {
	l.calls++
	l.lastEnv = prepared.EnvSet
	writeSessionAuth(l.t, prepared.EnvSet["HOME"], l.access, l.access+"-refresh", "user@example.com", l.expiry)
	return nil
}

func readTestVault(t *testing.T, accounts agents.AccountStore, account agents.Account) credentialVault {
	t.Helper()
	data, err := os.ReadFile(vaultPath(accounts, account))
	if err != nil {
		t.Fatal(err)
	}
	var vault credentialVault
	if err := json.Unmarshal(data, &vault); err != nil {
		t.Fatal(err)
	}
	return vault
}

// A sign-in again runs agy through the request's launcher, captures the new
// login with the next vault generation, and a running session's older tokens
// do not overwrite it when that session ends.
func TestReauthenticateBumpsGenerationAndKeepsRunningSessions(t *testing.T) {
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := New(accounts, sessions)
	provider.binaryResolver = &providerTestBinaryResolver{path: "agy-test"}
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	service := &agents.AccountService{Store: accounts, Sessions: sessions, Registry: registry, Out: &out}
	ctx := context.Background()

	first := &signInLauncher{t: t, access: "first", expiry: time.Now().Add(time.Hour)}
	result, err := service.SetupWithIO(ctx, ProviderID, "personal", agents.SetupOptions{}, agents.SetupIO{Launcher: first})
	if err != nil {
		t.Fatal(err)
	}
	account := result.Account
	if first.calls != 1 || readTestVault(t, accounts, account).Generation != 1 {
		t.Fatalf("setup: calls %d, vault %+v", first.calls, readTestVault(t, accounts, account))
	}

	running, err := sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.credentialMgr.Materialize(ctx, account, running); err != nil {
		t.Fatal(err)
	}

	second := &signInLauncher{t: t, access: "second", expiry: time.Now().Add(2 * time.Hour)}
	if _, err := service.Reauthenticate(ctx, account.ID, agents.SetupOptions{}, agents.SetupIO{Launcher: second}); err != nil {
		t.Fatal(err)
	}
	vault := readTestVault(t, accounts, account)
	if second.calls != 1 || vault.Generation != 2 || !strings.Contains(string(vault.OAuth), "second") {
		t.Fatalf("reauth: calls %d, vault generation %d", second.calls, vault.Generation)
	}
	if second.lastEnv["HOME"] == running.HomeDir {
		t.Fatal("sign-in again must use a fresh home, not a running session's")
	}

	// The running session refreshed nothing newer; ending it keeps the new login.
	writeSessionAuth(t, running.HomeDir, "first", "first-refresh", "user@example.com", time.Now().Add(time.Hour))
	if err := provider.credentialMgr.Reconcile(ctx, account, running); err != nil {
		t.Fatal(err)
	}
	if vault := readTestVault(t, accounts, account); vault.Generation != 2 || !strings.Contains(string(vault.OAuth), "second") {
		t.Fatalf("stale session overwrote the new login: generation %d", vault.Generation)
	}
	if out.Len() != 0 {
		t.Fatalf("antigravity wrote setup output: %q", out.String())
	}
}

func TestReauthenticateRejectsSetupOptions(t *testing.T) {
	no := false
	for name, options := range map[string]agents.SetupOptions{
		"auth":    {AuthMode: "token"},
		"secret":  {Secret: strings.NewReader("x")},
		"no seed": {Seed: &no},
	} {
		t.Run(name, func(t *testing.T) {
			launcher := &providerTestLauncher{}
			provider := &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
			_, err := provider.Reauthenticate(context.Background(), agents.ReauthRequest{Account: agents.Account{ID: "acct_s"}, RuntimeDir: t.TempDir(), HomeDir: t.TempDir(), Options: options, IO: agents.SetupIO{Launcher: launcher}})
			if err == nil || launcher.calls != 0 {
				t.Fatalf("err = %v, launches = %d", err, launcher.calls)
			}
		})
	}
}

func TestSetupSpecHasOneModeAndNotes(t *testing.T) {
	spec := New(nil, nil).SetupSpec(context.Background())
	if len(spec.AuthModes) != 0 || spec.Seed != nil || len(spec.Notes) == 0 || !strings.Contains(spec.Notes[0], "exit agy") {
		t.Fatalf("spec = %+v", spec)
	}
}
