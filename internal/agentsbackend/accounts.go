package agentsbackend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// ErrAccountInUse is returned by Remove while the account has active sessions.
var ErrAccountInUse = errors.New("account has running sessions; stop them first")

var _ agentsapi.Accounts = (*accounts)(nil)

// accounts adapts the agent runtime's account and usage services to the TUI
// contract. It reads stored state only and never refreshes a provider login.
type accounts struct {
	rt      *agentruntime.Runtime
	paths   agenthost.Paths
	selfExe string

	mu   sync.Mutex
	last map[agents.AccountID]agentsapi.Usage // last good usage per account
}

func newAccounts(rt *agentruntime.Runtime, paths agenthost.Paths, selfExe string) *accounts {
	return &accounts{rt: rt, paths: paths, selfExe: selfExe, last: map[agents.AccountID]agentsapi.Usage{}}
}

// running counts active, alive holder sessions per account.
func (a *accounts) running() (map[agents.AccountID][]string, error) {
	out := map[agents.AccountID][]string{}
	records, err := agenthost.List(a.paths)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Alive && r.Summary.Active() {
			out[r.Summary.AccountID] = append(out[r.Summary.AccountID], r.ID)
		}
	}
	return out, nil
}

func (a *accounts) List(ctx context.Context) ([]agentsapi.Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := a.rt.AccountService.List(ctx)
	if err != nil {
		return nil, safeAccountError(err)
	}
	running, err := a.running()
	if err != nil {
		return nil, ErrAccountUnavailable
	}
	out := make([]agentsapi.Account, 0, len(list))
	for _, acct := range list {
		item := agentsapi.Account{Account: acct, Running: len(running[acct.ID])}
		if p, err := a.rt.Registry.Get(acct.Provider); err == nil {
			item.ConcurrentSameAccount = p.Capabilities().ConcurrentSameAccount
			item.ReadOnly = agents.ReadOnlyOf(p)
			item.Structured = agents.StructuredOf(p)
		}
		out = append(out, item)
	}
	return out, nil
}

func (a *accounts) Describe(ctx context.Context, id agents.AccountID) (agents.AccountInfo, error) {
	if err := ctx.Err(); err != nil {
		return agents.AccountInfo{}, err
	}
	acct, err := a.rt.Accounts.Get(id)
	if err != nil {
		return agents.AccountInfo{}, safeAccountError(err)
	}
	provider, err := a.rt.Registry.Get(acct.Provider)
	if err != nil {
		return agents.AccountInfo{}, safeAccountError(err)
	}
	d, ok := provider.(agents.AccountDescriber)
	if !ok {
		return agents.AccountInfo{}, nil
	}
	info := d.DescribeAccount(ctx, acct)
	if err := ctx.Err(); err != nil {
		return agents.AccountInfo{}, err
	}
	return info, nil
}

// Usage is the only call that may reach a provider. refresh=false serves a
// stored snapshot no older than agents.UsageMaxAge and fetches otherwise;
// refresh=true always fetches.
func (a *accounts) Usage(ctx context.Context, id agents.AccountID, refresh bool) (agentsapi.Usage, error) {
	return a.fetchUsage(ctx, id, agents.UsageOptions{Refresh: refresh, MaxAge: agents.UsageMaxAge})
}

// UsageStored reads the persisted snapshot only; it never calls the provider.
func (a *accounts) UsageStored(ctx context.Context, id agents.AccountID) (agentsapi.Usage, bool, error) {
	if err := ctx.Err(); err != nil {
		return agentsapi.Usage{}, false, err
	}
	if a.rt.UsageService.Cache == nil {
		return agentsapi.Usage{}, false, nil
	}
	acct, err := a.rt.Accounts.Get(id)
	if err != nil {
		return agentsapi.Usage{}, false, safeAccountError(err)
	}
	snap, ok, err := a.rt.UsageService.Cache.Get(acct.Provider, id)
	if err != nil {
		return agentsapi.Usage{}, false, errors.New(sanitizeUsageError(err))
	}
	if !ok {
		return agentsapi.Usage{}, false, nil
	}
	return a.usage(snap), true, nil
}

func (a *accounts) fetchUsage(ctx context.Context, id agents.AccountID, opts agents.UsageOptions) (agentsapi.Usage, error) {
	snap, err := a.rt.UsageService.Account(ctx, id, opts)
	if err != nil {
		if errors.Is(err, agents.ErrAccountNotFound) {
			return agentsapi.Usage{}, agents.ErrAccountNotFound
		}
		msg := sanitizeUsageError(err)
		a.mu.Lock()
		prev, ok := a.last[id]
		a.mu.Unlock()
		if !ok {
			acct, getErr := a.rt.Accounts.Get(id)
			if getErr == nil && a.rt.UsageService.Cache != nil {
				cached, found, cacheErr := a.rt.UsageService.Cache.Get(acct.Provider, id)
				if cacheErr == nil && found {
					prev = a.usage(cached)
					ok = true
				}
			}
			if !ok {
				return agentsapi.Usage{}, errors.New(msg)
			}
		}
		prev.Stale, prev.Err = true, msg
		return prev, nil
	}
	u := a.usage(snap)
	a.mu.Lock()
	a.last[id] = u
	a.mu.Unlock()
	return u, nil
}

func (a *accounts) usage(snap agents.UsageSnapshot) agentsapi.Usage {
	u := agentsapi.Usage{Snapshot: snap, FetchedAt: snap.FetchedAt}
	if h := a.rt.UsageService.Headline(snap); h != nil {
		u.Headline = *h
	}
	return u
}

// sanitizeUsageError maps a usage failure to a fixed short message: provider
// errors can carry response bodies, paths or tokens.
func sanitizeUsageError(err error) string {
	switch {
	case errors.Is(err, agents.ErrUsageUnsupported):
		return "Usage is not supported for this profile"
	case errors.Is(err, agents.ErrNotAuthenticated):
		return "Not signed in"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "Usage request timed out"
	default:
		return "Usage is unavailable right now"
	}
}

func (a *accounts) Defaults(ctx context.Context, id agents.AccountID) (agentsapi.Defaults, error) {
	return a.defaults(ctx, id, true)
}

// CachedDefaults is Defaults for a form that opens before a confirmation: model
// choices come only from what the provider already knows. It never starts a
// provider, opens a credential session or writes anything.
func (a *accounts) CachedDefaults(ctx context.Context, id agents.AccountID) (agentsapi.Defaults, error) {
	return a.defaults(ctx, id, false)
}

func (a *accounts) defaults(ctx context.Context, id agents.AccountID, live bool) (agentsapi.Defaults, error) {
	if err := ctx.Err(); err != nil {
		return agentsapi.Defaults{}, err
	}
	acct, err := a.rt.Accounts.Get(id)
	if err != nil {
		return agentsapi.Defaults{}, safeAccountError(err)
	}
	provider, err := a.rt.Registry.Get(acct.Provider)
	if err != nil {
		return agentsapi.Defaults{}, safeAccountError(err)
	}
	d := agentsapi.Defaults{Values: map[string]string{}}
	if editor, ok := provider.(agents.ProfileEditor); ok {
		d.Fields = editor.ProfileFields()
		for k, v := range editor.ProfileDefaults(acct) {
			d.Values[k] = v
		}
	}
	if schema, ok := provider.(agents.LaunchSchema); ok {
		d.LaunchFields = schema.LaunchFields()
	}
	if !live {
		if lister, ok := provider.(agents.CachedModelLister); ok {
			d.Models = lister.CachedModels(acct)
		}
		return d, nil
	}
	if lister, ok := provider.(agents.ModelLister); ok {
		// Providers return their static list alongside an error; the form
		// still works with it, so the error is not the caller's problem.
		d.Models, _ = lister.Models(ctx, acct)
	}
	return d, nil
}

func (a *accounts) UpdateDefaults(ctx context.Context, id agents.AccountID, values map[string]*string) error {
	_, err := a.rt.AccountService.UpdateDefaults(ctx, id, values)
	return safeAccountError(err)
}

func (a *accounts) Remove(ctx context.Context, id agents.AccountID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	running, err := a.running()
	if err != nil {
		return ErrAccountUnavailable
	}
	if ids := running[id]; len(ids) > 0 {
		return fmt.Errorf("%w (%d running)", ErrAccountInUse, len(ids))
	}
	var scanErr error
	_, err = a.rt.AccountService.Remove(ctx, id, agents.RemoveOptions{
		// Closes the window between the check above and the removal.
		InUse: func(id agents.AccountID) []string {
			current, err := a.running()
			if err != nil {
				scanErr = err
				return []string{"unknown"} // Fail closed; RemoveOptions cannot return an error.
			}
			return current[id]
		},
	})
	if scanErr != nil {
		return ErrAccountUnavailable
	}
	if errors.Is(err, agents.ErrAccountInUse) {
		return fmt.Errorf("%w", ErrAccountInUse)
	}
	return safeAccountError(err)
}

// Service and provider errors may contain paths, settings or response bodies.
func safeAccountError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, agents.ErrAccountNotFound):
		return agents.ErrAccountNotFound
	case errors.Is(err, agents.ErrProviderNotFound):
		return agents.ErrProviderNotFound
	case errors.Is(err, agents.ErrInvalidProfile):
		return agents.ErrInvalidProfile
	case errors.Is(err, agents.ErrInvalidAccountName):
		return agents.ErrInvalidAccountName
	default:
		return ErrAccountUnavailable
	}
}

func (a *accounts) AddCommand(provider agents.ProviderID, name string) *exec.Cmd {
	return exec.Command(a.selfExe, "agent", "account", "add", string(provider), name)
}

func (a *accounts) LoginCommand(id agents.AccountID) *exec.Cmd {
	// The CLI resolves a selector as account ID, name or provider/name; the
	// ID is unambiguous.
	return exec.Command(a.selfExe, "agent", "account", "login", string(id))
}
