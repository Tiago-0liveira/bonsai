package agents

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"
	"time"
)

type UsageService struct {
	Accounts    AccountStore
	Registry    *Registry
	Cache       UsageCache
	Sessions    SessionStore
	TTL         time.Duration
	WorkerLimit int
}

func (s *UsageService) Account(ctx context.Context, accountID AccountID, opts UsageOptions) (snapshot UsageSnapshot, err error) {
	var account Account
	// A panic in provider code is this account's error, never a crash, and
	// returns before anything reaches the cache.
	defer func() {
		if r := recover(); r != nil {
			snapshot, err = UsageSnapshot{}, usagePanic(ctx, account.Provider, accountID, r)
		}
	}()
	account, err = s.Accounts.Get(accountID)
	if err != nil {
		return UsageSnapshot{}, err
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return UsageSnapshot{}, err
	}
	if !provider.Capabilities().Usage {
		return UsageSnapshot{}, fmt.Errorf("%w: %s", ErrUsageUnsupported, account.Provider)
	}
	ttl := s.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	// A provider that knows how fast its numbers move sets its own freshness.
	if policy, ok := provider.(UsagePolicy); ok {
		if providerTTL := policy.UsageTTL(); providerTTL > 0 {
			ttl = providerTTL
		}
	}
	// A caller's freshness window beats the provider's, so a consumer that
	// polls slowly does not refetch what a restart already has on disk.
	if opts.MaxAge > 0 {
		ttl = opts.MaxAge
	}
	if !opts.Refresh && s.Cache != nil {
		if cached, ok, err := s.Cache.Get(account.Provider, account.ID); err != nil {
			return UsageSnapshot{}, err
		} else if ok && time.Since(cached.FetchedAt) <= ttl {
			return cached, nil
		}
	}
	snapshot, err = provider.Usage(ctx, account, opts)
	if err != nil {
		return UsageSnapshot{}, err
	}
	snapshot.Provider = account.Provider
	snapshot.AccountID = account.ID
	if snapshot.FetchedAt.IsZero() {
		snapshot.FetchedAt = time.Now().UTC()
	}
	if s.Cache != nil {
		if err := s.Cache.Put(snapshot); err != nil {
			return UsageSnapshot{}, err
		}
	}
	return snapshot, nil
}

// Headline returns the provider's headline of a snapshot, or nil when the
// provider has no UsageLayout.
func (s *UsageService) Headline(snapshot UsageSnapshot) (headline *UsageHeadline) {
	if s.Registry == nil {
		return nil
	}
	// The headline is optional, so a provider that panics building it only
	// loses the headline.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("agents: usage headline for %s/%s panicked: %v\n%s", snapshot.Provider, snapshot.AccountID, r, debug.Stack())
			headline = nil
		}
	}()
	return s.Registry.UsageHeadline(snapshot)
}

func (s *UsageService) All(ctx context.Context, opts UsageOptions) []AccountUsageResult {
	return s.AllFunc(ctx, func(Account) UsageOptions { return opts })
}

// AllFunc is All with options chosen per account, such as a refresh throttled
// per account.
func (s *UsageService) AllFunc(ctx context.Context, opts func(Account) UsageOptions) []AccountUsageResult {
	return s.AllObserved(ctx, opts, nil)
}

// UsagePhase is where one account is in an AllObserved run.
type UsagePhase int

const (
	// UsageQueued is sent once for every account before any fetch starts.
	UsageQueued UsagePhase = iota
	UsageStarted
	UsageFinished
)

// UsageEvent reports one account's progress. Err is set on UsageFinished.
type UsageEvent struct {
	Account Account
	Phase   UsagePhase
	Err     error
}

// AllObserved is AllFunc that also reports each account's progress to observe.
// Every account gets UsageQueued before the first fetch starts, then
// UsageStarted and UsageFinished as its worker reaches it. Workers call observe
// concurrently, so it must be safe for that and quick. A panicking observe is
// ignored. A nil observe is allowed.
func (s *UsageService) AllObserved(ctx context.Context, opts func(Account) UsageOptions, observe func(UsageEvent)) []AccountUsageResult {
	notify := func(event UsageEvent) {
		if observe == nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				log.Printf("agents: usage observer panicked: %v\n%s", r, debug.Stack())
			}
		}()
		observe(event)
	}
	accounts, err := s.Accounts.List()
	if err != nil {
		return []AccountUsageResult{{Error: err}}
	}
	// Providers without usage support are not failures, so they are left out.
	accounts = slices.DeleteFunc(accounts, func(a Account) bool {
		provider, err := s.Registry.Get(a.Provider)
		return err == nil && !provider.Capabilities().Usage
	})
	results := make([]AccountUsageResult, len(accounts))
	if len(accounts) == 0 {
		return results
	}
	for _, account := range accounts {
		notify(UsageEvent{Account: account, Phase: UsageQueued})
	}
	limit := s.WorkerLimit
	if limit <= 0 {
		limit = 8
	}
	if limit > len(accounts) {
		limit = len(accounts)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				notify(UsageEvent{Account: accounts[idx], Phase: UsageStarted})
				results[idx] = s.accountResult(ctx, accounts[idx], opts)
				notify(UsageEvent{Account: accounts[idx], Phase: UsageFinished, Err: results[idx].Error})
			}
		}()
	}
	for i := range accounts {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// accountResult is one AllFunc job. It recovers too, so a panic outside the
// fetch, such as in opts, fails only this account and the worker goes on.
func (s *UsageService) accountResult(ctx context.Context, account Account, opts func(Account) UsageOptions) (result AccountUsageResult) {
	defer func() {
		if r := recover(); r != nil {
			result = AccountUsageResult{Account: account, Error: usagePanic(ctx, account.Provider, account.ID, r)}
		}
	}()
	snapshot, err := s.Account(ctx, account.ID, opts(account))
	result = AccountUsageResult{Account: account, Error: err}
	if err == nil {
		result.Usage = &snapshot
		result.Headline = s.Headline(snapshot)
	}
	return result
}

// usagePanic logs a recovered usage panic with its stack and returns the error
// shown for that account. A cancelled ctx still reads as cancelled.
func usagePanic(ctx context.Context, provider ProviderID, account AccountID, r any) error {
	log.Printf("agents: usage fetch for %s/%s panicked: %v\n%s", provider, account, r, debug.Stack())
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("usage fetch for %s/%s panicked: %v", provider, account, r)
}
