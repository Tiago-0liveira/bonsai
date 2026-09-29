package localapi

import (
	"context"
	"strings"
	"sync"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

const (
	providerReadyTTL       = 60 * time.Second
	providerPendingTTL     = 15 * time.Second
	providerErrorBackoff   = 15 * time.Second
	providerMaxErrorBackoff = 60 * time.Second
	providerReadTimeout    = 30 * time.Second
)

type providerRepoEntry struct {
	value     *browserRemoteSnapshot
	expiresAt time.Time
	retryAt   time.Time
	backoff   time.Duration
	running   bool
	wait      chan struct{}
	lastErr   error
}

type providerChecksEntry struct {
	value     []githubdomain.Check
	updatedAt *time.Time
	expiresAt time.Time
	retryAt   time.Time
	backoff   time.Duration
	running   bool
	wait      chan struct{}
	lastErr   error
}

type providerCache struct {
	mu     sync.Mutex
	repos  map[string]*providerRepoEntry
	checks map[string]*providerChecksEntry
}

func newProviderCache() *providerCache {
	return &providerCache{
		repos:  map[string]*providerRepoEntry{},
		checks: map[string]*providerChecksEntry{},
	}
}

func (c *providerCache) repository(ctx context.Context, service githubdomain.GitHubService, repository string, now time.Time, force bool) (*browserRemoteSnapshot, browserFreshness) {
	for {
		c.mu.Lock()
		entry := c.repos[repository]
		if entry == nil {
			entry = &providerRepoEntry{}
			c.repos[repository] = entry
		}
		if entry.running {
			wait := entry.wait
			cached := cloneRemote(entry.value)
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return cached, browserFreshness{State: "error", Error: &browserStateError{Code: "cancelled", Message: ctx.Err().Error()}}
			case <-wait:
				continue
			}
		}
		if entry.lastErr != nil && now.Before(entry.retryAt) {
			value := cloneRemote(entry.value)
			freshness := providerFailureFreshness(value != nil, valueTime(value), entry.lastErr)
			c.mu.Unlock()
			return value, freshness
		}
		if !force && entry.value != nil && now.Before(entry.expiresAt) {
			value := cloneRemote(entry.value)
			updated := value.UpdatedAt
			c.mu.Unlock()
			return value, browserFreshness{State: "ready", UpdatedAt: &updated}
		}
		entry.running = true
		entry.wait = make(chan struct{})
		c.mu.Unlock()

		repo, repoErr := service.Repository(ctx, repository)
		branches, branchErr := service.Branches(ctx, repository)
		prs, prErr := service.PullRequests(ctx, repository, githubdomain.PRFilter{State: "all"})
		err := firstError(repoErr, branchErr, prErr)
		var value *browserRemoteSnapshot
		if err == nil {
			value = &browserRemoteSnapshot{Repository: repo, Branches: branches, PullRequests: prs, UpdatedAt: now}
		}

		c.mu.Lock()
		entry = c.repos[repository]
		entry.running = false
		if err == nil {
			entry.value = value
			entry.expiresAt = now.Add(providerReadyTTL)
			entry.retryAt = time.Time{}
			entry.backoff = 0
			entry.lastErr = nil
		} else {
			if entry.backoff == 0 {
				entry.backoff = providerErrorBackoff
			} else {
				entry.backoff *= 2
				if entry.backoff > providerMaxErrorBackoff {
					entry.backoff = providerMaxErrorBackoff
				}
			}
			entry.retryAt = now.Add(entry.backoff)
			entry.lastErr = err
		}
		close(entry.wait)
		cached := cloneRemote(entry.value)
		lastErr := entry.lastErr
		c.mu.Unlock()
		if lastErr != nil {
			return cached, providerFailureFreshness(cached != nil, valueTime(cached), lastErr)
		}
		updated := value.UpdatedAt
		return cloneRemote(value), browserFreshness{State: "ready", UpdatedAt: &updated}
	}
}

func (c *providerCache) checksFor(ctx context.Context, service githubdomain.GitHubService, repository, sha string, now time.Time, force bool) ([]githubdomain.Check, browserFreshness) {
	key := repository + "\x00" + sha
	for {
		c.mu.Lock()
		entry := c.checks[key]
		if entry == nil {
			entry = &providerChecksEntry{}
			c.checks[key] = entry
		}
		if entry.running {
			wait := entry.wait
			cached := append([]githubdomain.Check(nil), entry.value...)
			var updatedAt *time.Time
			if entry.updatedAt != nil {
				copy := *entry.updatedAt
				updatedAt = &copy
			}
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return cached, browserFreshness{State: "error", UpdatedAt: updatedAt, Error: &browserStateError{Code: "cancelled", Message: ctx.Err().Error()}}
			case <-wait:
				continue
			}
		}
		if entry.lastErr != nil && now.Before(entry.retryAt) {
			value := append([]githubdomain.Check(nil), entry.value...)
			freshness := providerFailureFreshness(entry.updatedAt != nil, entry.updatedAt, entry.lastErr)
			c.mu.Unlock()
			return value, freshness
		}
		if !force && entry.updatedAt != nil && now.Before(entry.expiresAt) {
			value := append([]githubdomain.Check(nil), entry.value...)
			updated := *entry.updatedAt
			c.mu.Unlock()
			return value, browserFreshness{State: "ready", UpdatedAt: &updated}
		}
		entry.running = true
		entry.wait = make(chan struct{})
		c.mu.Unlock()

		checks, err := service.Checks(ctx, repository, sha)

		c.mu.Lock()
		entry = c.checks[key]
		entry.running = false
		if err == nil {
			entry.value = append([]githubdomain.Check(nil), checks...)
			updated := now
			entry.updatedAt = &updated
			ttl := providerReadyTTL
			if checksRollup(checks) == "running" {
				ttl = providerPendingTTL
			}
			entry.expiresAt = now.Add(ttl)
			entry.retryAt = time.Time{}
			entry.backoff = 0
			entry.lastErr = nil
		} else {
			if entry.backoff == 0 {
				entry.backoff = providerErrorBackoff
			} else {
				entry.backoff *= 2
				if entry.backoff > providerMaxErrorBackoff {
					entry.backoff = providerMaxErrorBackoff
				}
			}
			entry.retryAt = now.Add(entry.backoff)
			entry.lastErr = err
		}
		close(entry.wait)
		value := append([]githubdomain.Check(nil), entry.value...)
		updatedAt, lastErr := entry.updatedAt, entry.lastErr
		c.mu.Unlock()
		if lastErr != nil {
			return value, providerFailureFreshness(updatedAt != nil, updatedAt, lastErr)
		}
		updated := now
		return value, browserFreshness{State: "ready", UpdatedAt: &updated}
	}
}

func providerFailureFreshness(hadValue bool, updatedAt any, err error) browserFreshness {
	state := "error"
	if hadValue {
		state = "stale"
	}
	var updated *time.Time
	switch value := updatedAt.(type) {
	case *time.Time:
		if value != nil {
			copy := *value
			updated = &copy
		}
	case time.Time:
		if !value.IsZero() {
			copy := value
			updated = &copy
		}
	}
	code := domain.Code(err)
	if code == "" {
		code = "provider_unavailable"
	}
	return browserFreshness{State: state, UpdatedAt: updated, Error: &browserStateError{Code: code, Message: err.Error()}}
}

func valueTime(value *browserRemoteSnapshot) *time.Time {
	if value == nil || value.UpdatedAt.IsZero() {
		return nil
	}
	updated := value.UpdatedAt
	return &updated
}

func cloneRemote(value *browserRemoteSnapshot) *browserRemoteSnapshot {
	if value == nil {
		return nil
	}
	out := *value
	out.Branches = append([]githubdomain.RemoteBranch(nil), value.Branches...)
	out.PullRequests = append([]githubdomain.PullRequest(nil), value.PullRequests...)
	return &out
}

func firstError(values ...error) error {
	for _, err := range values {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *stateSync) refreshProvider(projectID string, force bool) {
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	before, ok := s.CachedSnapshot(projectID)
	if !ok || before.Local == nil {
		return
	}
	identity, ok := preferredRemote(before.Local.Remotes)
	if !ok || identity.FullName == "" {
		s.commitProviderIfCurrent(project, localIdentityToken(before), func(snapshot *browserSnapshot) {
			snapshot.Remote = nil
			markUnavailable(snapshot.Freshness, "provider", "provider_unavailable", "No supported provider remote is configured")
			for _, worktree := range snapshot.Local.Worktrees {
				snapshot.WorktreeState[worktree.ID] = browserWorktreeState{
					CI: browserCIState{Status: "unknown", Checks: []githubdomain.Check{}, Freshness: browserFreshness{State: "unavailable", Error: &browserStateError{Code: "provider_unavailable", Message: "No supported provider remote is configured"}}},
				}
			}
		})
		return
	}
	token := localIdentityToken(before)
	ctx, cancel := s.readContext(providerReadTimeout)
	defer cancel()
	remote, providerFreshness := s.providers.repository(ctx, project.github, identity.FullName, s.now(), force)
	if remote == nil {
		s.commitProviderIfCurrent(project, token, func(snapshot *browserSnapshot) {
			snapshot.Freshness["provider"] = providerFreshness
			for id, state := range snapshot.WorktreeState {
				state.CI.Freshness = staleFreshness(state.CI.Freshness)
				snapshot.WorktreeState[id] = state
			}
		})
		return
	}

	states := map[string]browserWorktreeState{}
	for _, worktree := range before.Local.Worktrees {
		state := browserWorktreeState{CI: browserCIState{Status: "unknown", Checks: []githubdomain.Check{}, Freshness: providerFreshness}}
		headRepo, headBranch := worktreeProviderHead(before.Local, worktree, identity)
		candidates := []githubdomain.PullRequest{}
		if headRepo != "" && headBranch != "" {
			for _, pull := range remote.PullRequests {
				if pull.State == "open" && pull.Head == headBranch && strings.EqualFold(pull.HeadRepository, headRepo) {
					candidates = append(candidates, pull)
				}
			}
		}
		if len(candidates) == 1 {
			pull := candidates[0]
			state.PullRequest = &pull
		} else if len(candidates) > 1 {
			state.PRDiagnostic = "ambiguous open pull requests for provider head"
		}

		sha := ciSHA(before.Local, worktree, state.PullRequest, identity)
		checkRepository := identity.FullName
		if headRepo != "" {
			checkRepository = headRepo
		}
		if state.PullRequest != nil && state.PullRequest.HeadRepository != "" {
			checkRepository = state.PullRequest.HeadRepository
		}
		if sha == "" || checkRepository == "" {
			state.CI.Freshness = browserFreshness{State: "unavailable", Error: &browserStateError{Code: "sha_unavailable", Message: "No provider repository and commit SHA are known for checks"}}
			states[worktree.ID] = state
			continue
		}
		checks, freshness := s.providers.checksFor(ctx, project.github, checkRepository, sha, s.now(), force)
		state.CI.CheckedSHA = sha
		state.CI.Checks = checks
		state.CI.Status = checksRollup(checks)
		state.CI.Freshness = freshness
		if freshness.State == "error" && freshness.UpdatedAt == nil {
			state.CI.Status = "unknown"
		}
		states[worktree.ID] = state
	}

	s.commitProviderIfCurrent(project, token, func(snapshot *browserSnapshot) {
		snapshot.Remote = remote
		snapshot.Freshness["provider"] = providerFreshness
		snapshot.WorktreeState = states
		if remote.Repository.FullName != "" {
			snapshot.Repository.FullName = remote.Repository.FullName
		}
		if snapshot.Repository.DefaultBranch == "" {
			snapshot.Repository.DefaultBranch = remote.Repository.DefaultBranch
		}
	})
}

func (s *stateSync) commitProviderIfCurrent(project projectServices, token string, mutate func(*browserSnapshot)) {
	current, ok := s.CachedSnapshot(project.info.ID)
	if !ok || localIdentityToken(current) != token {
		s.queueProvider(project.info.ID, false)
		return
	}
	s.commitProject(project, "provider", func(snapshot *browserSnapshot) {
		if localIdentityToken(*snapshot) != token {
			return
		}
		mutate(snapshot)
	})
}

func worktreeProviderHead(local *domain.RepositoryState, worktree domain.Worktree, preferred domain.RemoteIdentity) (string, string) {
	upstream := ""
	if worktree.Status != nil {
		upstream = worktree.Status.Upstream
	}
	if upstream == "" {
		for _, branch := range local.Branches {
			if !branch.Remote && branch.Name == worktree.Branch {
				upstream = branch.Upstream
				break
			}
		}
	}
	if remoteName, branch, ok := strings.Cut(upstream, "/"); ok {
		for _, remote := range local.Remotes {
			if remote.Name == remoteName && remote.FullName != "" {
				return remote.FullName, branch
			}
		}
	}
	for _, branch := range local.Branches {
		if branch.Remote && branch.Name == preferred.Name+"/"+worktree.Branch && branch.LocalRemoteRefSHA != "" {
			return preferred.FullName, worktree.Branch
		}
	}
	return "", ""
}

func ciSHA(local *domain.RepositoryState, worktree domain.Worktree, pull *githubdomain.PullRequest, preferred domain.RemoteIdentity) string {
	if pull != nil && pull.HeadSHA != "" {
		return pull.HeadSHA
	}
	if worktree.Status != nil && worktree.Status.LocalRemoteRefSHA != "" {
		return worktree.Status.LocalRemoteRefSHA
	}
	for _, branch := range local.Branches {
		if branch.Remote && branch.Name == preferred.Name+"/"+worktree.Branch && branch.LocalRemoteRefSHA != "" {
			return branch.LocalRemoteRefSHA
		}
	}
	if worktree.HeadSHA != "" {
		return worktree.HeadSHA
	}
	if worktree.Status != nil {
		return worktree.Status.HeadSHA
	}
	return ""
}

func checksRollup(checks []githubdomain.Check) string {
	if len(checks) == 0 {
		return "none"
	}
	running, unknown := false, false
	for _, check := range checks {
		status := strings.ToLower(check.Status)
		conclusion := strings.ToLower(check.Conclusion)
		if status != "completed" && conclusion == "" {
			running = true
			continue
		}
		switch conclusion {
		case "success", "neutral", "skipped":
		case "failure", "error", "cancelled", "canceled", "timed_out", "action_required", "startup_failure":
			return "failed"
		case "":
			if status != "completed" {
				running = true
			} else {
				unknown = true
			}
		default:
			unknown = true
		}
	}
	if running {
		return "running"
	}
	if unknown {
		return "unknown"
	}
	return "passed"
}

