package localapi

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

type blockingSyncDaemon struct {
	*syncTestDaemon
	mu      sync.Mutex
	calls   int
	release <-chan struct{}
}

func (d *blockingSyncDaemon) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	if command.Type != "git.repository.sync" {
		return d.syncTestDaemon.GitContext(ctx, command)
	}
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.release:
	}
	now := time.Now().UTC()
	raw, _ := json.Marshal(domain.RepositorySync{Fetch: domain.SyncOutcome{State: "ready", CompletedAt: &now}, Pull: domain.SyncOutcome{State: "skipped", Reason: "dirty_worktree"}})
	return &gitbridge.Result{ID: command.ID, Payload: raw}, nil
}

func TestRepositorySyncCoalescesTabsAndPublishesLocalBeforeNetwork(t *testing.T) {
	release := make(chan struct{})
	d := &blockingSyncDaemon{syncTestDaemon: &syncTestDaemon{root: t.TempDir()}, release: release}
	project := projectServices{info: ProjectInfo{ID: "repo", Path: d.root, Available: true}, daemon: d, github: &syncTestGitHub{}}
	registry := &syncTestRegistry{entries: map[string]projectServices{"repo": project}}
	s := newStateSync(registry, newEventHub())
	s.ReconcileCatalog()
	s.queueRepositorySync("repo", "first", true, true)
	before := waitForProjection(t, s, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Local != nil && snapshot.Sync.Fetch.State == "running"
	})
	for i := 0; i < 10; i++ {
		s.queueRepositorySync("repo", "another-tab", true, true)
	}
	close(release)
	after := waitForProjection(t, s, "repo", func(snapshot browserSnapshot) bool { return snapshot.Sync.Fetch.State == "ready" })
	if after.Sequence <= before.Sequence || after.Sync.Fetch.CompletedAt == nil || after.Sync.Pull.Reason != "dirty_worktree" {
		t.Fatal(after)
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 1 {
		t.Fatalf("tabs made %d fetches", calls)
	}
	s.queueRepositorySync("repo", "recent-load", true, true)
	d.mu.Lock()
	calls = d.calls
	d.mu.Unlock()
	if calls != 1 {
		t.Fatal("initial load ignored periodic bound")
	}
}

func TestColdRefreshPublishesInventoryBeforeStatusCollection(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	d := &syncTestDaemon{root: t.TempDir(), repositoryBlock: release, repositoryStarted: started}
	project := projectServices{info: ProjectInfo{ID: "repo", Path: d.root, Available: true}, daemon: d, github: &syncTestGitHub{}}
	registry := &syncTestRegistry{entries: map[string]projectServices{"repo": project}}
	s := newStateSync(registry, newEventHub())
	s.ReconcileCatalog()
	s.Queue("repo", refreshLocal, false)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("status collection did not start")
	}
	snapshot := waitForProjection(t, s, "repo", func(snapshot browserSnapshot) bool { return snapshot.Local != nil })
	if len(snapshot.Local.Worktrees) != 1 || snapshot.Local.Worktrees[0].Status != nil || snapshot.Freshness["local"].State != "loading" {
		t.Fatalf("inventory was not published while statuses were pending: %+v", snapshot)
	}
	if snapshot.Local.ID != "repo" || snapshot.Local.Worktrees[0].RepositoryID != "repo" || len(snapshot.BranchCandidates) != 2 {
		t.Fatalf("initial inventory lost canonical identity or branch refs: %+v", snapshot)
	}
}
