package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// unavailableReview is shown when this build has no attempt store or comment writer, so no plan can be published.
var unavailableReview = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Reviewing plans is unavailable in this build."}

// reviewService builds the plan-revision service on first use. It reads through gate one's verified log, so both
// gates trust the same viewer, and writes only comments.
func (b *Backend) reviewService() (*planrev.Service, error) {
	gate, err := b.approveService()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reviews != nil {
		return b.reviews, nil
	}
	if b.deps.Plans == nil || b.deps.Writer == nil {
		return nil, unavailableReview
	}
	svc, err := planrev.New(planrev.Deps{Log: gate, Writer: b.deps.Writer, Attempts: b.deps.Plans, Local: b.local})
	if err != nil {
		return nil, err
	}
	b.reviews = svc
	return svc, nil
}

// PlanReview implements gardenapi.Backend.
func (b *Backend) PlanReview(ctx context.Context, issue uint64) (gardenapi.PlanReviewView, error) {
	gen := b.gen.Add(1)
	b.invalidateAuth()
	svc, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanReviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.plannerTarget(ctx, issue)
	if err != nil {
		return gardenapi.PlanReviewView{}, err
	}
	rv, err := svc.Review(ctx, t)
	if err != nil {
		return gardenapi.PlanReviewView{}, apiError(err)
	}
	return gardenapi.PlanReviewView{Stamp: b.stamp(gen, repo), IssueID: issue, Actor: t.Login, ActorID: t.Viewer, Review: rv}, nil
}

// PreviewPlanPublish implements gardenapi.Backend.
func (b *Backend) PreviewPlanPublish(ctx context.Context, req gardenapi.PlanPublishRequest) (gardenapi.PlanPublishPreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanPublishPreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.approveTarget(ctx, req.IssueID)
	if err != nil {
		return gardenapi.PlanPublishPreviewView{}, err
	}
	p, err := svc.PreviewPublish(ctx, t, req.AttemptID, req.Text)
	if err != nil {
		return gardenapi.PlanPublishPreviewView{}, apiError(err)
	}
	rv, err := svc.Review(ctx, t)
	if err != nil {
		return gardenapi.PlanPublishPreviewView{}, apiError(err)
	}
	return gardenapi.PlanPublishPreviewView{Stamp: b.stamp(gen, repo), OperationID: "publish-plan-" + b.deps.NewID(), IssueID: req.IssueID, Number: rv.Number,
		Title: rv.Title, Actor: t.Login, Preview: p}, nil
}

// PublishPlan implements gardenapi.Backend.
func (b *Backend) PublishPlan(ctx context.Context, p gardenapi.PlanPublishPreviewView) (gardenapi.PlanPublishView, error) {
	gen := b.gen.Add(1)
	svc, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanPublishView{}, apiError(err)
	}
	if p.Stamp.LocalRepoID != b.local.String() || p.IssueID == 0 || p.Preview.Hash == "" {
		return gardenapi.PlanPublishView{}, apiError(planrev.ErrWrongAttempt)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.approveTarget(ctx, p.IssueID)
	if err != nil {
		return gardenapi.PlanPublishView{}, err
	}
	if uint64(repo) != p.Stamp.GardenID {
		return gardenapi.PlanPublishView{}, apiError(ErrRemoteChanged)
	}
	pv := p.Preview
	pv.Target = t
	res, err := svc.Publish(ctx, pv)
	v := gardenapi.PlanPublishView{Stamp: b.stamp(gen, repo), OperationID: p.OperationID, IssueID: p.IssueID, CommentID: res.CommentID, Hash: res.Hash,
		Ordinal: res.Ordinal, Replayed: res.Replayed}
	switch {
	case err == nil:
		v.State = gardenapi.PlanWriteDone
		return v, nil
	case errors.Is(err, planrev.ErrUnverified):
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "Bonsai could not confirm the plan revision on GitHub.", Next: "Refresh to read the current state; publishing again finds it instead of writing a second one."}
		return v, nil
	case errors.Is(err, planrev.ErrAppendFailed) && !definitiveWrite(err):
		// The comment may have reached GitHub. Previewing again finds it by its content.
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "The plan revision may have been recorded.", Next: "Preview and publish again: Bonsai checks GitHub first and never records it twice."}
		return v, nil
	}
	return gardenapi.PlanPublishView{}, apiError(err)
}

// DecidePlan implements gardenapi.Backend.
func (b *Backend) DecidePlan(ctx context.Context, req gardenapi.PlanDecisionRequest) (gardenapi.PlanDecisionView, error) {
	gen := b.gen.Add(1)
	svc, err := b.reviewService()
	if err != nil {
		return gardenapi.PlanDecisionView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	// Confirmation must observe live permissions and the actual authenticated account.
	b.invalidateAuth()
	t, repo, err := b.approveTarget(ctx, req.IssueID)
	if err != nil {
		return gardenapi.PlanDecisionView{}, err
	}
	if req.Decision == garden.DecisionApproved && (req.Stamp.LocalRepoID != b.local.String() || req.Stamp.GardenID != uint64(repo) || req.Stamp.Epoch != b.stamp(gen, repo).Epoch) {
		return gardenapi.PlanDecisionView{}, apiError(ErrRemoteChanged)
	}
	res, err := svc.Decide(ctx, planrev.DecisionRequest{Target: t, RevisionHash: req.RevisionHash, Fingerprint: req.Fingerprint, Decision: req.Decision, Reason: req.Reason, SpecHash: req.SpecHash, ActorID: req.ActorID})
	v := gardenapi.PlanDecisionView{Stamp: b.stamp(gen, repo), IssueID: req.IssueID, CommentID: res.CommentID, Ordinal: res.Ordinal, Decision: req.Decision, Replayed: res.Replayed}
	switch {
	case err == nil:
		v.State = gardenapi.PlanWriteDone
		return v, nil
	case errors.Is(err, planrev.ErrUnverified):
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "Bonsai could not confirm the decision on GitHub.", Next: "Refresh to read the current state before deciding again."}
		return v, nil
	case errors.Is(err, planrev.ErrAppendFailed) && !definitiveWrite(err):
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "The decision may have been recorded.", Next: "Refresh, then decide again: Bonsai checks GitHub first and never records it twice."}
		return v, nil
	}
	return gardenapi.PlanDecisionView{}, apiError(err)
}

// IssueLink resolves the canonical Issue before passing its URL to a local desktop action.
func (b *Backend) IssueLink(ctx context.Context, issue uint64, open bool) error {
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, _, err := b.target(ctx, false)
	if err != nil {
		return err
	}
	it, err := b.deps.Issues.GetIssue(ctx, t.Repository.ID, garden.IssueID(issue))
	if err != nil {
		return apiError(err)
	}
	if it.ID != garden.IssueID(issue) || it.Repo != t.Repository.ID || it.Number <= 0 {
		return apiError(garden.ErrInvalidRevision)
	}
	parts := strings.Split(t.FullName, "/")
	if len(parts) != 2 {
		return apiError(garden.ErrInvalidRevision)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return apiError(garden.ErrInvalidRevision)
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
				return apiError(garden.ErrInvalidRevision)
			}
		}
	}
	url := fmt.Sprintf("https://github.com/%s/issues/%d", t.FullName, it.Number)
	if open {
		err = b.deps.OpenURL(url)
	} else {
		err = b.deps.CopyText(url)
	}
	if err != nil {
		return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The local Issue link action failed."}
	}
	return nil
}
