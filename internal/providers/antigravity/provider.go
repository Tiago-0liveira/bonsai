package antigravity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const ProviderID agents.ProviderID = "antigravity"

type Provider struct {
	accounts       agents.AccountStore
	sessions       agents.SessionStore
	binaryResolver BinaryResolver
	credentialMgr  CredentialManager

	run cliRunner        // nil runs agy; tests replace it
	now func() time.Time // nil is time.Now

	modelsMu    sync.Mutex
	modelsCache map[agents.AccountID]modelsEntry

	// probe reads the plan from agy's screen; nil is probePlan.
	probe       func(ctx context.Context, executable, dir string, env []string, email string) (string, error)
	planMu      sync.Mutex
	planFlights map[agents.AccountID]bool
	planWG      sync.WaitGroup
	// planCtx is the probes' context; SettleUsage cancels it and the next
	// probe starts a new one.
	planCtx    context.Context
	planCancel context.CancelFunc
}

func (p *Provider) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
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

// DescribeAccount reads the stored login and plan; it never starts agy. A
// login is "logged_in" when its credentials are complete: whether agy still
// accepts them is only known when it runs.
func (p *Provider) DescribeAccount(_ context.Context, account agents.Account) agents.AccountInfo {
	settings, _ := ParseSettings(account)
	info := agents.AccountInfo{Options: map[string]any{"full_access": settings.DangerouslySkipPermissions}}
	if settings.Model != "" {
		info.Options["model"] = settings.Model
	}
	if settings.Effort != "" {
		info.Options["effort"] = settings.Effort
	}
	if p.accounts == nil {
		return info
	}
	email, complete, err := p.signedIn(account)
	switch {
	case err != nil:
		info.Options["auth_status"] = "unknown"
		info.Warnings = append(info.Warnings, "Stored login is unreadable")
	case !complete:
		info.Options["auth_status"] = "logged_out"
		info.Warnings = append(info.Warnings, fmt.Sprintf("Not logged in; run `bonsai agent account login %s`", account.Name))
	default:
		info.Options["auth_status"] = "logged_in"
		info.Identity = email
		info.Plan = p.planFor(account, email)
	}
	return info
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
	_ agents.ModelLister          = (*Provider)(nil)
	_ agents.LaunchValidator      = (*Provider)(nil)
	_ agents.UsageSettler         = (*Provider)(nil)
)
