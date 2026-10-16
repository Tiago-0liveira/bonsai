package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func writeSessionAuth(t *testing.T, home, access, refresh, email string, expiry time.Time) {
	t.Helper()
	if err := os.MkdirAll(antigravityDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	oauth, _ := json.Marshal(map[string]any{
		"auth_method": "consumer",
		"token": map[string]any{
			"email":         email,
			"access_token":  access,
			"refresh_token": refresh,
			"token_type":    "Bearer",
			"expiry":        expiry.UTC().Format(time.RFC3339),
		},
	})
	if err := os.WriteFile(oauthPath(home), oauth, 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(map[string]any{
		"gcp": map[string]any{"project": "test-project", "location": "global"},
	})
	if err := os.WriteFile(providerSettingsPath(home), settings, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialMaterializationAndStaleReconciliation(t *testing.T) {
	accountStore, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionStore, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{
		ID: "acct_personal", Provider: ProviderID, Name: "personal",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := accountStore.Create(account); err != nil {
		t.Fatal(err)
	}
	manager := NewCredentialManager(accountStore)
	ctx := context.Background()

	setup, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	baseExpiry := time.Now().Add(time.Hour).UTC()
	writeSessionAuth(t, setup.HomeDir, "base-access", "base-refresh", "user@example.com", baseExpiry)
	if err := manager.CaptureSetup(ctx, account, setup); err != nil {
		t.Fatal(err)
	}

	a, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Materialize(ctx, account, a); err != nil {
		t.Fatal(err)
	}
	if err := manager.Materialize(ctx, account, b); err != nil {
		t.Fatal(err)
	}
	if a.HomeDir == b.HomeDir {
		t.Fatal("same-account sessions share HOME")
	}
	if _, err := os.Stat(providerSettingsPath(a.HomeDir)); err != nil {
		t.Fatalf("provider settings were not materialized: %v", err)
	}

	writeSessionAuth(t, b.HomeDir, "new-access", "new-refresh", "user@example.com", baseExpiry.Add(time.Hour))
	writeSessionAuth(t, a.HomeDir, "stale-access", "stale-refresh", "user@example.com", baseExpiry.Add(-time.Minute))
	results := make(chan error, 2)
	go func() { results <- manager.Reconcile(ctx, account, a) }()
	go func() { results <- manager.Reconcile(ctx, account, b) }()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}

	check, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Materialize(ctx, account, check); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(oauthPath(check.HomeDir))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	token, ok := state["token"].(map[string]any)
	if !ok {
		t.Fatalf("missing nested token state: %#v", state)
	}
	if token["access_token"] != "new-access" || token["refresh_token"] != "new-refresh" {
		t.Fatalf("stale session overwrote newer vault: %#v", token)
	}
}

func TestCredentialIdentityMismatchDoesNotReplaceVault(t *testing.T) {
	accountStore, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionStore, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_identity", Provider: ProviderID, Name: "personal", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := accountStore.Create(account); err != nil {
		t.Fatal(err)
	}
	manager := NewCredentialManager(accountStore)
	ctx := context.Background()
	setup, _ := sessionStore.Create(account, t.TempDir())
	writeSessionAuth(t, setup.HomeDir, "a", "r", "user-a@example.com", time.Now().Add(time.Hour))
	if err := manager.CaptureSetup(ctx, account, setup); err != nil {
		t.Fatal(err)
	}
	session, _ := sessionStore.Create(account, t.TempDir())
	if err := manager.Materialize(ctx, account, session); err != nil {
		t.Fatal(err)
	}
	writeSessionAuth(t, session.HomeDir, "b", "r2", "user-b@example.com", time.Now().Add(2*time.Hour))
	if err := manager.Reconcile(ctx, account, session); err == nil {
		t.Fatal("expected identity mismatch")
	}
}

func TestCaptureSetupAcceptsTokenWithoutIdentityClaims(t *testing.T) {
	accountStore, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionStore, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_opaque", Provider: ProviderID, Name: "opaque", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := accountStore.Create(account); err != nil {
		t.Fatal(err)
	}
	session, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(antigravityDir(session.HomeDir), 0o700); err != nil {
		t.Fatal(err)
	}
	token := []byte(`{"auth_method":"consumer","token":{"access_token":"a","refresh_token":"r","expiry":"2030-01-01T00:00:00Z"}}`)
	if err := os.WriteFile(oauthPath(session.HomeDir), token, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewCredentialManager(accountStore).CaptureSetup(context.Background(), account, session); err != nil {
		t.Fatalf("capture rejected a valid Antigravity token without identity claims: %v", err)
	}
}

// agy's first-run state follows the profile into every session home, so the
// welcome screens do not repeat; a profile signed in before the vault kept it
// gets the state of its sign-in home.
func TestOnboardingStateReachesEverySession(t *testing.T) {
	accountStore, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionStore, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_onboard", Provider: ProviderID, Name: "onboard", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := accountStore.Create(account); err != nil {
		t.Fatal(err)
	}
	manager := NewCredentialManager(accountStore)
	ctx := context.Background()
	const onboarding = `{"onboardingComplete":true,"consumerOnboardingComplete":true}`

	setup, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeSessionAuth(t, setup.HomeDir, "access", "refresh", "user@example.com", time.Now().Add(time.Hour))
	if err := manager.CaptureSetup(ctx, account, setup); err != nil {
		t.Fatal(err)
	}

	// Vault without onboarding, sign-in home with it.
	probe := probeHomeDir(accountStore, account)
	if err := os.MkdirAll(filepath.Dir(onboardingPath(probe)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(onboardingPath(probe), []byte(onboarding), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Materialize(ctx, account, first); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(onboardingPath(first.HomeDir)); err != nil || string(got) != onboarding {
		t.Fatalf("session onboarding = %q, %v", got, err)
	}

	// A session that finishes onboarding on its own saves it to the vault.
	if err := os.RemoveAll(probe); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(ctx, account, first); err != nil {
		t.Fatal(err)
	}
	second, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Materialize(ctx, account, second); err != nil {
		t.Fatal(err)
	}
	// The vault is indented JSON, so compare the data, not the layout.
	got, err := os.ReadFile(onboardingPath(second.HomeDir))
	var state struct{ OnboardingComplete, ConsumerOnboardingComplete bool }
	if err != nil || json.Unmarshal(got, &state) != nil || !state.OnboardingComplete || !state.ConsumerOnboardingComplete {
		t.Fatalf("second session onboarding = %q, %v", got, err)
	}
}

// Two sessions of one profile each trust their own folder. Whichever ends
// last must not drop the other's.
func TestReconcileKeepsFoldersTrustedByOtherSessions(t *testing.T) {
	accountStore, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionStore, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_trust", Provider: ProviderID, Name: "trust", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := accountStore.Create(account); err != nil {
		t.Fatal(err)
	}
	manager := NewCredentialManager(accountStore)
	ctx := context.Background()
	setup, err := sessionStore.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour)
	writeSessionAuth(t, setup.HomeDir, "access", "refresh", "user@example.com", expiry)
	if err := manager.CaptureSetup(ctx, account, setup); err != nil {
		t.Fatal(err)
	}
	var sessions [2]agents.Session
	for i, folder := range []string{"/work/a", "/work/b"} {
		if sessions[i], err = sessionStore.Create(account, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if err := manager.Materialize(ctx, account, sessions[i]); err != nil {
			t.Fatal(err)
		}
		// The user trusts the folder: agy saves it into the session's settings.
		settings := `{"gcp":{"project":"test-project","location":"global"},"trustedWorkspaces":["` + folder + `"]}`
		if err := os.WriteFile(providerSettingsPath(sessions[i].HomeDir), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range sessions {
		if err := manager.Reconcile(ctx, account, session); err != nil {
			t.Fatal(err)
		}
	}
	var settings struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
	}
	vault := readTestVault(t, accountStore, account)
	if err := json.Unmarshal(vault.ProviderSettings, &settings); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"/work/a", "/work/b"} {
		if !slices.Contains(settings.TrustedWorkspaces, folder) {
			t.Fatalf("trusted workspaces = %v, missing %s", settings.TrustedWorkspaces, folder)
		}
	}
}
