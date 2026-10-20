// Package fake is an in-memory gardenapi.Backend for TUI tests. It is
// deterministic and never touches GitHub, Git or the disk: the views it
// returns are the ones the test scripted, stamped like the real backend
// stamps them (a new Generation per call, an Epoch that Skip and Unskip bump).
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Clone is the LocalRepoID every fake reply carries.
const Clone = "project-v1-fake-clone"

// Backend replays scripted views. Fill the exported fields before use; set an
// error field to make that call fail. Call New to get a usable value.
type Backend struct {
	WorkView       gardenapi.WorkView
	WorkErr        error
	StatusView     gardenapi.StatusView
	StatusErr      error
	CandidatesView gardenapi.CandidatesView
	CandidatesErr  error
	ProjectsView   gardenapi.ProjectsView
	ProjectsErr    error
	// PlanView is returned by PreviewPlan with its Request filled from the call.
	PlanView gardenapi.PlanView
	PlanErr  error
	// AppliedView and ResumedView are what Apply and Resume return. After a call
	// without error the status state becomes the view's State.
	AppliedView gardenapi.OperationView
	ApplyErr    error
	ResumedView gardenapi.OperationView
	ResumeErr   error
	SkipErr     error
	UnskipErr   error
	// PreviewCaptureErr, CaptureErr and ReconcileErr make that call fail with an error.
	PreviewCaptureErr error
	CaptureErr        error
	ReconcileErr      error
	// CapturedView is what Capture returns (default: done, Issue #150). When it
	// is done the new draft is added to WorkView so the next Work read shows it.
	CapturedView *gardenapi.CaptureView
	// ReconciledView is what ReconcileCapture returns (default: done, Issue #150).
	ReconciledView *gardenapi.CaptureView

	// Hook runs at the start of every call, outside any lock, with the method
	// name. A test blocks in it to make a reply late.
	Hook func(method string)

	// DocumentErr, PreviewSaveErr, SaveErr, PreviewResolveErr and ResolveErr make that call fail.
	DocumentErr, PreviewSaveErr, SaveErr, PreviewResolveErr, ResolveErr error
	// PreviewConflict, when set, is what PreviewSave and PreviewResolve report as the Conflict in the way.
	PreviewConflict *gardenapi.ConflictView
	// SaveResult, when set, is what Save and Resolve return instead of a successful save.
	SaveResult *gardenapi.SaveView
	// Saved lists the previews Save and Resolve ran, in order.
	Saved []gardenapi.SavePreviewView

	// Captured lists the previews Capture ran, in order, so a test can assert a
	// retry reused the same operation ID.
	Captured []gardenapi.CapturePreview

	mu    sync.Mutex
	gen   uint64
	epoch uint64
	calls []string
	ops   int
}

var _ gardenapi.Backend = (*Backend)(nil)

// New returns a backend whose status is the sample status of state, with the
// sample candidates, Projects, plan and operations scripted.
func New(state gardenapi.State) *Backend {
	return &Backend{
		StatusView:     StatusFor(state),
		CandidatesView: SampleCandidates(false),
		ProjectsView:   SampleProjects(),
		PlanView:       SamplePlan(),
		AppliedView:    SampleOperation(gardenapi.StateConnected),
		ResumedView:    SampleOperation(gardenapi.StateConnected),
		epoch:          1,
	}
}

// Calls lists the methods called so far, in order.
func (b *Backend) Calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

// Count is how many times method was called.
func (b *Backend) Count(method string) int {
	n := 0
	for _, c := range b.Calls() {
		if c == method {
			n++
		}
	}
	return n
}

// Writes counts the calls that change GitHub or the association: Apply,
// Resume, Skip and Unskip. A test that cancels setup asserts it is zero.
func (b *Backend) Writes() int {
	return b.Count("Apply") + b.Count("Resume") + b.Count("Skip") + b.Count("Unskip") + b.Count("Capture") + b.Count("Save") + b.Count("Resolve")
}

// begin records the call and returns its generation and the current epoch.
func (b *Backend) begin(method string) (gen, epoch uint64) {
	// The generation is taken before the hook, as the real backend takes it when
	// a request starts: a call held in the hook carries the older number.
	b.mu.Lock()
	b.calls = append(b.calls, method)
	b.gen++
	gen, epoch = b.gen, b.epoch
	b.mu.Unlock()
	if b.Hook != nil {
		b.Hook(method)
	}
	return gen, epoch
}

// bump advances the epoch for a call that changes the association and returns
// the stamp the reply carries.
func (b *Backend) bump() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.epoch++
	return b.epoch
}

func stamp(gen, epoch, garden uint64) gardenapi.Stamp {
	return gardenapi.Stamp{LocalRepoID: Clone, GardenID: garden, Epoch: epoch, Generation: gen}
}

func (b *Backend) status(gen, epoch uint64) gardenapi.StatusView {
	b.mu.Lock()
	v := b.StatusView
	b.mu.Unlock()
	v.Stamp = stamp(gen, epoch, v.Target.RepoID)
	v.Target.Aliases = append([]string(nil), v.Target.Aliases...)
	if v.Operation != nil {
		op := *v.Operation
		op.Stamp = v.Stamp
		op.Steps = append([]gardenapi.OperationStep(nil), op.Steps...)
		v.Operation = &op
	}
	return v
}

// setState rescripts the status for a new state, keeping the sample target.
func (b *Backend) setState(state gardenapi.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := StatusFor(state)
	if b.StatusView.Target.RepoID != 0 {
		next.Target = b.StatusView.Target
	}
	next.Identity, next.Access = b.StatusView.Identity, b.StatusView.Access
	b.StatusView = next
}

// Status implements gardenapi.Backend.
func (b *Backend) Status(ctx context.Context) (gardenapi.StatusView, error) {
	gen, epoch := b.begin("Status")
	if b.StatusErr != nil {
		return gardenapi.StatusView{}, b.StatusErr
	}
	return b.status(gen, epoch), ctx.Err()
}

// Candidates implements gardenapi.Backend.
func (b *Backend) Candidates(ctx context.Context) (gardenapi.CandidatesView, error) {
	gen, epoch := b.begin("Candidates")
	if b.CandidatesErr != nil {
		return gardenapi.CandidatesView{}, b.CandidatesErr
	}
	v := b.CandidatesView
	v.Candidates = append([]gardenapi.Target(nil), v.Candidates...)
	v.Stamp = stamp(gen, epoch, 0)
	return v, ctx.Err()
}

// Projects implements gardenapi.Backend.
func (b *Backend) Projects(ctx context.Context, target gardenapi.TargetRef) (gardenapi.ProjectsView, error) {
	gen, epoch := b.begin("Projects")
	if b.ProjectsErr != nil {
		return gardenapi.ProjectsView{}, b.ProjectsErr
	}
	v := b.ProjectsView
	v.Projects = append([]gardenapi.Project(nil), v.Projects...)
	v.Stamp = stamp(gen, epoch, target.RepoID)
	return v, ctx.Err()
}

// PreviewPlan implements gardenapi.Backend.
func (b *Backend) PreviewPlan(ctx context.Context, req gardenapi.PlanRequest) (gardenapi.PlanView, error) {
	gen, epoch := b.begin("PreviewPlan")
	if b.PlanErr != nil {
		return gardenapi.PlanView{}, b.PlanErr
	}
	v := b.PlanView
	v.Rows = append([]gardenapi.PlanRow(nil), v.Rows...)
	v.Request = req
	v.Stamp = stamp(gen, epoch, v.Target.RepoID)
	return v, ctx.Err()
}

// Apply implements gardenapi.Backend.
func (b *Backend) Apply(ctx context.Context, plan gardenapi.PlanView) (gardenapi.OperationView, error) {
	gen, epoch := b.begin("Apply")
	if b.ApplyErr != nil {
		return gardenapi.OperationView{}, b.ApplyErr
	}
	epoch = b.bump()
	v := b.AppliedView
	v.Steps = append([]gardenapi.OperationStep(nil), v.Steps...)
	v.Stamp = stamp(gen, epoch, plan.Target.RepoID)
	b.setState(v.State)
	return v, ctx.Err()
}

// Resume implements gardenapi.Backend.
func (b *Backend) Resume(ctx context.Context, operationID string) (gardenapi.OperationView, error) {
	gen, epoch := b.begin("Resume")
	if b.ResumeErr != nil {
		return gardenapi.OperationView{}, b.ResumeErr
	}
	v := b.ResumedView
	v.Steps = append([]gardenapi.OperationStep(nil), v.Steps...)
	v.ID = operationID
	v.Stamp = stamp(gen, epoch, b.StatusView.Target.RepoID)
	b.setState(v.State)
	return v, ctx.Err()
}

// Skip implements gardenapi.Backend.
func (b *Backend) Skip(ctx context.Context, _ gardenapi.TargetRef) (gardenapi.StatusView, error) {
	b.begin("Skip")
	if b.SkipErr != nil {
		return gardenapi.StatusView{}, b.SkipErr
	}
	epoch := b.bump()
	b.setState(gardenapi.StateSkipped)
	gen := b.nextGen()
	return b.status(gen, epoch), ctx.Err()
}

// Unskip implements gardenapi.Backend.
func (b *Backend) Unskip(ctx context.Context) (gardenapi.StatusView, error) {
	b.begin("Unskip")
	if b.UnskipErr != nil {
		return gardenapi.StatusView{}, b.UnskipErr
	}
	epoch := b.bump()
	b.setState(gardenapi.StateNone)
	gen := b.nextGen()
	return b.status(gen, epoch), ctx.Err()
}

// nextGen numbers the status read that follows Skip and Unskip, as the real
// backend does.
func (b *Backend) nextGen() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gen++
	return b.gen
}

func (b *Backend) Work(ctx context.Context) (gardenapi.WorkView, error) {
	gen, epoch := b.begin("Work")
	b.mu.Lock()
	v, err := b.WorkView, b.WorkErr
	repo := b.StatusView.Target.RepoID
	b.mu.Unlock()
	v.Stamp = stamp(gen, epoch, repo)
	v.Snapshot.GardenID = garden.GardenID{Repo: garden.RepoID(repo)}
	v.Snapshot.LocalRepoID = garden.LocalRepoID(Clone)
	v.Snapshot.Generation, v.Snapshot.Epoch = gen, epoch
	if v.Snapshot.Source == "" {
		v.Snapshot.Source = gardensync.SourceLive
		v.Snapshot.Complete = true
		v.Snapshot.SuccessfulAt = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	}
	if err != nil {
		return gardenapi.WorkView{}, err
	}
	return v, ctx.Err()
}

// PreviewCapture implements gardenapi.Backend. Like the real one it validates
// the title and writes nothing.
func (b *Backend) PreviewCapture(ctx context.Context, req gardenapi.CaptureRequest) (gardenapi.CapturePreview, error) {
	gen, epoch := b.begin("PreviewCapture")
	if b.PreviewCaptureErr != nil {
		return gardenapi.CapturePreview{}, b.PreviewCaptureErr
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return gardenapi.CapturePreview{}, &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "a title is required"}
	}
	b.mu.Lock()
	b.ops++
	op := fmt.Sprintf("cap-fake-%d", b.ops)
	target := b.StatusView.Target
	b.mu.Unlock()
	return gardenapi.CapturePreview{
		Stamp: stamp(gen, epoch, target.RepoID), OperationID: op, Request: req, Target: target,
		Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(req.Type), garden.FormatStageLabel(garden.StageDraft)},
	}, ctx.Err()
}

// Capture implements gardenapi.Backend.
func (b *Backend) Capture(ctx context.Context, p gardenapi.CapturePreview) (gardenapi.CaptureView, error) {
	gen, epoch := b.begin("Capture")
	if b.CaptureErr != nil {
		return gardenapi.CaptureView{}, b.CaptureErr
	}
	b.mu.Lock()
	b.Captured = append(b.Captured, p)
	b.mu.Unlock()
	v := gardenapi.CaptureView{State: gardenapi.CaptureDone, Issue: &gardenapi.IssueRef{ID: 9001, Number: 150}}
	if b.CapturedView != nil {
		v = *b.CapturedView
	}
	v.OperationID, v.Title = p.OperationID, p.Request.Title
	v.Stamp = stamp(gen, epoch, p.Target.RepoID)
	if v.State == gardenapi.CaptureDone && v.Issue != nil {
		b.mu.Lock()
		b.WorkView.Snapshot.Items = append(b.WorkView.Snapshot.Items, SampleDraft(garden.IssueID(v.Issue.ID), v.Issue.Number, p.Request.Title, p.Request.Type))
		b.mu.Unlock()
	}
	return v, ctx.Err()
}

// ReconcileCapture implements gardenapi.Backend.
func (b *Backend) ReconcileCapture(ctx context.Context, opID string) (gardenapi.CaptureView, error) {
	gen, epoch := b.begin("ReconcileCapture")
	if b.ReconcileErr != nil {
		return gardenapi.CaptureView{}, b.ReconcileErr
	}
	v := gardenapi.CaptureView{State: gardenapi.CaptureDone, Issue: &gardenapi.IssueRef{ID: 9001, Number: 150}}
	if b.ReconciledView != nil {
		v = *b.ReconciledView
	}
	v.OperationID = opID
	v.Stamp = stamp(gen, epoch, b.StatusView.Target.RepoID)
	if v.State == gardenapi.CaptureDone && v.Issue != nil {
		b.mu.Lock()
		b.WorkView.Snapshot.Items = append(b.WorkView.Snapshot.Items, SampleDraft(garden.IssueID(v.Issue.ID), v.Issue.Number, v.Title, garden.TypeFeature))
		b.WorkView.PendingCaptures = nil
		b.mu.Unlock()
	}
	return v, ctx.Err()
}

func (b *Backend) item(issue uint64) (gardensync.Item, int, bool) {
	for i, it := range b.WorkView.Snapshot.Items {
		if uint64(it.Issue.ID) == issue {
			return it, i, true
		}
	}
	return gardensync.Item{}, 0, false
}

// Document implements gardenapi.Backend from the scripted WorkView: the seed is the
// head revision, or the body when there is none.
func (b *Backend) Document(ctx context.Context, issue uint64) (gardenapi.DocumentView, error) {
	gen, epoch := b.begin("Document")
	if b.DocumentErr != nil {
		return gardenapi.DocumentView{}, b.DocumentErr
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	it, _, ok := b.item(issue)
	if !ok {
		return gardenapi.DocumentView{}, &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That Issue no longer exists on GitHub."}
	}
	body := garden.DisplayBody(it.Issue.Body)
	v := gardenapi.DocumentView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), IssueID: issue, Number: it.Issue.Number, Title: it.Issue.Title,
		Type: it.Classification.Type, Body: body, Base: gardenapi.DocumentBase{BodyHash: garden.ContentHash(body)}}
	var heads []gardenapi.RemoteRevision
	for _, r := range it.Verification.Revisions {
		if r.Kind == garden.RevisionSpec {
			v.Revisions = append(v.Revisions, remoteOf(r))
		}
	}
	for _, h := range it.Verification.Heads {
		if h.Kind == garden.RevisionSpec {
			heads = append(heads, remoteOf(h))
		}
	}
	switch len(heads) {
	case 0:
		v.Seed = spec.Render(spec.Parse(body))
	case 1:
		v.Base.HeadHash, v.Base.HeadCommentID = heads[0].Hash, heads[0].CommentID
		v.Seed = spec.Render(spec.Parse(heads[0].Content))
	default:
		v.Conflict = &gardenapi.ConflictView{IssueID: issue, Base: v.Base, Remote: heads, BodyHash: v.Base.BodyHash}
	}
	v.Base.Content = v.Seed
	return v, ctx.Err()
}

func remoteOf(r garden.Revision) gardenapi.RemoteRevision {
	return gardenapi.RemoteRevision{Hash: r.Hash, CommentID: r.CommentID, Author: r.Author, At: r.CreatedAt, Content: r.Content, Source: edit.Source}
}

func (b *Backend) savePreview(method string, issue uint64, base gardenapi.DocumentBase, content string, res *gardenapi.ConflictView) (gardenapi.SavePreviewView, error) {
	gen, epoch := b.begin(method)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops++
	v := gardenapi.SavePreviewView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), OperationID: fmt.Sprintf("save-fake-%d", b.ops), IssueID: issue}
	if b.PreviewConflict != nil {
		c := *b.PreviewConflict
		v.Conflict = &c
		return v, nil
	}
	canonical := spec.Render(spec.Parse(content))
	v.Base, v.Content, v.NewHash, v.Resolution = base, canonical, garden.ContentHash(canonical), res
	if base.HeadHash != "" {
		v.Parents = []string{base.HeadHash}
	}
	for _, d := range edit.Diff(spec.Parse(base.Content), spec.Parse(canonical)) {
		v.Diff = append(v.Diff, gardenapi.SectionDiff{Section: d.Section, Added: d.Added, Removed: d.Removed})
	}
	if it, _, ok := b.item(issue); ok {
		v.Number = it.Issue.Number
	}
	return v, nil
}

// PreviewSave implements gardenapi.Backend.
func (b *Backend) PreviewSave(ctx context.Context, req gardenapi.SaveRequest) (gardenapi.SavePreviewView, error) {
	if b.PreviewSaveErr != nil {
		b.begin("PreviewSave")
		return gardenapi.SavePreviewView{}, b.PreviewSaveErr
	}
	v, err := b.savePreview("PreviewSave", req.IssueID, req.Base, req.Content, nil)
	if err == nil && v.Conflict == nil && v.NewHash == garden.ContentHash(req.Base.Content) {
		return gardenapi.SavePreviewView{}, &gardenapi.Error{Code: gardenapi.CodeNoChange, Message: "There is nothing to save: the specification already says this."}
	}
	return v, errOr(err, ctx)
}

// PreviewResolve implements gardenapi.Backend.
func (b *Backend) PreviewResolve(ctx context.Context, req gardenapi.ResolveRequest) (gardenapi.SavePreviewView, error) {
	if b.PreviewResolveErr != nil {
		b.begin("PreviewResolve")
		return gardenapi.SavePreviewView{}, b.PreviewResolveErr
	}
	c := req.Conflict
	base := c.Base
	if len(c.Remote) > 0 {
		base.Content = c.Remote[len(c.Remote)-1].Content
	}
	v, err := b.savePreview("PreviewResolve", req.IssueID, base, req.Result, &c)
	v.Parents = nil
	for _, r := range c.Remote {
		if r.Hash != "" {
			v.Parents = append(v.Parents, r.Hash)
		}
	}
	return v, errOr(err, ctx)
}

func errOr(err error, ctx context.Context) error {
	if err != nil {
		return err
	}
	return ctx.Err()
}

// Save implements gardenapi.Backend.
func (b *Backend) Save(ctx context.Context, p gardenapi.SavePreviewView) (gardenapi.SaveView, error) {
	return b.write("Save", b.SaveErr, ctx, p)
}

// Resolve implements gardenapi.Backend.
func (b *Backend) Resolve(ctx context.Context, p gardenapi.SavePreviewView) (gardenapi.SaveView, error) {
	return b.write("Resolve", b.ResolveErr, ctx, p)
}

func (b *Backend) write(method string, fail error, ctx context.Context, p gardenapi.SavePreviewView) (gardenapi.SaveView, error) {
	gen, epoch := b.begin(method)
	if fail != nil {
		return gardenapi.SaveView{}, fail
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Saved = append(b.Saved, p)
	v := gardenapi.SaveView{Stamp: stamp(gen, epoch, b.StatusView.Target.RepoID), OperationID: p.OperationID, IssueID: p.IssueID, State: gardenapi.SaveDone, CommentID: 5000 + int64(len(b.Saved)), Hash: p.NewHash}
	if b.SaveResult != nil {
		r := *b.SaveResult
		r.Stamp, r.OperationID, r.IssueID = v.Stamp, p.OperationID, p.IssueID
		return r, ctx.Err()
	}
	if it, i, ok := b.item(p.IssueID); ok {
		rev := garden.Revision{Kind: garden.RevisionSpec, CommentID: v.CommentID, Hash: p.NewHash, Content: p.Content, Parents: p.Parents, Author: SampleLogin, AuthorID: "1001", CreatedAt: time.Date(2026, 10, 20, 12, 5, 0, 0, time.UTC)}
		it.Verification.Revisions = append(it.Verification.Revisions, rev)
		it.Verification.Heads = []garden.Revision{rev}
		it.Verification.ConflictingHeads = nil
		it.Lane = garden.LaneResult{Lane: garden.LaneDraft, Reason: "specification has not been accepted"}
		b.WorkView.Snapshot.Items[i] = it
	}
	return v, ctx.Err()
}
