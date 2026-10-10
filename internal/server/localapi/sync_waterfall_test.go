package localapi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

func syncTestProject(id, root string, daemon *syncTestDaemon, provider githubdomain.GitHubService) projectServices {
	return projectServices{
		info:   ProjectInfo{ID: id, Path: root, Name: id, FullName: id, Available: true, WorkspaceID: "local"},
		daemon: daemon,
		github: provider,
	}
}

func TestPriorityGateAdmitsHighLaneFirstAndKeepsFIFOWithinLanes(t *testing.T) {
	g := newPriorityGate(1)
	holder := g.enter(false)
	select {
	case <-holder:
	default:
		t.Fatal("free slot was not granted immediately")
	}
	low1, low2 := g.enter(false), g.enter(false)
	high1, high2 := g.enter(true), g.enter(true)
	admitted := func(ch <-chan struct{}) bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}
	for name, ch := range map[string]<-chan struct{}{"low1": low1, "low2": low2, "high1": high1, "high2": high2} {
		if admitted(ch) {
			t.Fatalf("%s admitted while the slot was held", name)
		}
	}
	var order []string
	for _, step := range []struct {
		name string
		ch   <-chan struct{}
	}{{"high1", high1}, {"high2", high2}, {"low1", low1}, {"low2", low2}} {
		g.release()
		if !admitted(step.ch) {
			t.Fatalf("expected %s to be admitted next (order so far %v)", step.name, order)
		}
		order = append(order, step.name)
	}
	g.release()
	if got := g.enter(false); !admitted(got) {
		t.Fatal("slot was lost")
	}
}

func TestSubscriberReadyRefreshesActiveProjectFirst(t *testing.T) {
	var mu sync.Mutex
	var order []string
	recorder := func(id string) func(gitbridge.Command) {
		return func(c gitbridge.Command) {
			if c.Type != "git.inventory" {
				return
			}
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
		}
	}
	entries := map[string]projectServices{}
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		root := t.TempDir()
		entries[id] = syncTestProject(id, root, &syncTestDaemon{root: root, onCommand: recorder(id)}, &syncTestGitHub{})
	}
	syncer := newStateSync(&syncTestRegistry{entries: entries}, newEventHub())
	syncer.localSem = newPriorityGate(1)
	syncer.ReconcileCatalog()
	syncer.SubscriberReady("p3")

	for id := range entries {
		waitForProjection(t, syncer, id, func(snapshot browserSnapshot) bool {
			return snapshot.Freshness["local"].State == "ready" && snapshot.Freshness["provider"].State == "ready"
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"p3", "p1", "p2", "p4"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("local refresh order = %v, want %v", order, want)
	}

	// The priority survives a reconnect that does not name a project.
	syncer.SubscriberReady("")
	if got := syncer.priorityID(); got != "p3" {
		t.Fatalf("priority project = %q", got)
	}
	syncer.SubscriberReady("unknown-project")
	if got := syncer.priorityID(); got != "p3" {
		t.Fatalf("unknown project replaced the priority: %q", got)
	}
}

func TestProviderStartsFromInventoryWhileLocalStatusesAreStillBlocked(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	started := make(chan struct{})
	daemon := &syncTestDaemon{root: root, repositoryBlock: release, repositoryStarted: started}
	provider := &syncTestGitHub{}
	projectID := "project-v1-early-provider"
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: syncTestProject(projectID, root, daemon, provider)}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.Queue(projectID, refreshAll, false)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("full local refresh never started")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		provider.mu.Lock()
		calls := provider.repositoryCalls
		provider.mu.Unlock()
		if calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider did not start while the local refresh was blocked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, _ := syncer.CachedSnapshot(projectID)
	if snapshot.Local == nil || snapshot.Local.Remotes == nil || len(snapshot.Local.Remotes) != 1 {
		t.Fatalf("inventory did not publish remotes: %+v", snapshot.Local)
	}
	if snapshot.Freshness["local"].State == "ready" {
		t.Fatal("local refresh finished although it is blocked")
	}

	close(release)
	waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["local"].State == "ready" && snapshot.Freshness["provider"].State == "ready" && snapshot.Remote != nil
	})
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.repositoryCalls != 1 || provider.branchCalls != 1 || provider.prCalls != 1 {
		t.Fatalf("provider reads were repeated: repo=%d branches=%d prs=%d", provider.repositoryCalls, provider.branchCalls, provider.prCalls)
	}
}

func TestInventoryFallsBackForDaemonWithoutGitInventory(t *testing.T) {
	root := t.TempDir()
	var inventoryCalls, legacyCalls atomic.Int32
	daemon := &syncTestDaemon{root: root, noInventory: true, onCommand: func(c gitbridge.Command) {
		switch c.Type {
		case "git.inventory":
			inventoryCalls.Add(1)
		case "git.worktrees", "git.branches":
			legacyCalls.Add(1)
		}
	}}
	projectID := "project-v1-legacy-daemon"
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: syncTestProject(projectID, root, daemon, &syncTestGitHub{})}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.Queue(projectID, refreshAll, false)
	snapshot := waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["local"].State == "ready" && snapshot.Freshness["provider"].State == "ready"
	})
	if snapshot.Local == nil || len(snapshot.Local.Worktrees) != 1 {
		t.Fatalf("local = %+v", snapshot.Local)
	}
	if inventoryCalls.Load() != 1 || legacyCalls.Load() != 2 {
		t.Fatalf("inventory=%d legacy=%d", inventoryCalls.Load(), legacyCalls.Load())
	}
}

// overlapGitHub blocks each of the three repository reads until all three are
// in flight, so a serial implementation can never complete.
type overlapGitHub struct {
	*syncTestGitHub
	arrived sync.WaitGroup
}

func (g *overlapGitHub) meet() error {
	g.arrived.Done()
	done := make(chan struct{})
	go func() { g.arrived.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("repository, branches and pull requests were not read concurrently")
	}
}

func (g *overlapGitHub) Repository(ctx context.Context, repo string) (githubdomain.RemoteRepository, error) {
	if err := g.meet(); err != nil {
		return githubdomain.RemoteRepository{}, err
	}
	return g.syncTestGitHub.Repository(ctx, repo)
}

func (g *overlapGitHub) Branches(ctx context.Context, repo string) ([]githubdomain.RemoteBranch, error) {
	if err := g.meet(); err != nil {
		return nil, err
	}
	return g.syncTestGitHub.Branches(ctx, repo)
}

func (g *overlapGitHub) PullRequests(ctx context.Context, repo string, filter githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	if err := g.meet(); err != nil {
		return nil, err
	}
	return g.syncTestGitHub.PullRequests(ctx, repo, filter)
}

func TestProviderReadsRepositoryBranchesAndPullRequestsConcurrently(t *testing.T) {
	provider := &overlapGitHub{syncTestGitHub: &syncTestGitHub{}}
	provider.arrived.Add(3)
	cache := newProviderCache()
	value, freshness := cache.repository(context.Background(), provider, "acme/repo", time.Now(), false)
	if freshness.State != "ready" || value == nil || len(value.PullRequests) != 1 || len(value.Branches) != 1 {
		t.Fatalf("value=%+v freshness=%+v", value, freshness)
	}
}

func TestProviderReadErrorOrderStaysRepositoryBranchesPullRequests(t *testing.T) {
	provider := &failingGitHub{syncTestGitHub: &syncTestGitHub{}, branchErr: fmt.Errorf("branches down"), prErr: fmt.Errorf("prs down")}
	cache := newProviderCache()
	_, freshness := cache.repository(context.Background(), provider, "acme/repo", time.Now(), false)
	if freshness.Error == nil || freshness.Error.Message != "branches down" {
		t.Fatalf("freshness = %+v", freshness)
	}
}

type failingGitHub struct {
	*syncTestGitHub
	branchErr, prErr error
}

func (g *failingGitHub) Branches(context.Context, string) ([]githubdomain.RemoteBranch, error) {
	return nil, g.branchErr
}

func (g *failingGitHub) PullRequests(context.Context, string, githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	return nil, g.prErr
}

// peakChecksGitHub records how many check reads are in flight at once.
type peakChecksGitHub struct {
	*syncTestGitHub
	inFlight, peak atomic.Int32
}

func (g *peakChecksGitHub) Checks(ctx context.Context, repo, sha string) ([]githubdomain.Check, error) {
	now := g.inFlight.Add(1)
	defer g.inFlight.Add(-1)
	for {
		old := g.peak.Load()
		if now <= old || g.peak.CompareAndSwap(old, now) {
			break
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Millisecond):
	}
	return g.syncTestGitHub.Checks(ctx, repo, sha)
}

func TestChecksAreReadThroughABoundedPool(t *testing.T) {
	const extra = 14
	root := t.TempDir()
	provider := &peakChecksGitHub{syncTestGitHub: &syncTestGitHub{}}
	daemon := &syncTestDaemon{root: root, extraWorktrees: extra}
	projectID := "project-v1-checks-pool"
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: syncTestProject(projectID, root, daemon, provider)}}
	syncer := newStateSync(registry, newEventHub())
	syncer.ReconcileCatalog()
	syncer.Queue(projectID, refreshAll, false)

	snapshot := waitForProjection(t, syncer, projectID, func(snapshot browserSnapshot) bool {
		if snapshot.Local == nil || len(snapshot.WorktreeState) != extra+1 {
			return false
		}
		for _, state := range snapshot.WorktreeState {
			if state.CI.Status != "passed" {
				return false
			}
		}
		return true
	})
	if len(snapshot.Local.Worktrees) != extra+1 {
		t.Fatalf("worktrees = %d", len(snapshot.Local.Worktrees))
	}
	peak := int(provider.peak.Load())
	if peak < 2 || peak > providerChecksWorkers {
		t.Fatalf("peak concurrent check reads = %d, want 2..%d", peak, providerChecksWorkers)
	}
}
