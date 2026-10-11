package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	// maxKeyInput bounds API key input; real keys are far shorter.
	maxKeyInput  = 8 << 10
	loginTimeout = 15 * time.Second
)

// apiKeyRE accepts OpenAI API keys (sk-..., sk-proj-..., sk-svcacct-...).
var apiKeyRE = regexp.MustCompile(`^sk-[A-Za-z0-9_-]{20,400}$`)

// setupMode picks the auth mode. A key on --token-stdin implies api-key mode.
func setupMode(options agents.SetupOptions) (string, error) {
	mode := options.AuthMode
	switch {
	case mode == "" && options.Secret != nil:
		mode = AuthAPIKey
	case mode == "":
		mode = AuthChatGPT
	case !slices.Contains(authModes, mode):
		return "", fmt.Errorf("unknown auth mode %q (use %s)", mode, strings.Join(authModes, ", "))
	}
	if mode != AuthAPIKey && options.Secret != nil {
		return "", fmt.Errorf("--token-stdin needs --auth api-key")
	}
	if options.Seed != nil && !*options.Seed && options.SeedFrom != "" {
		return "", fmt.Errorf("--no-seed and --seed-from cannot be combined")
	}
	return mode, nil
}

// SetupAccount creates the profile's CODEX_HOME, seeds it, forces the file
// credential store and signs in with Codex's own login. Codex writes auth.json;
// Bonsai keeps no copy of any secret.
func (p *Provider) SetupAccount(ctx context.Context, req agents.SetupRequest) (agents.SetupResult, error) {
	mode, err := setupMode(req.Options)
	if err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.requireSupported(ctx); err != nil {
		return agents.SetupResult{}, err
	}
	home := homeDir(p.accounts, req.Account)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return agents.SetupResult{}, err
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return agents.SetupResult{}, err
	}
	settings := Settings{AuthMode: mode}
	if req.Options.Seed == nil || *req.Options.Seed {
		if settings.SeededFrom, err = p.seed(req.Options.SeedFrom, home); err != nil {
			return agents.SetupResult{}, err
		}
	}
	if err := ensureFileStore(home); err != nil {
		return agents.SetupResult{}, fmt.Errorf("configure the credential store: %w", err)
	}
	account := req.Account
	if mode == AuthAPIKey {
		err = p.loginAPIKey(ctx, account, req.Options.Secret)
	} else {
		err = p.loginInteractive(ctx, account, mode == AuthDevice)
	}
	if err != nil {
		return agents.SetupResult{}, err
	}
	if err := p.requireLoggedIn(ctx, account); err != nil {
		return agents.SetupResult{}, err
	}
	id := readIdentity(home)
	if other := p.sameLoginAs(account.ID, id); other != "" {
		fmt.Fprintf(p.out, "warning: profile %q is already signed in as %s; two profiles on one login share its usage limits\n", other, id.Email)
	}
	account.Settings = settings.raw()
	return agents.SetupResult{Account: account}, nil
}

// loginInteractive runs `codex login` (browser) or `codex login --device-auth`
// (a code to enter on another device; for SSH, WSL or headless hosts).
func (p *Provider) loginInteractive(ctx context.Context, account agents.Account, device bool) error {
	args := []string{"login"}
	if device {
		args = append(args, "--device-auth")
	}
	prepared, err := p.profileCommand(account, args...)
	if err != nil {
		return err
	}
	if err := p.launcher.RunForeground(ctx, prepared); err != nil {
		return fmt.Errorf("codex login failed: %w", err)
	}
	return nil
}

// loginAPIKey pipes the key from --token-stdin or a hidden prompt into
// `codex login --with-api-key`. The key never reaches an argument, a log or an
// error, and Codex's own output is discarded.
func (p *Provider) loginAPIKey(ctx context.Context, account agents.Account, secret io.Reader) error {
	var key string
	if secret != nil {
		data, err := io.ReadAll(io.LimitReader(secret, maxKeyInput+1))
		if err != nil || len(data) > maxKeyInput {
			return fmt.Errorf("cannot read the API key")
		}
		key = string(data)
	} else {
		var err error
		if key, err = p.promptSecret(); err != nil {
			return err
		}
	}
	key = strings.TrimSpace(key)
	if !apiKeyRE.MatchString(key) {
		return fmt.Errorf("not a valid OpenAI API key (expected sk-...)")
	}
	prepared, err := p.profileCommand(account, "login", "--with-api-key")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	if _, err := p.runner.Output(ctx, prepared, strings.NewReader(key+"\n")); err != nil {
		return fmt.Errorf("codex login --with-api-key failed")
	}
	return nil
}

func (p *Provider) requireLoggedIn(ctx context.Context, account agents.Account) error {
	prepared, err := p.profileCommand(account, "login", "status")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	if _, err := p.runner.Output(ctx, prepared, nil); statusFromExit(err) != statusLoggedIn {
		return fmt.Errorf("codex login did not complete")
	}
	return nil
}

func promptSecretFromTerminal() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("no terminal to read the API key from; pipe it with --token-stdin")
	}
	fmt.Fprint(os.Stderr, "Paste the OpenAI API key (input hidden): ")
	data, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read the API key")
	}
	return string(data), nil
}
