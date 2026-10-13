package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/gorilla/websocket"
)

// warmTestProject is one project whose daemon and GitHub can be swapped
// between the "before" and "after" processes of a restart.
type warmTestProject struct {
	id     string
	root   string
	daemon daemonClient
	github githubdomain.GitHubService
}

func warmTestRegistry(projects ...warmTestProject) *syncTestRegistry {
	registry := &syncTestRegistry{entries: map[string]projectServices{}}
	for _, project := range projects {
		registry.entries[project.id] = projectServices{
			info:   ProjectInfo{ID: project.id, Name: project.id, Path: project.root, Available: true},
			daemon: project.daemon,
			github: project.github,
		}
	}
	return registry
}

// warmSeed runs one process to a fully validated state for every project and
// saves it, like a server that ran and shut down.
func warmSeed(t *testing.T, store *warmStore, projects ...warmTestProject) {
	t.Helper()
	syncer := newStateSync(warmTestRegistry(projects...), newEventHub())
	syncer.enableWarmStart(store)
	syncer.ReconcileCatalog()
	for _, project := range projects {
		syncer.Queue(project.id, refreshAll, false)
	}
	for _, project := range projects {
		waitForProjection(t, syncer, project.id, func(snapshot browserSnapshot) bool {
			if snapshot.Freshness["local"].State != "ready" || snapshot.Freshness["provider"].State != "ready" || snapshot.Freshness["processes"].State != "ready" {
				return false
			}
			for _, state := range snapshot.WorktreeState {
				if state.CI.Freshness.State != "ready" {
					return false
				}
			}
			return len(snapshot.WorktreeState) > 0
		})
	}
	syncer.closeWarm(5 * time.Second)
}

// warmRestore installs the "after" process's saved projections before any
// revalidation can commit. Tests can inspect sequence zero at this boundary.
func warmRestore(t *testing.T, store *warmStore, projects ...warmTestProject) *stateSync {
	t.Helper()
	syncer := newStateSync(warmTestRegistry(projects...), newEventHub())
	syncer.watchProject = func(ctx context.Context, _ ProjectInfo) { <-ctx.Done() }
	syncer.loadWarmStart(store)
	syncer.enableWarmStart(store)
	syncer.ReconcileCatalog()
	t.Cleanup(func() { syncer.closeWarm(time.Second) })
	return syncer
}

func warmRun(t *testing.T, syncer *stateSync) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		syncer.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// warmRestart starts the "after" process: restore, then Run.
func warmRestart(t *testing.T, store *warmStore, projects ...warmTestProject) *stateSync {
	t.Helper()
	syncer := warmRestore(t, store, projects...)
	warmRun(t, syncer)
	return syncer
}

func warmTestSeedProject(t *testing.T, id string) warmTestProject {
	root := t.TempDir()
	return warmTestProject{id: id, root: root, daemon: &syncTestDaemon{root: root, records: []*procstore.Record{{ID: 1, Label: "web", Status: "running"}}}, github: &syncTestGitHub{}}
}

func TestWarmStartBackgroundValidationDoesNotReplaceLastViewedProject(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	alpha, beta, gamma := warmTestSeedProject(t, "alpha"), warmTestSeedProject(t, "beta"), warmTestSeedProject(t, "gamma")
	warmSeed(t, store, alpha, beta, gamma)
	syncer := warmRestore(t, store, alpha, beta, gamma)
	visited := time.Now().UTC().Add(-time.Minute)
	syncer.now = func() time.Time { return visited }
	syncer.SetFocus(1, "alpha")
	// Unvisited projects finish their background reads after alpha was viewed.
	for _, id := range []string{"beta", "gamma"} {
		syncer.commit(id, "local", func(snapshot *browserSnapshot) {
			validated := visited.Add(30 * time.Second)
			snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &validated}
		})
	}
	exports, priority := syncer.warmExports()
	for _, export := range exports {
		if export.id != "alpha" && !export.lastUsed.IsZero() {
			t.Fatalf("background validation counted as a visit to %s: %v", export.id, export.lastUsed)
		}
	}
	data, _, err := encodeWarmDocument(store.namespace, exports, priority, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(data); err != nil {
		t.Fatal(err)
	}
	restarted := warmRestore(t, store, alpha, beta, gamma)
	if got := restarted.priorityID(); got != "alpha" {
		t.Fatalf("restored priority = %q, want the last viewed project alpha", got)
	}
}

func TestWarmStartRestoresStaleAtSequenceZeroThenRevalidatesPerComponent(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	before := warmTestSeedProject(t, "repo")
	warmSeed(t, store, before)

	saved, reason := store.load()
	if reason != "" {
		t.Fatal(reason)
	}
	var record warmProject
	if err := json.Unmarshal(saved.Projects[0], &record); err != nil {
		t.Fatal(err)
	}

	localBlock, providerBlock, checksBlock := make(chan struct{}), make(chan struct{}), make(chan struct{})
	daemon := &syncTestDaemon{root: before.root, repositoryBlock: localBlock, records: []*procstore.Record{{ID: 1, Label: "web", Status: "running"}}}
	github := &syncTestGitHub{block: providerBlock, checksBlock: checksBlock}
	syncer := warmRestore(t, store, warmTestProject{id: "repo", root: before.root, daemon: daemon, github: github})

	restored, _ := syncer.CachedSnapshot("repo")
	if restored.Epoch != syncer.epoch || restored.Sequence != 0 {
		t.Fatalf("restored at %s/%d, want %s/0", restored.Epoch, restored.Sequence, syncer.epoch)
	}
	local := restored.Freshness["local"]
	if local.State != "stale" || local.Source != freshnessSourceDisk || local.UpdatedAt == nil || !local.UpdatedAt.Equal(record.Local.ValidatedAt) {
		t.Fatalf("local freshness = %+v", local)
	}
	if provider := restored.Freshness["provider"]; provider.State != "stale" || provider.Source != freshnessSourceDisk {
		t.Fatalf("provider freshness = %+v", provider)
	}
	if restored.Freshness["processes"].State != "loading" || len(restored.Processes) != 0 {
		t.Fatalf("runtime state restored: %+v %+v", restored.Freshness["processes"], restored.Processes)
	}
	if restored.Remote == nil || len(restored.Remote.PullRequests) != 1 || restored.Remote.PullRequests[0].Body != "" {
		t.Fatalf("restored catalog = %+v", restored.Remote)
	}
	if len(restored.BranchCandidates) == 0 {
		t.Fatal("branch candidates were not rebuilt from restored metadata")
	}
	for id, state := range restored.WorktreeState {
		if state.PullRequest == nil || state.CI.Status != "passed" || state.CI.Freshness.State != "stale" || state.CI.Freshness.Source != freshnessSourceDisk {
			t.Fatalf("worktree %s restored as %+v", id, state)
		}
	}

	// Processes revalidate at startup without any browser connected.
	warmRun(t, syncer)
	waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["processes"].State == "ready" && len(snapshot.Processes) == 1
	})

	// Local Git validates on its own; provider data stays restored.
	close(localBlock)
	afterLocal := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["local"].State == "ready"
	})
	if afterLocal.Sequence == 0 || afterLocal.Freshness["local"].Source != "" || afterLocal.Freshness["provider"].Source != freshnessSourceDisk {
		t.Fatalf("after local revalidation: seq %d, %+v", afterLocal.Sequence, afterLocal.Freshness)
	}
	for _, state := range afterLocal.WorktreeState {
		if state.PullRequest == nil || state.CI.Freshness.Source != freshnessSourceDisk {
			t.Fatal("local revalidation cleared restored badges")
		}
	}

	// The catalog validates while checks are still pending: badges keep
	// their restored values and label until their own read finishes.
	close(providerBlock)
	afterCatalog := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["provider"].State == "ready"
	})
	if afterCatalog.Freshness["provider"].Source != "" {
		t.Fatalf("provider still marked restored: %+v", afterCatalog.Freshness["provider"])
	}
	for _, state := range afterCatalog.WorktreeState {
		if state.CI.Status != "passed" || state.CI.Freshness.Source != freshnessSourceDisk || state.CI.Freshness.State != "stale" {
			t.Fatalf("checks pending, badge = %+v", state.CI)
		}
	}
	close(checksBlock)
	afterChecks := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		for _, state := range snapshot.WorktreeState {
			if state.CI.Freshness.State != "ready" {
				return false
			}
		}
		return true
	})
	for _, state := range afterChecks.WorktreeState {
		if state.CI.Freshness.Source != "" {
			t.Fatalf("validated badge still restored: %+v", state.CI.Freshness)
		}
	}
}

func TestWarmStartBootstrapServesRestoredSnapshotWhileProviderIsBlocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheDir := t.TempDir()
	rootsPath := filepath.Join(t.TempDir(), "project-roots.json")
	project := warmTestSeedProject(t, "repo")
	ns := warmStartNamespace(rootsPath, "", BrowserSecurityProduction)
	warmSeed(t, newWarmStore(cacheDir, ns), project)

	block := make(chan struct{})
	defer close(block)
	project.github = &syncTestGitHub{block: block, checksBlock: block}
	project.daemon = &syncTestDaemon{root: project.root, repositoryBlock: block}
	s, err := newServer(Config{Address: "127.0.0.1:7001", SecurityMode: BrowserSecurityProduction, SnapshotCacheDir: cacheDir}, warmTestRegistry(project), rootsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.agents.manager.Close(true, 0)
	defer s.stateSync.closeWarm(time.Second)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	s.expectedHost = host
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	// The embedded UI's own origin.
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host+"/events", http.Header{"Origin": []string{"http://" + host}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(websocketAuth{Type: "authenticate", Token: session.Token}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var snapshot *browserSnapshot
	for {
		var event localEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "project_snapshot" {
			snapshot = event.Snapshot
		}
		if event.Type == "bootstrap_complete" {
			break
		}
	}
	// Processes are not blocked and may already have committed once.
	if snapshot == nil || snapshot.Sequence > 1 || snapshot.Epoch != s.stateSync.epoch || snapshot.Freshness["local"].Source != freshnessSourceDisk {
		t.Fatalf("bootstrap snapshot = %+v", snapshot)
	}
	if snapshot.Local == nil || snapshot.Remote == nil || len(snapshot.Remote.PullRequests) != 1 || snapshot.Freshness["provider"].Source != freshnessSourceDisk {
		t.Fatalf("bootstrap did not carry the restored, labeled data: %+v", snapshot.Freshness)
	}
}

// emptyCatalogGitHub validates the repository with no open pull requests.
type emptyCatalogGitHub struct{ *syncTestGitHub }

func (emptyCatalogGitHub) PullRequests(context.Context, string, githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	return []githubdomain.PullRequest{}, nil
}

func TestWarmStartEmptyLiveCatalogClearsRestoredPullRequests(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	project := warmTestSeedProject(t, "repo")
	warmSeed(t, store, project)
	project.github = emptyCatalogGitHub{&syncTestGitHub{}}
	syncer := warmRestart(t, store, project)
	after := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["provider"].State == "ready"
	})
	if len(after.Remote.PullRequests) != 0 {
		t.Fatalf("restored PRs survived an empty live catalog: %+v", after.Remote.PullRequests)
	}
	for _, state := range after.WorktreeState {
		if state.PullRequest != nil {
			t.Fatal("restored PR link survived an empty live catalog")
		}
	}
}

// offlineGitHub fails every provider read.
type offlineGitHub struct{ *syncTestGitHub }

func (offlineGitHub) Repository(context.Context, string) (githubdomain.RemoteRepository, error) {
	return githubdomain.RemoteRepository{}, errors.New("provider offline")
}
func (offlineGitHub) Checks(context.Context, string, string) ([]githubdomain.Check, error) {
	return nil, errors.New("provider offline")
}

func TestWarmStartFailedRevalidationKeepsRestoredBadges(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	project := warmTestSeedProject(t, "repo")
	warmSeed(t, store, project)
	project.github = offlineGitHub{&syncTestGitHub{}}
	syncer := warmRestart(t, store, project)
	after := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		provider := snapshot.Freshness["provider"]
		if provider.Error == nil {
			return false
		}
		for _, state := range snapshot.WorktreeState {
			if state.CI.Freshness.Error == nil {
				return false
			}
		}
		return true
	})
	provider := after.Freshness["provider"]
	if provider.State != "stale" || provider.Source != freshnessSourceDisk || after.Remote == nil || len(after.Remote.PullRequests) != 1 {
		t.Fatalf("failed revalidation dropped restored provider data: %+v", provider)
	}
	for _, state := range after.WorktreeState {
		if state.PullRequest == nil || state.CI.Status != "passed" || state.CI.Freshness.State != "stale" || state.CI.Freshness.Source != freshnessSourceDisk {
			t.Fatalf("failed checks dropped the restored badge: %+v", state)
		}
	}
}

// changedPageGitHub serves a two-page open catalog. Page one answers as an
// unchanged 304; page two no longer has the PR that closed after the save.
type changedPageGitHub struct {
	*syncTestGitHub
	mu    sync.Mutex
	pages []int
}

func (g *changedPageGitHub) PullRequestPage(_ context.Context, _ string, _ githubdomain.PRFilter, page int) (githubdomain.PullRequestPage, error) {
	g.mu.Lock()
	g.pages = append(g.pages, page)
	g.mu.Unlock()
	if page == 1 {
		return githubdomain.PullRequestPage{Items: []githubdomain.PullRequest{{Number: 1, State: "open", Head: "main", HeadRepository: "acme/repo", HeadSHA: "remote-head", UpdatedAt: time.Unix(10, 0)}}, NextPage: 2, NotModified: true}, nil
	}
	return githubdomain.PullRequestPage{Items: []githubdomain.PullRequest{{Number: 2, State: "open", Head: "other", HeadRepository: "acme/repo", UpdatedAt: time.Unix(5, 0)}}}, nil
}

// seedPagedGitHub saves a two-page catalog that still had PR 150 open.
type seedPagedGitHub struct{ *syncTestGitHub }

func (seedPagedGitHub) PullRequestPage(_ context.Context, _ string, _ githubdomain.PRFilter, page int) (githubdomain.PullRequestPage, error) {
	if page == 1 {
		return githubdomain.PullRequestPage{Items: []githubdomain.PullRequest{{Number: 1, State: "open", Head: "main", HeadRepository: "acme/repo", HeadSHA: "remote-head", UpdatedAt: time.Unix(10, 0)}}, NextPage: 2}, nil
	}
	return githubdomain.PullRequestPage{Items: []githubdomain.PullRequest{{Number: 2, State: "open", Head: "other", HeadRepository: "acme/repo", UpdatedAt: time.Unix(5, 0)}, {Number: 150, State: "open", Head: "closed-later", HeadRepository: "acme/repo", UpdatedAt: time.Unix(1, 0)}}}, nil
}

func TestWarmStartScansEveryCatalogPageAfterRestore(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	project := warmTestSeedProject(t, "repo")
	project.github = seedPagedGitHub{&syncTestGitHub{}}
	warmSeed(t, store, project)
	live := &changedPageGitHub{syncTestGitHub: &syncTestGitHub{}}
	project.github = live
	syncer := warmRestore(t, store, project)
	restored, _ := syncer.CachedSnapshot("repo")
	if restored.Remote == nil || len(restored.Remote.PullRequests) != 3 {
		t.Fatalf("restored catalog = %+v", restored.Remote)
	}
	warmRun(t, syncer)
	after := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["provider"].State == "ready"
	})
	for _, pull := range after.Remote.PullRequests {
		if pull.Number == 150 {
			t.Fatal("a page-one 304 kept a PR that closed on a later page")
		}
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if len(live.pages) < 2 || live.pages[1] != 2 {
		t.Fatalf("pages read after restore = %v", live.pages)
	}
}

func TestWarmStartNeverRestoresRemovedMovedOrExpiredProjects(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	kept, removed, moved := warmTestSeedProject(t, "kept"), warmTestSeedProject(t, "removed"), warmTestSeedProject(t, "moved")
	warmSeed(t, store, kept, removed, moved)
	moved.root = t.TempDir()
	moved.daemon = &syncTestDaemon{root: moved.root}
	syncer := warmRestore(t, store, kept, moved)
	if snapshot, _ := syncer.CachedSnapshot("kept"); snapshot.Freshness["local"].Source != freshnessSourceDisk {
		t.Fatal("kept project was not restored")
	}
	if snapshot, _ := syncer.CachedSnapshot("moved"); snapshot.Freshness["local"].Source == freshnessSourceDisk {
		t.Fatal("a project now at another path was restored")
	}
	if _, ok := syncer.CachedSnapshot("removed"); ok {
		t.Fatal("removed project reappeared")
	}
	if syncer.commit("removed", "local", func(*browserSnapshot) {}) {
		t.Fatal("a late write for a removed project was accepted")
	}

	// Seven days later nothing is restored.
	expired := newStateSync(warmTestRegistry(kept), newEventHub())
	expired.now = func() time.Time { return time.Now().UTC().Add(warmStartMaxAge + time.Hour) }
	expired.loadWarmStart(store)
	if snapshot, _ := expired.CachedSnapshot("kept"); snapshot.Freshness["local"].Source == freshnessSourceDisk {
		t.Fatal("expired record restored")
	}
}

// recordingDaemon records every command and can report another remote.
type recordingDaemon struct {
	*syncTestDaemon
	mu       sync.Mutex
	commands []string
	fullName string
}

func (d *recordingDaemon) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	d.mu.Lock()
	d.commands = append(d.commands, command.Type)
	d.mu.Unlock()
	result, err := d.syncTestDaemon.GitContext(ctx, command)
	if err != nil || d.fullName == "" || result.Error != nil || (command.Type != "git.repository.refresh" && command.Type != "git.inventory") {
		return result, err
	}
	var state domain.RepositoryState
	if err := json.Unmarshal(result.Payload, &state); err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(d.fullName, "/")
	state.Remotes = []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: owner, Repository: name, FullName: d.fullName}}
	result.Payload, err = json.Marshal(state)
	return result, err
}

func TestWarmStartRevalidationNeverFetchesOrPulls(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	project := warmTestSeedProject(t, "repo")
	warmSeed(t, store, project)
	daemon := &recordingDaemon{syncTestDaemon: &syncTestDaemon{root: project.root}}
	project.daemon = daemon
	syncer := warmRestart(t, store, project)
	waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["local"].State == "ready" && snapshot.Freshness["provider"].State == "ready"
	})
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for _, command := range daemon.commands {
		if command != "git.repository.refresh" && command != "git.inventory" {
			t.Fatalf("restoration issued %s (all: %v)", command, daemon.commands)
		}
	}
}

func TestWarmStartDropsRestoredProviderDataWhenTheRemoteChanged(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	project := warmTestSeedProject(t, "repo")
	warmSeed(t, store, project)
	block := make(chan struct{})
	defer close(block)
	project.daemon = &recordingDaemon{syncTestDaemon: &syncTestDaemon{root: project.root}, fullName: "acme/other"}
	project.github = &syncTestGitHub{block: block}
	syncer := warmRestart(t, store, project)
	after := waitForProjection(t, syncer, "repo", func(snapshot browserSnapshot) bool {
		return snapshot.Freshness["local"].State == "ready"
	})
	if after.Remote != nil || len(after.WorktreeState) != 0 || after.Freshness["provider"].Source == freshnessSourceDisk {
		t.Fatalf("restored data of the old repository survived: %+v", after.Freshness["provider"])
	}
}
