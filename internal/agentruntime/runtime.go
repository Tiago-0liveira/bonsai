package agentruntime

import (
	"io"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
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
	registry := agents.NewRegistry()
	if err := registry.Register(antigravity.New(accounts, sessions, launcher)); err != nil {
		return nil, err
	}
	if err := registry.Register(claude.New(accounts, sessions, launcher, out)); err != nil {
		return nil, err
	}
	return &Runtime{
		Accounts: accounts, Sessions: sessions, Registry: registry,
		AccountService: &agents.AccountService{
			Store: accounts, Sessions: sessions, Registry: registry,
			Launcher: launcher, Cache: cache,
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
