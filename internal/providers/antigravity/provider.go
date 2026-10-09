package antigravity

import (
	"context"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const ProviderID agents.ProviderID = "antigravity"

type Provider struct {
	accounts       agents.AccountStore
	sessions       agents.SessionStore
	launcher       agents.Launcher
	binaryResolver BinaryResolver
	credentialMgr  CredentialManager
}

func New(accounts agents.AccountStore, sessions agents.SessionStore, launcher agents.Launcher) *Provider {
	return &Provider{
		accounts:       accounts,
		sessions:       sessions,
		launcher:       launcher,
		binaryResolver: PathBinaryResolver{},
		credentialMgr:  NewCredentialManager(accounts),
	}
}

func (p *Provider) ID() agents.ProviderID { return ProviderID }

func (p *Provider) Capabilities() agents.Capabilities {
	return agents.Capabilities{
		Interactive:            true,
		Usage:                  true,
		MultiAccount:           true,
		ConcurrentSameAccount:  true,
		ConcurrentCrossAccount: true,
	}
}

func (p *Provider) Label() string { return "Antigravity" }

func (p *Provider) Availability(context.Context) agents.Availability {
	if _, err := p.binaryResolver.Resolve(); err != nil {
		return agents.Availability{Reason: "Install agy and restart Bonsai"}
	}
	return agents.Availability{Available: true}
}

func (p *Provider) DescribeAccount(_ context.Context, account agents.Account) agents.AccountInfo {
	settings, _ := ParseSettings(account)
	return agents.AccountInfo{Options: map[string]any{"full_access": settings.DangerouslySkipPermissions}}
}

// checkSetupOptions rejects setup options agy has no equivalent for.
func checkSetupOptions(options agents.SetupOptions) error {
	switch {
	case options.AuthMode != "":
		return fmt.Errorf("antigravity does not support --auth")
	case options.Secret != nil:
		return fmt.Errorf("antigravity does not support --token-stdin")
	case options.Seed != nil || options.SeedFrom != "":
		return fmt.Errorf("antigravity does not support profile seeding")
	}
	return nil
}
