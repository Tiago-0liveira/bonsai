package antigravity

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) PrepareSession(ctx context.Context, req agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	settings, err := ParseSettings(req.Account)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	if err := p.credentialMgr.Materialize(ctx, req.Account, req.Session); err != nil {
		return agents.PreparedSession{}, err
	}
	return agents.PreparedSession{
		Executable: executable,
		Args: invocationArgs(settings, req.Args),
		Dir: req.Session.WorkDir,
		EnvSet: buildEnvironment(req.Account, req.Session),
	}, nil
}

func (p *Provider) FinalizeSession(ctx context.Context, req agents.FinalizeSessionRequest) error {
	return p.credentialMgr.Reconcile(ctx, req.Account, req.Session)
}
