package antigravity

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) Usage(ctx context.Context, account agents.Account, _ agents.UsageOptions) (agents.UsageSnapshot, error) {
	workDir, _ := os.Getwd()
	session, err := p.sessions.Create(account, workDir)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}

	if err := p.credentialMgr.Materialize(ctx, account, session); err != nil {
		return agents.UsageSnapshot{}, errors.Join(err, p.sessions.Cleanup(session))
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.UsageSnapshot{}, errors.Join(err, p.sessions.Cleanup(session))
	}
	out, runErr := runUsageCLI(ctx, executable, session.WorkDir, buildEnvironment(account, session))
	var parseErr error
	var limits []agents.UsageLimit
	if runErr == nil {
		limits, parseErr = ParseUsage(out)
	}
	reconcileErr := p.credentialMgr.Reconcile(ctx, account, session)
	cleanupErr := p.sessions.Cleanup(session)
	if err := errors.Join(runErr, parseErr, reconcileErr, cleanupErr); err != nil {
		return agents.UsageSnapshot{}, err
	}
	return agents.UsageSnapshot{
		Provider:  ProviderID,
		AccountID: account.ID,
		FetchedAt: time.Now().UTC(),
		Limits:    limits,
	}, nil
}
