// Package codex runs the OpenAI Codex CLI. Each profile owns a persistent
// CODEX_HOME shared by all of its sessions; Bonsai never copies, stores or
// refreshes Codex credentials.
package codex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const ProviderID agents.ProviderID = "codex"

const (
	versionTimeout = 5 * time.Second
	statusTTL      = 60 * time.Second
	// unknownStatusTTL is how long a failed `login status` is not retried.
	unknownStatusTTL = 10 * time.Second
	logoutTimeout    = 10 * time.Second
)

// CommandRunner runs a short, non-interactive codex command with stdin as its
// input and returns its standard output. A non-zero exit still returns the
// output with the error.
type CommandRunner interface {
	Output(ctx context.Context, prepared agents.PreparedSession, stdin io.Reader) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, prepared agents.PreparedSession, stdin io.Reader) ([]byte, error) {
	cmd := exec.CommandContext(ctx, prepared.Executable, prepared.Args...)
	cmd.Dir = prepared.Dir
	cmd.Env = prepared.Environment(os.Environ())
	cmd.Stdin = stdin
	isolate(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	return stdout.Bytes(), err
}

// statusTimeout bounds `codex login status`; a variable so tests can shorten it.
var statusTimeout = 5 * time.Second

type statusEntry struct {
	status    authStatus
	fetchedAt time.Time
}

type Provider struct {
	accounts agents.AccountStore
	sessions agents.SessionStore
	launcher agents.Launcher
	out      io.Writer

	binaryResolver BinaryResolver
	runner         CommandRunner
	promptSecret   func() (string, error)
	now            func() time.Time
	hostEnv        func(string) string
	hostHome       func() (string, error)
	// scratchRoot holds throwaway CODEX_HOMEs (version probe). It is not the
	// system temp dir: Codex refuses to install its helpers under /tmp and warns.
	scratchRoot func() (string, error)

	versionMu sync.Mutex
	version   string

	statusMu sync.Mutex
	statuses map[agents.AccountID]statusEntry
}

// New builds the Codex provider. out receives setup progress (what was seeded,
// duplicate-identity warnings); nil discards it.
func New(accounts agents.AccountStore, sessions agents.SessionStore, launcher agents.Launcher, out io.Writer) *Provider {
	if out == nil {
		out = io.Discard
	}
	return &Provider{
		accounts:       accounts,
		sessions:       sessions,
		launcher:       launcher,
		out:            out,
		binaryResolver: PathBinaryResolver{},
		runner:         execRunner{},
		promptSecret:   promptSecretFromTerminal,
		now:            time.Now,
		hostEnv:        os.Getenv,
		hostHome:       os.UserHomeDir,
		scratchRoot:    agents.DefaultAgentsCacheRoot,
		statuses:       make(map[agents.AccountID]statusEntry),
	}
}

func (p *Provider) ID() agents.ProviderID { return ProviderID }

// Capabilities leaves Usage off until rate limits are read through the app-server.
func (p *Provider) Capabilities() agents.Capabilities {
	return agents.Capabilities{
		Interactive:            true,
		MultiAccount:           true,
		ConcurrentSameAccount:  true,
		ConcurrentCrossAccount: true,
	}
}

func (p *Provider) Label() string { return "Codex" }

func (p *Provider) Availability(ctx context.Context) agents.Availability {
	version, err := p.installedVersion(ctx)
	switch {
	case err == nil:
	case isMissingBinary(err):
		return agents.Availability{Reason: "Install Codex CLI and restart Bonsai"}
	default:
		return agents.Availability{Reason: "Cannot determine the Codex version"}
	}
	found, text, _ := parseVersion(version)
	minimum, _, _ := parseVersion(MinVersion)
	if found.less(minimum) {
		return agents.Availability{Reason: fmt.Sprintf("Update Codex (found %s, need %s)", text, MinVersion), Version: text}
	}
	return agents.Availability{Available: true, Version: text}
}

// Usage is not available yet; UsageService never calls it while the Usage
// capability is off.
func (p *Provider) Usage(context.Context, agents.Account, agents.UsageOptions) (agents.UsageSnapshot, error) {
	return agents.UsageSnapshot{}, fmt.Errorf("%w: codex", agents.ErrUsageUnsupported)
}

type missingBinaryError struct{ err error }

func (e missingBinaryError) Error() string { return e.err.Error() }
func (e missingBinaryError) Unwrap() error { return e.err }

func isMissingBinary(err error) bool {
	_, ok := err.(missingBinaryError)
	return ok
}

// installedVersion runs `codex --version`. Only successes are cached, so
// installing or updating Codex while Bonsai runs is picked up.
func (p *Provider) installedVersion(ctx context.Context) (string, error) {
	p.versionMu.Lock()
	defer p.versionMu.Unlock()
	if p.version != "" {
		return p.version, nil
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return "", missingBinaryError{err}
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	// Whatever the binary writes at startup must not land in the user's real
	// ~/.codex, so it gets a throwaway home.
	root, err := p.scratchRoot()
	if err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	scratch, err := os.MkdirTemp(root, "codex-version-")
	if err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	defer os.RemoveAll(scratch)
	out, err := p.runner.Output(ctx, agents.PreparedSession{
		Executable: executable, Args: []string{"--version"}, Dir: scratch,
		EnvUnsetPrefixes: scrubPrefixes, EnvSet: map[string]string{"CODEX_HOME": scratch},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	if _, text, ok := parseVersion(string(out)); ok {
		p.version = text
		return text, nil
	}
	return "", fmt.Errorf("codex --version: unrecognized output")
}

// requireSupported fails with an actionable message when Codex is missing or
// too old.
func (p *Provider) requireSupported(ctx context.Context) error {
	if a := p.Availability(ctx); !a.Available {
		return fmt.Errorf("%s", a.Reason)
	}
	return nil
}

var (
	_ agents.Provider         = (*Provider)(nil)
	_ agents.Describer        = (*Provider)(nil)
	_ agents.LaunchValidator  = (*Provider)(nil)
	_ agents.LaunchSchema     = (*Provider)(nil)
	_ agents.AccountDescriber = (*Provider)(nil)
	_ agents.AccountRemover   = (*Provider)(nil)
)
