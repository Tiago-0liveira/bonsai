package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const ProviderID agents.ProviderID = "claude"

const (
	versionTimeout = 5 * time.Second
	statusTTL      = 60 * time.Second
	// unknownStatusTTL is how long a failed `auth status` is not retried.
	unknownStatusTTL = 10 * time.Second
	logoutTimeout    = 10 * time.Second
)

// RunOptions are the extras of CommandRunner.Run.
type RunOptions struct {
	// Stdin is written to the process's standard input. Nil means no input.
	Stdin []byte
	// MaxStdout kills the process once it writes more than this many bytes and
	// fails with errOutputTooLarge. Zero means no cap.
	MaxStdout int64
}

// errOutputTooLarge reports a run killed for exceeding RunOptions.MaxStdout.
var errOutputTooLarge = errors.New("command output exceeded the cap")

// CommandRunner runs a short, non-interactive claude command and returns its
// standard output. A non-zero exit still returns the output with the error.
// The whole process group dies when ctx ends.
type CommandRunner interface {
	Output(context.Context, agents.PreparedSession) ([]byte, error)
	Run(context.Context, agents.PreparedSession, RunOptions) ([]byte, error)
}

type execRunner struct{}

func (r execRunner) Output(ctx context.Context, prepared agents.PreparedSession) ([]byte, error) {
	return r.Run(ctx, prepared, RunOptions{})
}

func (execRunner) Run(ctx context.Context, prepared agents.PreparedSession, opts RunOptions) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, prepared.Executable, prepared.Args...)
	cmd.Dir = prepared.Dir
	cmd.Env = prepared.Environment(os.Environ())
	stdout := &capWriter{max: opts.MaxStdout, cancel: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	isolate(cmd)
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if stdout.exceeded {
		return nil, errOutputTooLarge
	}
	return stdout.buf.Bytes(), err
}

// capWriter buffers output and cancels the run when it passes max.
type capWriter struct {
	buf      bytes.Buffer
	max      int64
	exceeded bool
	cancel   context.CancelFunc
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.max > 0 && int64(w.buf.Len()+len(p)) > w.max {
		w.exceeded = true
		w.cancel()
		return 0, errOutputTooLarge
	}
	return w.buf.Write(p)
}

// statusTimeout bounds `claude auth status`; a variable so tests can shorten it.
var statusTimeout = 5 * time.Second

type statusEntry struct {
	status    authStatus
	known     bool
	fetchedAt time.Time
}

type Provider struct {
	accounts agents.AccountStore
	sessions agents.SessionStore

	binaryResolver BinaryResolver
	runner         CommandRunner
	now            func() time.Time
	hostEnv        func(string) string
	hostHome       func() (string, error)

	// Usage endpoint plumbing; tests replace these.
	usageEndpoint string
	usageClient   *http.Client
	usageOS       string

	modelsEndpoint string
	modelsMu       sync.Mutex
	modelsCache    map[agents.AccountID]modelsEntry

	usageMu       sync.Mutex
	usageFailures map[agents.AccountID]usageFailure

	versionMu sync.Mutex
	version   string

	statusMu sync.Mutex
	statuses map[agents.AccountID]statusEntry
}

// New builds the Claude provider. Setup talks to the user only through each
// request's SetupIO.
func New(accounts agents.AccountStore, sessions agents.SessionStore) *Provider {
	return &Provider{
		accounts:       accounts,
		sessions:       sessions,
		binaryResolver: PathBinaryResolver{},
		runner:         execRunner{},
		now:            time.Now,
		hostEnv:        os.Getenv,
		hostHome:       os.UserHomeDir,
		statuses:       make(map[agents.AccountID]statusEntry),
		usageEndpoint:  UsageEndpoint,
		modelsEndpoint: ModelsEndpoint,
	}
}

func (p *Provider) ID() agents.ProviderID { return ProviderID }

func (p *Provider) Capabilities() agents.Capabilities {
	return agents.Capabilities{
		Interactive:            true,
		Usage:                  true,
		MultiAccount:           true,
		ConcurrentSameAccount:  true,
		ConcurrentCrossAccount: true,
	}
}

// installHint and updateHint are shown while Claude Code is missing or too old.
const (
	installHint = "npm install -g @anthropic-ai/claude-code"
	updateHint  = "claude update"
)

func (p *Provider) Label() string { return "Claude Code" }

func (p *Provider) Availability(ctx context.Context) agents.Availability {
	version, err := p.installedVersion(ctx)
	switch {
	case err == nil:
	case isMissingBinary(err):
		return agents.Availability{Reason: "Install Claude Code and restart Bonsai", Hint: installHint}
	default:
		return agents.Availability{Reason: "Cannot determine the Claude Code version"}
	}
	found, text, _ := parseVersion(version)
	minimum, _, _ := parseVersion(MinVersion)
	if found.less(minimum) {
		return agents.Availability{Reason: fmt.Sprintf("Update Claude Code (found %s, need %s)", text, MinVersion), Version: text, Hint: updateHint}
	}
	return agents.Availability{Available: true, Version: text}
}

type missingBinaryError struct{ err error }

func (e missingBinaryError) Error() string { return e.err.Error() }
func (e missingBinaryError) Unwrap() error { return e.err }

func isMissingBinary(err error) bool {
	_, ok := err.(missingBinaryError)
	return ok
}

// installedVersion runs `claude --version`. Only successes are cached, so
// installing or updating Claude Code while Bonsai runs is picked up.
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
	// ~/.claude, so it gets a throwaway config dir.
	scratch, err := os.MkdirTemp("", "bonsai-claude-version-")
	if err != nil {
		return "", fmt.Errorf("claude --version: %w", err)
	}
	defer os.RemoveAll(scratch)
	out, err := p.runner.Output(ctx, agents.PreparedSession{
		Executable: executable, Args: []string{"--version"}, EnvUnsetPrefixes: scrubPrefixes,
		EnvSet: map[string]string{"CLAUDE_CONFIG_DIR": scratch},
	})
	if err != nil {
		return "", fmt.Errorf("claude --version: %w", err)
	}
	if _, text, ok := parseVersion(string(out)); ok {
		p.version = text
		return text, nil
	}
	return "", fmt.Errorf("claude --version: unrecognized output")
}

// requireSupported fails with an actionable message when Claude Code is
// missing or too old.
func (p *Provider) requireSupported(ctx context.Context) error {
	if a := p.Availability(ctx); !a.Available {
		return fmt.Errorf("%s", a.Reason)
	}
	return nil
}

var (
	_ agents.Provider             = (*Provider)(nil)
	_ agents.Describer            = (*Provider)(nil)
	_ agents.LaunchValidator      = (*Provider)(nil)
	_ agents.ReadOnlyEnforcer     = (*Provider)(nil)
	_ agents.AccountDescriber     = (*Provider)(nil)
	_ agents.AccountRemover       = (*Provider)(nil)
	_ agents.SetupSchema          = (*Provider)(nil)
	_ agents.AccountAuthenticator = (*Provider)(nil)
	_ agents.ActivityReporter     = (*Provider)(nil)
	_ agents.StructuredRunner     = (*Provider)(nil)
)
