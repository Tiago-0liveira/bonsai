package antigravity

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// setupNote tells the user how an Antigravity sign-in ends: agy has no login
// command, so Bonsai captures the sign-in once agy exits. Its exit code is
// ignored; the captured credentials are the success check.
const setupNote = "Sign in when agy starts, then exit agy to save the profile."

func (p *Provider) SetupAccount(ctx context.Context, req agents.SetupRequest) (agents.SetupResult, error) {
	if err := checkSetupOptions(req.Options); err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.signIn(ctx, req.Account, req.RuntimeDir, req.HomeDir, req.IO.Launcher); err != nil {
		return agents.SetupResult{}, err
	}
	return agents.SetupResult{Account: req.Account}, nil
}

// Reauthenticate runs agy in a fresh home and captures the new sign-in into the
// profile's vault with the next generation, so running sessions keep their own
// fresher tokens and new sessions get the new login.
func (p *Provider) Reauthenticate(ctx context.Context, req agents.ReauthRequest) (agents.SetupResult, error) {
	if err := checkSetupOptions(req.Options); err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.signIn(ctx, req.Account, req.RuntimeDir, req.HomeDir, req.IO.Launcher); err != nil {
		return agents.SetupResult{}, err
	}
	return agents.SetupResult{Account: req.Account}, nil
}

// SetupSpec: Antigravity has one implicit sign-in and nothing to seed.
func (p *Provider) SetupSpec(context.Context) agents.SetupSpec {
	return agents.SetupSpec{Notes: []string{setupNote}}
}

func (p *Provider) signIn(ctx context.Context, account agents.Account, runtimeDir, homeDir string, launcher agents.Launcher) error {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return err
	}
	session := agents.Session{
		ID:         filepathBaseSessionID(runtimeDir),
		Provider:   ProviderID,
		AccountID:  account.ID,
		RuntimeDir: runtimeDir,
		HomeDir:    homeDir,
	}
	if err := os.MkdirAll(antigravityDir(homeDir), 0o700); err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = homeDir
	}
	prepared := agents.PreparedSession{
		Executable: executable,
		Dir:        dir,
		EnvSet:     buildEnvironment(account, session),
	}
	if err := launcher.RunForeground(ctx, prepared); err != nil {
		return err
	}
	return p.credentialMgr.CaptureSetup(ctx, account, session)
}

func filepathBaseSessionID(runtimeDir string) agents.SessionID {
	return agents.SessionID(filepath.Base(filepath.Clean(runtimeDir)))
}
