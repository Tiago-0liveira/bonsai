package gardenbackend

import (
	"context"
	"errors"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func (b *Backend) approveService() (*approve.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.approvals != nil {
		return b.approvals, nil
	}
	if b.deps.Issues == nil || b.deps.Writer == nil {
		return nil, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Validating specifications is unavailable in this build."}
	}
	svc, err := approve.New(approve.Deps{Reader: b.deps.Issues, Writer: b.deps.Writer})
	if err != nil {
		return nil, err
	}
	b.approvals = svc
	return svc, nil
}

func (b *Backend) approveTarget(ctx context.Context, issue uint64) (approve.Target, garden.RepoID, error) {
	t, id, err := b.target(ctx, true)
	if err != nil {
		return approve.Target{}, 0, err
	}
	if issue == 0 {
		return approve.Target{}, 0, apiError(approve.ErrWrongIssue)
	}
	return approve.Target{Repo: t.Repository.ID, Issue: garden.IssueID(issue), Viewer: id.ID, Login: id.Login}, t.Repository.ID, nil
}

// PreviewValidate implements gardenapi.Backend.
func (b *Backend) PreviewValidate(ctx context.Context, issue uint64) (gardenapi.ValidatePreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.approveService()
	if err != nil {
		return gardenapi.ValidatePreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.approveTarget(ctx, issue)
	if err != nil {
		return gardenapi.ValidatePreviewView{}, err
	}
	p, err := svc.Preview(ctx, t)
	if err != nil {
		return gardenapi.ValidatePreviewView{}, apiError(err)
	}
	return gardenapi.ValidatePreviewView{Stamp: b.stamp(gen, repo), OperationID: "validate-" + b.deps.NewID(), IssueID: issue, Number: p.Number, Title: p.Title,
		Type: p.Type, Actor: t.Login, Gate: p.Assessment, Steps: p.Steps, Labels: p.Labels}, nil
}

// Validate implements gardenapi.Backend.
func (b *Backend) Validate(ctx context.Context, p gardenapi.ValidatePreviewView) (gardenapi.ValidateView, error) {
	gen := b.gen.Add(1)
	svc, err := b.approveService()
	if err != nil {
		return gardenapi.ValidateView{}, apiError(err)
	}
	if p.Stamp.LocalRepoID != b.local.String() || p.Gate.Head == nil {
		return gardenapi.ValidateView{}, apiError(approve.ErrWrongIssue)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.approveTarget(ctx, p.IssueID)
	if err != nil {
		return gardenapi.ValidateView{}, err
	}
	if uint64(repo) != p.Stamp.GardenID {
		return gardenapi.ValidateView{}, apiError(ErrRemoteChanged)
	}
	res, err := svc.Validate(ctx, approve.Preview{Target: t, Number: p.Number, Title: p.Title, Type: p.Type, Assessment: p.Gate, Steps: p.Steps, Labels: p.Labels})
	v := gardenapi.ValidateView{Stamp: b.stamp(gen, repo), OperationID: p.OperationID, IssueID: p.IssueID, ApprovalCommentID: res.ApprovalCommentID,
		RevisionHash: res.RevisionHash, Done: res.Done}
	switch {
	case err == nil:
		v.State = gardenapi.ValidateDone
		return v, nil
	case errors.Is(err, approve.ErrProjection):
		v.State = gardenapi.ValidatePartial
		v.Err = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The approval is recorded, but the Issue's stage label could not be updated. The Issue is still a draft.",
			Next: "Validate again to finish: Bonsai finds the recorded approval and never writes a second one."}
		return v, nil
	case errors.Is(err, approve.ErrUnverified):
		v.State = gardenapi.ValidateUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "Bonsai could not confirm the result on GitHub.", Next: "Refresh to read the current state, then validate again if the Issue is still a draft."}
		return v, nil
	case errors.Is(err, approve.ErrAppendFailed) && !definitiveWrite(err):
		// The approval may have reached GitHub. Validating again finds it by the log, not by appending.
		v.State = gardenapi.ValidateUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "The approval may have been recorded.", Next: "Validate again: Bonsai checks GitHub first and never records it twice."}
		return v, nil
	}
	return gardenapi.ValidateView{}, apiError(err)
}
