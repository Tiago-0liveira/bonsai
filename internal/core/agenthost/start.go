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
func (s Starter) Start(ctx context.Context, req StartRequest) (Record, error) {
	if !agentterminal.Supported {
		return Record{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if s.Paths.Records == "" {
		return Record{}, fmt.Errorf("agent hosts unavailable")
	}
	if !agentterminal.Dimensions(req.Cols, req.Rows) {
		return Record{}, fmt.Errorf("terminal dimensions must be between 1 and 500")
	}
	account, err := s.Accounts.Get(req.AccountID)
	if err != nil {
		return Record{}, fmt.Errorf("profile unavailable")
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return Record{}, fmt.Errorf("provider not available")
	}
	capabilities := provider.Capabilities()
	if !capabilities.Interactive {
		return Record{}, fmt.Errorf("provider not available")
	}
	launch := req.Launch
	launch.DisplayName = req.Name
	if validator, ok := provider.(agents.LaunchValidator); ok {
		if err := validator.ValidateLaunch(account, launch); err != nil {
			return Record{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
		}
	}

	// The lock is held until the record exists, so the next starter, in this
	// process or another, counts this session.
	lockCtx, cancel := context.WithTimeout(ctx, admitTimeout)
	defer cancel()
	lock, err := agents.LockFileContext(lockCtx, filepath.Join(s.Paths.Records, "admit.lock"))
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Record{}, fmt.Errorf("%w: %w", ErrAdmissionBusy, err)
	case errors.Is(err, context.Canceled):
		return Record{}, fmt.Errorf("agent start cancelled: %w", err)
	case err != nil:
		return Record{}, fmt.Errorf("cannot lock agent admission: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	records, err := List(s.Paths)
	if err != nil {
		return Record{}, fmt.Errorf("cannot list agent sessions")
	}
	active := 0
	for _, r := range records {
		if !r.Alive || !r.Summary.Active() {
			continue
		}
		active++
		if r.Summary.Provider != account.Provider {
			continue
		}
		if r.Summary.AccountID == req.AccountID && !capabilities.ConcurrentSameAccount {
			return Record{}, agents.ErrAccountBusy
		}
		if r.Summary.AccountID != req.AccountID && !capabilities.ConcurrentCrossAccount {
			return Record{}, agents.ErrProviderBusy
		}
	}
	if active >= ActiveLimit {
		return Record{}, fmt.Errorf("%d active session limit reached", ActiveLimit)
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
			// Without a readable record the socket still names the holder.
			r, err := Read(s.Paths, summary.ID)
			if err != nil {
				r = Record{ID: summary.ID, Socket: s.Paths.socket(summary.ID), Protocol: Version}
			}
			_ = StopHolder(s.Paths, r, "")
			Wait(s.Paths, summary.ID, 5*time.Second)
		}
		_ = s.Sessions.Cleanup(runtime)
	}()
	if err := launcher(s.Paths, Spec{Summary: summary, WorkDir: req.Dir, Cols: req.Cols, Rows: req.Rows, Launch: launch}); err != nil {
		return Record{}, fmt.Errorf("cannot start agent host: %v", err)
	}
	launched = true
	record, err := Read(s.Paths, summary.ID)
	if err != nil {
		return Record{}, fmt.Errorf("cannot start agent host: %v", err)
	}
	ok = true
	return record, nil
}
