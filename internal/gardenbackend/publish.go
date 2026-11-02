package gardenbackend

import (
	"context"
	"errors"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/publish"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// publishTimeout bounds one publish or resume: several GitHub round trips. A timeout is ambiguous by
// design; every step is recorded first, so resuming reuses whatever GitHub already holds.
const publishTimeout = 3 * time.Minute

// publishOpPrefix marks the operation records of publications, including their child captures.
const publishOpPrefix = "publish-"

var _ gardenapi.PrivatePublisher = (*Backend)(nil)

func (b *Backend) publishService() (*publish.Service, error) {
	cap, err := b.captureService()
	if err != nil {
		return nil, err
	}
	ed, err := b.editService()
	if err != nil {
		return nil, err
	}
	ap, err := b.approveService()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.publishes != nil {
		return b.publishes, nil
	}
	if b.deps.PrivateDrafts == nil || b.deps.Ops == nil {
		return nil, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Publishing drafts is unavailable in this build."}
	}
	svc, err := publish.New(publish.Deps{Drafts: b.deps.PrivateDrafts, Ops: b.deps.Ops, Capture: cap, Edit: ed, Approve: ap})
	if err != nil {
		return nil, err
	}
	b.publishes = svc
	return svc, nil
}

// publishRequest rechecks the connection, Write access and the account right now.
func (b *Backend) publishRequest(ctx context.Context, repo garden.RepoID, id drafts.ID) (publish.Request, error) {
	t, who, err := b.target(ctx, true)
	if err != nil {
		return publish.Request{}, err
	}
	if t.Repository.ID != repo {
		return publish.Request{}, apiError(publish.ErrStale)
	}
	return publish.Request{Local: b.local, Repo: t.Repository.ID, RepoFullName: t.FullName, Viewer: who.ID, Login: who.Login, DraftID: id}, nil
}

// PreviewPublish implements gardenapi.PrivatePublisher.
func (b *Backend) PreviewPublish(ctx context.Context, repo garden.RepoID, id drafts.ID) (gardenapi.PublishPreviewView, error) {
	svc, err := b.publishService()
	if err != nil {
		return gardenapi.PublishPreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	req, err := b.publishRequest(ctx, repo, id)
	if err != nil {
		return gardenapi.PublishPreviewView{}, err
	}
	p, err := svc.Preview(ctx, req)
	if err != nil {
		return gardenapi.PublishPreviewView{}, apiError(err)
	}
	return gardenapi.PublishPreviewView{RepoID: uint64(p.Repo), RepoFullName: p.RepoFullName, ActorID: p.Viewer, ActorLogin: p.Login, DraftID: p.DraftID,
		DraftLabel: drafts.DraftLabel(p.DraftID), DraftRevision: p.DraftRevision, Title: p.Title, Type: p.Type, Description: p.Description,
		SpecText: p.SpecText, Labels: p.Labels, ContentHash: p.ContentHash, OperationID: p.OperationID}, nil
}

// Publish implements gardenapi.PrivatePublisher.
func (b *Backend) Publish(ctx context.Context, repo garden.RepoID, v gardenapi.PublishPreviewView) (gardenapi.PublishView, error) {
	svc, err := b.publishService()
	if err != nil {
		return gardenapi.PublishView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	req, err := b.publishRequest(ctx, repo, v.DraftID)
	if err != nil {
		var ae *gardenapi.Error
		if errors.As(err, &ae) && ae.Code == gardenapi.CodeReadOnly {
			// Access was lost since the preview: the confirmation no longer holds.
			return gardenapi.PublishView{}, apiError(publish.ErrStale)
		}
		return gardenapi.PublishView{}, err
	}
	// The preview was made for one repository and one account: a changed connection invalidates it.
	if v.RepoID != uint64(req.Repo) || v.RepoFullName != req.RepoFullName || v.ActorID != req.Viewer {
		return gardenapi.PublishView{}, apiError(publish.ErrStale)
	}
	res, err := svc.Publish(ctx, publish.Preview{Request: req, DraftRevision: v.DraftRevision, Title: v.Title, Type: v.Type, Description: v.Description,
		SpecText: v.SpecText, Labels: v.Labels, ContentHash: v.ContentHash, OperationID: v.OperationID})
	return publishView(res, err)
}

// ResumePublish implements gardenapi.PrivatePublisher.
func (b *Backend) ResumePublish(ctx context.Context, repo garden.RepoID, id drafts.ID) (gardenapi.PublishView, error) {
	svc, err := b.publishService()
	if err != nil {
		return gardenapi.PublishView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	req, err := b.publishRequest(ctx, repo, id)
	if err != nil {
		return gardenapi.PublishView{}, err
	}
	res, err := svc.Resume(ctx, req)
	return publishView(res, err)
}

// publishView reduces a service outcome to the contract. Errors that mean "not started" come back as
// errors; a started publication always has a state and keeps its Issue link.
func publishView(res publish.Result, err error) (gardenapi.PublishView, error) {
	if err != nil {
		return gardenapi.PublishView{}, apiError(err)
	}
	v := gardenapi.PublishView{Step: res.Step}
	if res.Issue != nil {
		v.IssueNumber, v.IssueURL = res.Issue.Number, res.Issue.URL
	}
	switch res.State {
	case publish.Published:
		v.State = gardenapi.PublishDone
	case publish.Failed:
		v.State = gardenapi.PublishFailed
		v.Err = safeAPIError(res.Err)
	case publish.Unknown:
		v.State = gardenapi.PublishUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodePublishUnknown, Message: "The issue may already exist on GitHub.", Next: "Resume to check GitHub. Bonsai will not create it again until it has looked."}
	default:
		v.State = gardenapi.PublishIncomplete
		v.Err = &gardenapi.Error{Code: gardenapi.CodePublishIncomplete, Message: "Publishing is incomplete: the issue exists but its " + stepName(res.Step) + " is not verified.", Next: "Resume to continue. Bonsai reuses what exists and never creates a second issue."}
		if errors.Is(res.Err, publish.ErrConflict) {
			v.Err.Message = "Someone else changed the new issue's specification, so Bonsai stopped without approving it."
			v.Err.Next = "Open the issue on GitHub to review it."
		}
	}
	return v, nil
}

func stepName(step string) string {
	switch step {
	case publish.StepSpec:
		return "specification"
	case publish.StepApproval:
		return "approval"
	case publish.StepVerify:
		return "read-back"
	}
	return "link to the draft"
}
