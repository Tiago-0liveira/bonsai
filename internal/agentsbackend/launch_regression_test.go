//go:build !windows

package agentsbackend

import (
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"testing"
)

type reviewResolutionProvider struct {
	*fakeLaunchResolvingProvider
	auth   string
	models int
}

func (p *reviewResolutionProvider) DescribeAccount(context.Context, agents.Account) agents.AccountInfo {
	return agents.AccountInfo{Options: map[string]any{"auth_status": p.auth}}
}
func (p *reviewResolutionProvider) Models(context.Context, agents.Account) ([]agents.ModelOption, error) {
	p.models++
	return nil, nil
}
func TestReviewRegressionMissingStoredTokenRefusesResolution(t *testing.T) {
	f := newFixture(t)
	p := &reviewResolutionProvider{fakeLaunchResolvingProvider: &fakeLaunchResolvingProvider{testProvider: f.provider, available: true}, auth: "missing"}
	registry := agents.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	f.acc.rt.Registry = registry
	_, err := f.acc.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{})
	if err == nil {
		t.Fatal("known missing stored token accepted by resolver")
	}
}
func TestReviewRegressionResolveDoesNotInvokeLiveModelListing(t *testing.T) {
	f := newFixture(t)
	p := &reviewResolutionProvider{fakeLaunchResolvingProvider: &fakeLaunchResolvingProvider{testProvider: f.provider, available: true}, auth: "logged_in"}
	registry := agents.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	f.acc.rt.Registry = registry
	_, err := f.acc.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.models != 0 {
		t.Fatalf("read-only launch resolution invoked live ModelLister %d times", p.models)
	}
}

func TestFinalRegressionCachedDefaultsNeverInvokeLiveModels(t *testing.T) {
	f := newFixture(t)
	p := &reviewResolutionProvider{fakeLaunchResolvingProvider: &fakeLaunchResolvingProvider{testProvider: f.provider, available: true}, auth: "logged_in"}
	registry := agents.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	f.acc.rt.Registry = registry
	d, ok := any(f.acc).(agentsapi.CachedDefaulter)
	if !ok {
		t.Fatal("accounts lacks the cache-only defaults path")
	}
	if _, err := d.CachedDefaults(context.Background(), f.account.ID); err != nil {
		t.Fatal(err)
	}
	if p.models != 0 {
		t.Fatalf("cache-only defaults invoked live Models %d times", p.models)
	}
}
