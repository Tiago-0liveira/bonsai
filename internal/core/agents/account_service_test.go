package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type accountServiceTestProvider struct {
	setupErr error
	settings json.RawMessage
	setups   int
	lastReq  SetupRequest
}

func (p *accountServiceTestProvider) ID() ProviderID { return "setup-fake" }

func (p *accountServiceTestProvider) Capabilities() Capabilities {
	return Capabilities{Interactive: true, Usage: true, MultiAccount: true}
}

func (p *accountServiceTestProvider) SetupAccount(_ context.Context, req SetupRequest) (SetupResult, error) {
	p.setups++
	p.lastReq = req
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

	account, err := service.Setup(context.Background(), provider.ID(), "personal", SetupOptions{})
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

	_, err := service.Setup(context.Background(), provider.ID(), "personal", SetupOptions{})
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

	warnings, err := service.Remove(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
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

	if _, err := service.Remove(context.Background(), account.ID); !errors.Is(err, sentinel) {
		t.Fatalf("remove error = %v, want %v", err, sentinel)
	}
	if _, err := store.Get(account.ID); err != nil {
		t.Fatalf("account removed despite cache failure: %v", err)
	}
}

func TestAccountServiceSetupForwardsOptionsToProvider(t *testing.T) {
	provider := &accountServiceTestProvider{}
	service, _, _, _ := newAccountServiceTestRuntime(t, provider, nil)
	noSeed := false
	secret := strings.NewReader("sk-test")
	options := SetupOptions{AuthMode: "token", Seed: &noSeed, SeedFrom: "/some/dir", Secret: secret}

	if _, err := service.Setup(context.Background(), provider.ID(), "personal", options); err != nil {
		t.Fatal(err)
	}
	got := provider.lastReq.Options
	if got.AuthMode != "token" {
		t.Fatalf("AuthMode = %q, want token", got.AuthMode)
	}
	if got.Seed == nil || *got.Seed {
		t.Fatalf("Seed = %v, want pointer to false", got.Seed)
	}
	if got.SeedFrom != "/some/dir" {
		t.Fatalf("SeedFrom = %q, want /some/dir", got.SeedFrom)
	}
	if got.Secret != io.Reader(secret) {
		t.Fatalf("Secret was not forwarded unchanged")
	}
}

func TestAccountServiceSetupZeroOptionsAreEmpty(t *testing.T) {
	provider := &accountServiceTestProvider{}
	service, _, _, _ := newAccountServiceTestRuntime(t, provider, nil)

	if _, err := service.Setup(context.Background(), provider.ID(), "personal", SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if !provider.lastReq.Options.Empty() {
		t.Fatalf("options = %+v, want empty", provider.lastReq.Options)
	}
}

type accountServiceRemoverProvider struct {
	accountServiceTestProvider
	removeErr  error
	calls      []Account
	dirExisted bool
	accountDir func(AccountID) string
}

func (p *accountServiceRemoverProvider) RemoveAccount(_ context.Context, account Account) error {
	p.calls = append(p.calls, account)
	if info, err := os.Stat(p.accountDir(account.ID)); err == nil && info.IsDir() {
		p.dirExisted = true
	}
	return p.removeErr
}

func newAccountServiceRemoverRuntime(t *testing.T, provider *accountServiceRemoverProvider) (*AccountService, AccountStore) {
	t.Helper()
	accounts, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	provider.accountDir = accounts.AccountDir
	return &AccountService{Store: accounts, Sessions: sessions, Registry: registry}, accounts
}

func TestAccountServiceRemoveCallsAccountRemoverBeforeDeletingDir(t *testing.T) {
	provider := &accountServiceRemoverProvider{}
	service, store := newAccountServiceRemoverRuntime(t, provider)
	account := testAccount("acct_remover", provider.ID(), "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}

	warnings, err := service.Remove(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(provider.calls) != 1 || provider.calls[0].ID != account.ID {
		t.Fatalf("RemoveAccount calls = %+v, want one for %s", provider.calls, account.ID)
	}
	if !provider.dirExisted {
		t.Fatal("account directory did not exist when RemoveAccount ran")
	}
	if _, err := os.Stat(store.AccountDir(account.ID)); !os.IsNotExist(err) {
		t.Fatalf("account directory still present after removal: %v", err)
	}
	if _, err := store.Get(account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("removed account lookup error = %v", err)
	}
}

func TestAccountServiceRemoveAccountRemoverErrorBecomesWarning(t *testing.T) {
	provider := &accountServiceRemoverProvider{removeErr: errors.New("keychain locked")}
	service, store := newAccountServiceRemoverRuntime(t, provider)
	account := testAccount("acct_warn", provider.ID(), "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}

	warnings, err := service.Remove(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("remove error = %v, want nil", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "keychain locked") {
		t.Fatalf("warnings = %v, want one mentioning the remover error", warnings)
	}
	if _, err := store.Get(account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("account not removed despite remover failure: %v", err)
	}
	if _, err := os.Stat(store.AccountDir(account.ID)); !os.IsNotExist(err) {
		t.Fatalf("account directory still present: %v", err)
	}
}

func TestAccountServiceRemoveWithoutRemoverHasNoWarnings(t *testing.T) {
	provider := &accountServiceTestProvider{}
	service, store, _, _ := newAccountServiceTestRuntime(t, provider, nil)
	account := testAccount("acct_plain", provider.ID(), "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	warnings, err := service.Remove(context.Background(), account.ID)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("Remove = %v, %v; want no warnings and no error", warnings, err)
	}
}
