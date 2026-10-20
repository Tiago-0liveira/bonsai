package gardenbackend

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// captureTimeout bounds one create or reconcile call. A timeout is ambiguous by
// design: the record turns unknown and only Reconcile may unblock a retry.
const captureTimeout = 45 * time.Second

// captureService builds the capture service on first use, so a backend without
// write seams (tests, read-only builds) never needs them.
func (b *Backend) captureService() (*capture.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.captures != nil {
		return b.captures, nil
	}
	reader, ok := b.deps.Issues.(garden.RepositoryIssueReader)
	if b.deps.Creator == nil || !ok || b.deps.Ops == nil {
		return nil, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Capturing ideas is unavailable in this build."}
	}
	svc, err := capture.New(capture.Deps{Creator: b.deps.Creator, Reader: reader, Store: b.deps.Ops})
	if err != nil {
		return nil, err
	}
	b.captures = svc
	return svc, nil
}

// writeTarget resolves the recorded Garden, requires live Write access and the
// authenticated account. Nothing is written.
func (b *Backend) writeTarget(ctx context.Context) (connect.Target, connect.GitHubIdentity, error) {
	return b.target(ctx, true)
}

// target is writeTarget with the access level chosen: reads need Read access only.
func (b *Backend) target(ctx context.Context, write bool) (connect.Target, connect.GitHubIdentity, error) {
	_, a, err := b.association(ctx)
	if err != nil {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(err)
	}
	switch {
	case a == nil:
		return connect.Target{}, connect.GitHubIdentity{}, &gardenapi.Error{Code: gardenapi.CodeNotConnected, Message: "Garden is not set up for this clone."}
	case a.Mode == connect.Skip:
		return connect.Target{}, connect.GitHubIdentity{}, &gardenapi.Error{Code: gardenapi.CodeSkipped, Message: "Garden is off for this clone."}
	}
	t, _, err := b.resolve(ctx, gardenapi.TargetRef{RepoID: uint64(a.GardenID.Repo)})
	if err != nil {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(err)
	}
	if t.GardenID != a.GardenID {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(ErrRemoteChanged)
	}
	if write {
		if err := requireWriteAccess(t.Repository.Access, t.FullName); err != nil {
			return connect.Target{}, connect.GitHubIdentity{}, err
		}
	}
	if t.Repository.Access < connect.AccessRead {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(connect.ErrPermissionDenied)
	}
	id, err := b.identity(ctx)
	if err != nil {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(err)
	}
	if id.ID == "" {
		return connect.Target{}, connect.GitHubIdentity{}, apiError(connect.ErrAuthRequired)
	}
	return t, id, nil
}

func requireWriteAccess(access connect.Access, fullName string) error {
	if access < connect.AccessWrite {
		return &gardenapi.Error{Code: gardenapi.CodeReadOnly, Message: "Changing ideas needs Write access to " + fullName + ".", Next: "Ask a maintainer for Write access."}
	}
	return nil
}

// PreviewCapture implements gardenapi.Backend.
func (b *Backend) PreviewCapture(ctx context.Context, req gardenapi.CaptureRequest) (gardenapi.CapturePreview, error) {
	gen := b.gen.Add(1)
	t, id, err := b.writeTarget(ctx)
	if err != nil {
		return gardenapi.CapturePreview{}, err
	}
	req.Title = strings.TrimSpace(req.Title)
	opID := "cap-" + b.deps.NewID()
	if err = capture.Validate(captureRequest(opID, b.local, t.Repository.ID, id.ID, req)); err != nil {
		return gardenapi.CapturePreview{}, apiError(err)
	}
	return gardenapi.CapturePreview{
		Stamp: b.stamp(gen, t.Repository.ID), OperationID: opID, Request: req,
		Target: targetView(t), Labels: capture.Labels(req.Type),
	}, nil
}

func captureRequest(opID string, local garden.LocalRepoID, repo garden.RepoID, viewer string, r gardenapi.CaptureRequest) capture.Request {
	return capture.Request{OpID: opID, Local: local, Repo: repo, Viewer: viewer, Title: r.Title, Description: r.Description, Type: r.Type}
}

// Capture implements gardenapi.Backend.
func (b *Backend) Capture(ctx context.Context, p gardenapi.CapturePreview) (gardenapi.CaptureView, error) {
	gen := b.gen.Add(1)
	svc, err := b.captureService()
	if err != nil {
		return gardenapi.CaptureView{}, apiError(err)
	}
	if p.Stamp.LocalRepoID != b.local.String() || p.OperationID == "" {
		return gardenapi.CaptureView{}, apiError(capture.ErrMismatch)
	}
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	t, id, err := b.writeTarget(ctx)
	if err != nil {
		return gardenapi.CaptureView{}, err
	}
	if uint64(t.Repository.ID) != p.Target.RepoID {
		return gardenapi.CaptureView{}, apiError(ErrRemoteChanged)
	}
	res, err := svc.Create(ctx, captureRequest(p.OperationID, b.local, t.Repository.ID, id.ID, p.Request))
	return b.captureView(gen, t.Repository.ID, p.OperationID, p.Request.Title, res, err)
}

// ReconcileCapture implements gardenapi.Backend.
func (b *Backend) ReconcileCapture(ctx context.Context, opID string) (gardenapi.CaptureView, error) {
	gen := b.gen.Add(1)
	svc, err := b.captureService()
	if err != nil {
		return gardenapi.CaptureView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	t, id, err := b.writeTarget(ctx)
	if err != nil {
		return gardenapi.CaptureView{}, err
	}
	res, err := svc.Reconcile(ctx, opID, id.ID)
	if errors.Is(err, ops.ErrNotFound) {
		return gardenapi.CaptureView{}, &gardenapi.Error{Code: gardenapi.CodeOperationNotFound, Message: "That capture is not known to this clone."}
	}
	if res.Record.Repo != 0 && res.Record.Repo != t.Repository.ID {
		return gardenapi.CaptureView{}, apiError(ErrRemoteChanged)
	}
	return b.captureView(gen, t.Repository.ID, opID, res.Record.Title, res, err)
}

// captureView reduces a service outcome to the contract. Errors that mean "not
// started" come back as errors; a started capture always has a state.
func (b *Backend) captureView(gen uint64, repo garden.RepoID, opID, title string, res capture.Result, err error) (gardenapi.CaptureView, error) {
	v := gardenapi.CaptureView{Stamp: b.stamp(gen, repo), OperationID: opID, Title: title}
	if res.Record.Title != "" {
		v.Title = res.Record.Title
	}
	switch id, num := res.Record.IssueID, res.Record.Number; {
	case res.Issue.ID != 0:
		v.Issue = &gardenapi.IssueRef{ID: uint64(res.Issue.ID), Number: res.Issue.Number}
	case id != 0:
		v.Issue = &gardenapi.IssueRef{ID: uint64(id), Number: num}
	}
	unknown := &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "The idea may already exist on GitHub.", Next: "Reconcile to check GitHub. Bonsai will not create it again until then."}
	switch {
	case err == nil && res.Record.State == ops.StateFailed:
		// Reconcile looked and found nothing: the same operation may be retried.
		v.State = gardenapi.CaptureFailed
		return v, nil
	case err == nil:
		v.State = gardenapi.CaptureDone
		return v, nil
	case errors.Is(err, capture.ErrInvalid), errors.Is(err, capture.ErrInFlight), errors.Is(err, capture.ErrMismatch), errors.Is(err, ops.ErrCorrupt),
		errors.Is(err, ops.ErrUnsupportedSchema), errors.Is(err, ops.ErrTooLarge), errors.Is(err, ops.ErrNotFound):
		return gardenapi.CaptureView{}, apiError(err)
	case errors.Is(err, capture.ErrTooSoon):
		v.State = gardenapi.CaptureUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "GitHub may still be catching up.", Next: "Reconcile again in a few seconds."}
	case errors.Is(err, capture.ErrIncomplete):
		v.State = gardenapi.CaptureUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "GitHub could not be checked completely.", Next: "Reconcile again. Bonsai will not create the idea again until it has looked."}
	case errors.Is(err, capture.ErrNeedsReconcile):
		v.State = gardenapi.CaptureUnknown
		v.Err = unknown
	case res.Record.State == ops.StateFailed:
		v.State = gardenapi.CaptureFailed
		v.Err = safeAPIError(err)
	default:
		// A failed read while reconciling leaves the capture unknown.
		v.State = gardenapi.CaptureUnknown
		v.Err = safeAPIError(err)
	}
	return v, nil
}

// pendingCaptures keeps a failed store read distinct from a successful empty
// result, so the UI can retain unresolved captures and block new operations.
func (b *Backend) pendingCaptures(ctx context.Context, gen uint64, repo garden.RepoID) ([]gardenapi.CaptureView, error) {
	if b.deps.Ops == nil {
		return nil, nil
	}
	recs, err := b.deps.Ops.List(ctx, b.local)
	if err != nil {
		return nil, err
	}
	var out []gardenapi.CaptureView
	for _, r := range recs {
		if r.Repo != repo || r.Kind != ops.KindCapture || (r.State != ops.StatePending && r.State != ops.StateUnknown) {
			continue
		}
		out = append(out, gardenapi.CaptureView{Stamp: b.stamp(gen, repo), OperationID: r.ID, State: gardenapi.CaptureState(r.State), Title: r.Title,
			Err: &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "The idea may already exist on GitHub.", Next: "Reconcile to check GitHub."}})
	}
	return out, nil
}
