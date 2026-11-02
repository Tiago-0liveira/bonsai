package fake

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// Review scripts the optional playgroundapi.ReviewBackend. The fields are
// separate from Backend's so a test that does not use review leaves them empty;
// each call is recorded in Backend.Calls like the others. Set an Err field to
// make that call fail.
type Review struct {
	Policy    playgroundapi.ReviewPolicyView
	PolicyErr error
	// ReviewsBySession is keyed by session ID; a session without an entry is
	// reviewable with no reports.
	ReviewsBySession map[string]playgroundapi.ReviewsView
	ReviewsErr       error
	StartResult      playgroundapi.StartReviewView
	StartErr         error
	DismissResult    playgroundapi.DismissView
	DismissErr       error
	// PreviewBySession is keyed by session ID.
	PreviewBySession map[string]playgroundapi.AcceptPreviewView
	PreviewErr       error
	AcceptResult     playgroundapi.AcceptView
	AcceptErr        error
	Reconcile        playgroundapi.ReconcileView
	ReconcileErr     error

	// Recorded arguments, newest last.
	StartRequests   []playgroundapi.StartReviewRequest
	DismissRequests []playgroundapi.DismissRequest
	AcceptRequests  []playgroundapi.AcceptRequest
}

// WithReview returns a Backend that also implements playgroundapi.ReviewBackend
// by replaying r. Wrap it so a test that wants a Backend without review keeps
// using the plain *Backend, which does not implement ReviewBackend.
func (b *Backend) WithReview(r *Review) *ReviewBackend {
	if r.ReviewsBySession == nil {
		r.ReviewsBySession = map[string]playgroundapi.ReviewsView{}
	}
	if r.PreviewBySession == nil {
		r.PreviewBySession = map[string]playgroundapi.AcceptPreviewView{}
	}
	return &ReviewBackend{Backend: b, R: r}
}

// ReviewBackend is a fake Backend with the review calls.
type ReviewBackend struct {
	*Backend
	R *Review
}

var _ playgroundapi.ReviewBackend = (*ReviewBackend)(nil)

func (b *ReviewBackend) ReviewPolicy(context.Context) (playgroundapi.ReviewPolicyView, error) {
	st := b.begin("ReviewPolicy")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.R.PolicyErr != nil {
		return playgroundapi.ReviewPolicyView{}, b.R.PolicyErr
	}
	v := b.R.Policy
	v.Stamp = st
	return v, nil
}

func (b *ReviewBackend) Reviews(_ context.Context, req playgroundapi.ReviewsRequest) (playgroundapi.ReviewsView, error) {
	st := b.begin("Reviews")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.R.ReviewsErr != nil {
		return playgroundapi.ReviewsView{}, b.R.ReviewsErr
	}
	v, ok := b.R.ReviewsBySession[req.SessionID]
	if !ok {
		v = playgroundapi.ReviewsView{Reviewable: true, AttemptID: "attempt-" + req.SessionID}
	}
	v.Stamp, v.SessionID = st, req.SessionID
	return v, nil
}

func (b *ReviewBackend) StartReview(_ context.Context, req playgroundapi.StartReviewRequest) (playgroundapi.StartReviewView, error) {
	st := b.begin("StartReview")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.R.StartRequests = append(b.R.StartRequests, req)
	if b.R.StartErr != nil {
		return playgroundapi.StartReviewView{}, b.R.StartErr
	}
	v := b.R.StartResult
	v.Stamp = st
	return v, nil
}

func (b *ReviewBackend) DismissFinding(_ context.Context, req playgroundapi.DismissRequest) (playgroundapi.DismissView, error) {
	st := b.begin("DismissFinding")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.R.DismissRequests = append(b.R.DismissRequests, req)
	if b.R.DismissErr != nil {
		return playgroundapi.DismissView{}, b.R.DismissErr
	}
	v := b.R.DismissResult
	v.Stamp, v.FindingID = st, req.FindingID
	return v, nil
}

func (b *ReviewBackend) PreviewAccept(_ context.Context, req playgroundapi.AcceptPreviewRequest) (playgroundapi.AcceptPreviewView, error) {
	st := b.begin("PreviewAccept")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.R.PreviewErr != nil {
		return playgroundapi.AcceptPreviewView{}, b.R.PreviewErr
	}
	v, ok := b.R.PreviewBySession[req.SessionID]
	if !ok {
		v = playgroundapi.AcceptPreviewView{AcceptAllowed: true}
	}
	v.Stamp, v.SessionID = st, req.SessionID
	return v, nil
}

func (b *ReviewBackend) Accept(_ context.Context, req playgroundapi.AcceptRequest) (playgroundapi.AcceptView, error) {
	st := b.begin("Accept")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.R.AcceptRequests = append(b.R.AcceptRequests, req)
	if b.R.AcceptErr != nil {
		return playgroundapi.AcceptView{}, b.R.AcceptErr
	}
	v := b.R.AcceptResult
	v.Stamp = st
	return v, nil
}

func (b *ReviewBackend) ReconcileReviews(context.Context) (playgroundapi.ReconcileView, error) {
	st := b.begin("ReconcileReviews")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.R.ReconcileErr != nil {
		return playgroundapi.ReconcileView{}, b.R.ReconcileErr
	}
	v := b.R.Reconcile
	v.Stamp = st
	return v, nil
}
