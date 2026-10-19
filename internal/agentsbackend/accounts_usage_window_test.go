//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestUsageStoredNeverCallsProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, ok, err := f.acc.UsageStored(ctx, f.account.ID); err != nil || ok {
		t.Fatalf("empty store = ok %v, err %v", ok, err)
	}
	f.provider.usage = agents.UsageSnapshot{Plan: "pro"}
	if _, err := f.acc.Usage(ctx, f.account.ID, true); err != nil {
		t.Fatal(err)
	}
	calls := f.provider.calls
	// A fresh adapter has no in-memory state: this reads the disk cache.
	fresh := newAccounts(f.acc.rt, f.paths, f.acc.selfExe)
	got, ok, err := fresh.UsageStored(ctx, f.account.ID)
	if err != nil || !ok || got.Snapshot.Plan != "pro" || got.FetchedAt.IsZero() {
		t.Fatalf("stored = %+v ok %v err %v", got, ok, err)
	}
	if f.provider.calls != calls {
		t.Fatalf("provider calls = %d, want %d", f.provider.calls, calls)
	}
}

func TestUsageSharedWindow(t *testing.T) {
	for name, tc := range map[string]struct {
		age       time.Duration
		refresh   bool
		wantCalls int
	}{
		"fresh snapshot makes no provider call":    {age: 2 * time.Minute, wantCalls: 1},
		"snapshot past the window refetches":       {age: 4 * time.Minute, wantCalls: 2},
		"manual refresh bypasses a fresh snapshot": {age: time.Minute, refresh: true, wantCalls: 2},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			f.provider.usage = agents.UsageSnapshot{Plan: "pro"}
			if _, err := f.acc.Usage(ctx, f.account.ID, true); err != nil {
				t.Fatal(err)
			}
			cache := f.acc.rt.UsageService.Cache
			snap, _, _ := cache.Get(f.account.Provider, f.account.ID)
			snap.FetchedAt = time.Now().Add(-tc.age)
			if err := cache.Put(snap); err != nil {
				t.Fatal(err)
			}
			if _, err := f.acc.Usage(ctx, f.account.ID, tc.refresh); err != nil {
				t.Fatal(err)
			}
			if f.provider.calls != tc.wantCalls {
				t.Fatalf("provider calls = %d, want %d", f.provider.calls, tc.wantCalls)
			}
		})
	}
}

func TestUsageFailurePastWindowKeepsLastGoodStale(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.provider.usage = agents.UsageSnapshot{Plan: "pro"}
	if _, err := f.acc.Usage(ctx, f.account.ID, true); err != nil {
		t.Fatal(err)
	}
	cache := f.acc.rt.UsageService.Cache
	snap, _, _ := cache.Get(f.account.Provider, f.account.ID)
	snap.FetchedAt = time.Now().Add(-10 * time.Minute)
	if err := cache.Put(snap); err != nil {
		t.Fatal(err)
	}
	f.provider.usageErr = errors.New("boom " + secretPath)
	got, err := f.acc.Usage(ctx, f.account.ID, false)
	if err != nil || !got.Stale || got.Snapshot.Plan != "pro" || got.Err == "" {
		t.Fatalf("within = %+v, %v", got, err)
	}
}
