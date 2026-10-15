package agents

import (
	"context"
	"fmt"
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

func (s *UsageService) Account(ctx context.Context, accountID AccountID, opts UsageOptions) (UsageSnapshot, error) {
	account, err := s.Accounts.Get(accountID)
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
	if !opts.Refresh && s.Cache != nil {
		if cached, ok, err := s.Cache.Get(account.Provider, account.ID); err != nil {
			return UsageSnapshot{}, err
		} else if ok && time.Since(cached.FetchedAt) <= ttl {
			return cached, nil
		}
	}
	snapshot, err := provider.Usage(ctx, account, opts)
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
func (s *UsageService) Headline(snapshot UsageSnapshot) *UsageHeadline {
	if s.Registry == nil {
		return nil
	}
	return s.Registry.UsageHeadline(snapshot)
}

// Settle waits for the background work providers left from reading usage
// (UsageSettler), until ctx ends. A long-running caller never needs it.
func (s *UsageService) Settle(ctx context.Context) {
	if s.Registry == nil {
		return
	}
	var wg sync.WaitGroup
	for _, provider := range s.Registry.List() {
		if settler, ok := provider.(UsageSettler); ok {
			wg.Go(func() { settler.SettleUsage(ctx) })
		}
	}
	wg.Wait()
}

func (s *UsageService) All(ctx context.Context, opts UsageOptions) []AccountUsageResult {
	return s.AllFunc(ctx, func(Account) UsageOptions { return opts })
}

// AllFunc is All with options chosen per account, such as a refresh throttled
// per account.
func (s *UsageService) AllFunc(ctx context.Context, opts func(Account) UsageOptions) []AccountUsageResult {
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
				account := accounts[idx]
				snapshot, err := s.Account(ctx, account.ID, opts(account))
				results[idx] = AccountUsageResult{Account: account, Error: err}
				if err == nil {
					results[idx].Usage = &snapshot
					results[idx].Headline = s.Headline(snapshot)
				}
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
