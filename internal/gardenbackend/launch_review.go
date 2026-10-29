package gardenbackend

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func (b *Backend) resolveLaunchReview(ctx context.Context) gardenapi.LaunchReview {
	load := b.deps.LaunchReviewPrefs
	if load == nil {
		load = func() (*config.GardenAutoReviewPrefs, error) {
			state, err := config.LoadState()
			if err != nil {
				return nil, err
			}
			return state.PrefsSnapshot().GardenAutoReview, nil
		}
	}
	prefs, err := load()
	if err != nil {
		return gardenapi.LaunchReview{Enabled: true, Reason: "The automatic review preferences could not be read."}
	}
	resolved, err := reviewpass.Resolve(ctx, prefs, nil, b.deps.LaunchAccounts)
	if err != nil {
		return gardenapi.LaunchReview{Enabled: prefs != nil && prefs.Enabled, Reason: "The reviewer availability could not be checked."}
	}
	view := gardenapi.LaunchReview{Enabled: resolved.Enabled, Available: resolved.Available, Reason: resolved.Reason, Policy: resolved.Policy}
	if view.Enabled && view.Available {
		view.Hash = view.Policy.Hash()
	}
	return view
}

func (b *Backend) launchReviewStore() (reviewpass.Store, error) {
	if b.deps.LaunchReviews != nil {
		return b.deps.LaunchReviews, nil
	}
	return reviewpass.Open()
}

func launchReviewID(attempt string) string {
	sum := sha256.Sum256([]byte(attempt))
	return fmt.Sprintf("rp-%x", sum[:16])
}

func (b *Backend) launchReviewPass(ctx context.Context, a launch.Attempt) (*reviewpass.Pass, bool) {
	if a.ID == "" {
		return nil, false
	}
	store, err := b.launchReviewStore()
	if err != nil {
		return nil, true
	}
	p, err := store.Get(ctx, launchReviewID(a.ID))
	if err != nil {
		return nil, !errors.Is(err, reviewpass.ErrNotFound)
	}
	if p.Snapshot.AttemptID != a.ID || p.Snapshot.Local != b.local || p.Snapshot.Repo != a.Repo || p.Snapshot.Issue != a.Issue {
		return nil, true
	}
	return &p, false
}

func (b *Backend) reserveLaunchReview(ctx context.Context, p launch.Preview, review gardenapi.LaunchReview, actor string, escalation bool) (reviewpass.Pass, error) {
	store, err := b.launchReviewStore()
	if err != nil {
		return reviewpass.Pass{}, fmt.Errorf("open review passes: %w", err)
	}
	snapshot := reviewpass.Snapshot{
		Local: b.local, Repo: p.Request.Target.Repo, Issue: p.Request.Target.Issue, Number: p.Remote.Issue.Number,
		Inputs:    reviewpass.Inputs{SpecRevision: p.Inputs.Spec.RevisionHash, SpecFingerprint: p.Inputs.Spec.Fingerprint, SpecApprovalID: p.Inputs.SpecApprovalID, PlanRevision: p.Inputs.Plan.RevisionHash, PlanDecisionID: p.Inputs.Plan.DecisionID},
		AttemptID: p.ID, Worktree: reviewpass.Worktree{Path: p.Proposal.Path, Branch: p.Proposal.Branch, BaseSHA: p.Proposal.BaseSHA}, Policy: review.Policy, PolicyHash: review.Hash,
	}
	id := launchReviewID(p.ID)
	matches := func(prior reviewpass.Pass) bool {
		return reflect.DeepEqual(prior.Snapshot, snapshot) && prior.Authorization.ActorID == actor && prior.Authorization.PreviewID == p.ID
	}
	prior, err := store.Get(ctx, id)
	if err == nil {
		if !matches(prior) {
			return reviewpass.Pass{}, reviewpass.ErrConflict
		}
		return prior, nil
	}
	if !errors.Is(err, reviewpass.ErrNotFound) {
		return reviewpass.Pass{}, fmt.Errorf("read review pass: %w", err)
	}
	clock := b.deps.LaunchClock
	if clock == nil {
		clock = launch.SystemClock()
	}
	now := clock.Now().UTC()
	pass, err := reviewpass.NewPass(id, snapshot, reviewpass.Authorization{ActorID: actor, PreviewID: p.ID, ConfirmedAt: now, EscalationAcknowledged: escalation}, now)
	if err != nil {
		return reviewpass.Pass{}, fmt.Errorf("authorize review pass: %w", err)
	}
	reserved, _, err := store.Reserve(ctx, pass)
	if errors.Is(err, reviewpass.ErrConflict) {
		// Concurrent confirmations have different timestamps but authorize the
		// same frozen sequence. Keep the first reservation's consent unchanged.
		if prior, readErr := store.Get(ctx, id); readErr == nil && matches(prior) {
			return prior, nil
		}
	}
	if err != nil {
		return reviewpass.Pass{}, fmt.Errorf("reserve review pass: %w", err)
	}
	return reserved, nil
}
