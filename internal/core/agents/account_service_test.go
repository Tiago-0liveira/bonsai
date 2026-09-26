package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type accountServiceTestProvider struct {
	setupErr error
	settings json.RawMessage
	setups   int
}

func (p *accountServiceTestProvider) ID() ProviderID { return "setup-fake" }

func (p *accountServiceTestProvider) Capabilities() Capabilities {
	return Capabilities{Interactive: true, Usage: true, MultiAccount: true}
}

func (p *accountServiceTestProvider) SetupAccount(_ context.Context, req SetupRequest) (SetupResult, error) {
	p.setups++
	if _, err := os.Stat(req.HomeDir); err != nil {
		return SetupResult{}, err
	}
	if p.setupErr != nil {
		return SetupResult{}, p.setupErr
	}
	account := req.Account
	account.Settings = append(json.RawMessage(nil), p.settings...)
	return SetupResult{Account: account}, nil
}

func (p *accountServiceTestProvider) PrepareSession(context.Context, PrepareSessionRequest) (PreparedSession, error) {
	return PreparedSession{}, nil
}

func (p *accountServiceTestProvider) FinalizeSession(context.Context, FinalizeSessionRequest) error {
	return nil
}

func (p *accountServiceTestProvider) Usage(context.Context, Account, UsageOptions) (UsageSnapshot, error) {
	return UsageSnapshot{}, nil
}

type accountServiceTestCache struct {
	removeErr error
	removed   []AccountID
}

func (c *accountServiceTestCache) Get(ProviderID, AccountID) (UsageSnapshot, bool, error) {
	return UsageSnapshot{}, false, nil
}

func (c *accountServiceTestCache) Put(UsageSnapshot) error { return nil }

func (c *accountServiceTestCache) RemoveAccount(_ ProviderID, accountID AccountID) error {
	c.removed = append(c.removed, accountID)
	return c.removeErr
}

func newAccountServiceTestRuntime(t *testing.T, provider *accountServiceTestProvider, cache UsageCache) (*AccountService, AccountStore, string, string) {
	t.Helper()
	accountRoot := t.TempDir()
	sessionRoot := t.TempDir()
	accounts, err := NewFileAccountStore(accountRoot)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewFileSessionStore(sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return &AccountService{
		Store: accounts, Sessions: sessions, Registry: registry, Cache: cache,
	}, accounts, accountRoot, sessionRoot
}

func TestAccountServiceSetupPersistsProviderStateAndCleansSession(t *testing.T) {
	provider := &accountServiceTestProvider{settings: json.RawMessage(`{"model":"test-model"}`)}
	service, store, _, sessionRoot := newAccountServiceTestRuntime(t, provider, nil)

	account, err := service.Setup(context.Background(), provider.ID(), "personal")
	if err != nil {
		t.Fatal(err)
	}
	if provider.setups != 1 {
		t.Fatalf("provider setup calls = %d, want 1", provider.setups)
	}
	if account.ID == "" || account.Provider != provider.ID() || account.Name != "personal" {
		t.Fatalf("account identity = %+v", account)
	}
	if string(account.Settings) != string(provider.settings) {
		t.Fatalf("settings = %s, want %s", account.Settings, provider.settings)
	}
	stored, err := store.Get(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	var storedSettings, wantSettings any
	if err := json.Unmarshal(stored.Settings, &storedSettings); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(provider.settings, &wantSettings); err != nil {
		t.Fatal(err)
	}
	storedCanonical, err := json.Marshal(storedSettings)
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical, err := json.Marshal(wantSettings)
	if err != nil {
		t.Fatal(err)
	}
	if string(storedCanonical) != string(wantCanonical) {
		t.Fatalf("stored settings = %s, want %s", stored.Settings, provider.settings)
	}
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("setup session was not cleaned: %v", entries)
	}
}

func TestAccountServiceSetupFailureRollsBackAccountAndSession(t *testing.T) {
	sentinel := errors.New("provider login failed")
	provider := &accountServiceTestProvider{setupErr: sentinel}
	service, store, accountRoot, sessionRoot := newAccountServiceTestRuntime(t, provider, nil)

	_, err := service.Setup(context.Background(), provider.ID(), "personal")
	if !errors.Is(err, sentinel) {
		t.Fatalf("setup error = %v, want %v", err, sentinel)
	}
	accounts, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 0 {
		t.Fatalf("failed setup persisted accounts: %+v", accounts)
	}
	accountDirs, err := os.ReadDir(filepath.Join(accountRoot, "accounts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(accountDirs) != 0 {
		t.Fatalf("failed setup left account directories: %v", accountDirs)
	}
	sessionDirs, err := os.ReadDir(sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionDirs) != 0 {
		t.Fatalf("failed setup left session directories: %v", sessionDirs)
	}
}

func TestAccountServiceRemoveClearsUsageCacheBeforeAccount(t *testing.T) {
	provider := &accountServiceTestProvider{}
	cache := &accountServiceTestCache{}
	service, store, _, _ := newAccountServiceTestRuntime(t, provider, cache)
	account := testAccount("acct_remove", provider.ID(), "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}

	if err := service.Remove(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if len(cache.removed) != 1 || cache.removed[0] != account.ID {
		t.Fatalf("cache removals = %v, want [%s]", cache.removed, account.ID)
	}
	if _, err := store.Get(account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("removed account lookup error = %v", err)
	}
}

func TestAccountServiceRemovePreservesAccountWhenCacheCleanupFails(t *testing.T) {
	provider := &accountServiceTestProvider{}
	sentinel := errors.New("cache unavailable")
	cache := &accountServiceTestCache{removeErr: sentinel}
	service, store, _, _ := newAccountServiceTestRuntime(t, provider, cache)
	account := testAccount("acct_keep", provider.ID(), "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}

	if err := service.Remove(context.Background(), account.ID); !errors.Is(err, sentinel) {
		t.Fatalf("remove error = %v, want %v", err, sentinel)
	}
	if _, err := store.Get(account.ID); err != nil {
		t.Fatalf("account removed despite cache failure: %v", err)
	}
}
