package antigravity

import (
	"context"
	"errors"
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

// Reauthenticate runs agy in the profile's probe home, without the old login,
// and captures the new sign-in into the profile's vault with the next
// generation, so running sessions keep their own fresher tokens and new
// sessions get the new login.
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

// signIn runs agy in the foreground for the user to sign in and captures the
// login. It runs in the profile's persistent probe home, from its workspace, so
// the user goes through agy's first-run wizard there once and the plan probe
// finds it done. Without an account store it falls back to the request's home.
func (p *Provider) signIn(ctx context.Context, account agents.Account, runtimeDir, homeDir string, launcher agents.Launcher) error {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return err
	}
	home, dir := homeDir, ""
	if p.accounts != nil {
		home, dir = probeHomeDir(p.accounts, account), probeWorkspaceDir(p.accounts, account)
		// No plan probe runs agy in the home while the user is in it.
		lock, err := agents.LockFileContext(ctx, probeLockPath(p.accounts, account))
		if err != nil {
			return err
		}
		defer lock.Unlock()
		// The home may hold the previous login; agy would take it instead
		// of asking. The vault keeps it until the new one is captured.
		if err := os.Remove(oauthPath(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	session := agents.Session{
		ID:         filepathBaseSessionID(runtimeDir),
		Provider:   ProviderID,
		AccountID:  account.ID,
		RuntimeDir: runtimeDir,
		HomeDir:    home,
	}
	if err := os.MkdirAll(antigravityDir(home), 0o700); err != nil {
		return err
	}
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			dir = home
		}
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
