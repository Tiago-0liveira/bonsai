package launch

import (
	"context"
	"errors"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func inconclusive(o SessionObservation) bool {
	return o.State == "" && !o.Alive && !o.Acknowledged && !o.Closed
}

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

// applyObservation folds one holder observation into the run it names and then
// into the attempt, never moving anything backwards: a terminal run is final,
// acknowledgment and the start time are kept once seen, and only the newest run
// decides the attempt. Idle or turn-completed activity is not a lifecycle
// change: a live holder stays running. It reports whether anything changed.
func (s *Service) applyObservation(a *Attempt, id StartIdentity, obs SessionObservation) bool {
	if a.State == StateCleaned || inconclusive(obs) {
		return false
	}
	idx := -1
	for i := range a.Runs {
		if a.Runs[i].OperationID == id.OperationID && a.Runs[i].SessionID == id.SessionID {
			idx = i
		}
	}
	if idx < 0 || terminalRun(a.Runs[idx].State) {
		return false
	}
	r := &a.Runs[idx]
	latest := idx == len(a.Runs)-1
	changed := false
	if obs.Acknowledged && !r.Acknowledged {
		r.Acknowledged, changed = true, true
	}
	if obs.StartedAt != nil && r.StartedAt == nil {
		r.StartedAt, changed = copyTime(obs.StartedAt), true
	}
	switch {
	case obs.Closed || obs.State == "exited" || obs.State == "failed":
		end := s.now()
		if obs.EndedAt != nil {
			end = obs.EndedAt.UTC()
		}
		r.EndedAt = &end
		if obs.ExitCode != nil {
			code := *obs.ExitCode
			r.ExitCode = &code
		}
		switch {
		case !r.Acknowledged:
			r.State = "not-started"
		case obs.State == "failed":
			r.State = "failed"
		default:
			r.State = "exited"
		}
		changed = true
	case r.Acknowledged:
		want := "running"
		if a.StopRequested && latest {
			want = "stopping"
		}
		if r.State != want {
			r.State, changed = want, true
		}
	default:
		if a.StopRequested && latest && r.State != "stopping" {
			r.State, changed = "stopping", true
		}
	}
	if latest {
		changed = s.deriveAttempt(a) || changed
	}
	return changed
}

// raiseStage moves the stage the Issue should show forward: in progress first,
// then review or blocked. It never goes back and never reaches Done: nothing
// here verifies or accepts the work.
func (s *Service) raiseStage(a *Attempt, stage garden.Stage) bool {
	if a.DesiredStage == stage || a.DesiredStage == garden.StageReview || a.DesiredStage == garden.StageBlocked {
		return false
	}
	a.DesiredStage = stage
	s.setStep(a, StepMirror, StepPending, "")
	return true
}

// deriveAttempt sets the attempt's state from its newest run.
func (s *Service) deriveAttempt(a *Attempt) bool {
	if len(a.Runs) == 0 {
		return false
	}
	r := a.Runs[len(a.Runs)-1]
	before := a.State
	changed := false
	switch r.State {
	case "starting":
		if a.State == StatePreparing || a.State == StateStartUnknown {
			a.State, changed = StateStarting, true
		}
	case "running", "stopping":
		if r.Acknowledged {
			if a.State != StateRunning {
				a.State, changed = StateRunning, true
			}
			changed = s.setStep(a, StepHolder, StepDone, stepCode(a, codeStopRequested, r.State == "stopping")) || changed
			changed = s.raiseStage(a, garden.StageInProgress) || changed
		} else if a.State != StateStarting {
			a.State, changed = StateStarting, true
		}
	default:
		if !r.Acknowledged {
			a.State, a.EndedAt = StateRecoverable, nil
			changed = s.setStep(a, StepHolder, StepFailed, codeHolderStartFailed) || before != a.State
			break
		}
		code, step := "", StepDone
		switch {
		case a.StopRequested:
			a.State, code = StateStopped, codeStopRequested
			s.raiseStage(a, garden.StageBlocked)
		case r.State == "exited" && r.ExitCode != nil && *r.ExitCode == 0:
			a.State = StateAwaitingReview
			s.raiseStage(a, garden.StageReview)
		default:
			a.State, step = StateFailed, StepFailed
			code = codeHolderExitFailed
			if r.State == "failed" {
				code = codeHolderLost
			}
			s.raiseStage(a, garden.StageBlocked)
		}
		a.EndedAt = copyTime(r.EndedAt)
		s.setStep(a, StepHolder, step, code)
		changed = true
	}
	return changed || before != a.State
}

func stepCode(a *Attempt, code string, when bool) string {
	if when || a.StopRequested {
		return code
	}
	return ""
}

func mirrorSequence(mirrored, desired garden.Stage) []garden.Stage {
	switch {
	case desired == "" || desired == mirrored:
		return nil
	case mirrored != garden.StageInProgress && desired != garden.StageInProgress:
		return []garden.Stage{garden.StageInProgress, desired}
	default:
		return []garden.Stage{desired}
	}
}

// syncMirror moves the Issue's stage label toward the stage the local truth
// implies, one verified expected-label step at a time. A failure only marks the
// mirror step: running, terminal and session facts stay as they are, and a later
// reconcile retries exactly the pair that was accepted.
func (s *Service) syncMirror(ctx context.Context, a Attempt) Attempt {
	stages := mirrorSequence(a.MirroredStage, a.DesiredStage)
	if len(stages) == 0 {
		return a
	}
	target := approve.Target{Repo: a.Repo, Issue: a.Issue, Viewer: a.ActorID}
	for _, stage := range stages {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.startTimeout)
		err := s.d.Remote.Mirror(mctx, MirrorRequest{AttemptID: a.ID, Target: target, Local: a.Local, Epoch: a.Epoch, Inputs: a.Inputs, Stage: stage})
		cancel()
		next, uerr := s.update(ctx, a.ID, func(a *Attempt) bool {
			if err == nil {
				changed := a.MirroredStage != stage
				a.MirroredStage = stage
				state := StepRunning
				if stage == a.DesiredStage {
					state = StepDone
				}
				return s.setStep(a, StepMirror, state, "") || changed
			}
			code := codeRemotePending
			if errors.Is(err, garden.ErrMetadataChanged) {
				code = codeRemoteConflict
			}
			return s.setStep(a, StepMirror, StepPending, code)
		})
		if uerr != nil {
			return a
		}
		a = next
		if err != nil {
			break
		}
	}
	return a
}

// settlePreparing resolves an attempt that was reserved but whose driver is gone,
// which the held lock proves. It records what the daemon says exists and leaves
// the attempt Recoverable: whatever step is left needs an explicit retry.
func (s *Service) settlePreparing(ctx context.Context, a Attempt) (Attempt, error) {
	qctx, cancel := s.queryCtx(ctx)
	check, err := s.d.Worktrees.InspectIsolation(qctx, a.Proposal)
	cancel()
	step, code := stepOf(a, StepWorktree).State, stepOf(a, StepWorktree).Code
	if err == nil {
		switch {
		case check.Exists && check.Owned && check.Hooks == "done" && check.Worktree.ID != "":
			step, code = StepDone, ""
		case check.Exists && check.Owned && check.Hooks == "failed":
			step, code = StepFailed, codeHooksFailed
		case check.Exists && check.Owned && (check.Hooks == "unknown" || check.Hooks == "running"):
			step, code = StepUnknown, codeHooksUnknown
		}
	}
	return s.update(ctx, a.ID, func(a *Attempt) bool {
		changed := s.setStep(a, StepWorktree, step, code)
		if step == StepDone && a.WorktreeID != check.Worktree.ID {
			a.WorktreeID, changed = check.Worktree.ID, true
		}
		if a.State == StatePreparing {
			a.State, changed = StateRecoverable, true
		}
		return changed
	})
}

// observeLatest queries the newest run's exact identity. A failed or empty
// answer never invents a closure; a start that cannot be confirmed becomes
// StartUnknown, and a running one keeps its last good state.
func (s *Service) observeLatest(ctx context.Context, a Attempt) (Attempt, error) {
	if len(a.Runs) == 0 {
		return a, nil
	}
	run := a.Runs[len(a.Runs)-1]
	if terminalRun(run.State) {
		return a, nil
	}
	identity := StartIdentity{OperationID: run.OperationID, SessionID: run.SessionID}
	qctx, cancel := s.queryCtx(ctx)
	obs, err := s.d.Sessions.Query(qctx, identity)
	cancel()
	if err != nil || inconclusive(obs) || (obs.Identity != (StartIdentity{}) && obs.Identity != identity) {
		if a.State != StateStarting {
			return a, nil
		}
		return s.update(ctx, a.ID, func(a *Attempt) bool {
			changed := s.setStep(a, StepHolder, StepUnknown, codeHolderStartUnknown)
			if a.State == StateStarting {
				a.State, changed = StateStartUnknown, true
			}
			return changed
		})
	}
	return s.update(ctx, a.ID, func(a *Attempt) bool { return s.applyObservation(a, identity, obs) })
}

// reconcileHeld observes one attempt whose lock the caller holds. It never
// starts a holder, creates a worktree, reruns hooks, pushes or creates a pull
// request; it inspects, records and retries the already authorized mirror.
func (s *Service) reconcileHeld(ctx context.Context, a Attempt) (Attempt, error) {
	if a.State == StateCleaned {
		return a, nil
	}
	var err error
	switch a.State {
	case StatePreparing:
		a, err = s.settlePreparing(ctx, a)
	case StateStarting, StateStartUnknown, StateRunning:
		a, err = s.observeLatest(ctx, a)
	}
	if err != nil {
		return s.partial(ctx, a.ID, err)
	}
	a = s.syncMirror(ctx, a)
	return s.reconcilePR(ctx, a), nil
}

// Reconcile observes every local attempt of the Issue and returns them all.
// Remote and pull request read failures are recorded as orthogonal state and
// never empty a good local list; only a store fault is an error.
func (s *Service) Reconcile(ctx context.Context, issue garden.IssueID) ([]Attempt, error) {
	list, err := s.d.Store.List(ctx, s.d.Local, issue)
	if err != nil {
		return nil, err
	}
	out := make([]Attempt, 0, len(list))
	for _, a := range list {
		got, err := s.reconcileOne(ctx, a)
		if err != nil {
			return out, err
		}
		out = append(out, got)
	}
	return out, nil
}

func (s *Service) reconcileOne(ctx context.Context, a Attempt) (Attempt, error) {
	if a.State == StateCleaned {
		return a, nil
	}
	lctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	unlock, err := s.d.Store.Lock(lctx, a.ID)
	cancel()
	if err != nil {
		// Another process is working on it; its result is what a later read shows.
		return a, nil
	}
	defer unlock()
	cur, err := s.d.Store.Get(ctx, a.ID)
	if err != nil {
		return Attempt{}, err
	}
	got, err := s.reconcileHeld(ctx, cur)
	if err != nil && errors.Is(err, domain.ErrUncertain) {
		return got, nil
	}
	return got, err
}
