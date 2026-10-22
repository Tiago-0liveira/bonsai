package usagestore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

type accountBlockingUsage struct {
	*fake.Accounts
	entered chan struct{}
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
}

func (a *accountBlockingUsage) Usage(ctx context.Context, _ agents.AccountID, _ bool) (agentsapi.Usage, error) {
	a.calls.Add(1)
	active := a.active.Add(1)
	defer a.active.Add(-1)
	for peak := a.peak.Load(); active > peak; peak = a.peak.Load() {
		if a.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	a.entered <- struct{}{}
	select {
	case <-a.release:
		return usageAt(0, "fresh"), nil
	case <-ctx.Done():
		return agentsapi.Usage{}, ctx.Err()
	}
}

func TestAccountsUsageLimiterBoundsConcurrentProviderCalls(t *testing.T) {
	const slots, requests = 3, 12
	api := &accountBlockingUsage{Accounts: &fake.Accounts{}, entered: make(chan struct{}, requests), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lim := NewLimiter(slots)
	done := make(chan Result, requests)
	var workers sync.WaitGroup
	for range requests {
		workers.Go(func() { done <- Fetch(ctx, api, lim, "account", 1, true)().(Result) })
	}
	for range slots {
		select {
		case <-api.entered:
		case <-time.After(5 * time.Second):
			cancel()
			close(api.release)
			workers.Wait()
			t.Fatal("provider slots never filled")
		}
	}
	if got := api.active.Load(); got != slots {
		t.Errorf("active calls=%d, want %d", got, slots)
	}
	close(api.release)
	workers.Wait()
	close(done)
	for result := range done {
		if result.Err != nil {
			t.Errorf("provider result: %v", result.Err)
		}
	}
	if api.calls.Load() != requests || api.peak.Load() > slots || api.active.Load() != 0 || len(lim) != 0 {
		t.Errorf("calls=%d peak=%d active=%d slots held=%d", api.calls.Load(), api.peak.Load(), api.active.Load(), len(lim))
	}
	// A settled limiter must accept a later explicit refresh without leaking slots.
	if result := Fetch(ctx, api, lim, "account", 2, true)().(Result); result.Err != nil {
		t.Fatal(result.Err)
	}
}

func TestAccountsUsageBeginApplyInterleavingPreservesIndependentAccounts(t *testing.T) {
	initial := Store{"a": {Usage: usageAt(time.Minute, "old a"), Have: true}, "b": {Usage: usageAt(time.Minute, "old b"), Have: true}}
	current, a1 := initial.Begin("a")
	current, b1 := current.Begin("b")
	current, a2 := current.Begin("a")
	var ok bool
	current, ok = current.Apply(Result{Account: "a", Seq: a2, Usage: usageAt(0, "new a")})
	if !ok {
		t.Fatal("latest a request dropped")
	}
	current, ok = current.Apply(Result{Account: "a", Seq: a1, Err: context.DeadlineExceeded})
	if ok {
		t.Error("superseded a failure applied")
	}
	current, ok = current.Apply(Result{Account: "b", Seq: b1, Err: context.DeadlineExceeded})
	if !ok {
		t.Fatal("independent b failure dropped")
	}
	if a, b := current["a"], current["b"]; a.Usage.Snapshot.Plan != "new a" || a.Stale() || b.Usage.Snapshot.Plan != "old b" || !b.Stale() || a.Refreshing || b.Refreshing {
		t.Fatalf("interleaving corrupted account states: a=%+v b=%+v", a, b)
	}
	if initial["a"].Usage.Snapshot.Plan != "old a" || initial["b"].Stale() {
		t.Error("copy-on-write store mutated the original snapshot")
	}
}
