package gardenbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const (
	// launchReadTimeout bounds a preview, a reconcile or a stop.
	launchReadTimeout = 60 * time.Second
	// launchRunTimeout bounds a start, retry, publish or cleanup: worktree hooks
	// can run for minutes. The caller's own context still ends it sooner.
	launchRunTimeout = 15 * time.Minute

	defaultCols, defaultRows = 80, 24
)

var _ gardenapi.LaunchBackend = (*Backend)(nil)

// unavailableLaunch is shown when this build or host lacks a part of the launch.
var unavailableLaunch = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Starting an implementation locally is unavailable here.",
	Next: "It needs the agent runtime, terminal sessions, the repository daemon and a writable Bonsai config directory."}

// launchService builds the launch service on first use from the injected seams.
// A missing seam disables the launch with a fixed message and nothing falls back.
func (b *Backend) launchService() (*launch.Service, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.launches != nil {
		return b.launches, nil
	}
	d := b.deps
	if d.LaunchStore == nil || d.LaunchAccounts == nil || d.LaunchWorktrees == nil || d.LaunchHolder == nil || d.LaunchSessions == nil ||
		d.Issues == nil || d.Writer == nil {
		return nil, unavailableLaunch
	}
	clock, ids := d.LaunchClock, d.LaunchIDs
	if clock == nil {
		clock = launch.SystemClock()
	}
	if ids == nil {
		ids = launch.SystemIDs()
	}
	svc, err := launch.New(launch.Deps{
		Remote: launchRemote{b: b}, Accounts: d.LaunchAccounts, Worktrees: d.LaunchWorktrees, Holder: d.LaunchHolder,
		Sessions: d.LaunchSessions, PRs: d.LaunchPRs, Store: d.LaunchStore, Clock: clock, IDs: ids, Local: b.local,
	})
	if err != nil {
		return nil, err
	}
	b.launches = svc
	return svc, nil
}

// launchTarget resolves the Issue and the authenticated viewer. Previewing needs
// only Read access: the core decides about Write from the same live read.
func (b *Backend) launchTarget(ctx context.Context, issue uint64) (approve.Target, garden.RepoID, error) {
	if issue == 0 {
		return approve.Target{}, 0, apiError(launch.ErrInvalid)
	}
	t, id, err := b.target(ctx, false)
	if err != nil {
		return approve.Target{}, 0, err
	}
	return approve.Target{Repo: t.Repository.ID, Issue: garden.IssueID(issue), Viewer: id.ID, Login: id.Login, ReadOnly: t.Repository.Access < connect.AccessWrite}, t.Repository.ID, nil
}

func clampDimension(v, fallback int) int {
	switch {
	case v == 0:
		return fallback
	case v < 1:
		return 1
	case v > 500:
		return 500
	}
	return v
}

// launchRequest turns a UI request into the core's, filling the documented
// defaults here: the core never infers them. Branch and name come from the
// approved plan and the Issue, the base from the project config, the remote from
// the repositories the remotes really are.
func (b *Backend) launchRequest(ctx context.Context, req gardenapi.LaunchPreviewRequest, target approve.Target) (launch.Request, error) {
	out := launch.Request{
		Target: target, Branch: req.Branch, BaseRef: req.BaseRef, RemoteName: req.RemoteName, AccountID: req.AccountID,
		Launch: req.Launch, Name: req.Name, Cols: clampDimension(req.Cols, defaultCols), Rows: clampDimension(req.Rows, defaultRows),
	}
	remote := launchRemote{b: b}
	if out.Branch == "" || out.Name == "" {
		snap, err := remote.Verify(ctx, target)
		if err != nil {
			return launch.Request{}, err
		}
		if snap.Review.Approved != nil {
			if out.Branch == "" {
				out.Branch = fmt.Sprintf("bonsai/%d-%s", snap.Issue.Number, shortHash(snap.Review.Approved.Hash))
			}
			if out.Name == "" {
				out.Name = fmt.Sprintf("Implement #%d", snap.Issue.Number)
			}
		}
	}
	if out.BaseRef == "" {
		cfg, err := config.Load(b.dir)
		if err != nil {
			return launch.Request{}, err
		}
		out.BaseRef = cfg.Upstream
	}
	if out.RemoteName == "" {
		name, err := remote.matchingRemote(ctx, target.Repo)
		if err != nil {
			return launch.Request{}, err
		}
		out.RemoteName = name
	}
	return out, nil
}

// localAttempt loads a stored attempt and proves it belongs to this checkout and
// its association. It reads local records only, so it works offline.
func (b *Backend) localAttempt(ctx context.Context, svc *launch.Service, id string) (launch.Attempt, error) {
	a, err := svc.Get(ctx, id)
	if err != nil {
		return launch.Attempt{}, err
	}
	_, assoc, err := b.association(ctx)
	if err != nil {
		return launch.Attempt{}, err
	}
	if assoc != nil && assoc.Mode != connect.Skip && assoc.GardenID.Repo != a.Repo {
		return launch.Attempt{}, launch.ErrOwnership
	}
	return a, nil
}

func (b *Backend) attemptView(gen uint64, a launch.Attempt) gardenapi.LaunchAttemptView {
	return gardenapi.LaunchAttemptView{Stamp: b.stamp(gen, a.Repo), IssueID: uint64(a.Issue), Attempt: a}
}

// PreviewLaunch implements gardenapi.LaunchBackend. It reads and verifies;
// nothing is reserved or started.
func (b *Backend) PreviewLaunch(ctx context.Context, req gardenapi.LaunchPreviewRequest) (gardenapi.LaunchPreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchPreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	t, repo, err := b.launchTarget(ctx, req.IssueID)
	if err != nil {
		return gardenapi.LaunchPreviewView{}, err
	}
	cr, err := b.launchRequest(ctx, req, t)
	if err != nil {
		return gardenapi.LaunchPreviewView{}, apiError(err)
	}
	p, err := svc.Preview(ctx, cr)
	if err != nil {
		return gardenapi.LaunchPreviewView{}, apiError(err)
	}
	return gardenapi.LaunchPreviewView{Stamp: b.stamp(gen, repo), IssueID: req.IssueID, Preview: p}, nil
}

// StartLaunch implements gardenapi.LaunchBackend. The view's stamp is checked
// before any effect, and the core verifies the actor and association again. An
// exact replay of a start already recorded here is answered from local records.
// After the reservation a failure still returns the populated attempt.
func (b *Backend) StartLaunch(ctx context.Context, req gardenapi.LaunchStartRequest) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	p := req.Preview
	if p.Stamp.LocalRepoID != b.local.String() || p.IssueID == 0 || p.Preview.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrOwnership)
	}
	ctx, cancel := context.WithTimeout(ctx, launchRunTimeout)
	defer cancel()
	start := launch.StartRequest{Preview: p.Preview, Confirmed: req.Confirmed, EscalationConfirmed: req.EscalationConfirmed}
	if _, gerr := svc.Get(ctx, p.Preview.ID); gerr == nil {
		a, err := svc.Start(ctx, start)
		return b.attemptView(gen, a), apiError(err)
	}
	_, repo, err := b.launchTarget(ctx, p.IssueID)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	if uint64(repo) != p.Stamp.GardenID || b.stamp(gen, repo).Epoch != p.Stamp.Epoch {
		return gardenapi.LaunchAttemptView{}, apiError(ErrRemoteChanged)
	}
	a, err := svc.Start(ctx, start)
	if a.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	return b.attemptView(gen, a), apiError(err)
}

// LaunchAttempts implements gardenapi.LaunchBackend. It reconciles the local
// attempts of the Issue, which observes and retries the authorized mirror but
// never starts, creates, pushes or opens anything, and works without GitHub.
func (b *Backend) LaunchAttempts(ctx context.Context, issue uint64) (gardenapi.LaunchAttemptsView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptsView{}, apiError(err)
	}
	if issue == 0 {
		return gardenapi.LaunchAttemptsView{}, apiError(launch.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	_, assoc, err := b.association(ctx)
	if err != nil {
		return gardenapi.LaunchAttemptsView{}, apiError(err)
	}
	if assoc == nil || assoc.Mode == connect.Skip {
		return gardenapi.LaunchAttemptsView{}, apiError(launch.ErrOwnership)
	}
	list, err := svc.Reconcile(ctx, garden.IssueID(issue))
	if err != nil {
		return gardenapi.LaunchAttemptsView{}, apiError(err)
	}
	return gardenapi.LaunchAttemptsView{Stamp: b.stamp(gen, assoc.GardenID.Repo), IssueID: issue, Attempts: list}, nil
}

// RetryLaunch implements gardenapi.LaunchBackend.
func (b *Backend) RetryLaunch(ctx context.Context, req launch.RetryRequest) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchRunTimeout)
	defer cancel()
	if _, err = b.localAttempt(ctx, svc, req.AttemptID); err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	a, err := svc.Retry(ctx, req)
	if a.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	return b.attemptView(gen, a), apiError(err)
}

// StopLaunch implements gardenapi.LaunchBackend. It needs no GitHub access.
func (b *Backend) StopLaunch(ctx context.Context, id string) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	if _, err = b.localAttempt(ctx, svc, id); err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	a, err := svc.Stop(ctx, id)
	if a.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	return b.attemptView(gen, a), apiError(err)
}

// PublishLaunch implements gardenapi.LaunchBackend.
func (b *Backend) PublishLaunch(ctx context.Context, req launch.PublishRequest) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchRunTimeout)
	defer cancel()
	if _, err = b.localAttempt(ctx, svc, req.AttemptID); err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	a, err := svc.Publish(ctx, req)
	if a.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	return b.attemptView(gen, a), apiError(err)
}

// PreviewLaunchCleanup implements gardenapi.LaunchBackend. It writes nothing.
func (b *Backend) PreviewLaunchCleanup(ctx context.Context, id string) (gardenapi.LaunchCleanupView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchCleanupView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	a, err := b.localAttempt(ctx, svc, id)
	if err != nil {
		return gardenapi.LaunchCleanupView{}, apiError(err)
	}
	p, err := svc.PreviewCleanup(ctx, id)
	if err != nil {
		return gardenapi.LaunchCleanupView{}, apiError(err)
	}
	return gardenapi.LaunchCleanupView{Stamp: b.stamp(gen, a.Repo), IssueID: uint64(a.Issue), Preview: p}, nil
}

// CleanupLaunch implements gardenapi.LaunchBackend. The preview is sent back
// unchanged; anything that moved since refuses it.
func (b *Backend) CleanupLaunch(ctx context.Context, view gardenapi.LaunchCleanupView, confirmed bool) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	if view.Stamp.LocalRepoID != b.local.String() || view.Preview.AttemptID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrOwnership)
	}
	ctx, cancel := context.WithTimeout(ctx, launchRunTimeout)
	defer cancel()
	if _, err = b.localAttempt(ctx, svc, view.Preview.AttemptID); err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	a, err := svc.Cleanup(ctx, launch.CleanupRequest{Preview: view.Preview, Confirmed: confirmed})
	if a.ID == "" {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	return b.attemptView(gen, a), apiError(err)
}
