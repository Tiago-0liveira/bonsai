package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

type syncTestRegistry struct {
	mu      sync.Mutex
	entries map[string]projectServices
}

func (r *syncTestRegistry) Default() projectServices {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.entries {
		return value
	}
	return projectServices{}
}
func (r *syncTestRegistry) Lookup(id string) (projectServices, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.entries[id]
	return value, ok
}
func (r *syncTestRegistry) Worktree(context.Context, string) (projectServices, bool) {
	return projectServices{}, false
}
func (r *syncTestRegistry) Refresh(context.Context) (bool, error) { return false, nil }
func (r *syncTestRegistry) List() []ProjectInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProjectInfo, 0, len(r.entries))
	for _, value := range r.entries {
		out = append(out, value.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (r *syncTestRegistry) remove(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

type syncTestDaemon struct {
	root                string
	repositoryBlock     <-chan struct{}
	repositoryStarted   chan struct{}
	repositoryStartOnce sync.Once
	records             []*procstore.Record
	// extraWorktrees adds linked worktrees, each with its own upstream SHA.
	extraWorktrees int
	// noInventory makes git.inventory fail like a daemon that predates it.
	noInventory bool
	// onCommand, when set, sees every command before it is served.
	onCommand func(gitbridge.Command)
}

func (d *syncTestDaemon) worktrees(mainStatus *domain.WorkingTreeStatus) []domain.Worktree {
	main := domain.Worktree{ID: gitlocal.ID(localRepositoryID, d.root), RepositoryID: localRepositoryID, Path: d.root, Branch: "main", HeadSHA: "local-head", Main: true, Status: mainStatus}
	out := []domain.Worktree{main}
	for i := 0; i < d.extraWorktrees; i++ {
		path := filepath.Join(d.root, fmt.Sprintf("wt-%02d", i))
		tree := domain.Worktree{ID: gitlocal.ID(localRepositoryID, path), RepositoryID: localRepositoryID, Path: path, Branch: fmt.Sprintf("feature-%02d", i), HeadSHA: fmt.Sprintf("head-%02d", i)}
		if mainStatus != nil {
			st := *mainStatus
			st.Branch, st.HeadSHA, st.LocalRemoteRefSHA, st.Upstream = tree.Branch, tree.HeadSHA, fmt.Sprintf("remote-%02d", i), "origin/"+tree.Branch
			tree.Status = &st
		}
		out = append(out, tree)
	}
	return out
}

func (d *syncTestDaemon) result(command gitbridge.Command) (*gitbridge.Result, error) {
	status := &domain.WorkingTreeStatus{
		Branch:              "main",
		HeadState:           "branch",
		HeadSHA:             "local-head",
		Upstream:            "origin/main",
		LocalRemoteRefSHA:   "remote-head",
		DivergenceAvailable: true,
		Files:               []domain.FileStatus{},
		Conflicted:          []string{},
	}
	var payload any
	if d.onCommand != nil {
		d.onCommand(command)
	}
	switch command.Type {
	case "git.inventory":
		if d.noInventory {
			return &gitbridge.Result{ID: command.ID, Error: &domain.Error{Code: "invalid", Message: "invalid"}}, nil
		}
		payload = domain.RepositoryState{
			ID: localRepositoryID,
			Branches: []domain.Branch{
				{Name: "main", LocalHeadSHA: "local-head", Upstream: "origin/main"},
				{Name: "origin/main", Remote: true, LocalRemoteRefSHA: "remote-head"},
			},
			Worktrees: d.worktrees(nil),
			Remotes:   []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: "acme", Repository: "repo", FullName: "acme/repo"}},
		}
	case "git.branches":
		payload = []domain.Branch{
			{Name: "main", LocalHeadSHA: "local-head", Upstream: "origin/main"},
			{Name: "origin/main", Remote: true, LocalRemoteRefSHA: "remote-head"},
		}
	case "git.worktrees":
		payload = d.worktrees(nil)
	case "git.repository.refresh":
		if d.repositoryBlock != nil {
			<-d.repositoryBlock
		}
		payload = domain.RepositoryState{
			ID:            localRepositoryID,
			DefaultBranch: "main",
			Branches: []domain.Branch{
				{Name: "main", LocalHeadSHA: "local-head", Upstream: "origin/main"},
				{Name: "origin/main", Remote: true, LocalRemoteRefSHA: "remote-head"},
			},
			Worktrees: d.worktrees(status),
			Remotes:   []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: "acme", Repository: "repo", FullName: "acme/repo"}},
		}
	default:
		return nil, errors.New("unexpected git command: " + command.Type)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &gitbridge.Result{ID: command.ID, Payload: raw}, nil
}
func (d *syncTestDaemon) Git(command gitbridge.Command) (*gitbridge.Result, error) {
	return d.result(command)
}
func (d *syncTestDaemon) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	if command.Type == "git.repository.refresh" && d.repositoryBlock != nil {
		if d.repositoryStarted != nil {
			d.repositoryStartOnce.Do(func() { close(d.repositoryStarted) })
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-d.repositoryBlock:
		}
		copy := syncTestDaemon{root: d.root, records: d.records, extraWorktrees: d.extraWorktrees, noInventory: d.noInventory, onCommand: d.onCommand}
		return copy.result(command)
	}
	return d.result(command)
}
func (d *syncTestDaemon) List() ([]*procstore.Record, error) { return d.records, nil }
func (d *syncTestDaemon) ListContext(ctx context.Context) ([]*procstore.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.records, nil
}
func (d *syncTestDaemon) Restart(int) (*procstore.Record, error) { return nil, nil }
func (d *syncTestDaemon) Kill(int, bool, string) ([]int, error)  { return nil, nil }
func (d *syncTestDaemon) Logs(int, bool, int, string, bool, func(string) error) error {
	return nil
}

type syncTestGitHub struct {
	mu              sync.Mutex
	repositoryCalls int
	branchCalls     int
	prCalls         int
	checkCalls      int
	block           <-chan struct{}
	checksBlock     <-chan struct{}
}

func (g *syncTestGitHub) Repository(ctx context.Context, repo string) (githubdomain.RemoteRepository, error) {
	g.mu.Lock()
	g.repositoryCalls++
	g.mu.Unlock()
	if g.block != nil {
		select {
		case <-ctx.Done():
			return githubdomain.RemoteRepository{}, ctx.Err()
		case <-g.block:
		}
	}
	return githubdomain.RemoteRepository{ID: 1, FullName: repo, DefaultBranch: "main"}, nil
}
func (g *syncTestGitHub) Branches(context.Context, string) ([]githubdomain.RemoteBranch, error) {
	g.mu.Lock()
	g.branchCalls++
	g.mu.Unlock()
	return []githubdomain.RemoteBranch{{Name: "main", RemoteHeadSHA: "remote-head"}}, nil
}
func (g *syncTestGitHub) PullRequests(context.Context, string, githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	g.mu.Lock()
	g.prCalls++
	g.mu.Unlock()
	return []githubdomain.PullRequest{{
		Number: 1, State: "open", Head: "main", HeadRepository: "acme/repo",
		Base: "main", HeadSHA: "remote-head", UpdatedAt: time.Unix(10, 0),
	}}, nil
}
func (g *syncTestGitHub) Checks(ctx context.Context, _ string, _ string) ([]githubdomain.Check, error) {
	g.mu.Lock()
	g.checkCalls++
	g.mu.Unlock()
	if g.checksBlock != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-g.checksBlock:
		}
	}
	return []githubdomain.Check{{Name: "ci", Status: "completed", Conclusion: "success"}}, nil
}
func (*syncTestGitHub) PullRequest(context.Context, string, int) (githubdomain.PullRequestDetail, error) {
	return githubdomain.PullRequestDetail{}, nil
}
func (*syncTestGitHub) CreatePullRequest(context.Context, githubdomain.CreatePullRequestRequest) (githubdomain.PullRequest, error) {
	return githubdomain.PullRequest{}, nil
}
func (*syncTestGitHub) ReviewPullRequest(context.Context, githubdomain.ReviewRequest) error {
	return nil
}
func (*syncTestGitHub) ReadyPullRequest(context.Context, string, int) error  { return nil }
func (*syncTestGitHub) ClosePullRequest(context.Context, string, int) error  { return nil }
func (*syncTestGitHub) ReopenPullRequest(context.Context, string, int) error { return nil }
func (*syncTestGitHub) MergePullRequest(context.Context, githubdomain.MergePullRequestRequest) error {
	return nil
}
func (*syncTestGitHub) Comment(context.Context, string, int, string) error { return nil }
func (*syncTestGitHub) WorkflowRuns(context.Context, string, string) ([]githubdomain.WorkflowRun, error) {
	return nil, nil
}

func waitForProjection(t *testing.T, syncer *stateSync, projectID string, predicate func(browserSnapshot) bool) browserSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot, ok := syncer.CachedSnapshot(projectID); ok && predicate(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, _ := syncer.CachedSnapshot(projectID)
	t.Fatalf("projection did not converge: %+v", snapshot)
	return browserSnapshot{}
}

func TestStateSyncPublishesLocalAndProcessesWhileProviderIsBlocked(t *testing.T) {
	root := t.TempDir()
	providerRelease := make(chan struct{})
	daemon := &syncTestDaemon{
		root: root,
		records: []*procstore.Record{{
			ID: 7, Label: "dev", Command: "go run .", Worktree: root, Status: procstore.StatusRunning, PID: 123,
		}},
	}
	provider := &syncTestGitHub{block: providerRelease}
	projectID := "project-v1-test"
	registry := &syncTestRegistry{entries: map[string]projectServices{
		projectID: {
			info:   ProjectInfo{ID: projectID, Path: root, Name: "repo", FullName: "repo", Available: true, WorkspaceID: "local"},
			daemon: daemon,
			github: provider,
		},
	}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.Queue(projectID, refreshAll, false)

	local := waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.Local != nil && snapshot.Freshness["local"].State == "ready" &&
			snapshot.Freshness["processes"].State == "ready" && len(snapshot.Processes) == 1
	})
	if local.Remote != nil {
		t.Fatalf("provider blocked but remote state already published: %+v", local.Remote)
	}
	if local.Processes[0].ID != projectID+":7" || local.Processes[0].WorktreeID == "" {
		t.Fatalf("process summary = %+v", local.Processes[0])
	}

	close(providerRelease)
	enriched := waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.Remote != nil && snapshot.Freshness["provider"].State == "ready" && snapshot.WorktreeState[gitlocal.ID(localRepositoryID, root)].CI.Status == "passed"
	})
	state := enriched.WorktreeState[gitlocal.ID(localRepositoryID, root)]
	if state.PullRequest == nil || state.PullRequest.Number != 1 || state.CI.Status != "passed" || state.CI.CheckedSHA != "remote-head" {
		t.Fatalf("enrichment = %+v", state)
	}
}

func TestStateSyncPublishesPRsBeforeChecksFinish(t *testing.T) {
	root := t.TempDir()
	checksRelease := make(chan struct{})
	provider := &syncTestGitHub{checksBlock: checksRelease}
	projectID := "project-v1-pr-first"
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: {
		info:   ProjectInfo{ID: projectID, Path: root, Available: true, FullName: "repo"},
		daemon: &syncTestDaemon{root: root}, github: provider,
	}}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.SubscriberReady("")
	first := waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.Remote != nil && len(snapshot.Remote.PullRequests) == 1
	})
	worktreeID := gitlocal.ID(localRepositoryID, root)
	if first.WorktreeState[worktreeID].PullRequest == nil || first.WorktreeState[worktreeID].CI.Freshness.State != "loading" {
		t.Fatalf("PR was not published before checks: %+v", first.WorktreeState[worktreeID])
	}
	close(checksRelease)
	waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.WorktreeState[worktreeID].CI.Status == "passed"
	})
}

func TestProcessRefreshUsesCanonicalInventoryWorktreeID(t *testing.T) {
	root := t.TempDir()
	projectID := "project-v1-process-id"
	daemon := &syncTestDaemon{records: []*procstore.Record{{
		ID: 11, Label: "worker", Command: "go run .", Worktree: root, Status: procstore.StatusRunning,
	}}}
	project := projectServices{
		info:   ProjectInfo{ID: projectID, Path: root, Name: "repo", FullName: "repo", Available: true, WorkspaceID: "local"},
		daemon: daemon,
		github: &syncTestGitHub{},
	}
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: project}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	if !syncer.commitProject(project, "seed", func(snapshot *browserSnapshot) {
		snapshot.Local = &domain.RepositoryState{ID: projectID, Worktrees: []domain.Worktree{{
			ID: "stable-inventory-id", RepositoryID: projectID, Path: root, Branch: "main", Main: true,
		}}}
	}) {
		t.Fatal("failed to seed local inventory")
	}
	syncer.refreshProcesses(projectID)
	snapshot, _ := syncer.CachedSnapshot(projectID)
	if len(snapshot.Processes) != 1 || snapshot.Processes[0].WorktreeID != "stable-inventory-id" {
		t.Fatalf("process did not adopt inventory worktree id: %+v", snapshot.Processes)
	}
}

func TestProviderCacheCoalescesConcurrentRepositoryReads(t *testing.T) {
	release := make(chan struct{})
	provider := &syncTestGitHub{block: release}
	cache := newProviderCache()
	now := time.Unix(100, 0).UTC()
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			value, freshness := cache.repository(context.Background(), provider, "acme/repo", now, false)
			if value == nil || freshness.State != "ready" {
				t.Errorf("value=%+v freshness=%+v", value, freshness)
			}
			done <- struct{}{}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		provider.mu.Lock()
		calls := provider.repositoryCalls
		provider.mu.Unlock()
		if calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider request did not start")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-done
	<-done
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.repositoryCalls != 1 || provider.branchCalls != 1 || provider.prCalls != 1 {
		t.Fatalf("duplicate provider reads: repo=%d branches=%d prs=%d", provider.repositoryCalls, provider.branchCalls, provider.prCalls)
	}
}

func TestChecksRollupPreservesNoneRunningFailureAndUnknown(t *testing.T) {
	tests := []struct {
		name   string
		checks []githubdomain.Check
		want   string
	}{
		{name: "none", want: "none"},
		{name: "running", checks: []githubdomain.Check{{Status: "queued"}}, want: "running"},
		{name: "passed", checks: []githubdomain.Check{{Status: "completed", Conclusion: "success"}, {Status: "completed", Conclusion: "skipped"}}, want: "passed"},
		{name: "failed", checks: []githubdomain.Check{{Status: "completed", Conclusion: "timed_out"}}, want: "failed"},
		{name: "unknown", checks: []githubdomain.Check{{Status: "completed", Conclusion: "mystery"}}, want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := checksRollup(test.checks); got != test.want {
				t.Fatalf("rollup = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStateSyncDoesNotRepublishProjectRemovedDuringRead(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	started := make(chan struct{})
	projectID := "project-v1-removed"
	daemon := &syncTestDaemon{root: root, repositoryBlock: release, repositoryStarted: started}
	registry := &syncTestRegistry{entries: map[string]projectServices{
		projectID: {
			info:   ProjectInfo{ID: projectID, Path: root, Name: "repo", FullName: "repo", Available: true, WorkspaceID: "local"},
			daemon: daemon,
			github: &syncTestGitHub{},
		},
	}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.Queue(projectID, refreshLocal, false)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("local refresh did not start")
	}

	registry.remove(projectID)
	syncer.ReconcileCatalog()
	close(release)
	time.Sleep(30 * time.Millisecond)

	syncer.mu.Lock()
	_, exists := syncer.projects[projectID]
	syncer.mu.Unlock()
	if exists {
		t.Fatal("removed project was republished by a late local read")
	}
}

func TestStateSyncDeduplicatesSemanticProjectUpdates(t *testing.T) {
	root := t.TempDir()
	projectID := "project-v1-dedupe"
	project := projectServices{
		info: ProjectInfo{ID: projectID, Path: root, Name: "repo", FullName: "repo", Available: true, WorkspaceID: "local"},
	}
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: project}}
	hub := newEventHub()
	_, events := hub.subscribe()
	syncer := newStateSync(registry, hub)
	syncer.ReconcileCatalog()

	local := domain.RepositoryState{
		ID:        projectID,
		Worktrees: []domain.Worktree{{ID: "wt", RepositoryID: projectID, Path: root, Branch: "main", Main: true}},
	}
	firstTime := time.Unix(100, 0).UTC()
	if !syncer.commitProject(project, "local", func(snapshot *browserSnapshot) {
		snapshot.Local = &local
		snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &firstTime}
	}) {
		t.Fatal("first semantic state was not published")
	}
	event := <-events
	if event.Type != "project_update" || event.Snapshot == nil || event.Sequence != 1 || event.Snapshot.Sequence != 1 {
		t.Fatalf("unexpected project event: %+v", event)
	}

	secondTime := time.Unix(200, 0).UTC()
	if syncer.commitProject(project, "local", func(snapshot *browserSnapshot) {
		snapshot.Local = &local
		snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &secondTime}
	}) {
		t.Fatal("timestamp-only refresh was published")
	}
	snapshot, _ := syncer.CachedSnapshot(projectID)
	if snapshot.Sequence != 1 {
		t.Fatalf("timestamp-only refresh incremented sequence: %d", snapshot.Sequence)
	}
	select {
	case event := <-events:
		t.Fatalf("timestamp-only refresh emitted event: %+v", event)
	default:
	}
}
