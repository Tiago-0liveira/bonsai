package claude

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

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
		if settings.SeededFrom, err = p.seed(req.Options.SeedFrom, dir); err != nil {
			return agents.SetupResult{}, err
		}
	}
	session := agents.Session{
		ID:        agents.SessionID(filepath.Base(filepath.Clean(req.RuntimeDir))),
		Provider:  ProviderID,
		AccountID: req.Account.ID,
	}
	if mode == AuthToken {
		err = p.setupToken(ctx, req, dir, session)
	} else {
		err = p.setupLogin(ctx, req, dir, session)
	}
	if err != nil {
		return agents.SetupResult{}, err
	}
	account := req.Account
	account.Settings = settings.raw()
	return agents.SetupResult{Account: account}, nil
}

// seed copies the user's own Claude setup into the new profile and reports it.
func (p *Provider) seed(from, dst string) (string, error) {
	src, candidates, explicit, err := p.seedSource(from)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(src); statErr != nil {
		if explicit {
			return "", fmt.Errorf("seed source: %w", statErr)
		}
		fmt.Fprintf(p.out, "nothing to seed: %s does not exist\n", src)
		return "", nil
	}
	report, err := seedProfile(src, candidates, dst, maxSeedBytes)
	if err != nil {
		return "", fmt.Errorf("seed profile: %w", err)
	}
	if len(report.copied) == 0 {
		fmt.Fprintf(p.out, "nothing to seed from %s\n", src)
	} else {
		fmt.Fprintf(p.out, "seeded profile from %s: %s\n", src, strings.Join(report.copied, ", "))
	}
	for _, skipped := range report.skipped {
		fmt.Fprintf(p.out, "  skipped %s\n", skipped)
	}
	return src, nil
}

func (p *Provider) foregroundCommand(dir string, req agents.SetupRequest, session agents.Session, token string, args ...string) (agents.PreparedSession, error) {
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
		EnvSet:           profileEnvironment(dir, req.Account, session, token),
		EnvUnsetPrefixes: scrubPrefixes,
	}, nil
}

// setupLogin runs `claude auth login` in the profile's config dir, then checks
// the result with `claude auth status`.
func (p *Provider) setupLogin(ctx context.Context, req agents.SetupRequest, dir string, session agents.Session) error {
	login, err := p.foregroundCommand(dir, req, session, "", "auth", "login")
	if err != nil {
		return err
	}
	if err := p.launcher.RunForeground(ctx, login); err != nil {
		return fmt.Errorf("claude auth login failed: %w", err)
	}
	statusCmd, err := p.foregroundCommand(dir, req, session, "", "auth", "status")
	if err != nil {
		return err
	}
	statusCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	out, _ := p.runner.Output(statusCtx, statusCmd)
	status, ok := parseAuthStatus(out)
	if !ok || !status.LoggedIn {
		return fmt.Errorf("claude login did not complete")
	}
	identity := storedIdentity(dir)
	if identity == "" {
		identity = strings.TrimSpace(status.Email)
	}
	p.warnDuplicateIdentity(identity, req.Account.ID)
	return nil
}

// setupToken stores a `claude setup-token` token. It comes from --token-stdin
// or, otherwise, from running `claude setup-token` and a hidden prompt.
func (p *Provider) setupToken(ctx context.Context, req agents.SetupRequest, dir string, session agents.Session) error {
	var token string
	if req.Options.Secret != nil {
		data, err := io.ReadAll(io.LimitReader(req.Options.Secret, maxTokenInput+1))
		if err != nil || len(data) > maxTokenInput {
			return fmt.Errorf("cannot read token")
		}
		token = strings.TrimSpace(string(data))
	} else {
		cmd, err := p.foregroundCommand(dir, req, session, "", "setup-token")
		if err != nil {
			return err
		}
		if err := p.launcher.RunForeground(ctx, cmd); err != nil {
			return fmt.Errorf("claude setup-token failed: %w", err)
		}
		if token, err = p.promptSecret(); err != nil {
			return err
		}
		token = strings.TrimSpace(token)
	}
	if !validToken(token) {
		return fmt.Errorf("not a valid Claude setup token (expected sk-ant-oat01-...)")
	}
	if err := writeToken(tokenPath(p.accounts, req.Account), token, p.now()); err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	return nil
}

func (p *Provider) warnDuplicateIdentity(identity string, self agents.AccountID) {
	if identity == "" {
		return
	}
	accounts, err := p.accounts.List()
	if err != nil {
		return
	}
	for _, other := range accounts {
		if other.Provider != ProviderID || other.ID == self {
			continue
		}
		if strings.EqualFold(storedIdentity(configDir(p.accounts, other)), identity) {
			fmt.Fprintf(p.out, "warning: profile %q is already signed in as %s; two profiles on one login share its usage limits\n", other.Name, identity)
		}
	}
}

func promptSecretFromTerminal() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("no terminal to read the token from; pipe it with --token-stdin")
	}
	fmt.Fprint(os.Stderr, "Paste the token (input hidden): ")
	data, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read token")
	}
	return string(data), nil
}
