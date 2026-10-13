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
	binaryResolver BinaryResolver
	credentialMgr  CredentialManager
}

// New builds the Antigravity provider. Setup talks to the user only through
// each request's SetupIO.
func New(accounts agents.AccountStore, sessions agents.SessionStore) *Provider {
	return &Provider{
		accounts:       accounts,
		sessions:       sessions,
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

// installHint is shown while agy is missing from PATH.
const installHint = "Install Antigravity, then make sure agy is on PATH"

func (p *Provider) Label() string { return "Antigravity" }

func (p *Provider) Availability(context.Context) agents.Availability {
	if _, err := p.binaryResolver.Resolve(); err != nil {
		return agents.Availability{Reason: "Install agy and restart Bonsai", Hint: installHint}
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

var (
	_ agents.SetupSchema          = (*Provider)(nil)
	_ agents.AccountAuthenticator = (*Provider)(nil)
)
