package agents

import (
	"context"
	"errors"
	"testing"
)

type registryFakeProvider struct{ id ProviderID }

func (p registryFakeProvider) ID() ProviderID           { return p.id }
func (registryFakeProvider) Capabilities() Capabilities { return Capabilities{} }
func (registryFakeProvider) SetupAccount(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}
func (registryFakeProvider) PrepareSession(context.Context, PrepareSessionRequest) (PreparedSession, error) {
	return PreparedSession{}, nil
}
func (registryFakeProvider) FinalizeSession(context.Context, FinalizeSessionRequest) error {
	return nil
}
func (registryFakeProvider) Usage(context.Context, Account, UsageOptions) (UsageSnapshot, error) {
	return UsageSnapshot{}, nil
}

func TestRegistrySupportsMultipleProviders(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(registryFakeProvider{id: "zeta"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(registryFakeProvider{id: "alpha"}); err != nil {
		t.Fatal(err)
	}
	list := r.List()
	if len(list) != 2 || list[0].ID() != "alpha" || list[1].ID() != "zeta" {
		t.Fatalf("unexpected provider order: %#v", list)
	}
	if _, err := r.Get("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(registryFakeProvider{id: "alpha"}); err == nil {
		t.Fatal("expected duplicate provider registration to fail")
	}
	if _, err := r.Get("missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("missing provider error = %v", err)
	}
}
