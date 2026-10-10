package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	daemonwatcher "github.com/Tiago-0liveira/bonsai/internal/daemon/watcher"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

// StandardUpdateInterval is how often GitHub data is re-read for the project
// in view in standard (polling) update mode; `bonsai web` reports it to the
// user.
const StandardUpdateInterval = providerVisiblePollInterval

// Provider polling cadence. Reads are conditional (ETag), so a poll of an
// unchanged repository is answered with 304s that do not count against
// GitHub's primary rate limit.
const (
	// providerPollTick is how often due polls are looked for; it only compares
	// timestamps.
	providerPollTick = 5 * time.Second
	// providerVisiblePollInterval applies to a project open in some browser.
	providerVisiblePollInterval = 30 * time.Second
	// providerRunningPollInterval applies to a visible project with CI running.
	providerRunningPollInterval = 15 * time.Second
	// providerBackgroundPollInterval applies to every other project.
	providerBackgroundPollInterval = 5 * time.Minute
	// providerRateLimitedFactor stretches every interval while less than 10% of
	// the rate-limit window is left.
	providerRateLimitedFactor = 4
)

const (
	processRefreshInterval  = 30 * time.Second
	watcherRecoveryInterval = 30 * time.Second
	localReadTimeout        = 20 * time.Second
	processReadTimeout      = 5 * time.Second
	localRefreshWorkers     = 4
	providerRefreshWorkers  = 2
)

type refreshScope uint8

const (
	refreshLocal refreshScope = 1 << iota
	refreshProcesses
	refreshProvider
	refreshAll = refreshLocal | refreshProcesses | refreshProvider
)

type contextDaemonClient interface {
	GitContext(context.Context, gitbridge.Command) (*gitbridge.Result, error)
	ListContext(context.Context) ([]*procstore.Record, error)
}

type syncJobState struct {
	syncRunning     bool
	activeUntil     time.Time
	lastSyncAttempt time.Time
	syncRetryAt     time.Time
	syncBackoff     time.Duration
	localRunning    bool
	localPending    bool
	processRunning  bool
	processPending  bool
	providerRunning bool
	providerPending bool
	providerForce   bool
	// providerStartedAt is when the last provider job began; polls are timed
	// from it so any refresh, not only a poll, resets the clock.
	providerStartedAt time.Time
}

type projectProjection struct {
	path     string
	snapshot browserSnapshot
}

type stateSync struct {
	registry projectRegistry
	events   *eventHub
	epoch    string
	now      func() time.Time

	processAuthorityMu sync.Mutex // prevents a late read from overtaking deletion
	mu                 sync.Mutex
	runCtx             context.Context
	closed             bool
	running            bool
	projects           map[string]*projectProjection
	jobs               map[string]*syncJobState
	watchCancels       map[string]context.CancelFunc

	localSem    *priorityGate
	providerSem *priorityGate
	// prefetchSem bounds provider cache warm-ups separately, so a warm-up never
	// holds a provider refresh slot while the real job waits behind it.
	prefetchSem *priorityGate
	gitSyncSem  chan struct{}
	providers   *providerCache

	// priorityProject is the project the user last had open. Its refreshes are
	// admitted before other projects' when workers are scarce.
	priorityProject string
	// focus maps each event subscriber to the project it has in view ("" when
	// its page is hidden). Their union is polled at the visible cadence.
	focus map[int]string
	// liveQueue replaces Queue for live-update deliveries in tests.
	liveQueue func(projectID string, scope refreshScope, forceProvider bool)
}

func newStateSync(registry projectRegistry, events *eventHub) *stateSync {
	return &stateSync{
		registry:     registry,
		events:       events,
		epoch:        randomID(),
		now:          func() time.Time { return time.Now().UTC() },
		runCtx:       context.Background(),
		projects:     map[string]*projectProjection{},
		jobs:         map[string]*syncJobState{},
		watchCancels: map[string]context.CancelFunc{},
		focus:        map[int]string{},
		localSem:     newPriorityGate(localRefreshWorkers),
		providerSem:  newPriorityGate(providerRefreshWorkers),
		prefetchSem:  newPriorityGate(providerRefreshWorkers),
		gitSyncSem:   make(chan struct{}, 2),
		providers:    newProviderCache(),
	}
}

func (s *stateSync) Run(ctx context.Context) {
	s.mu.Lock()
	s.runCtx = ctx
	s.running = true
	s.mu.Unlock()
	s.ReconcileCatalog()
	s.syncWatchers()

	processTicker := time.NewTicker(processRefreshInterval)
	pollTicker := time.NewTicker(providerPollTick)
	syncTicker := time.NewTicker(time.Minute)
	defer processTicker.Stop()
	defer pollTicker.Stop()
	defer syncTicker.Stop()
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.running = false
		for id, cancel := range s.watchCancels {
			cancel()
			delete(s.watchCancels, id)
		}
		s.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-processTicker.C:
			if s.events.count() > 0 {
				s.RefreshAll(refreshProcesses, false)
			}
		case <-pollTicker.C:
			if s.events.count() > 0 {
				s.pollProviders()
			}
		case <-syncTicker.C:
			s.syncActiveProjects()
		}
	}
}
func (s *stateSync) ReconcileCatalog() {
	infos := s.registry.List()
	present := make(map[string]ProjectInfo, len(infos))
	for _, info := range infos {
		present[info.ID] = info
	}
	s.mu.Lock()
	for id := range s.projects {
		if _, ok := present[id]; !ok {
			delete(s.projects, id)
			delete(s.jobs, id)
		}
	}
	for _, info := range infos {
		p := s.ensureLocked(info)
		next := cloneSnapshot(p.snapshot)
		previousRepository := next.Repository
		next.Repository = info
		if previousRepository.FullName != "" && previousRepository.FullName != previousRepository.Name {
			next.Repository.FullName = previousRepository.FullName
		}
		if previousRepository.DefaultBranch != "" {
			next.Repository.DefaultBranch = previousRepository.DefaultBranch
		}
		next.Online = info.Available
		if !info.Available {
			next.Freshness["local"] = unavailableOrStale(next.Freshness["local"], "project_unavailable", "Project directory is unavailable")
			next.Freshness["processes"] = unavailableOrStale(next.Freshness["processes"], "project_unavailable", "Project directory is unavailable")
			next.Freshness["provider"] = unavailableOrStale(next.Freshness["provider"], "project_unavailable", "Project directory is unavailable")
		}
		p.snapshot = next
	}
	s.mu.Unlock()
	s.syncWatchers()
}

// SubscriberReady refreshes every project for a new subscriber. active is the
// project the subscriber has open, if it said so; it is refreshed first, and
// stays the priority project until another one is named.
func (s *stateSync) SubscriberReady(active string) {
	s.ReconcileCatalog()
	if active != "" {
		if _, ok := s.registry.Lookup(active); ok {
			s.mu.Lock()
			s.priorityProject = active
			s.mu.Unlock()
		}
	}
	s.RefreshAll(refreshAll, false)
}

func (s *stateSync) RefreshAll(scope refreshScope, forceProvider bool) {
	infos := s.registry.List()
	first := s.priorityID()
	if first != "" {
		sort.SliceStable(infos, func(i, j int) bool { return infos[i].ID == first && infos[j].ID != first })
	}
	for _, info := range infos {
		if info.Available {
			s.Queue(info.ID, scope, forceProvider)
		}
	}
}

// SetFocus records the project subscriber has in view. The ID is kept even
// when no such project is known yet (a root may still be scanning); it counts
// once the project appears, since the browser does not send it again. A known
// project also becomes the priority project.
func (s *stateSync) SetFocus(subscriber int, projectID string) {
	if len(projectID) > 256 {
		projectID = ""
	}
	_, known := s.registry.Lookup(projectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focus[subscriber] = projectID
	if projectID != "" && known {
		s.priorityProject = projectID
	}
}

// ClearFocus forgets a subscriber that disconnected.
func (s *stateSync) ClearFocus(subscriber int) {
	s.mu.Lock()
	delete(s.focus, subscriber)
	s.mu.Unlock()
}

func (s *stateSync) visibleProjects() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	visible := map[string]bool{}
	for _, projectID := range s.focus {
		if projectID != "" {
			visible[projectID] = true
		}
	}
	return visible
}

// pollProviders queues a provider refresh for every project whose poll is
// due. Projects in view are polled often, the rest rarely, and everything
// slows down while GitHub's rate limit runs low.
func (s *stateSync) pollProviders() {
	now := s.now()
	visible := s.visibleProjects()
	for _, info := range s.registry.List() {
		if !info.Available {
			continue
		}
		project, ok := s.registry.Lookup(info.ID)
		if !ok || project.github == nil {
			continue
		}
		s.mu.Lock()
		j := s.jobLocked(info.ID)
		running, last := j.providerRunning, j.providerStartedAt
		ciRunning := false
		if p := s.projects[info.ID]; p != nil {
			for _, state := range p.snapshot.WorktreeState {
				if state.CI.Status == "running" {
					ciRunning = true
					break
				}
			}
		}
		s.mu.Unlock()
		if running {
			continue
		}
		rate, known := githubRate(project.github)
		if !now.Before(nextProviderPoll(last, visible[info.ID], ciRunning, rate, known, now)) {
			s.queueProvider(info.ID, false)
		}
	}
}

// nextProviderPoll is when a project whose last provider job started at last
// is due again.
func nextProviderPoll(last time.Time, visible, ciRunning bool, rate ghcli.Rate, rateKnown bool, now time.Time) time.Time {
	interval := providerBackgroundPollInterval
	if visible {
		interval = providerVisiblePollInterval
		if ciRunning {
			interval = providerRunningPollInterval
		}
	}
	if rateKnown && rate.Low() && rate.Reset.After(now) {
		interval *= providerRateLimitedFactor
	}
	next := last.Add(interval)
	if rateKnown && rate.Exhausted(now) && next.Before(rate.Reset) {
		// Nothing would succeed before the window resets.
		next = rate.Reset
	}
	return next
}

func (s *stateSync) priorityID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.priorityProject
}

func (s *stateSync) isPriority(projectID string) bool {
	return projectID != "" && s.priorityID() == projectID
}

func (s *stateSync) Queue(projectID string, scope refreshScope, forceProvider bool) {
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	if scope&refreshLocal != 0 {
		s.queueLocal(projectID)
	}
	if scope&refreshProcesses != 0 {
		s.queueProcesses(projectID)
	}
	if scope&refreshProvider != 0 {
		s.queueProvider(projectID, forceProvider)
	}
}

func (s *stateSync) queueLocal(projectID string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	j := s.jobLocked(projectID)
	if j.localRunning {
		j.localPending = true
		s.mu.Unlock()
		return
	}
	j.localRunning = true
	s.mu.Unlock()
	ticket := s.localSem.enter(s.isPriority(projectID))
	go func() {
		<-ticket
		s.refreshLocal(projectID)
		s.localSem.release()
		s.mu.Lock()
		j := s.jobLocked(projectID)
		again := j.localPending
		j.localPending = false
		j.localRunning = false
		s.mu.Unlock()
		if again {
			s.queueLocal(projectID)
		}
	}()
}

func (s *stateSync) queueProcesses(projectID string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	j := s.jobLocked(projectID)
	if j.processRunning {
		j.processPending = true
		s.mu.Unlock()
		return
	}
	j.processRunning = true
	s.mu.Unlock()
	ticket := s.localSem.enter(s.isPriority(projectID))
	go func() {
		<-ticket
		s.refreshProcesses(projectID)
		s.localSem.release()
		s.mu.Lock()
		j := s.jobLocked(projectID)
		again := j.processPending
		j.processPending = false
		j.processRunning = false
		s.mu.Unlock()
		if again {
			s.queueProcesses(projectID)
		}
	}()
}

func (s *stateSync) queueProvider(projectID string, force bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	j := s.jobLocked(projectID)
	if j.providerRunning {
		j.providerPending = true
		j.providerForce = j.providerForce || force
		s.mu.Unlock()
		return
	}
	j.providerRunning = true
	// This flag belongs to the pending job, not the job starting now.
	j.providerForce = false
	s.mu.Unlock()
	ticket := s.providerSem.enter(s.isPriority(projectID))
	go func() {
		<-ticket
		s.mu.Lock()
		s.jobLocked(projectID).providerStartedAt = s.now()
		s.mu.Unlock()
		s.refreshProvider(projectID, force)
		s.providerSem.release()
		s.mu.Lock()
		j := s.jobLocked(projectID)
		again, nextForce := j.providerPending, j.providerForce
		j.providerPending = false
		j.providerForce = false
		j.providerRunning = false
		s.mu.Unlock()
		if again {
			s.queueProvider(projectID, nextForce)
		}
	}()
}

func (s *stateSync) jobLocked(projectID string) *syncJobState {
	j := s.jobs[projectID]
	if j == nil {
		j = &syncJobState{}
		s.jobs[projectID] = j
	}
	return j
}

func (s *stateSync) Snapshot(projectID string) (browserSnapshot, bool) {
	project, ok := s.registry.Lookup(projectID)
	if !ok {
		return browserSnapshot{}, false
	}
	s.mu.Lock()
	p := s.ensureLocked(project.info)
	snapshot := cloneSnapshot(p.snapshot)
	s.mu.Unlock()
	if project.info.Available {
		var scope refreshScope
		if snapshot.Local == nil || snapshot.Freshness["local"].State == "loading" {
			scope |= refreshLocal
		}
		if snapshot.Freshness["processes"].State == "loading" {
			scope |= refreshProcesses
		}
		if snapshot.Local != nil && snapshot.Freshness["provider"].State == "loading" {
			scope |= refreshProvider
		}
		if scope != 0 {
			s.Queue(projectID, scope, false)
		}
	}
	return snapshot, true
}

func (s *stateSync) CachedSnapshot(projectID string) (browserSnapshot, bool) {
	project, ok := s.registry.Lookup(projectID)
	if !ok {
		return browserSnapshot{}, false
	}
	s.mu.Lock()
	p := s.ensureLocked(project.info)
	snapshot := cloneSnapshot(p.snapshot)
	s.mu.Unlock()
	return snapshot, true
}

func (s *stateSync) ensureLocked(info ProjectInfo) *projectProjection {
	p := s.projects[info.ID]
	if p != nil {
		return p
	}
	p = &projectProjection{
		path: info.Path,
		snapshot: browserSnapshot{
			Epoch:         s.epoch,
			Repository:    info,
			Online:        info.Available,
			Metadata:      map[string]worktreeMetadata{},
			Processes:     []browserProcessSummary{},
			Agents:        []agentterminal.Summary{},
			Freshness:     map[string]browserFreshness{},
			WorktreeState: map[string]browserWorktreeState{},
		},
	}
	p.snapshot.Freshness["agents"] = browserFreshness{State: "ready"}
	p.snapshot.Freshness["local"] = browserFreshness{State: "loading"}
	p.snapshot.Freshness["processes"] = browserFreshness{State: "loading"}
	p.snapshot.Freshness["provider"] = browserFreshness{State: "loading"}
	s.projects[info.ID] = p
	return p
}

func (s *stateSync) refreshLocal(projectID string) {
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	defer trace.Start("local.ready", projectID)()
	before, _ := s.CachedSnapshot(projectID)
	previousIdentity := localIdentityToken(before)
	ctx, cancel := s.readContext(localReadTimeout)
	defer cancel()
	if before.Local == nil {
		if local, err := s.readInventory(ctx, project); err == nil {
			local.ID = projectID
			if local.DefaultBranch == "" {
				local.DefaultBranch = project.info.DefaultBranch
			}
			for i := range local.Worktrees {
				local.Worktrees[i].RepositoryID = projectID
				if local.DefaultBranch == "" && local.Worktrees[i].Main {
					local.DefaultBranch = local.Worktrees[i].Branch
				}
			}
			s.commitProject(project, "local", func(snapshot *browserSnapshot) {
				if snapshot.Local != nil {
					return
				}
				snapshot.Local = &local
				snapshot.Repository = descriptorFromLocal(project.info, &local)
				snapshot.Freshness["local"] = browserFreshness{State: "loading"}
			})
			s.queueProcesses(projectID)
			// Remotes are known now; the provider's slow reads need not wait for
			// the per-worktree statuses below.
			if identity, ok := preferredRemote(local.Remotes); ok && identity.FullName != "" {
				s.queueProviderPrefetch(project, identity.FullName)
			}
		}
	}

	value, err := s.gitPayload(ctx, project, "git.repository.refresh", func(raw json.RawMessage) (any, error) {
		var state domain.RepositoryState
		err := json.Unmarshal(raw, &state)
		return state, err
	})
	if err != nil {
		s.commitProject(project, "local", func(snapshot *browserSnapshot) {
			snapshot.Online = true
			snapshot.Freshness["local"] = failedFreshness(snapshot.Freshness["local"], snapshot.Local != nil, err)
		})
		return
	}
	local := value.(domain.RepositoryState)
	local.ID = projectID
	for i := range local.Worktrees {
		local.Worktrees[i].RepositoryID = projectID
	}
	metadata, metadataErr := metadataSnapshotFor(project)
	committed := s.commitProject(project, "local", func(snapshot *browserSnapshot) {
		snapshot.Local = &local
		if metadataErr == nil {
			snapshot.Metadata = metadata
		}
		now := s.now()
		snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &now}
		if metadataErr != nil {
			snapshot.Freshness["local"] = failedFreshness(snapshot.Freshness["local"], true, metadataErr)
		}
		snapshot.Repository = descriptorFromLocal(project.info, &local)
		snapshot.Online = true
		resetWorktreeAssociationsForHeads(snapshot)
	})
	if committed {
		after, ok := s.CachedSnapshot(projectID)
		if ok && previousIdentity != localIdentityToken(after) {
			s.queueProcesses(projectID)
			s.queueProvider(projectID, false)
		} else if ok && after.Freshness["provider"].State == "loading" {
			s.queueProvider(projectID, false)
		}
	}
}

// readInventory returns worktrees, branches and remotes without statuses. It
// asks for git.inventory and falls back to the two older reads when the daemon
// is a version that does not know the command.
func (s *stateSync) readInventory(ctx context.Context, project projectServices) (domain.RepositoryState, error) {
	value, err := s.gitPayload(ctx, project, "git.inventory", func(raw json.RawMessage) (any, error) {
		var state domain.RepositoryState
		err := json.Unmarshal(raw, &state)
		return state, err
	})
	if err == nil {
		return value.(domain.RepositoryState), nil
	}
	if ctx.Err() != nil {
		return domain.RepositoryState{}, err
	}
	// This runs once per project, when its first snapshot is built, so a
	// daemon that predates git.inventory costs one failed call.
	trees, err := s.gitPayload(ctx, project, "git.worktrees", func(raw json.RawMessage) (any, error) {
		var trees []domain.Worktree
		err := json.Unmarshal(raw, &trees)
		return trees, err
	})
	if err != nil {
		return domain.RepositoryState{}, err
	}
	local := domain.RepositoryState{Worktrees: trees.([]domain.Worktree)}
	if branches, err := s.gitPayload(ctx, project, "git.branches", func(raw json.RawMessage) (any, error) {
		var branches []domain.Branch
		err := json.Unmarshal(raw, &branches)
		return branches, err
	}); err == nil {
		local.Branches = branches.([]domain.Branch)
	}
	return local, nil
}

// queueProviderPrefetch warms the provider cache for repository so the real
// provider refresh, which has to wait for the local statuses, finds the
// repository, branches and PR catalog already read. It publishes nothing.
func (s *stateSync) queueProviderPrefetch(project projectServices, repository string) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || project.github == nil {
		return
	}
	ticket := s.prefetchSem.enter(s.isPriority(project.info.ID))
	go func() {
		<-ticket
		defer s.prefetchSem.release()
		ctx, cancel := s.readContext(providerReadTimeout)
		defer cancel()
		s.providers.repository(ctx, project.github, repository, s.now(), false)
	}()
}

func descriptorFromLocal(info ProjectInfo, local *domain.RepositoryState) ProjectInfo {
	out := info
	if local == nil {
		return out
	}
	if local.DefaultBranch != "" {
		out.DefaultBranch = local.DefaultBranch
	}
	if identity, ok := preferredRemote(local.Remotes); ok && identity.FullName != "" {
		out.FullName = identity.FullName
	}
	return out
}

func preferredRemote(remotes []domain.RemoteIdentity) (domain.RemoteIdentity, bool) {
	for _, remote := range remotes {
		if remote.Name == "origin" && remote.FullName != "" {
			return remote, true
		}
	}
	for _, remote := range remotes {
		if remote.FullName != "" {
			return remote, true
		}
	}
	return domain.RemoteIdentity{}, false
}

func (s *stateSync) gitPayload(ctx context.Context, project projectServices, kind string, decode func(json.RawMessage) (any, error)) (any, error) {
	command := gitbridge.Command{
		ID:           randomID(),
		UserID:       localBrowserUserID,
		RepositoryID: localRepositoryID,
		Type:         kind,
		CreatedAt:    s.now(),
	}
	var result *gitbridge.Result
	var err error
	if daemon, ok := project.daemon.(contextDaemonClient); ok {
		result, err = daemon.GitContext(ctx, command)
	} else {
		result, err = project.daemon.Git(command)
	}
	if err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return decode(result.Payload)
}

func (s *stateSync) refreshProcesses(projectID string) {
	s.processAuthorityMu.Lock()
	defer s.processAuthorityMu.Unlock()
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	ctx, cancel := s.readContext(processReadTimeout)
	defer cancel()
	var records []*procstore.Record
	var err error
	var visibility procstore.ProcessVisibility
	if daemon, ok := project.daemon.(interface {
		ProcessAuthorityContext(context.Context) ([]*procstore.Record, procstore.ProcessVisibility, error)
	}); ok {
		records, visibility, err = daemon.ProcessAuthorityContext(ctx)
	} else if daemon, ok := project.daemon.(contextDaemonClient); ok {
		records, err = daemon.ListContext(ctx)
	} else {
		records, err = project.daemon.List()
	}
	if err != nil {
		s.commitProject(project, "processes", func(snapshot *browserSnapshot) {
			snapshot.Freshness["processes"] = failedFreshness(snapshot.Freshness["processes"], snapshot.Processes != nil, err)
		})
		return
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	summaries := make([]browserProcessSummary, 0, len(records))
	s.commitProject(project, "processes", func(snapshot *browserSnapshot) {
		validWorktrees := map[string]bool{}
		haveLocalInventory := snapshot.Local != nil
		if snapshot.Local != nil {
			for _, worktree := range snapshot.Local.Worktrees {
				validWorktrees[worktree.ID] = true
			}
		}
		for _, record := range records {
			summary := processSummary(projectID, record)
			if haveLocalInventory {
				summary.WorktreeID = ""
				for _, worktree := range snapshot.Local.Worktrees {
					if sameWorktreePath(record.Worktree, worktree.Path) {
						summary.WorktreeID = worktree.ID
						break
					}
				}
				if summary.WorktreeID == "" && validWorktrees[publicWorktreeID(record.Worktree)] {
					summary.WorktreeID = publicWorktreeID(record.Worktree)
				}
			}
			summaries = append(summaries, summary)
		}
		snapshot.Processes = summaries
		snapshot.ProcessVisibility = visibility
		now := s.now()
		snapshot.Freshness["processes"] = browserFreshness{State: "ready", UpdatedAt: &now}
	})
}

func sameWorktreePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	cleanA, cleanB := filepath.Clean(a), filepath.Clean(b)
	if cleanA == cleanB || (runtime.GOOS == "windows" && strings.EqualFold(cleanA, cleanB)) {
		return true
	}
	infoA, errA := os.Stat(cleanA)
	infoB, errB := os.Stat(cleanB)
	if errA == nil && errB == nil && os.SameFile(infoA, infoB) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(cleanA)
	resolvedB, errB := filepath.EvalSymlinks(cleanB)
	if errA != nil || errB != nil {
		return false
	}
	return resolvedA == resolvedB || (runtime.GOOS == "windows" && strings.EqualFold(resolvedA, resolvedB))
}

func (s *stateSync) readContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	s.mu.Lock()
	base := s.runCtx
	s.mu.Unlock()
	return context.WithTimeout(base, timeout)
}

func (s *stateSync) commitProject(expected projectServices, component string, mutate func(*browserSnapshot)) bool {
	project, ok := s.registry.Lookup(expected.info.ID)
	if !ok || project.state != expected.state {
		return false
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	p := s.projects[expected.info.ID]
	if p == nil || p.path != project.info.Path {
		s.mu.Unlock()
		return false
	}
	next := cloneSnapshot(p.snapshot)
	next.Repository.Available = project.info.Available
	mutate(&next)
	if next.Local != nil {
		domain.ClassifyWorktrees(next.Local, p.snapshot.Local)
	}
	deriveBranchCandidates(&next)
	next.Epoch = s.epoch
	if snapshotsSemanticallyEqual(p.snapshot, next) {
		s.mu.Unlock()
		return false
	}
	next.Sequence = p.snapshot.Sequence + 1
	p.snapshot = next
	published := cloneSnapshot(next)
	s.mu.Unlock()
	s.events.publish(localEvent{
		Type:      "project_update",
		ProjectID: expected.info.ID,
		Component: component,
		Epoch:     published.Epoch,
		Sequence:  published.Sequence,
		Snapshot:  &published,
	})
	return true
}

func snapshotsSemanticallyEqual(a, b browserSnapshot) bool {
	left := semanticSnapshot(a)
	right := semanticSnapshot(b)
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func semanticSnapshot(in browserSnapshot) browserSnapshot {
	out := cloneSnapshot(in)
	out.Epoch = ""
	out.Sequence = 0
	for key, freshness := range out.Freshness {
		if key != "provider" {
			freshness.UpdatedAt = nil
		}
		out.Freshness[key] = freshness
	}
	for id, state := range out.WorktreeState {
		state.CI.Freshness.UpdatedAt = nil
		out.WorktreeState[id] = state
	}
	return out
}

func (s *stateSync) syncWatchers() {
	infos := s.registry.List()
	desired := make(map[string]ProjectInfo, len(infos))
	for _, info := range infos {
		if info.Available {
			desired[info.ID] = info
		}
	}

	type watcherStart struct {
		ctx  context.Context
		info ProjectInfo
	}
	var starts []watcherStart

	s.mu.Lock()
	if !s.running || s.closed {
		s.mu.Unlock()
		return
	}
	for id, cancel := range s.watchCancels {
		info, ok := desired[id]
		projection := s.projects[id]
		if ok && projection != nil && projection.path == info.Path {
			continue
		}
		cancel()
		delete(s.watchCancels, id)
	}
	for id, info := range desired {
		if s.watchCancels[id] != nil {
			continue
		}
		watchCtx, cancel := context.WithCancel(s.runCtx)
		s.watchCancels[id] = cancel
		starts = append(starts, watcherStart{ctx: watchCtx, info: info})
	}
	s.mu.Unlock()

	for _, start := range starts {
		go s.runProjectWatcher(start.ctx, start.info)
	}
}

func (s *stateSync) runProjectWatcher(ctx context.Context, info ProjectInfo) {
	for ctx.Err() == nil {
		service, err := gitlocal.New([]gitlocal.Config{{
			ID:   localRepositoryID,
			Root: info.Path,
			WithWorktreeRoot: func(_ context.Context, fn func(string) error) error {
				return fn(filepath.Join(info.Path, ".bonsai", "worktrees"))
			},
		}})
		if err == nil {
			w := daemonwatcher.Watcher{
				Local:        service,
				RepositoryID: localRepositoryID,
				Roots:        []string{info.Path},
				DiscoverRoots: func(context.Context) []string {
					roots := []string{info.Path}
					if snapshot, ok := s.CachedSnapshot(info.ID); ok && snapshot.Local != nil {
						for _, tree := range snapshot.Local.Worktrees {
							if tree.Path != "" && tree.Path != info.Path {
								roots = append(roots, tree.Path)
							}
						}
					}
					return roots
				},
				Interval: watcherRecoveryInterval,
				WorktreePaths: func(context.Context) map[string]string {
					snapshot, ok := s.CachedSnapshot(info.ID)
					if !ok || snapshot.Local == nil {
						return nil
					}
					paths := make(map[string]string, len(snapshot.Local.Worktrees))
					for _, worktree := range snapshot.Local.Worktrees {
						paths[worktree.ID] = worktree.Path
					}
					return paths
				},
				Publish: func(_ context.Context, local domain.RepositoryState) error {
					s.commitWatchedLocal(info.ID, local)
					return nil
				},
			}
			err = w.Run(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		s.Queue(info.ID, refreshLocal, false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (s *stateSync) commitWatchedLocal(projectID string, local domain.RepositoryState) {
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	before, _ := s.CachedSnapshot(projectID)
	previousIdentity := localIdentityToken(before)
	local.ID = projectID
	for i := range local.Worktrees {
		local.Worktrees[i].RepositoryID = projectID
	}
	metadata, metadataErr := metadataSnapshotFor(project)
	committed := s.commitProject(project, "local", func(snapshot *browserSnapshot) {
		snapshot.Local = &local
		if metadataErr == nil {
			snapshot.Metadata = metadata
		}
		now := s.now()
		snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &now}
		if metadataErr != nil {
			snapshot.Freshness["local"] = failedFreshness(snapshot.Freshness["local"], true, metadataErr)
		}
		snapshot.Repository = descriptorFromLocal(project.info, &local)
		snapshot.Online = true
		resetWorktreeAssociationsForHeads(snapshot)
	})
	if !committed {
		return
	}
	after, ok := s.CachedSnapshot(projectID)
	if !ok {
		return
	}
	if previousIdentity != localIdentityToken(after) {
		s.queueProcesses(projectID)
		s.queueProvider(projectID, false)
	} else if after.Freshness["provider"].State == "loading" {
		s.queueProvider(projectID, false)
	}
}
func (s *stateSync) commit(projectID, component string, mutate func(*browserSnapshot)) bool {
	project, ok := s.registry.Lookup(projectID)
	if !ok {
		return false
	}
	return s.commitProject(project, component, mutate)
}

func (s *stateSync) MarkStale(projectID string, scope refreshScope) {
	s.commit(projectID, "invalidate", func(snapshot *browserSnapshot) {
		if scope&refreshLocal != 0 {
			snapshot.Freshness["local"] = staleFreshness(snapshot.Freshness["local"])
		}
		if scope&refreshProcesses != 0 {
			snapshot.Freshness["processes"] = staleFreshness(snapshot.Freshness["processes"])
		}
		if scope&refreshProvider != 0 {
			snapshot.Freshness["provider"] = staleFreshness(snapshot.Freshness["provider"])
		}
	})
	if s.events.count() > 0 {
		s.Queue(projectID, scope, scope&refreshProvider != 0)
	}
}

func staleFreshness(current browserFreshness) browserFreshness {
	if current.State == "ready" || current.UpdatedAt != nil {
		current.State = "stale"
		return current
	}
	current.State = "loading"
	return current
}

func failedFreshness(current browserFreshness, hadValue bool, err error) browserFreshness {
	state := "error"
	if hadValue || current.UpdatedAt != nil {
		state = "stale"
	}
	code := domain.Code(err)
	if code == "" {
		code = "unavailable"
	}
	return browserFreshness{State: state, UpdatedAt: current.UpdatedAt, Error: &browserStateError{Code: code, Message: err.Error()}}
}

func unavailableOrStale(current browserFreshness, code, message string) browserFreshness {
	state := "unavailable"
	if current.UpdatedAt != nil || current.State == "ready" || current.State == "stale" {
		state = "stale"
	}
	current.State = state
	current.Error = &browserStateError{Code: code, Message: message}
	return current
}

func markUnavailable(freshness map[string]browserFreshness, component, code, message string) {
	current := freshness[component]
	current.State = "unavailable"
	current.Error = &browserStateError{Code: code, Message: message}
	freshness[component] = current
}

func cloneSnapshot(in browserSnapshot) browserSnapshot {
	out := in
	out.Agents = append(in.Agents[:0:0], in.Agents...)
	if in.Local != nil {
		local := *in.Local
		local.Branches = append([]domain.Branch(nil), in.Local.Branches...)
		local.Worktrees = append([]domain.Worktree(nil), in.Local.Worktrees...)
		local.Remotes = append([]domain.RemoteIdentity(nil), in.Local.Remotes...)
		local.Groups = append([]domain.WorktreeGroup(nil), in.Local.Groups...)
		out.Local = &local
	}
	out.BranchCandidates = append([]browserBranchCandidate(nil), in.BranchCandidates...)
	if in.Remote != nil {
		remote := *in.Remote
		remote.Branches = append([]githubdomain.RemoteBranch(nil), in.Remote.Branches...)
		remote.PullRequests = append([]githubdomain.PullRequest(nil), in.Remote.PullRequests...)
		out.Remote = &remote
	}
	out.Metadata = make(map[string]worktreeMetadata, len(in.Metadata))
	for key, value := range in.Metadata {
		out.Metadata[key] = value
	}
	out.Processes = append([]browserProcessSummary(nil), in.Processes...)
	out.Freshness = make(map[string]browserFreshness, len(in.Freshness))
	for key, value := range in.Freshness {
		if value.Error != nil {
			copyError := *value.Error
			value.Error = &copyError
		}
		out.Freshness[key] = value
	}
	out.WorktreeState = make(map[string]browserWorktreeState, len(in.WorktreeState))
	for key, value := range in.WorktreeState {
		value.CI.Checks = append([]githubdomain.Check(nil), value.CI.Checks...)
		if value.CI.Freshness.Error != nil {
			copyError := *value.CI.Freshness.Error
			value.CI.Freshness.Error = &copyError
		}
		if value.PullRequest != nil {
			copyPR := *value.PullRequest
			value.PullRequest = &copyPR
		}
		out.WorktreeState[key] = value
	}
	return out
}

func resetWorktreeAssociationsForHeads(snapshot *browserSnapshot) {
	if snapshot.Local == nil {
		return
	}
	valid := map[string]bool{}
	for _, worktree := range snapshot.Local.Worktrees {
		valid[worktree.ID] = true
		state := snapshot.WorktreeState[worktree.ID]
		checked := state.CI.CheckedSHA
		if checked != "" && worktree.HeadSHA != "" && checked != worktree.HeadSHA {
			state.CI.Freshness = staleFreshness(state.CI.Freshness)
			snapshot.WorktreeState[worktree.ID] = state
		}
	}
	for id := range snapshot.WorktreeState {
		if !valid[id] {
			delete(snapshot.WorktreeState, id)
		}
	}
}

func localIdentityToken(snapshot browserSnapshot) string {
	if snapshot.Local == nil {
		return ""
	}
	parts := []string{snapshot.Repository.FullName}
	for _, worktree := range snapshot.Local.Worktrees {
		upstream := ""
		if worktree.Status != nil {
			upstream = worktree.Status.Upstream
		}
		parts = append(parts, worktree.ID, worktree.HeadSHA, upstream)
	}
	return strings.Join(parts, "\x00")
}
