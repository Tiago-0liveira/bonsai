//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"strings"
	"testing"
)

func TestUsageFirstFailureReadsPersistedCache(t *testing.T) {
	f := newFixture(t)
	f.provider.usage = agents.UsageSnapshot{Plan: "cached", Limits: []agents.UsageLimit{{ID: "5h"}}}
	if _, err := f.acc.Usage(context.Background(), f.account.ID, true); err != nil {
		t.Fatal(err)
	}
	f.provider.usageErr = errors.New("read " + secretPath)
	fresh := newAccounts(f.acc.rt, f.paths, f.acc.selfExe)
	got, err := fresh.Usage(context.Background(), f.account.ID, true)
	if err != nil || !got.Stale || got.Snapshot.Plan != "cached" || got.Err == "" || strings.Contains(got.Err, secretPath) {
		t.Fatalf("persisted fallback = %+v, %v", got, err)
	}
	// A successful empty refresh is allowed to clear the persisted snapshot.
	f.provider.usageErr = nil
	f.provider.usage = agents.UsageSnapshot{}
	got, err = fresh.Usage(context.Background(), f.account.ID, true)
	if err != nil || got.Stale || got.Snapshot.Plan != "" || got.Err != "" {
		t.Fatalf("empty refresh = %+v, %v", got, err)
	}
}
