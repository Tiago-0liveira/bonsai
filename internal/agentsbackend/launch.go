package agentsbackend

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var (
	_ agentsapi.LaunchResolver    = (*accounts)(nil)
	_ agentsapi.CachedDefaulter   = (*accounts)(nil)
	_ agentsapi.OperationSessions = (*sessions)(nil)
)

// describeTimeout bounds the read-only sign-in check of one resolution.
const describeTimeout = 5 * time.Second

// ResolveLaunch reports the launch an account would run with the overrides
// applied. It checks that the provider is installed and interactive, that the
// account is not busy and not known to be signed out, and asks the provider
// which options are effective. It never logs in, refreshes a credential, reads
// usage or starts the provider; an unknown sign-in state is not presented as
// signed in, and the runtime's own checks still refuse a launch that cannot run.
func (a *accounts) ResolveLaunch(ctx context.Context, id agents.AccountID, overrides agents.LaunchOptions) (agents.LaunchResolution, error) {
	if err := ctx.Err(); err != nil {
		return agents.LaunchResolution{}, err
	}
	if overrides.Prompt != "" || overrides.DisplayName != "" || overrides.ReadOnly || overrides.PlanFile != "" {
		return agents.LaunchResolution{}, fmt.Errorf("%w: the prompt, display name and read-only flag cannot be overridden", agents.ErrInvalidLaunch)
	}
	acct, err := a.rt.Accounts.Get(id)
	if err != nil {
		return agents.LaunchResolution{}, safeAccountError(err)
	}
	provider, err := a.rt.Registry.Get(acct.Provider)
	if err != nil {
		return agents.LaunchResolution{}, safeAccountError(err)
	}
	capabilities := provider.Capabilities()
	if !capabilities.Interactive {
		return agents.LaunchResolution{}, fmt.Errorf("%w: the provider has no interactive terminal", ErrAccountUnavailable)
	}
	// Only the availability half of Describer is needed.
	if d, ok := provider.(interface {
		Availability(context.Context) agents.Availability
	}); ok && !d.Availability(ctx).Available {
		return agents.LaunchResolution{}, fmt.Errorf("%w: the provider is not installed", ErrAccountUnavailable)
	}
	if err := a.busy(acct, capabilities); err != nil {
		return agents.LaunchResolution{}, err
	}
	if d, ok := provider.(agents.AccountDescriber); ok {
		dctx, cancel := context.WithTimeout(ctx, describeTimeout)
		info := d.DescribeAccount(dctx, acct)
		cancel()
		if err := ctx.Err(); err != nil {
			return agents.LaunchResolution{}, err
		}
		// "missing" is a stored token that is absent or unreadable; both it and a
		// signed-out profile are known unusable. An unknown status is not.
		if status, _ := info.Options["auth_status"].(string); status == "logged_out" || status == "missing" {
			return agents.LaunchResolution{}, fmt.Errorf("%w: %w", ErrAccountUnavailable, agents.ErrNotAuthenticated)
		}
	}
	resolver, ok := provider.(agents.LaunchResolver)
	if !ok {
		return agents.LaunchResolution{}, fmt.Errorf("%w: the provider cannot resolve launch options", ErrAccountUnavailable)
	}
	effective, err := resolver.ResolveLaunch(acct, overrides)
	if err != nil {
		if errors.Is(err, agents.ErrInvalidLaunch) {
			// The provider's own text could echo user input, so only the class is kept.
			return agents.LaunchResolution{}, fmt.Errorf("%w: the launch options are not valid for this provider", agents.ErrInvalidLaunch)
		}
		return agents.LaunchResolution{}, fmt.Errorf("%w: launch options could not be resolved", ErrAccountUnavailable)
	}
	effective.Prompt, effective.DisplayName, effective.ReadOnly, effective.PlanFile = "", "", false, ""
	if effective.FullAccess != nil {
		v := *effective.FullAccess
		effective.FullAccess = &v
	}
	out := agents.LaunchResolution{AccountID: acct.ID, AccountName: acct.Name, Provider: acct.Provider, Launch: effective, Inherited: inherited(overrides, effective)}
	if schema, ok := provider.(agents.LaunchSchema); ok {
		out.Fields = append([]agents.LaunchField(nil), schema.LaunchFields()...)
	}
	// Only models the provider already knows: ModelLister can start the provider
	// or open a credential session, which resolution must never do.
	if lister, ok := provider.(agents.CachedModelLister); ok {
		out.Models = lister.CachedModels(acct)
	}
	return out, nil
}

// busy mirrors the admission rule a start applies, so a preview refuses what the
// start would, with the same sentinels.
func (a *accounts) busy(acct agents.Account, c agents.Capabilities) error {
	records, err := agenthost.List(a.paths)
	if err != nil {
		return fmt.Errorf("%w: sessions could not be listed", ErrAccountUnavailable)
	}
	for _, r := range records {
		if !r.Alive || !r.Summary.Active() || r.Summary.Provider != acct.Provider {
			continue
		}
		if r.Summary.AccountID == acct.ID && !c.ConcurrentSameAccount {
			return fmt.Errorf("%w: %w", ErrAccountUnavailable, agents.ErrAccountBusy)
		}
		if r.Summary.AccountID != acct.ID && !c.ConcurrentCrossAccount {
			return fmt.Errorf("%w: %w", ErrAccountUnavailable, agents.ErrProviderBusy)
		}
	}
	return nil
}

// inherited marks the effective values the profile supplied: present in the
// result and not chosen by the caller. It is display only.
func inherited(overrides, effective agents.LaunchOptions) map[string]bool {
	out := map[string]bool{}
	mark := func(key, over, eff string) {
		if eff != "" && over == "" {
			out[key] = true
		}
	}
	mark("model", overrides.Model, effective.Model)
	mark("effort", overrides.Effort, effective.Effort)
	mark("sandbox", overrides.Sandbox, effective.Sandbox)
	mark("approval", overrides.Approval, effective.Approval)
	mark("permission_mode", overrides.PermissionMode, effective.PermissionMode)
	if effective.FullAccess != nil && overrides.FullAccess == nil {
		out["full_access"] = true
	}
	return out
}

func canonicalDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return dir
}

func operationSession(obs agenthost.OperationObservation) agentsapi.OperationSession {
	return agentsapi.OperationSession{Session: session(obs.Record), Acknowledged: obs.Acknowledged, Closed: obs.Closed}
}

// matches checks an observation is this request's: a receipt for another
// operation, task, project, tree, account or directory is never accepted as the
// answer.
func matches(obs agenthost.OperationObservation, req agentsapi.OperationStartRequest) bool {
	s := obs.Record.Summary
	return s.ID == string(req.SessionID) && s.OperationID == req.OperationID && s.Task != nil && *s.Task == req.Task &&
		s.ProjectID == req.ProjectID && s.WorktreeID == req.WorktreeID && s.AccountID == req.AccountID &&
		canonicalDir(obs.Record.WorkDir) == canonicalDir(req.WorkDir)
}

// operationError names only what the caller can act on and hides every other
// detail behind ErrSessionUnavailable.
func operationError(prefix string, err error) error {
	for _, known := range []error{
		agenthost.ErrOperationConflict, agenthost.ErrOperationClosed, agenthost.ErrOperationUnknown,
		agents.ErrReadOnlyUnsupported, agents.ErrAccountBusy, agents.ErrProviderBusy, agents.ErrInvalidLaunch,
	} {
		if errors.Is(err, known) {
			return fmt.Errorf("%s: %w: %w", prefix, known, ErrSessionUnavailable)
		}
	}
	return fmt.Errorf("%s: %w", prefix, ErrSessionUnavailable)
}

// StartOperation starts the holder once for a durable operation identity. Repeating
// the identical request returns the receipt; a different request under the same
// identity is a conflict, and a closed receipt never starts a holder.
func (s *sessions) StartOperation(ctx context.Context, req agentsapi.OperationStartRequest) (agentsapi.OperationSession, error) {
	if !s.Supported() {
		return agentsapi.OperationSession{}, fmt.Errorf("start agent session: %w", ErrSessionUnavailable)
	}
	obs, err := s.starter.StartOperation(ctx, agenthost.OperationStartRequest{
		StartRequest: agenthost.StartRequest{
			Project: req.ProjectID, Tree: req.WorktreeID, Dir: req.WorkDir, Name: req.Name,
			AccountID: req.AccountID, Cols: req.Cols, Rows: req.Rows, Launch: req.Launch,
		},
		OperationID: req.OperationID, SessionID: req.SessionID, Task: req.Task,
	})
	if err != nil {
		if ctx.Err() != nil && obs.Record.ID == "" {
			return agentsapi.OperationSession{}, ctx.Err()
		}
		out := agentsapi.OperationSession{}
		if obs.Record.ID != "" && matches(obs, req) {
			out = operationSession(obs)
		}
		return out, operationError("start agent session", err)
	}
	if !matches(obs, req) {
		return agentsapi.OperationSession{}, operationError("start agent session", agenthost.ErrOperationConflict)
	}
	return operationSession(obs), nil
}

// QueryOperation reads, and where no holder exists fences, a durable start
// identity. It starts nothing.
func (s *sessions) QueryOperation(ctx context.Context, id agents.SessionID, operationID string) (agentsapi.OperationSession, error) {
	if !s.Supported() {
		return agentsapi.OperationSession{}, fmt.Errorf("query agent session: %w", ErrSessionUnavailable)
	}
	obs, err := agenthost.QueryOperation(ctx, s.paths, id, operationID)
	if err != nil {
		if ctx.Err() != nil {
			return agentsapi.OperationSession{}, ctx.Err()
		}
		return agentsapi.OperationSession{}, operationError("query agent session", err)
	}
	if obs.Record.ID != string(id) || obs.Record.Summary.OperationID != operationID {
		return agentsapi.OperationSession{}, operationError("query agent session", agenthost.ErrOperationConflict)
	}
	return operationSession(obs), nil
}
