package antigravity

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) SetupAccount(ctx context.Context, req agents.SetupRequest) (agents.SetupResult, error) {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.SetupResult{}, err
	}
	session := agents.Session{
		ID:         filepathBaseSessionID(req.RuntimeDir),
		Provider:   ProviderID,
		AccountID:  req.Account.ID,
		RuntimeDir: req.RuntimeDir,
		HomeDir:    req.HomeDir,
	}
	if err := os.MkdirAll(antigravityDir(req.HomeDir), 0o700); err != nil {
		return agents.SetupResult{}, err
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = req.HomeDir
	}
	prepared := agents.PreparedSession{
		Executable: executable,
		Dir:        dir,
		EnvSet:     buildEnvironment(req.Account, session),
	}
	if err := p.launcher.RunForeground(ctx, prepared); err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.credentialMgr.CaptureSetup(ctx, req.Account, session); err != nil {
		return agents.SetupResult{}, err
	}
	return agents.SetupResult{Account: req.Account}, nil
}

func filepathBaseSessionID(runtimeDir string) agents.SessionID {
	return agents.SessionID(filepath.Base(filepath.Clean(runtimeDir)))
}
