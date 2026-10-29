package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// ViewerID is the account ID the scripted plan revisions and decisions are attributed to.
const ViewerID = "1001"

func (b *Backend) planLog(issue uint64) (approve.Log, int, bool) {
	it, i, ok := b.item(issue)
	if !ok {
		return approve.Log{}, 0, false
	}
	return approve.Log{Issue: it.Issue, Verification: it.Verification, Comments: b.PlanComments[issue]}, i, true
}

func notFound() error {
	return &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That Issue no longer exists on GitHub."}
}

// AddPlanRevision scripts a plan revision on the Issue, written by author (ViewerID for the viewer) for the
// current specification and superseding the current plan heads. The text is used as given, so a test can script an
// incomplete or foreign plan. It returns the revision as the verified log would carry it.
func (b *Backend) AddPlanRevision(issue uint64, content, authorID string) garden.Revision {
	b.mu.Lock()
	defer b.mu.Unlock()
	log, i, ok := b.planLog(issue)
	if !ok {
		panic(fmt.Sprintf("fake: no Issue %d", issue))
	}
	rv := planrev.Derive(log, ViewerID)
	var parents []string
	for _, h := range rv.Heads() {
		parents = append(parents, h.Hash)
	}
	text := garden.NormalizeContent(content)
	next := int64(8000 + len(log.Verification.Revisions) + len(log.Verification.PlanDecisions))
	r := garden.Revision{Version: 1, Kind: garden.RevisionPlan, Content: text, Hash: garden.ContentHash(text), SourceSpecFingerprint: rv.Gate.Fingerprint,
		CommentID: next, Parents: parents, Author: SampleLogin, AuthorID: authorID, CreatedAt: time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC).Add(time.Duration(next-8000) * time.Second)}
	b.WorkView.Snapshot.Items[i].Verification.Revisions = append(b.WorkView.Snapshot.Items[i].Verification.Revisions, r)
	return r
}

// AddPlanDecision scripts a verified decision on a plan revision, as another client or gate two would record it.
func (b *Backend) AddPlanDecision(issue uint64, rev garden.Revision, d garden.Decision, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, i, ok := b.planLog(issue)
	if !ok {
		panic(fmt.Sprintf("fake: no Issue %d", issue))
	}
	v := &b.WorkView.Snapshot.Items[i].Verification
	id := int64(8000 + len(v.Revisions) + len(v.PlanDecisions))
	text := "Decision on plan."
	if reason != "" {
		text += "\n\n" + reason
	}
	v.PlanDecisions = append(v.PlanDecisions, garden.PlanDecision{RevisionCommentID: rev.CommentID, Decision: d, RevisionHash: rev.Hash, SourceSpecFingerprint: rev.SourceSpecFingerprint, CommentID: id,
		Author: SampleLogin, AuthorID: ViewerID, CreatedAt: time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC).Add(time.Duration(id-8000) * time.Second), Reason: text, Source: "external"})
}

// PlanReview implements gardenapi.Backend by deriving the review from the scripted verification, exactly as the
// real backend derives it from a verified log.
func (b *Backend) PlanReview(ctx context.Context, issue uint64) (gardenapi.PlanReviewView, error) {
	gen, epoch := b.begin("PlanReview")
	if b.PlanReviewErr != nil {
		return gardenapi.PlanReviewView{}, b.PlanReviewErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	log, _, ok := b.planLog(issue)
	if !ok {
		return gardenapi.PlanReviewView{}, notFound()
	}
	rv := planrev.Derive(log, ViewerID)
	rv.VerifiedAt = time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	return gardenapi.PlanReviewView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), IssueID: issue, Actor: SampleLogin, ActorID: ViewerID, Review: rv}, ctx.Err()
}

// PreviewPlanPublish implements gardenapi.Backend with the real preparation rules over the scripted attempts.
func (b *Backend) PreviewPlanPublish(ctx context.Context, req gardenapi.PlanPublishRequest) (gardenapi.PlanPublishPreviewView, error) {
	gen, epoch := b.begin("PreviewPlanPublish")
	if b.PreviewPlanPublishErr != nil {
		return gardenapi.PlanPublishPreviewView{}, b.PreviewPlanPublishErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	log, _, ok := b.planLog(req.IssueID)
	if !ok {
		return gardenapi.PlanPublishPreviewView{}, notFound()
	}
	var a plan.Attempt
	found := false
	for _, x := range b.attempts {
		if x.ID == req.AttemptID {
			a, found = x, true
		}
	}
	if !found {
		return gardenapi.PlanPublishPreviewView{}, &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That planner attempt no longer exists."}
	}
	t := planrev.Target{Repo: log.Issue.Repo, Issue: log.Issue.ID, Viewer: ViewerID, Login: SampleLogin}
	p, err := planrev.Prepare(t, a, req.Text, planrev.Derive(log, ViewerID))
	if err != nil {
		return gardenapi.PlanPublishPreviewView{}, &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: err.Error()}
	}
	b.ops++
	return gardenapi.PlanPublishPreviewView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), OperationID: fmt.Sprintf("publish-plan-fake-%d", b.ops), IssueID: req.IssueID,
		Number: log.Issue.Number, Title: log.Issue.Title, Actor: SampleLogin, Preview: p}, ctx.Err()
}

// PublishPlan implements gardenapi.Backend: the revision joins the scripted verification, so the next PlanReview shows it.
func (b *Backend) PublishPlan(ctx context.Context, p gardenapi.PlanPublishPreviewView) (gardenapi.PlanPublishView, error) {
	gen, epoch := b.begin("PublishPlan")
	if b.PublishPlanErr != nil {
		return gardenapi.PlanPublishView{}, b.PublishPlanErr
	}
	b.mu.Lock()
	b.PlanPublished = append(b.PlanPublished, p)
	b.mu.Unlock()
	r := b.AddPlanRevision(p.IssueID, p.Preview.Content, ViewerID)
	b.mu.Lock()
	defer b.mu.Unlock()
	return gardenapi.PlanPublishView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), OperationID: p.OperationID, IssueID: p.IssueID, State: gardenapi.PlanWriteDone,
		CommentID: r.CommentID, Hash: r.Hash, Ordinal: p.Preview.Ordinal}, ctx.Err()
}

// DecidePlan implements gardenapi.Backend: the decision joins the scripted verification.
func (b *Backend) DecidePlan(ctx context.Context, req gardenapi.PlanDecisionRequest) (gardenapi.PlanDecisionView, error) {
	gen, epoch := b.begin("DecidePlan")
	if b.DecidePlanErr != nil {
		return gardenapi.PlanDecisionView{}, b.DecidePlanErr
	}
	b.mu.Lock()
	b.PlanDecided = append(b.PlanDecided, req)
	log, _, ok := b.planLog(req.IssueID)
	b.mu.Unlock()
	if !ok {
		return gardenapi.PlanDecisionView{}, notFound()
	}
	rv := planrev.Derive(log, ViewerID)
	if req.Decision == garden.DecisionApproved && (req.ActorID != ViewerID || rv.Gate.Head == nil || rv.Gate.Head.Hash != req.SpecHash || rv.Current == nil || rv.Current.Hash != req.RevisionHash || !rv.CanDecide(req.Decision)) {
		return gardenapi.PlanDecisionView{}, &gardenapi.Error{Code: gardenapi.CodeConflict, Message: "Approval heads or actor changed."}
	}
	rev, found := rv.ByHash(req.RevisionHash)
	if !found {
		return gardenapi.PlanDecisionView{}, &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That plan revision does not belong to this Issue."}
	}
	b.AddPlanDecision(req.IssueID, rev.Revision, req.Decision, req.Reason)
	b.mu.Lock()
	defer b.mu.Unlock()
	_, idx, _ := b.planLog(req.IssueID)
	ds := b.WorkView.Snapshot.Items[idx].Verification.PlanDecisions
	ds[len(ds)-1].Source = "local"
	return gardenapi.PlanDecisionView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), IssueID: req.IssueID, State: gardenapi.PlanWriteDone, Ordinal: rev.Ordinal,
		Decision: req.Decision}, ctx.Err()
}

// PreviewPlanRevise implements gardenapi.Backend with the scripted preview.
func (b *Backend) PreviewPlanRevise(ctx context.Context, issue uint64) (gardenapi.PlanRevisePreviewView, error) {
	gen, epoch := b.begin("PreviewPlanRevise")
	if b.PreviewPlanReviseErr != nil {
		return gardenapi.PlanRevisePreviewView{}, b.PreviewPlanReviseErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	v := b.PlanRevisePreview
	v.Stamp, v.IssueID = stamp(gen, epoch, b.StatusView.Target.RepoID), issue
	return v, ctx.Err()
}

// RevisePlan implements gardenapi.Backend with the scripted result.
func (b *Backend) RevisePlan(ctx context.Context, req gardenapi.PlanReviseRequest) (gardenapi.PlanReviseView, error) {
	gen, epoch := b.begin("RevisePlan")
	if b.RevisePlanErr != nil {
		return gardenapi.PlanReviseView{}, b.RevisePlanErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.PlanRevised = append(b.PlanRevised, req)
	v := b.PlanReviseResult
	v.Stamp, v.IssueID = stamp(gen, epoch, b.StatusView.Target.RepoID), req.IssueID
	return v, ctx.Err()
}

// StartPlanRevise implements gardenapi.Backend with the scripted result.
func (b *Backend) StartPlanRevise(ctx context.Context, req gardenapi.PlanReviseRequest) (gardenapi.PlanReviseView, error) {
	gen, epoch := b.begin("StartPlanRevise")
	if b.StartPlanReviseErr != nil {
		return gardenapi.PlanReviseView{}, b.StartPlanReviseErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.PlanReviseStarted = append(b.PlanReviseStarted, req)
	v := b.PlanReviseResult
	v.Stamp, v.IssueID = stamp(gen, epoch, b.StatusView.Target.RepoID), req.IssueID
	return v, ctx.Err()
}

// IssueLink records a desktop action without opening a browser or clipboard in tests.
func (b *Backend) IssueLink(ctx context.Context, issue uint64, open bool) error {
	action := "CopyIssueLink"
	if open {
		action = "OpenIssueLink"
	}
	b.begin(action)
	return ctx.Err()
}
