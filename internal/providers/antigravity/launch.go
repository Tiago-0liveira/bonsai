package antigravity

import (
	"context"
	"fmt"

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
		Args:       launchArgs(settings, req.Args, req.Launch),
		Dir:        req.Session.WorkDir,
		EnvSet:     buildEnvironment(req.Account, req.Session),
	}, nil
}

func (p *Provider) FinalizeSession(ctx context.Context, req agents.FinalizeSessionRequest) error {
	return p.credentialMgr.Reconcile(ctx, req.Account, req.Session)
}

// ValidateLaunch rejects options agy has no equivalent for and unreadable
// profile settings before any session is created.
func (p *Provider) ValidateLaunch(account agents.Account, launch agents.LaunchOptions) error {
	if launch.PermissionMode != "" {
		return fmt.Errorf("antigravity does not support a permission mode; use full access")
	}
	if launch.Effort != "" {
		return fmt.Errorf("antigravity does not support an effort level")
	}
	if _, err := ParseSettings(account); err != nil {
		return fmt.Errorf("invalid profile settings")
	}
	return nil
}

// launchArgs applies per-launch options on top of the profile settings without
// saving them. The prompt is bound to its flag so prompts that begin with a dash
// stay literal, and it always comes last.
func launchArgs(settings Settings, explicit []string, launch agents.LaunchOptions) []string {
	args := append([]string(nil), explicit...)
	if launch.Model != "" && !hasFlag(args, "--model", "-m") {
		args = append(args, "--model", launch.Model)
	}
	if launch.FullAccess != nil {
		settings.DangerouslySkipPermissions = *launch.FullAccess
	}
	args = invocationArgs(settings, args)
	if launch.Prompt != "" {
		args = append(args, "--prompt-interactive="+launch.Prompt)
	}
	return args
}
