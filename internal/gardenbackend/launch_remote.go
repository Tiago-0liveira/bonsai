package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// pullRequestSubset exposes only what publishing needs from the GitHub service:
// reading branches and pull requests and opening one. Merge, review, ready,
// close and comment are not reachable through it.
type pullRequestSubset struct{ svc github.GitHubService }

var _ launch.PullRequests = pullRequestSubset{}

func (p pullRequestSubset) Branches(ctx context.Context, repo string) ([]github.RemoteBranch, error) {
	return p.svc.Branches(ctx, repo)
}

func (p pullRequestSubset) PullRequests(ctx context.Context, repo string, f github.PRFilter) ([]github.PullRequest, error) {
	return p.svc.PullRequests(ctx, repo, f)
}

func (p pullRequestSubset) CreatePullRequest(ctx context.Context, r github.CreatePullRequestRequest) (github.PullRequest, error) {
	return p.svc.CreatePullRequest(ctx, r)
}

// launchRemote is the Garden side of a launch: a fresh verified read for every
// confirmation and the expected-label stage mirror. It never reuses the read
// model, the retained sync snapshot or a cached authorization.
type launchRemote struct{ b *Backend }

var _ launch.Remote = launchRemote{}

// resolved is the live target and account of one call, with raw errors the core
// classifies.
type resolved struct {
	target connect.Target
	id     connect.GitHubIdentity
	write  bool
}

// live re-resolves the association, the numeric repository, current access and
// the authenticated account.
func (r launchRemote) live(ctx context.Context) (resolved, error) {
	b := r.b
	b.invalidateAuth()
	_, a, err := b.association(ctx)
	switch {
	case err != nil:
		return resolved{}, err
	case a == nil || a.Mode == connect.Skip:
		return resolved{}, fmt.Errorf("%w: Garden is not set up for this clone", launch.ErrOwnership)
	}
	t, _, err := b.resolve(ctx, gardenapi.TargetRef{RepoID: uint64(a.GardenID.Repo)})
	if err != nil {
		if errors.Is(err, ErrRemoteChanged) || errors.Is(err, connect.ErrRepositoryIdentityChanged) {
			return resolved{}, fmt.Errorf("%w: %w", launch.ErrOwnership, err)
		}
		return resolved{}, err
	}
	if t.GardenID != a.GardenID {
		return resolved{}, fmt.Errorf("%w: %w", launch.ErrOwnership, ErrRemoteChanged)
	}
	if t.Repository.Access < connect.AccessRead {
		return resolved{}, connect.ErrPermissionDenied
	}
	id, err := b.identity(ctx)
	if err != nil {
		return resolved{}, err
	}
	if id.ID == "" {
		return resolved{}, connect.ErrAuthRequired
	}
	return resolved{target: t, id: id, write: t.Repository.Access >= connect.AccessWrite}, nil
}

// Verify reads the Issue's whole comment log with current access and derives the
// plan review for the same actor. Live is true only when identity, access and the
// log were all read fresh: a fully read but unapproved log is live evidence of a
// blocker, not a valid approval.
func (r launchRemote) Verify(ctx context.Context, target approve.Target) (launch.RemoteSnapshot, error) {
	live, err := r.live(ctx)
	if err != nil {
		return launch.RemoteSnapshot{}, err
	}
	gate, err := r.b.approveService()
	if err != nil {
		return launch.RemoteSnapshot{}, err
	}
	t := approve.Target{Repo: live.target.Repository.ID, Issue: target.Issue, Viewer: live.id.ID, Login: live.id.Login, ReadOnly: !live.write}
	log, err := gate.Read(ctx, t)
	if err != nil {
		return launch.RemoteSnapshot{}, err
	}
	return launch.RemoteSnapshot{
		Target: t, Local: r.b.local, Epoch: r.b.stamp(0, t.Repo).Epoch, Issue: log.Issue, FullName: live.target.FullName,
		Review: planrev.Derive(log, t.Viewer), Live: log.Verification.Complete, VerifiedAt: time.Now().UTC(),
	}, nil
}

// previousStages are the stages an Issue may show before it moves to stage. A
// blocked attempt that is retried moves from Blocked back to In progress.
func previousStages(stage garden.Stage) []garden.Stage {
	switch stage {
	case garden.StageInProgress:
		return []garden.Stage{garden.StageReady, garden.StageBlocked}
	case garden.StageReview, garden.StageBlocked:
		return []garden.Stage{garden.StageInProgress}
	}
	return nil
}

// Mirror moves the stage label once, with the exact current labels as the
// expected previous state. It refuses, without writing, when the association,
// actor, epoch or approved records are no longer the ones the attempt ran, or
// when the Issue shows any stage but the expected one: an external change is
// never overwritten. A stage already shown is a no-op.
func (r launchRemote) Mirror(ctx context.Context, req launch.MirrorRequest) error {
	if req.Local != r.b.local {
		return fmt.Errorf("%w: another checkout", garden.ErrMetadataChanged)
	}
	allowed := previousStages(req.Stage)
	if len(allowed) == 0 {
		return fmt.Errorf("%w: unsupported stage", garden.ErrMetadataChanged)
	}
	live, err := r.live(ctx)
	if err != nil {
		return err
	}
	if live.target.Repository.ID != req.Target.Repo || live.id.ID != req.Target.Viewer || r.b.stamp(0, req.Target.Repo).Epoch != req.Epoch {
		return fmt.Errorf("%w: association or actor changed", garden.ErrMetadataChanged)
	}
	if !live.write {
		return connect.ErrPermissionDenied
	}
	gate, err := r.b.approveService()
	if err != nil {
		return err
	}
	t := approve.Target{Repo: req.Target.Repo, Issue: req.Target.Issue, Viewer: live.id.ID, Login: live.id.Login}
	log, err := gate.Read(ctx, t)
	if err != nil {
		return err
	}
	if !log.Verification.Complete {
		return fmt.Errorf("%w: incomplete log", garden.ErrOffline)
	}
	current, ok := launch.CurrentInputs(planrev.Derive(log, t.Viewer))
	if !ok || current != req.Inputs {
		return fmt.Errorf("%w: the approved records changed", garden.ErrMetadataChanged)
	}
	if meta, merr := garden.DecodeMetadata(log.Issue.Metadata); merr != nil || meta.Owner != live.id.ID {
		return fmt.Errorf("%w: capture owner", garden.ErrMetadataChanged)
	}
	c := garden.Classify(log.Issue.Labels)
	if len(c.Problems) > 0 {
		return fmt.Errorf("%w: conflicting labels", garden.ErrMetadataChanged)
	}
	from := c.Stage
	if from == req.Stage {
		return nil
	}
	if !slices.Contains(allowed, from) {
		return fmt.Errorf("%w: the Issue shows another stage", garden.ErrMetadataChanged)
	}
	w := r.b.deps.Writer
	if w == nil {
		return fmt.Errorf("%w: no writer", garden.ErrOffline)
	}
	return w.UpdateStage(ctx, req.Target.Repo, req.Target.Issue, log.Issue.Labels, from, req.Stage)
}

// matchingRemote picks the remote a launch pushes to by default: origin when it
// is the selected repository itself, otherwise the only remote that is. A fork
// is never chosen by name alone.
func (r launchRemote) matchingRemote(ctx context.Context, repo garden.RepoID) (string, error) {
	remotes, err := r.b.deps.Remotes.ListRemotes(ctx)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, remote := range remotes {
		c, err := r.b.deps.Resolver.ResolveRepository(ctx, remote)
		if err != nil || c.ID != repo {
			continue
		}
		matches = append(matches, remote.Name)
	}
	for _, name := range matches {
		if name == "origin" {
			return name, nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", nil
}

func shortHash(h string) string { return strings.TrimSpace(h[:min(8, len(h))]) }
