// Package fake is an in-memory gardenapi.Backend for TUI tests. It is
// deterministic and never touches GitHub, Git or the disk: the views it
// returns are the ones the test scripted, stamped like the real backend
// stamps them (a new Generation per call, an Epoch that Skip and Unskip bump).
package fake

import (
	"context"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
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

	// Hook runs at the start of every call, outside any lock, with the method
	// name. A test blocks in it to make a reply late.
	Hook func(method string)

	mu    sync.Mutex
	gen   uint64
	epoch uint64
	calls []string
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
	return b.Count("Apply") + b.Count("Resume") + b.Count("Skip") + b.Count("Unskip")
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
