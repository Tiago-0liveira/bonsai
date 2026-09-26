package antigravity

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

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
