package agenthost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Resume relaunches the holder of an ended session under the same id. If the
// holder is alive it returns its record unchanged (caller just attaches).
func (s Starter) Resume(ctx context.Context, id string, cols, rows int) (_ Record, resumeErr error) {
	r, err := Read(s.Paths, id)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, fmt.Errorf("%w: %s", agents.ErrSessionNotFound, id)
	}
	if err != nil {
		return Record{}, err
	}
	if r.Alive {
		return r, nil
	}
	if r.Summary.OperationID != "" {
		return Record{}, agents.NotResumable(agents.ReasonGarden)
	}
	if r.StartPhase == phaseReserved || r.StartPhase == phaseClaimed {
		return Record{}, agents.NotResumable(agents.ReasonNeverStarted)
	}
	if info, err := os.Stat(r.WorkDir); err != nil || !info.IsDir() {
		return Record{}, agents.NotResumable(agents.ReasonWorktreeGone)
	}
	account, err := s.Accounts.Get(r.Summary.AccountID)
	if err != nil {
		return Record{}, agents.NotResumable(agents.ReasonProfileGone)
	}
	provider, err := s.Registry.Get(account.Provider)
	if err != nil {
		return Record{}, agents.NotResumable(agents.ReasonProviderMissing)
	}
	resumer, ok := provider.(agents.Resumer)
	if !ok {
		return Record{}, agents.NotResumable(agents.ReasonUnsupported)
	}
	if !agentterminal.Supported {
		return Record{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if !agentterminal.Dimensions(cols, rows) {
		return Record{}, fmt.Errorf("terminal dimensions must be between 1 and 500")
	}

	// Keep admission until the holder has written its record, just as Start does.
	lock, err := s.admit(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = lock.Unlock() }()
	// A competing resumer may have finished while we waited for admission.
	r, err = Read(s.Paths, id)
	if err != nil {
		return Record{}, err
	}
	if r.Alive {
		return r, nil
	}
	records, err := admissibleRecords(s.Paths, time.Now())
	if err != nil {
		return Record{}, err
	}
	if err := checkAdmission(records, account, account.ID, provider.Capabilities()); err != nil {
		return Record{}, err
	}
	runtime, err := s.Sessions.Get(agents.SessionID(id))
	created := false
	if errors.Is(err, agents.ErrSessionNotFound) {
		store, ok := s.Sessions.(agents.ReservedSessionStore)
		if !ok {
			return Record{}, fmt.Errorf("cannot recreate session runtime: store does not support reserved ids")
		}
		runtime, err = store.CreateWithID(account, r.WorkDir, agents.SessionID(id))
		created = err == nil
	}
	if err != nil {
		return Record{}, fmt.Errorf("cannot get session runtime: %w", err)
	}
	holderStarted := false
	defer func() {
		if !created || holderStarted {
			return
		}
		// A launcher may panic or fail after handing the runtime to a holder. Its
		// lock or a new generation proves ownership passed; never remove its files.
		current, readErr := Read(s.Paths, id)
		if held(s.Paths.lock(id)) || readErr == nil && current.Generation != "" && current.Generation != r.Generation {
			return
		}
		if err := s.Sessions.Cleanup(runtime); err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("cannot clean recreated session runtime: %w", err))
		}
	}()
	started := r.Summary.CreatedAt
	if r.Summary.StartedAt != nil {
		started = *r.Summary.StartedAt
	}
	var claimed []string
	for _, other := range records {
		if other.ID != id && other.Summary.AccountID == account.ID && other.Summary.ProviderSessionID != "" {
			claimed = append(claimed, other.Summary.ProviderSessionID)
		}
	}
	target, err := resumer.ResumeTarget(ctx, agents.ResumeRequest{Account: account, Session: runtime, ProviderSessionID: r.Summary.ProviderSessionID, StartedAt: started, Claimed: claimed})
	if err != nil {
		return Record{}, err
	}
	summary := r.Summary
	summary.State = "starting"
	summary.Error = ""
	summary.EndedAt = nil
	summary.ExitCode = nil
	summary.StartedAt = nil
	summary.Activity = nil
	summary.Origin = s.Origin
	if target.ID != "" {
		summary.ProviderSessionID = target.ID
	}
	launcher := s.Launch
	if launcher == nil {
		launcher = Launch
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("agent resume cancelled: %w", err)
	}
	spec := Spec{Summary: summary, WorkDir: r.WorkDir, Cols: cols, Rows: rows, Launch: agents.LaunchOptionsFromValues(r.Summary.Model, r.Summary.Name, r.Summary.Options), Resume: &target}
	if err := launcher(ctx, s.Paths, spec); err != nil {
		// The holder lock, not the launcher's error text, decides who won a race.
		if current, readErr := Read(s.Paths, id); readErr == nil && current.Alive {
			holderStarted = true
			return current, nil
		}
		return Record{}, fmt.Errorf("cannot resume agent host: %w", err)
	}
	record, err := Read(s.Paths, id)
	if err != nil {
		return Record{}, fmt.Errorf("cannot resume agent host: %w", err)
	}
	holderStarted = record.Alive || record.Generation != "" && record.Generation != r.Generation
	if err := ctx.Err(); err != nil {
		// As in Start, cancellation after readiness stops our newly launched holder.
		stopErr := StopHolder(s.Paths, record, "")
		if !Wait(s.Paths, id, 5*time.Second) {
			return Record{}, errors.Join(fmt.Errorf("agent resume cancelled: %w", err), fmt.Errorf("agent holder %s did not exit after stop; it may still be running", id), stopErr)
		}
		return Record{}, fmt.Errorf("agent resume cancelled: %w", err)
	}
	return record, nil
}
