package agents

import (
	"context"
	"errors"
	"fmt"
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
