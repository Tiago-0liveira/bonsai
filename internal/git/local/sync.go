package local

import (
	"context"
	"strings"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

// SyncRepository holds the same gate as create/delete/other Git mutations for
// the entire fetch and guarded pull. Network failures are component outcomes.
func (s *Service) SyncRepository(ctx context.Context, id string, policy domain.PullPolicy) (domain.RepositorySync, error) {
	if !policy.FastForwardOnly {
		return domain.RepositorySync{}, domain.ErrInvalid
	}
	r, err := s.repo(id)
	if err != nil {
		return domain.RepositorySync{}, err
	}
	unlock, err := r.lock(ctx)
	if err != nil {
		return domain.RepositorySync{}, err
	}
	defer unlock()
	result := domain.RepositorySync{}
	remotes, err := trimmed(ctx, r.Root, "remote")
	if err != nil {
		return result, err
	}
	hasOrigin := false
	for _, name := range strings.Fields(remotes) {
		if name == "origin" {
			hasOrigin = true
		}
	}
	if !hasOrigin {
		reason := "no_origin"
		if remotes == "" {
			reason = "no_remote"
		}
		result.Fetch = domain.SyncOutcome{State: "skipped", Reason: reason}
		result.Pull = domain.SyncOutcome{State: "skipped", Reason: reason}
		return result, nil
	}
	// Cover narrow clones without rewriting remote.origin.fetch.
	_, err = run(ctx, r.Root, "fetch", "--prune", "--no-tags", "origin", "+refs/heads/*:refs/remotes/origin/*")
	if err != nil {
		result.Fetch = domain.SyncOutcome{State: "error", Error: err.Error()}
		result.Pull = domain.SyncOutcome{State: "skipped", Reason: "fetch_failed"}
		return result, nil
	}
	now := time.Now().UTC()
	result.Fetch = domain.SyncOutcome{State: "ready", CompletedAt: &now}
	st, err := statusOverview(ctx, r.Root, nil)
	if err != nil {
		result.Pull = domain.SyncOutcome{State: "skipped", Reason: "status_unavailable", Error: err.Error()}
		return result, nil
	}
	reason := ""
	switch {
	case st.GitState != "normal":
		reason = "git_operation_in_progress"
	case st.HeadState == "detached":
		reason = "detached_head"
	case st.HeadState == "unborn":
		reason = "unborn_branch"
	case st.Dirty:
		reason = "dirty_worktree"
	case st.Upstream == "":
		reason = "no_upstream"
	case st.LocalRemoteRefSHA == "":
		reason = "upstream_missing"
	case !strings.HasPrefix(st.Upstream, "origin/"):
		reason = "upstream_not_origin"
	case st.Ahead > 0 && st.Behind > 0:
		reason = "diverged"
	}
	if reason != "" {
		result.Pull = domain.SyncOutcome{State: "skipped", Reason: reason}
		return result, nil
	}
	_, err = run(ctx, r.Root, "pull", "--ff-only", "--no-rebase", "--no-autostash", "--no-edit")
	if err != nil {
		result.Pull = domain.SyncOutcome{State: "error", Error: err.Error()}
		return result, nil
	}
	now = time.Now().UTC()
	result.Pull = domain.SyncOutcome{State: "ready", CompletedAt: &now}
	return result, nil
}
