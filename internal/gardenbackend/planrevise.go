package gardenbackend

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// readOnlyAccount checks that the account still exists and its provider can run a planner confined to one file.
func (b *Backend) readOnlyAccount(ctx context.Context, id agents.AccountID) error {
	acct, err := b.deps.Agents.Account(ctx, id)
	if err != nil {
		return err
	}
	if !acct.ReadOnly.Supported || !acct.ReadOnly.PlanFile {
		return fmt.Errorf("%w: %s", plan.ErrReadOnlyUnsupported, acct.Provider)
	}
	return nil
}

// PreviewPlanRevise implements gardenapi.Backend.
func (b *Backend) PreviewPlanRevise(ctx context.Context, issue uint64) (gardenapi.PlanRevisePreviewView, error) {
	gen := b.gen.Add(1)
	b.invalidateAuth()
	reviews, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanRevisePreviewView{}, apiError(err)
	}
	planner, err := b.planService()
	if err != nil {
		return gardenapi.PlanRevisePreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.plannerTarget(ctx, issue)
	if err != nil {
		return gardenapi.PlanRevisePreviewView{}, err
	}
	rv, err := reviews.Review(ctx, t)
	if err != nil {
		return gardenapi.PlanRevisePreviewView{}, apiError(err)
	}
	p, err := planner.Preview(ctx, t)
	if err != nil {
		return gardenapi.PlanRevisePreviewView{}, apiError(err)
	}
	v := gardenapi.PlanRevisePreviewView{Stamp: b.stamp(gen, repo), IssueID: issue, Base: p.Base}
	rev, d, pending := pendingRevise(rv)
	switch {
	case pending:
		v.Revision = rev
		v.Pending = &gardenapi.PendingRevise{DecisionCommentID: d.CommentID, Reason: planrev.ReasonOf(d)}
		// Attempts are oldest first: the newest one for the decision is what the user should see.
		for i := len(p.Attempts) - 1; i >= 0; i-- {
			if a := p.Attempts[i]; a.Revises != nil && a.Revises.DecisionCommentID == d.CommentID {
				v.Pending.Attempt = &a
				break
			}
		}
	case rv.Unavailable != "":
		v.Unavailable = rv.Unavailable
	case rv.Current == nil:
		v.Unavailable = "There is no current plan revision to request changes on."
	case !rv.CanDecide(garden.DecisionChangesRequested):
		v.Revision = *rv.Current
		v.Unavailable = "The current plan revision cannot take a changes request now."
	default:
		v.Revision = *rv.Current
	}
	if v.Unavailable == "" {
		v.Unavailable = p.Unavailable
	}
	if v.Revision.Hash != "" {
		if err := b.retainSettings(ctx, planner, t.Issue, &v); err != nil {
			return gardenapi.PlanRevisePreviewView{}, apiError(err)
		}
	}
	return v, nil
}

// retainSettings reuses the account, model and effort of the attempt that produced the revision, but only while
// that account could still run a planner; otherwise it says why and the caller chooses an account again.
func (b *Backend) retainSettings(ctx context.Context, planner *plan.Service, issue garden.IssueID, v *gardenapi.PlanRevisePreviewView) error {
	a, ok, err := planner.SourceAttempt(ctx, issue, v.Revision.Hash)
	if err != nil {
		return err
	}
	if !ok {
		v.SettingsUnavailable = "No planner attempt on this machine produced this revision."
		return nil
	}
	switch err := b.readOnlyAccount(ctx, a.AccountID); {
	case errors.Is(err, agents.ErrAccountNotFound):
		v.SettingsUnavailable = "The account that wrote this plan no longer exists."
	case err != nil:
		v.SettingsUnavailable = "The account that wrote this plan cannot run a read-only planner now."
	default:
		v.Settings = &gardenapi.PlanReviseSettings{AccountID: a.AccountID, Model: a.Model, Effort: a.Effort}
		v.SettingsSource = a.ID
	}
	return nil
}

// reviseSpec refuses, before anything is written, what a revision start would refuse afterwards: another
// specification, another checkout, an unconfirmed read, or an account that cannot plan. Beyond that, only
// failures nobody could foresee remain for the partial-success path.
func (b *Backend) reviseSpec(ctx context.Context, planner *plan.Service, t approve.Target, req gardenapi.PlanReviseRequest) (plan.SpecRef, plan.Preview, error) {
	if !req.AcceptReadOnly {
		return plan.SpecRef{}, plan.Preview{}, apiError(plan.ErrNotAccepted)
	}
	p, err := planner.Preview(ctx, t)
	if err != nil {
		return plan.SpecRef{}, plan.Preview{}, apiError(err)
	}
	// A live attempt is the caller's to judge (it may be the one this request already made); any other reason
	// the planner is unavailable is a refusal now, not after a decision was written.
	if _, live := (gardenapi.PlannerAttemptsView{Attempts: p.Attempts}).Live(); p.Unavailable != "" && !live {
		return plan.SpecRef{}, plan.Preview{}, apiError(plan.ErrNotApproved)
	}
	spec := p.Spec()
	if spec.RevisionHash == "" || spec.RevisionHash != req.SpecHash || spec.Fingerprint != req.Fingerprint {
		return plan.SpecRef{}, plan.Preview{}, apiError(plan.ErrSpecChanged)
	}
	if p.Base.Commit != req.Base.Commit || p.Base.Dirty != req.Base.Dirty || p.Base.DirtyDigest != req.Base.DirtyDigest {
		return plan.SpecRef{}, plan.Preview{}, apiError(plan.ErrBaseChanged)
	}
	if p.Base.Dirty && !req.IncludeDirty {
		return plan.SpecRef{}, plan.Preview{}, apiError(plan.ErrDirtyContext)
	}
	if err := b.readOnlyAccount(ctx, req.Settings.AccountID); err != nil {
		return plan.SpecRef{}, plan.Preview{}, apiError(err)
	}
	return spec, p, nil
}

// reviseRequest validates the parts of a request every revise action shares and resolves its services and target.
func (b *Backend) reviseRequest(ctx context.Context, req gardenapi.PlanReviseRequest) (*plan.Service, approve.Target, garden.RepoID, error) {
	planner, err := b.planService()
	if err != nil {
		return nil, approve.Target{}, 0, apiError(err)
	}
	if req.Stamp.LocalRepoID != b.local.String() || req.IssueID == 0 || req.RevisionHash == "" {
		return nil, approve.Target{}, 0, apiError(planrev.ErrNoRevision)
	}
	t, repo, err := b.approveTarget(ctx, req.IssueID)
	if err != nil {
		return nil, approve.Target{}, 0, err
	}
	if uint64(repo) != req.Stamp.GardenID {
		return nil, approve.Target{}, 0, apiError(ErrRemoteChanged)
	}
	return planner, t, repo, nil
}

// RevisePlan implements gardenapi.Backend.
func (b *Backend) RevisePlan(ctx context.Context, req gardenapi.PlanReviseRequest) (gardenapi.PlanReviseView, error) {
	gen := b.gen.Add(1)
	if _, err := b.reviewService(); err != nil {
		return gardenapi.PlanReviseView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	b.invalidateAuth()
	planner, t, repo, err := b.reviseRequest(ctx, req)
	if err != nil {
		return gardenapi.PlanReviseView{}, err
	}
	spec, p, err := b.reviseSpec(ctx, planner, t, req)
	if err != nil {
		return gardenapi.PlanReviseView{}, err
	}
	// Starting would refuse while an attempt runs, so nothing may be written first. The one live attempt that
	// is not a refusal is this request's own earlier start.
	if live, ok := (gardenapi.PlannerAttemptsView{Attempts: p.Attempts}).Live(); ok {
		if live.Revises == nil || live.Revises.RevisionHash != req.RevisionHash {
			return gardenapi.PlanReviseView{}, apiError(plan.ErrAttemptActive)
		}
		stamp := b.stamp(gen, repo)
		return gardenapi.PlanReviseView{Stamp: stamp, IssueID: req.IssueID, Attempt: &live, Existing: true, Decision: gardenapi.PlanDecisionView{Stamp: stamp,
			IssueID: req.IssueID, State: gardenapi.PlanWriteDone, CommentID: live.Revises.DecisionCommentID, Ordinal: live.Revises.Ordinal,
			Decision: garden.DecisionChangesRequested, Replayed: true}}, nil
	}
	d, err := b.decidePlan(ctx, gen, gardenapi.PlanDecisionRequest{Stamp: req.Stamp, SpecHash: req.SpecHash, ActorID: req.ActorID, IssueID: req.IssueID,
		RevisionHash: req.RevisionHash, Fingerprint: req.Fingerprint, Decision: garden.DecisionChangesRequested, Reason: req.Reason})
	if err != nil {
		return gardenapi.PlanReviseView{}, err
	}
	v := gardenapi.PlanReviseView{Stamp: b.stamp(gen, repo), IssueID: req.IssueID, Decision: d}
	// An unknown outcome may or may not be on GitHub; starting from it would guess, so the user reconciles first.
	if d.State != gardenapi.PlanWriteDone {
		return v, nil
	}
	return b.startRevise(ctx, planner, t, spec, req, d.CommentID, v), nil
}

// StartPlanRevise implements gardenapi.Backend.
func (b *Backend) StartPlanRevise(ctx context.Context, req gardenapi.PlanReviseRequest) (gardenapi.PlanReviseView, error) {
	gen := b.gen.Add(1)
	reviews, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanReviseView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	b.invalidateAuth()
	planner, t, repo, err := b.reviseRequest(ctx, req)
	if err != nil {
		return gardenapi.PlanReviseView{}, err
	}
	rv, err := reviews.Review(ctx, t)
	if err != nil {
		return gardenapi.PlanReviseView{}, apiError(err)
	}
	rev, d, ok := pendingRevise(rv)
	if !ok || rev.Hash != req.RevisionHash || d.CommentID != req.DecisionCommentID {
		return gardenapi.PlanReviseView{}, apiError(plan.ErrFeedbackChanged)
	}
	spec, _, err := b.reviseSpec(ctx, planner, t, req)
	if err != nil {
		return gardenapi.PlanReviseView{}, err
	}
	stamp := b.stamp(gen, repo)
	v := gardenapi.PlanReviseView{Stamp: stamp, IssueID: req.IssueID, Decision: gardenapi.PlanDecisionView{Stamp: stamp, IssueID: req.IssueID,
		State: gardenapi.PlanWriteDone, CommentID: d.CommentID, Ordinal: rev.Ordinal, Decision: garden.DecisionChangesRequested, Replayed: true}}
	return b.startRevise(ctx, planner, t, spec, req, d.CommentID, v), nil
}

// startRevise starts the one revision attempt for a recorded decision. A failure is reported in the view and
// never undoes the decision; a repeated request shows the attempt it already made.
func (b *Backend) startRevise(ctx context.Context, planner *plan.Service, t approve.Target, spec plan.SpecRef, req gardenapi.PlanReviseRequest, commentID int64, v gardenapi.PlanReviseView) gardenapi.PlanReviseView {
	a, err := planner.Start(ctx, plan.StartRequest{Target: t, Spec: spec, Base: req.Base, IncludeDirty: req.IncludeDirty, AcceptReadOnly: req.AcceptReadOnly,
		AccountID: req.Settings.AccountID, Model: req.Settings.Model, Effort: req.Settings.Effort, Cols: req.Cols, Rows: req.Rows,
		Revise: &plan.ReviseRequest{RevisionHash: req.RevisionHash, DecisionCommentID: commentID}})
	var dup *plan.RevisionRequestedError
	switch {
	case err == nil:
		v.Attempt = &a
	case errors.As(err, &dup):
		existing, gerr := b.deps.Plans.Get(ctx, dup.AttemptID)
		if gerr != nil {
			v.StartErr = safeAPIError(gerr)
			break
		}
		v.Attempt, v.Existing = &existing, true
	default:
		v.StartErr = safeAPIError(err)
	}
	return v
}
