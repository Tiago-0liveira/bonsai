package agenthost

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// admitTimeout bounds the wait for the start lock. A holder launch takes a few
// seconds at most, so a longer wait means a stuck starter.
const admitTimeout = 15 * time.Second

// Starter launches holders with admission that holds across processes: the
// web API and the TUI start sessions independently, so the in-memory count
// one Manager keeps cannot see the other's sessions.
type Starter struct {
	Accounts agents.AccountStore
	Sessions agents.SessionStore
	Registry *agents.Registry
	Paths    Paths
	Launch   Launcher // nil = Launch
	Origin   string   // "web" | "tui" | "cli"
}

// StartRequest is one session to start.
type StartRequest struct {
	Project, Tree, Name, Dir string
	AccountID                agents.AccountID
	Cols, Rows               int
	Launch                   agents.LaunchOptions
}

// Start validates the launch, admits it against the sessions every process has
// running, creates the session's runtime and starts its holder. It returns the
// holder's record, whose session is starting until the holder reports it
// running.
func (s Starter) Start(ctx context.Context, req StartRequest) (_ Record, startErr error) {
	prep, err := s.prepare(req)
	if err != nil {
		return Record{}, err
	}
	account, launch, capabilities := prep.account, prep.launch, prep.capabilities

	// The lock is held until the record exists, so the next starter, in this
	// process or another, counts this session.
	lock, err := s.admit(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() {
		if lock != nil {
			_ = lock.Unlock()
		}
	}()

	records, err := List(s.Paths)
	if err != nil {
		return Record{}, fmt.Errorf("cannot list agent sessions")
	}
	if err := checkAdmission(records, account, req.AccountID, capabilities); err != nil {
		return Record{}, err
	}
	runtime, err := s.Sessions.Create(account, req.Dir)
	if err != nil {
		return Record{}, fmt.Errorf("cannot create session")
	}
	name := req.Name
	if name == "" {
		name = account.Name
	}
	summary := Summary{ID: string(runtime.ID), ProjectID: req.Project, WorktreeID: req.Tree, AccountID: req.AccountID, Provider: account.Provider, ProfileName: account.Name, Name: name, State: "starting", CreatedAt: time.Now().UTC(), Model: launch.Model, Options: launch.Values(), Origin: s.Origin}
	launcher := s.Launch
	if launcher == nil {
		launcher = Launch
	}
	// Any exit but success, a launcher panic included, must not leave a
	// runtime dir or a holder without one.
	launched, ok := false, false
	defer func() {
		if ok {
			return
		}
		if launched {
			// Keep an unresponsive holder visible to admission if launch failed
			// before it wrote its own record.
			r, err := Read(s.Paths, summary.ID)
			if err != nil {
				r = Record{Version: RecordVersion, ID: summary.ID, Socket: s.Paths.socket(summary.ID), Protocol: Version, Summary: summary}
				if writeErr := writeRecord(s.Paths, r); writeErr != nil {
					startErr = errors.Join(startErr, fmt.Errorf("cannot preserve agent admission record: %w", writeErr))
				}
			}
			stopErr := StopHolder(s.Paths, r, "")
			if !Wait(s.Paths, summary.ID, 5*time.Second) {
				startErr = errors.Join(startErr, fmt.Errorf("agent holder %s did not exit after stop; it may still be running", summary.ID), stopErr)
				// A still-running holder needs its runtime directory.
				return
			}
		}
		if r, err := Read(s.Paths, summary.ID); err == nil && r.Generation == "" {
			_ = Remove(s.Paths, summary.ID)
		}
		_ = s.Sessions.Cleanup(runtime)
	}()
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("agent start cancelled: %w", err)
	}
	launched = true
	if err := launcher(ctx, s.Paths, Spec{Summary: summary, WorkDir: req.Dir, Cols: req.Cols, Rows: req.Rows, Launch: launch}); err != nil {
		return Record{}, fmt.Errorf("cannot start agent host: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("agent start cancelled: %w", err)
	}
	record, err := Read(s.Paths, summary.ID)
	if err != nil {
		return Record{}, fmt.Errorf("cannot start agent host: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("agent start cancelled: %w", err)
	}
	ok = true
	return record, nil
}

// prepared is a start request resolved against its profile and provider.
type prepared struct {
	account      agents.Account
	provider     agents.Provider
	capabilities agents.Capabilities
	launch       agents.LaunchOptions
}

// prepare checks the request and resolves its profile and provider, before any
// admission or runtime exists.
func (s Starter) prepare(req StartRequest) (prepared, error) {
	if !agentterminal.Supported {
		return prepared{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if s.Paths.Records == "" {
		return prepared{}, fmt.Errorf("agent hosts unavailable")
	}
	if !agentterminal.Dimensions(req.Cols, req.Rows) {
		return prepared{}, fmt.Errorf("terminal dimensions must be between 1 and 500")
	}
	account, err := s.Accounts.Get(req.AccountID)
	if err != nil {
		return prepared{}, fmt.Errorf("profile unavailable")
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return prepared{}, fmt.Errorf("provider not available")
	}
	capabilities := provider.Capabilities()
	if !capabilities.Interactive {
		return prepared{}, fmt.Errorf("provider not available")
	}
	launch := req.Launch
	launch.DisplayName = req.Name
	if launch.ReadOnly {
		// Checked here as well as in PrepareSession: the holder is another
		// process, and a refusal should happen before any admission or runtime.
		if err := agents.RequireReadOnly(provider); err != nil {
			return prepared{}, err
		}
	}
	if launch.PlanFile != "" {
		// A writable plan file only exists inside a read-only launch whose provider can confine the write to it.
		if !launch.ReadOnly || !agents.ReadOnlyOf(provider).PlanFile {
			return prepared{}, fmt.Errorf("%w: %s cannot confine a read-only session to a plan file", agents.ErrInvalidLaunch, provider.ID())
		}
	}
	if validator, ok := provider.(agents.LaunchValidator); ok {
		if err := validator.ValidateLaunch(account, launch); err != nil {
			return prepared{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
		}
	}
	return prepared{account: account, provider: provider, capabilities: capabilities, launch: launch}, nil
}

// admit takes the cross-process admission lock.
func (s Starter) admit(ctx context.Context) (*agents.FileLock, error) {
	lockCtx, cancel := context.WithTimeout(ctx, admitTimeout)
	defer cancel()
	lock, err := agents.LockFileContext(lockCtx, filepath.Join(s.Paths.Records, "admit.lock"))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return nil, fmt.Errorf("%w: %w", ErrAdmissionBusy, err)
	case errors.Is(err, context.Canceled):
		return nil, fmt.Errorf("agent start cancelled: %w", err)
	case err != nil:
		return nil, fmt.Errorf("cannot lock agent admission: %w", err)
	}
	return lock, nil
}

// checkAdmission refuses a start the running sessions in records rule out. A
// reservation no holder has claimed yet still counts: its holder may be
// forking, and another account must not slip in beside it.
func checkAdmission(records []Record, account agents.Account, id agents.AccountID, capabilities agents.Capabilities) error {
	active := 0
	for _, r := range records {
		if !r.Summary.Active() || !r.Alive && r.StartPhase != phaseReserved {
			continue
		}
		active++
		if r.Summary.Provider != account.Provider {
			continue
		}
		if r.Summary.AccountID == id && !capabilities.ConcurrentSameAccount {
			return agents.ErrAccountBusy
		}
		if r.Summary.AccountID != id && !capabilities.ConcurrentCrossAccount {
			return agents.ErrProviderBusy
		}
	}
	if active >= ActiveLimit {
		return fmt.Errorf("%d active session limit reached", ActiveLimit)
	}
	return nil
}
