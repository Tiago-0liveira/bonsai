package agentruntime

import (
	"io"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthook"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
)

type Runtime struct {
	Accounts       agents.AccountStore
	Sessions       agents.SessionStore
	Registry       *agents.Registry
	AccountService *agents.AccountService
	SessionService *agents.SessionService
	UsageService   *agents.UsageService
}

func New(in io.Reader, out, errOut io.Writer) (*Runtime, error) {
	accounts, err := agents.NewFileAccountStore("")
	if err != nil {
		return nil, err
	}
	sessions, err := agents.NewFileSessionStore("")
	if err != nil {
		return nil, err
	}
	cache, err := agents.NewFileUsageCache("")
	if err != nil {
		return nil, err
	}
	launcher := agents.NewForegroundLauncher(in, out, errOut)
	registry, err := registerProviders(accounts, sessions)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Accounts: accounts, Sessions: sessions, Registry: registry,
		AccountService: &agents.AccountService{
			Store: accounts, Sessions: sessions, Registry: registry,
			Launcher: launcher, Cache: cache, Out: out,
		},
		SessionService: &agents.SessionService{
			Accounts: accounts, Sessions: sessions, Registry: registry, Launcher: launcher,
		},
		UsageService: &agents.UsageService{
			Accounts: accounts, Sessions: sessions, Registry: registry,
			Cache: cache, TTL: time.Minute, WorkerLimit: 8,
		},
	}, nil
}

// registerProviders is the one place that lists the providers.
func registerProviders(accounts agents.AccountStore, sessions agents.SessionStore) (*agents.Registry, error) {
	registry := agents.NewRegistry()
	for _, p := range []agents.Provider{antigravity.New(accounts, sessions), claude.New(accounts, sessions), codex.New(accounts, sessions)} {
		if err := registry.Register(p); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// RunHook runs `bonsai __agent-hook <args...>` and returns its exit code, which
// is always 0. It runs inside the agent's hook timeout for every tool call, so
// it takes the lightweight path: providers without stores (hooks only
// translate payloads) and no config, account or usage setup, which would touch
// the disk.
func RunHook(args []string, stdin io.Reader, stdout io.Writer) int {
	start := time.Now()
	root, _ := agents.DefaultSessionsRoot()
	return agenthook.Run(agenthook.Options{
		Args: args, Stdin: stdin, Stdout: stdout, SessionsRoot: root,
		Now: func() time.Time { return start },
		Lookup: func(id string) (agents.ActivityReporter, bool) {
			registry, err := registerProviders(nil, nil)
			if err != nil {
				return nil, false
			}
			p, err := registry.Get(agents.ProviderID(id))
			if err != nil {
				return nil, false
			}
			reporter, ok := p.(agents.ActivityReporter)
			return reporter, ok
		},
	})
}
