package antigravity

import (
	"context"
	"encoding/json"
	"os"
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
	if err := manager.Reconcile(ctx, account, b); err != nil {
		t.Fatal(err)
	}
	writeSessionAuth(t, a.HomeDir, "stale-access", "stale-refresh", "user@example.com", baseExpiry.Add(-time.Minute))
	if err := manager.Reconcile(ctx, account, a); err != nil {
		t.Fatal(err)
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
