package claude

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	// maxTokenInput bounds token input; real tokens are far shorter.
	maxTokenInput = 8 << 10
	loginTimeout  = 15 * time.Second
)

// setupMode picks the auth mode. A token on --token-stdin implies token mode.
func setupMode(options agents.SetupOptions) (string, error) {
	mode := options.AuthMode
	switch {
	case mode == "" && options.Secret != nil:
		mode = AuthToken
	case mode == "":
		mode = AuthLogin
	case mode != AuthLogin && mode != AuthToken:
		return "", fmt.Errorf("unknown auth mode %q (use login or token)", mode)
	}
	if mode == AuthLogin && options.Secret != nil {
		return "", fmt.Errorf("--token-stdin needs --auth token")
	}
	if options.Seed != nil && !*options.Seed && options.SeedFrom != "" {
		return "", fmt.Errorf("--no-seed and --seed-from cannot be combined")
	}
	return mode, nil
}

func (p *Provider) SetupAccount(ctx context.Context, req agents.SetupRequest) (agents.SetupResult, error) {
	mode, err := setupMode(req.Options)
	if err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.requireSupported(ctx); err != nil {
		return agents.SetupResult{}, err
	}
	dir := configDir(p.accounts, req.Account)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return agents.SetupResult{}, err
	}
	settings := Settings{AuthMode: mode}
	if req.Options.Seed == nil || *req.Options.Seed {
		if settings.SeededFrom, err = p.seed(req.IO.Out, req.Options.SeedFrom, dir); err != nil {
			return agents.SetupResult{}, err
		}
	}
	warnings, err := p.signIn(ctx, req.Account, req.RuntimeDir, mode, req.Options.Secret, req.IO)
	if err != nil {
		return agents.SetupResult{}, err
	}
	account := req.Account
	account.Settings = settings.raw()
	return agents.SetupResult{Account: account, Warnings: warnings}, nil
}

// Reauthenticate signs an existing profile in again: login mode runs
// `claude auth login` in the profile's config dir, token mode stores a new
// setup token. Without --auth it keeps the profile's mode. Switching from token
// to login removes the stored token once the login succeeded.
func (p *Provider) Reauthenticate(ctx context.Context, req agents.ReauthRequest) (agents.SetupResult, error) {
	settings, err := ParseSettings(req.Account)
	if err != nil {
		return agents.SetupResult{}, err
	}
	options := req.Options
	if options.Seed != nil || options.SeedFrom != "" {
		return agents.SetupResult{}, fmt.Errorf("seeding only applies to new profiles")
	}
	if options.AuthMode == "" && options.Secret == nil {
		options.AuthMode = settings.AuthMode
	}
	mode, err := setupMode(options)
	if err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.requireSupported(ctx); err != nil {
		return agents.SetupResult{}, err
	}
	if err := os.MkdirAll(configDir(p.accounts, req.Account), 0o700); err != nil {
		return agents.SetupResult{}, err
	}
	defer p.forgetAccount(req.Account.ID)
	p.forgetStatus(req.Account.ID)
	warnings, err := p.signIn(ctx, req.Account, req.RuntimeDir, mode, options.Secret, req.IO)
	if err != nil {
		return agents.SetupResult{}, err
	}
	if mode == AuthLogin {
		if err := os.Remove(tokenPath(p.accounts, req.Account)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return agents.SetupResult{}, fmt.Errorf("remove the old token: %w", err)
		}
	}
	settings.AuthMode = mode
	account := req.Account
	account.Settings = settings.raw()
	return agents.SetupResult{Account: account, Warnings: warnings}, nil
}

// signIn runs the sign-in of mode for the profile.
func (p *Provider) signIn(ctx context.Context, account agents.Account, runtimeDir, mode string, secret io.Reader, setupIO agents.SetupIO) ([]string, error) {
	dir := configDir(p.accounts, account)
	session := agents.Session{
		ID:        agents.SessionID(filepath.Base(filepath.Clean(runtimeDir))),
		Provider:  ProviderID,
		AccountID: account.ID,
	}
	if mode == AuthToken {
		return nil, p.setupToken(ctx, account, dir, session, secret, setupIO)
	}
	return p.setupLogin(ctx, account, dir, session, setupIO.Launcher)
}

// forgetAccount drops what the provider cached for a profile whose login changed.
func (p *Provider) forgetAccount(id agents.AccountID) {
	p.forgetStatus(id)
	p.modelsMu.Lock()
	delete(p.modelsCache, id)
	p.modelsMu.Unlock()
	p.usageMu.Lock()
	delete(p.usageFailures, id)
	p.usageMu.Unlock()
}

// seed copies the user's own Claude setup into the new profile and reports it.
func (p *Provider) seed(out io.Writer, from, dst string) (string, error) {
	src, candidates, explicit, err := p.seedSource(from)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(src); statErr != nil {
		if explicit {
			return "", fmt.Errorf("seed source: %w", statErr)
		}
		fmt.Fprintf(out, "nothing to seed: %s does not exist\n", src)
		return "", nil
	}
	report, err := seedProfile(src, candidates, dst, maxSeedBytes)
	if err != nil {
		return "", fmt.Errorf("seed profile: %w", err)
	}
	if len(report.copied) == 0 {
		fmt.Fprintf(out, "nothing to seed from %s\n", src)
	} else {
		fmt.Fprintf(out, "seeded profile from %s: %s\n", src, strings.Join(report.copied, ", "))
	}
	for _, skipped := range report.skipped {
		fmt.Fprintf(out, "  skipped %s\n", skipped)
	}
	return src, nil
}

func (p *Provider) foregroundCommand(dir string, account agents.Account, session agents.Session, token string, args ...string) (agents.PreparedSession, error) {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	workDir, err := os.Getwd()
	if err != nil {
		workDir = dir
	}
	return agents.PreparedSession{
		Executable:       executable,
		Args:             args,
		Dir:              workDir,
		EnvSet:           profileEnvironment(dir, account, session, token),
		EnvUnsetPrefixes: scrubPrefixes,
	}, nil
}

// setupLogin runs `claude auth login` in the profile's config dir, then checks
// the result with `claude auth status`.
func (p *Provider) setupLogin(ctx context.Context, account agents.Account, dir string, session agents.Session, launcher agents.Launcher) ([]string, error) {
	login, err := p.foregroundCommand(dir, account, session, "", "auth", "login")
	if err != nil {
		return nil, err
	}
	if err := launcher.RunForeground(ctx, login); err != nil {
		return nil, fmt.Errorf("claude auth login failed: %w", err)
	}
	statusCmd, err := p.foregroundCommand(dir, account, session, "", "auth", "status")
	if err != nil {
		return nil, err
	}
	statusCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	out, _ := p.runner.Output(statusCtx, statusCmd)
	status, ok := parseAuthStatus(out)
	if !ok || !status.LoggedIn {
		return nil, fmt.Errorf("claude login did not complete")
	}
	identity := storedIdentity(dir)
	if identity == "" {
		identity = strings.TrimSpace(status.Email)
	}
	return p.duplicateIdentityWarnings(identity, account.ID), nil
}

// setupToken stores a `claude setup-token` token. It comes from --token-stdin
// or, otherwise, from running `claude setup-token` and a hidden prompt.
func (p *Provider) setupToken(ctx context.Context, account agents.Account, dir string, session agents.Session, secret io.Reader, setupIO agents.SetupIO) error {
	var token string
	if secret != nil {
		data, err := io.ReadAll(io.LimitReader(secret, maxTokenInput+1))
		if err != nil || len(data) > maxTokenInput {
			return fmt.Errorf("cannot read token")
		}
		token = strings.TrimSpace(string(data))
	} else {
		cmd, err := p.foregroundCommand(dir, account, session, "", "setup-token")
		if err != nil {
			return err
		}
		if err := setupIO.Launcher.RunForeground(ctx, cmd); err != nil {
			return fmt.Errorf("claude setup-token failed: %w", err)
		}
		if token, err = promptToken(ctx, setupIO.Prompt); err != nil {
			return err
		}
		token = strings.TrimSpace(token)
	}
	if !validToken(token) {
		return fmt.Errorf("not a valid Claude setup token (expected sk-ant-oat01-...)")
	}
	if err := writeToken(tokenPath(p.accounts, account), token, p.now()); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

// duplicateIdentityWarnings names every other Claude profile signed in as identity.
func (p *Provider) duplicateIdentityWarnings(identity string, self agents.AccountID) []string {
	if identity == "" {
		return nil
	}
	accounts, err := p.accounts.List()
	if err != nil {
		return nil
	}
	var warnings []string
	for _, other := range accounts {
		if other.Provider != ProviderID || other.ID == self {
			continue
		}
		if strings.EqualFold(storedIdentity(configDir(p.accounts, other)), identity) {
			warnings = append(warnings, fmt.Sprintf("profile %q is already signed in as %s; two profiles on one login share its usage limits", other.Name, identity))
		}
	}
	return warnings
}

// promptToken asks for the setup token. Errors never carry what was typed.
func promptToken(ctx context.Context, prompt agents.SecretPrompter) (string, error) {
	token, err := prompt.Secret(ctx, "token")
	switch {
	case err == nil:
		return token, nil
	case errors.Is(err, agents.ErrNoTerminal):
		return "", fmt.Errorf("no terminal to read the token from; pipe it with --token-stdin")
	case ctx.Err() != nil:
		return "", ctx.Err()
	}
	return "", fmt.Errorf("cannot read token")
}
