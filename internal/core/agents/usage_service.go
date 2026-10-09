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

func (s *UsageService) All(ctx context.Context, opts UsageOptions) []AccountUsageResult {
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
				snapshot, err := s.Account(ctx, account.ID, opts)
				results[idx] = AccountUsageResult{Account: account, Error: err}
				if err == nil {
					results[idx].Usage = &snapshot
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
