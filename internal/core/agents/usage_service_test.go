package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type usageTestProvider struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	calls       int
	fail        map[AccountID]error
}

func (p *usageTestProvider) ID() ProviderID             { return "usage-fake" }
func (p *usageTestProvider) Capabilities() Capabilities { return Capabilities{Usage: true} }
func (p *usageTestProvider) SetupAccount(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}
func (p *usageTestProvider) PrepareSession(context.Context, PrepareSessionRequest) (PreparedSession, error) {
	return PreparedSession{}, nil
}
func (p *usageTestProvider) FinalizeSession(context.Context, FinalizeSessionRequest) error {
	return nil
}
func (p *usageTestProvider) Usage(_ context.Context, account Account, _ UsageOptions) (UsageSnapshot, error) {
	p.mu.Lock()
	p.inFlight++
	p.calls++
	if p.inFlight > p.maxInFlight {
		p.maxInFlight = p.inFlight
	}
	p.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	p.mu.Lock()
	p.inFlight--
	err := p.fail[account.ID]
	p.mu.Unlock()
	if err != nil {
		return UsageSnapshot{}, err
	}
	f := 0.5
	return UsageSnapshot{
		AccountID: account.ID,
		Limits:    []UsageLimit{{ID: "window", Window: "5h", RemainingFraction: &f}},
	}, nil
}

func TestUsageServiceBoundedConcurrencyAndErrorIsolation(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &usageTestProvider{fail: map[AccountID]error{}}
	for i := 0; i < 20; i++ {
		id := AccountID(fmt.Sprintf("acct_%02d", i))
		account := testAccount(id, provider.ID(), fmt.Sprintf("account-%02d", i))
		if err := store.Create(account); err != nil {
			t.Fatal(err)
		}
	}
	provider.fail["acct_03"] = errors.New("quota failed")
	provider.fail["acct_11"] = errors.New("auth failed")
	registry := NewRegistry()
	_ = registry.Register(provider)
	service := &UsageService{Accounts: store, Registry: registry, WorkerLimit: 4}
	results := service.All(context.Background(), UsageOptions{Refresh: true})
	if len(results) != 20 {
		t.Fatalf("result count = %d", len(results))
	}
	provider.mu.Lock()
	max := provider.maxInFlight
	provider.mu.Unlock()
	if max > 4 {
		t.Fatalf("max concurrency = %d, want <= 4", max)
	}
	failures := 0
	successes := 0
	for _, result := range results {
		if result.Error != nil {
			failures++
		} else if result.Usage != nil {
			successes++
		}
	}
	if failures != 2 || successes != 18 {
		t.Fatalf("success/failure = %d/%d, want 18/2", successes, failures)
	}
}

func TestUsageServiceCacheSurvivesRename(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFileUsageCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_cache", "usage-fake", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	provider := &usageTestProvider{fail: map[AccountID]error{}}
	registry := NewRegistry()
	_ = registry.Register(provider)
	service := &UsageService{Accounts: store, Registry: registry, Cache: cache, TTL: time.Minute}
	if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rename(account.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls := provider.calls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1 cached call after rename", calls)
	}
}

func TestUsageServiceRefreshAndExpiredCacheBypass(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFileUsageCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_ttl", "usage-fake", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	provider := &usageTestProvider{fail: map[AccountID]error{}}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	service := &UsageService{Accounts: store, Registry: registry, Cache: cache, TTL: time.Minute}

	if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls := provider.calls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("provider calls after cache hit = %d, want 1", calls)
	}

	if _, err := service.Account(context.Background(), account.ID, UsageOptions{Refresh: true}); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls = provider.calls
	provider.mu.Unlock()
	if calls != 2 {
		t.Fatalf("provider calls after refresh = %d, want 2", calls)
	}

	stale := UsageSnapshot{
		Provider: account.Provider, AccountID: account.ID,
		FetchedAt: time.Now().Add(-2 * time.Minute),
	}
	if err := cache.Put(stale); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	calls = provider.calls
	provider.mu.Unlock()
	if calls != 3 {
		t.Fatalf("provider calls after expired cache = %d, want 3", calls)
	}
}

type noUsageProvider struct{ usageTestProvider }

func (p *noUsageProvider) ID() ProviderID             { return "no-usage" }
func (p *noUsageProvider) Capabilities() Capabilities { return Capabilities{} }

func TestUsageServiceAllSkipsProvidersWithoutUsage(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	with, without := &usageTestProvider{fail: map[AccountID]error{}}, &noUsageProvider{}
	registry := NewRegistry()
	_ = registry.Register(with)
	_ = registry.Register(without)
	_ = store.Create(testAccount("acct_a", with.ID(), "a"))
	_ = store.Create(testAccount("acct_b", without.ID(), "b"))
	service := &UsageService{Accounts: store, Registry: registry}
	results := service.All(context.Background(), UsageOptions{Refresh: true})
	if len(results) != 1 || results[0].Account.ID != "acct_a" || results[0].Error != nil {
		t.Fatalf("results = %+v", results)
	}
}

// policyUsageProvider reports its own freshness window.
type policyUsageProvider struct {
	usageTestProvider
	ttl time.Duration
}

func (p *policyUsageProvider) UsageTTL() time.Duration { return p.ttl }

func TestUsageServiceHonoursProviderTTL(t *testing.T) {
	for name, tc := range map[string]struct {
		ttl       time.Duration
		age       time.Duration
		wantCalls int
	}{
		"fresh under a long provider ttl":    {ttl: 5 * time.Minute, age: 2 * time.Minute, wantCalls: 1},
		"stale past the provider ttl":        {ttl: 5 * time.Minute, age: 6 * time.Minute, wantCalls: 2},
		"service ttl applies without policy": {ttl: 0, age: 2 * time.Minute, wantCalls: 2},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := NewFileAccountStore(t.TempDir())
			cache, _ := NewFileUsageCache(t.TempDir())
			account := testAccount("acct_policy", "usage-fake", "personal")
			if err := store.Create(account); err != nil {
				t.Fatal(err)
			}
			provider := &policyUsageProvider{usageTestProvider: usageTestProvider{fail: map[AccountID]error{}}, ttl: tc.ttl}
			registry := NewRegistry()
			_ = registry.Register(provider)
			service := &UsageService{Accounts: store, Registry: registry, Cache: cache, TTL: time.Minute}
			if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
				t.Fatal(err)
			}
			// Age the cached snapshot.
			cached, ok, err := cache.Get("usage-fake", account.ID)
			if err != nil || !ok {
				t.Fatalf("cache: ok=%v err=%v", ok, err)
			}
			cached.FetchedAt = time.Now().Add(-tc.age)
			if err := cache.Put(cached); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Account(context.Background(), account.ID, UsageOptions{}); err != nil {
				t.Fatal(err)
			}
			provider.mu.Lock()
			defer provider.mu.Unlock()
			if provider.calls != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", provider.calls, tc.wantCalls)
			}
		})
	}
}

// panicUsageProvider panics fetching usage for the accounts in panics.
type panicUsageProvider struct {
	usageTestProvider
	panics map[AccountID]bool
}

func (p *panicUsageProvider) Usage(ctx context.Context, account Account, opts UsageOptions) (UsageSnapshot, error) {
	if p.panics[account.ID] {
		var snapshot *UsageSnapshot
		return *snapshot, nil
	}
	return p.usageTestProvider.Usage(ctx, account, opts)
}

func panicUsageService(t *testing.T) (*UsageService, *FileUsageCache) {
	t.Helper()
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFileUsageCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &panicUsageProvider{usageTestProvider: usageTestProvider{fail: map[AccountID]error{}}, panics: map[AccountID]bool{"acct_bad": true}}
	for _, id := range []AccountID{"acct_bad", "acct_good"} {
		if err := store.Create(testAccount(id, provider.ID(), string(id))); err != nil {
			t.Fatal(err)
		}
	}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return &UsageService{Accounts: store, Registry: registry, Cache: cache, TTL: time.Minute, WorkerLimit: 1}, cache
}

func TestUsageServiceAccountRecoversProviderPanic(t *testing.T) {
	service, cache := panicUsageService(t)
	_, err := service.Account(context.Background(), "acct_bad", UsageOptions{})
	if err == nil || !strings.Contains(err.Error(), "usage fetch for usage-fake/acct_bad panicked: runtime error") {
		t.Fatalf("err = %v", err)
	}
	if _, ok, _ := cache.Get("usage-fake", "acct_bad"); ok {
		t.Fatal("panicked fetch was cached")
	}
	if _, err := service.Account(context.Background(), "acct_good", UsageOptions{}); err != nil {
		t.Fatalf("good account: %v", err)
	}

	// A cancelled caller sees its cancellation, not the panic.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Account(ctx, "acct_bad", UsageOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err = %v", err)
	}
}

func TestUsageServiceAllFuncIsolatesProviderPanic(t *testing.T) {
	service, cache := panicUsageService(t)
	// One worker: the panic must not stop it before the next account.
	results := service.All(context.Background(), UsageOptions{Refresh: true})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		switch result.Account.ID {
		case "acct_bad":
			if result.Usage != nil || result.Error == nil || !strings.Contains(result.Error.Error(), "panicked") {
				t.Errorf("bad = %+v", result)
			}
		case "acct_good":
			if result.Error != nil || result.Usage == nil {
				t.Errorf("good = %+v", result)
			}
		default:
			t.Errorf("unexpected result %+v", result)
		}
	}
	if _, ok, _ := cache.Get("usage-fake", "acct_bad"); ok {
		t.Fatal("panicked fetch was cached")
	}

	// A panic in the per-account options still yields a result per account.
	results = service.AllFunc(context.Background(), func(a Account) UsageOptions {
		if a.ID == "acct_good" {
			panic("options exploded")
		}
		return UsageOptions{}
	})
	if len(results) != 2 || results[1].Account.ID != "acct_good" || results[1].Error == nil || !strings.Contains(results[1].Error.Error(), "options exploded") {
		t.Fatalf("results = %+v", results)
	}
}

// valuePanicProvider panics with value for the accounts in panics, and
// delegates otherwise. headlinePanics makes UsageHeadline panic too.
type valuePanicProvider struct {
	usageTestProvider
	value          any
	panics         map[AccountID]bool
	headlinePanics bool
}

func (p *valuePanicProvider) Usage(ctx context.Context, account Account, opts UsageOptions) (UsageSnapshot, error) {
	if p.panics[account.ID] {
		panic(p.value)
	}
	return p.usageTestProvider.Usage(ctx, account, opts)
}

func (p *valuePanicProvider) UsageHeadline(UsageSnapshot) UsageHeadline {
	if p.headlinePanics {
		panic("headline exploded")
	}
	return UsageHeadline{}
}

// valuePanicService builds a service over n accounts acct_0..acct_n-1 whose
// listed indexes panic with value. It silences the panic stack logging.
func valuePanicService(t *testing.T, n, workers int, value any, panicking ...int) (*UsageService, *valuePanicProvider, *FileUsageCache) {
	t.Helper()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewFileUsageCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &valuePanicProvider{usageTestProvider: usageTestProvider{fail: map[AccountID]error{}}, value: value, panics: map[AccountID]bool{}}
	for i := 0; i < n; i++ {
		id := AccountID(fmt.Sprintf("acct_%d", i))
		if err := store.Create(testAccount(id, provider.ID(), string(id))); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range panicking {
		provider.panics[AccountID(fmt.Sprintf("acct_%d", i))] = true
	}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return &UsageService{Accounts: store, Registry: registry, Cache: cache, TTL: time.Minute, WorkerLimit: workers}, provider, cache
}

// allWithin runs All and fails fast when it hangs.
func allWithin(t *testing.T, service *UsageService, ctx context.Context, opts UsageOptions) []AccountUsageResult {
	t.Helper()
	done := make(chan []AccountUsageResult, 1)
	go func() { done <- service.All(ctx, opts) }()
	select {
	case results := <-done:
		return results
	case <-time.After(5 * time.Second):
		t.Fatal("All did not return")
		return nil
	}
}

func TestUsageServiceAllFuncEveryAccountPanics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		workers int
		cancel  bool
	}{
		{"one worker", 1, false},
		{"more workers than accounts", 16, false},
		{"cancelled", 2, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, _, cache := valuePanicService(t, 4, tt.workers, "boom", 0, 1, 2, 3)
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			results := allWithin(t, service, ctx, UsageOptions{Refresh: true})
			if len(results) != 4 {
				t.Fatalf("results = %+v", results)
			}
			for _, r := range results {
				if r.Usage != nil || r.Headline != nil || r.Error == nil {
					t.Fatalf("result = %+v", r)
				}
				switch {
				case tt.cancel && !errors.Is(r.Error, context.Canceled):
					t.Fatalf("cancelled error = %v", r.Error)
				case !tt.cancel && !strings.Contains(r.Error.Error(), "panicked: boom"):
					t.Fatalf("error = %v", r.Error)
				case tt.cancel && strings.Contains(r.Error.Error(), "boom"):
					t.Fatalf("panic text in cancelled error: %v", r.Error)
				}
				if _, ok, _ := cache.Get("usage-fake", r.Account.ID); ok {
					t.Fatalf("%s was cached", r.Account.ID)
				}
			}
		})
	}
}

func TestUsageServiceAccountPanicValues(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{"int", 42, "usage fetch for usage-fake/acct_0 panicked: 42"},
		{"struct", struct{ N int }{7}, "usage fetch for usage-fake/acct_0 panicked: {7}"},
		{"wrapped error", fmt.Errorf("outer: %w", errors.New("inner")), "usage fetch for usage-fake/acct_0 panicked: outer: inner"},
		{"nil error", error(nil), "usage fetch for usage-fake/acct_0 panicked: panic called with nil argument"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, _, _ := valuePanicService(t, 1, 1, tt.value, 0)
			_, err := service.Account(context.Background(), "acct_0", UsageOptions{})
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("err = %v, want prefix %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "goroutine ") || strings.Contains(err.Error(), ".go:") {
				t.Fatalf("error carries a stack: %v", err)
			}
		})
	}
}

func TestUsageServicePanicKeepsCachedSnapshot(t *testing.T) {
	service, provider, cache := valuePanicService(t, 1, 1, "boom")
	first, err := service.Account(context.Background(), "acct_0", UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	provider.panics["acct_0"] = true
	if _, err := service.Account(context.Background(), "acct_0", UsageOptions{Refresh: true}); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("refresh err = %v", err)
	}
	cached, ok, err := cache.Get("usage-fake", "acct_0")
	if err != nil || !ok || !cached.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("cached = %+v ok = %v err = %v", cached, ok, err)
	}
	// The panicking call left nothing locked: the cache still serves.
	served, err := service.Account(context.Background(), "acct_0", UsageOptions{})
	if err != nil || !served.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("served = %+v err = %v", served, err)
	}
	// And a recovered provider refetches normally.
	provider.panics["acct_0"] = false
	fresh, err := service.Account(context.Background(), "acct_0", UsageOptions{Refresh: true})
	if err != nil || fresh.FetchedAt.Before(first.FetchedAt) || len(fresh.Limits) != 1 {
		t.Fatalf("fresh = %+v err = %v", fresh, err)
	}
}

func TestUsageServiceHeadlinePanic(t *testing.T) {
	service, provider, _ := valuePanicService(t, 2, 2, nil)
	provider.headlinePanics = true
	if h := service.Headline(UsageSnapshot{Provider: provider.ID(), AccountID: "acct_0"}); h != nil {
		t.Fatalf("headline = %+v", h)
	}
	results := allWithin(t, service, context.Background(), UsageOptions{Refresh: true})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	for _, r := range results {
		if r.Error != nil || r.Usage == nil || r.Headline != nil {
			t.Fatalf("result = %+v", r)
		}
	}
}

func TestUsageServiceAllFuncKeepsOrderAroundPanic(t *testing.T) {
	service, _, _ := valuePanicService(t, 5, 3, "boom", 2)
	accounts, err := service.Accounts.List()
	if err != nil {
		t.Fatal(err)
	}
	results := allWithin(t, service, context.Background(), UsageOptions{Refresh: true})
	if len(results) != len(accounts) {
		t.Fatalf("results = %+v", results)
	}
	for i, r := range results {
		if !reflect.DeepEqual(r.Account, accounts[i]) {
			t.Fatalf("result %d account = %+v, want %+v", i, r.Account, accounts[i])
		}
		if bad := accounts[i].ID == "acct_2"; bad != (r.Error != nil) || bad == (r.Usage != nil) {
			t.Fatalf("result %d = %+v", i, r)
		}
	}
}
