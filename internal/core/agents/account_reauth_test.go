package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// reauthTestProvider signs accounts in again, optionally waiting on gate so a
// test can hold one sign-in open.
type reauthTestProvider struct {
	accountServiceTestProvider
	gate     chan struct{}
	active   atomic.Int32
	overlap  atomic.Bool
	calls    atomic.Int32
	settings json.RawMessage
	err      error
	lastReq  ReauthRequest
	mu       sync.Mutex
}

func (p *reauthTestProvider) Reauthenticate(_ context.Context, req ReauthRequest) (SetupResult, error) {
	if p.active.Add(1) > 1 {
		p.overlap.Store(true)
	}
	defer p.active.Add(-1)
	p.calls.Add(1)
	p.mu.Lock()
	p.lastReq = req
	p.mu.Unlock()
	if _, err := os.Stat(req.HomeDir); err != nil {
		return SetupResult{}, err
	}
	if p.gate != nil {
		<-p.gate
	}
	if p.err != nil {
		return SetupResult{}, p.err
	}
	account := req.Account
	account.Settings = p.settings
	account.Name = "renamed-by-provider" // ignored: only settings are stored
	return SetupResult{Account: account, Warnings: []string{"shared login"}}, nil
}

func newReauthService(t *testing.T, provider Provider, cache UsageCache) (*AccountService, Account) {
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
	id, _ := NewAccountID()
	account := Account{ID: id, Provider: provider.ID(), Name: "work", Settings: json.RawMessage(`{"auth_mode":"old"}`)}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	stored, _ := accounts.Get(id)
	return &AccountService{Store: accounts, Sessions: sessions, Registry: registry, Cache: cache}, stored
}

func TestReauthenticateStoresSettingsAndDropsCachedUsage(t *testing.T) {
	provider := &reauthTestProvider{settings: json.RawMessage(`{"auth_mode":"new"}`)}
	cache := &accountServiceTestCache{}
	service, account := newReauthService(t, provider, cache)
	time.Sleep(time.Millisecond)

	result, err := service.Reauthenticate(context.Background(), account.ID, SetupOptions{AuthMode: "new"}, SetupIO{})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Account.Settings) != `{"auth_mode":"new"}` || result.Account.Name != "work" || !result.Account.UpdatedAt.After(account.UpdatedAt) {
		t.Fatalf("account = %+v", result.Account)
	}
	if len(result.Warnings) != 1 || len(cache.removed) != 1 || cache.removed[0] != account.ID {
		t.Fatalf("warnings = %v, cache removed = %v", result.Warnings, cache.removed)
	}
	if provider.lastReq.Options.AuthMode != "new" || provider.lastReq.Account.ID != account.ID {
		t.Fatalf("request = %+v", provider.lastReq)
	}
	// The throwaway session is gone.
	if _, err := os.Stat(provider.lastReq.HomeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session home left behind: %v", err)
	}
	// Missing IO is filled in: a launcher, a writer and the terminal prompter.
	io := provider.lastReq.IO
	if io.Launcher == nil || io.Out == nil || io.Prompt == nil {
		t.Fatalf("io = %+v", io)
	}
	if _, ok := io.Prompt.(TerminalPrompter); !ok {
		t.Fatalf("prompt = %T", io.Prompt)
	}
}

func TestReauthenticateFailureChangesNothing(t *testing.T) {
	provider := &reauthTestProvider{err: errors.New("login failed")}
	cache := &accountServiceTestCache{}
	service, account := newReauthService(t, provider, cache)
	if _, err := service.Reauthenticate(context.Background(), account.ID, SetupOptions{}, SetupIO{}); err == nil {
		t.Fatal("expected error")
	}
	stored, err := service.Store.Get(account.ID)
	if err != nil || string(stored.Settings) != string(account.Settings) || len(cache.removed) != 0 {
		t.Fatalf("stored = %+v, %v; cache removed = %v", stored, err, cache.removed)
	}
}

func TestReauthenticateUnsupportedProvider(t *testing.T) {
	service, account := newReauthService(t, &accountServiceTestProvider{}, nil)
	_, err := service.Reauthenticate(context.Background(), account.ID, SetupOptions{}, SetupIO{})
	if !errors.Is(err, ErrReauthUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if _, err := service.Reauthenticate(context.Background(), "acct_missing", SetupOptions{}, SetupIO{}); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing account err = %v", err)
	}
}

// Two sign-ins of one profile never run at once.
func TestReauthenticateSerializesPerAccount(t *testing.T) {
	provider := &reauthTestProvider{gate: make(chan struct{})}
	service, account := newReauthService(t, provider, nil)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = service.Reauthenticate(context.Background(), account.ID, SetupOptions{}, SetupIO{})
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for provider.calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if provider.calls.Load() != 1 {
		t.Fatalf("second sign-in started while the first held the lock: %d", provider.calls.Load())
	}
	provider.gate <- struct{}{}
	provider.gate <- struct{}{}
	wg.Wait()
	if provider.calls.Load() != 2 || provider.overlap.Load() {
		t.Fatalf("calls = %d, overlap = %v", provider.calls.Load(), provider.overlap.Load())
	}
}

// A canceled wait for the lock returns without signing in.
func TestReauthenticateLockWaitHonorsContext(t *testing.T) {
	provider := &reauthTestProvider{gate: make(chan struct{})}
	service, account := newReauthService(t, provider, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = service.Reauthenticate(context.Background(), account.ID, SetupOptions{}, SetupIO{})
	}()
	for provider.calls.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := service.Reauthenticate(ctx, account.ID, SetupOptions{}, SetupIO{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	provider.gate <- struct{}{}
	<-done
}

func TestSetupWithIOReturnsWarningsAndSetupPrintsThem(t *testing.T) {
	provider := &warningSetupProvider{}
	accounts, _ := NewFileAccountStore(t.TempDir())
	sessions, _ := NewFileSessionStore(t.TempDir())
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	service := &AccountService{Store: accounts, Sessions: sessions, Registry: registry, Out: &out}

	result, err := service.SetupWithIO(context.Background(), provider.ID(), "a", SetupOptions{}, SetupIO{})
	if err != nil || len(result.Warnings) != 1 || out.String() != "progress\n" {
		t.Fatalf("result = %+v, %v, out = %q", result, err, out.String())
	}
	out.Reset()
	if _, err := service.Setup(context.Background(), provider.ID(), "b", SetupOptions{}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "progress\nwarning: same login\n" {
		t.Fatalf("out = %q", out.String())
	}
}

type warningSetupProvider struct{ accountServiceTestProvider }

func (p *warningSetupProvider) SetupAccount(_ context.Context, req SetupRequest) (SetupResult, error) {
	_, _ = req.IO.Out.Write([]byte("progress\n"))
	return SetupResult{Account: req.Account, Warnings: []string{"same login"}}, nil
}

func TestHomeRelative(t *testing.T) {
	for _, tc := range []struct{ path, home, want string }{
		{"/home/me/.codex", "/home/me", "~/.codex"},
		{"/home/me", "/home/me", "~"},
		{"/opt/codex", "/home/me", "/opt/codex"},
		{"/home/meme/.codex", "/home/me", "/home/meme/.codex"},
		{"/x", "", "/x"},
	} {
		path, home, want := filepath.FromSlash(tc.path), filepath.FromSlash(tc.home), filepath.FromSlash(tc.want)
		if got := HomeRelative(path, home); got != want {
			t.Errorf("HomeRelative(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
		}
	}
}

func TestRemoteHost(t *testing.T) {
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	if RemoteHost(env(nil)) || !RemoteHost(env(map[string]string{"WSL_DISTRO_NAME": "Ubuntu"})) || !RemoteHost(env(map[string]string{"SSH_CONNECTION": "1 2 3 4"})) {
		t.Fatal("RemoteHost")
	}
}
