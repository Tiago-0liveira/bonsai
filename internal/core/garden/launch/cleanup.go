package launch

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

// closedObservation is positive proof that a holder start can no longer run.
func closedObservation(o SessionObservation) bool {
	return o.Closed || o.State == "exited" || o.State == "failed"
}

// previewCleanup says whether the retained worktree could be removed now, and
// why not. Nothing is written. Every run is queried again, whatever the record
// says, because cleanup must be preceded by proof that no holder is or will be
// running in the directory.
func (s *Service) previewCleanup(ctx context.Context, a Attempt) CleanupPreview {
	p := CleanupPreview{AttemptID: a.ID, Revision: a.Revision}
	block := func(why string) CleanupPreview {
		p.Allowed, p.Blocker = false, why
		return p
	}
	switch a.State {
	case StateCleaned:
		return block("This attempt was already cleaned.")
	case StatePreparing, StateStarting, StateStartUnknown, StateRunning:
		return block("The attempt may still be running or starting; reconcile it first.")
	}
	for _, r := range a.Runs {
		qctx, cancel := s.queryCtx(ctx)
		obs, err := s.d.Sessions.Query(qctx, StartIdentity{OperationID: r.OperationID, SessionID: r.SessionID})
		cancel()
		switch {
		case err != nil:
			return block("The agent session could not be confirmed closed.")
		case obs.Alive || (!closedObservation(obs) && !inconclusive(obs)):
			return block("The agent session is still active.")
		case inconclusive(obs) && !terminalRun(r.State):
			return block("The agent session could not be confirmed closed.")
		}
	}
	if dir, ok := s.d.Sessions.(DirectorySessions); ok {
		qctx, cancel := s.queryCtx(ctx)
		active, err := dir.ActiveAt(qctx, a.Proposal.Path)
		cancel()
		switch {
		case err != nil:
			return block("The agent sessions in this worktree could not be confirmed closed.")
		case active:
			return block("An agent session is still active in this worktree.")
		}
	}
	qctx, cancel := s.queryCtx(ctx)
	check, err := s.d.Worktrees.InspectIsolation(qctx, a.Proposal)
	cancel()
	p.Check = check
	switch {
	case err != nil:
		return block("The worktree could not be inspected.")
	case !check.Exists:
		// Nothing to remove. An owned branch with no worktree stays: cleanup
		// never deletes a branch.
		p.Allowed = true
	case !check.Owned:
		return block("The worktree is not proven to belong to this attempt.")
	case !check.Clean:
		return block("The worktree has uncommitted, ignored or nested content.")
	case check.HeadSHA != a.Proposal.BaseSHA:
		return block("The branch has commits beyond the approved base.")
	case check.Hooks == "unknown" || check.Hooks == "running":
		return block("Worktree hooks may have partly run.")
	default:
		p.Allowed = true
	}
	return p
}

// PreviewCleanup is the read-only first step of cleanup. An unsafe worktree or a
// live holder is a normal answer, Allowed false with a fixed blocker.
func (s *Service) PreviewCleanup(ctx context.Context, id string) (CleanupPreview, error) {
	a, err := s.owned(ctx, id)
	if err != nil {
		return CleanupPreview{}, err
	}
	return s.previewCleanup(ctx, a), nil
}

// Cleanup removes the retained worktree after a separate confirmation of the
// exact preview. Anything that moved since the preview refuses it. Removal is
// non-forced and leaves the branch, the history and this record; an unknown
// outcome is reconciled from the inventory, never compensated.
func (s *Service) Cleanup(ctx context.Context, req CleanupRequest) (Attempt, error) {
	if !req.Confirmed {
		return Attempt{}, ErrNotAccepted
	}
	if _, err := s.owned(ctx, req.Preview.AttemptID); err != nil {
		return Attempt{}, err
	}
	id := req.Preview.AttemptID
	unlock, err := s.d.Store.Lock(ctx, id)
	if err != nil {
		return s.partial(ctx, id, fmt.Errorf("%w: the attempt lock was not acquired: %w", ErrOutcomeUnknown, err))
	}
	defer unlock()
	a, err := s.d.Store.Get(ctx, id)
	if err != nil {
		return Attempt{}, err
	}
	fresh := s.previewCleanup(ctx, a)
	if fresh.Revision != req.Preview.Revision || fresh.Allowed != req.Preview.Allowed || !reflect.DeepEqual(fresh.Check, req.Preview.Check) {
		return a, ErrCleanupChanged
	}
	if !fresh.Allowed {
		return a, ErrCleanupUnsafe
	}
	a, err = s.update(ctx, id, func(a *Attempt) bool { return s.setStep(a, StepCleanup, StepRunning, "") })
	if err != nil {
		return s.partial(ctx, id, err)
	}
	if fresh.Check.Exists {
		rerr := s.d.Worktrees.RemoveIsolation(ctx, domain.IsolationRemoveRequest{Check: fresh.Check})
		if rerr != nil && !s.gone(ctx, a) {
			state, code := StepFailed, codeCleanupUnsafe
			cause := fmt.Errorf("cleanup: %w", isolationError(rerr))
			if errors.Is(rerr, domain.ErrUncertain) || isContext(rerr) {
				state, code = StepUnknown, codeCleanupUnknown
				cause = fmt.Errorf("%w: cleanup: %w", ErrOutcomeUnknown, rerr)
			}
			if _, uerr := s.update(ctx, id, func(a *Attempt) bool { return s.setStep(a, StepCleanup, state, code) }); uerr != nil {
				return s.partial(ctx, id, uerr)
			}
			return s.partial(ctx, id, cause)
		}
	}
	return s.update(ctx, id, func(a *Attempt) bool {
		a.State = StateCleaned
		if a.EndedAt == nil {
			now := s.now()
			a.EndedAt = &now
		}
		s.setStep(a, StepCleanup, StepDone, "")
		return true
	})
}

// gone reports whether the inventory proves the worktree no longer exists, which
// settles a removal whose answer was lost.
func (s *Service) gone(ctx context.Context, a Attempt) bool {
	qctx, cancel := s.queryCtx(ctx)
	defer cancel()
	check, err := s.d.Worktrees.InspectIsolation(qctx, a.Proposal)
	return err == nil && !check.Exists
}
