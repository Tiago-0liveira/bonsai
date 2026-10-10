package localapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

// Sentinels that must never reach the warm-start document or its logs.
var warmSentinels = []string{
	"SENTINEL_CREDENTIAL_ghp_0123",
	"SENTINEL_CAPABILITY",
	"SENTINEL_COMMAND",
	"SENTINEL_PR_BODY",
	"SENTINEL_PATCH",
	"SENTINEL_LOG",
	"SENTINEL_ERROR_MESSAGE",
	"SENTINEL_FILE_PATH",
	"SENTINEL_AGENT",
	"SENTINEL_SYNC",
}

func warmTestSnapshot(t *testing.T, worktrees, prs int) browserSnapshot {
	t.Helper()
	validated := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	local := domain.RepositoryState{
		ID:            "repo",
		DefaultBranch: "main",
		Remotes:       []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: "acme", Repository: "repo", FullName: "acme/repo"}},
		Groups:        []domain.WorktreeGroup{{ID: "g", Kind: "stack", WorktreeIDs: []string{"w-0"}}},
	}
	snapshot := browserSnapshot{
		Repository: ProjectInfo{ID: "repo", Path: "/repo", Available: true},
		Online:     true,
		Agents:     []agentterminal.Summary{{ID: "agent", Name: "SENTINEL_AGENT"}},
		Sync:       domain.RepositorySync{Fetch: domain.SyncOutcome{State: "error", Error: "SENTINEL_SYNC"}},
		Metadata:   map[string]worktreeMetadata{"w-0": {WorktreeID: "w-0", RepositoryID: "repo", MergeTargetBranch: "main", StackPreference: "auto"}},
		Processes: []browserProcessSummary{{
			ID: "repo:1", Command: "SENTINEL_COMMAND --token SENTINEL_CREDENTIAL_ghp_0123", ExitError: "SENTINEL_LOG", Label: "SENTINEL_CAPABILITY",
		}},
		Freshness: map[string]browserFreshness{
			"local":     {State: "ready", UpdatedAt: &validated},
			"provider":  {State: "stale", UpdatedAt: &validated, Error: &browserStateError{Code: "provider_unavailable", Message: "SENTINEL_ERROR_MESSAGE"}},
			"processes": {State: "ready", UpdatedAt: &validated},
			"agents":    {State: "ready"},
		},
		WorktreeState: map[string]browserWorktreeState{},
		Remote: &browserRemoteSnapshot{
			Repository:        githubdomain.RemoteRepository{ID: 1, FullName: "acme/repo", DefaultBranch: "main"},
			Branches:          []githubdomain.RemoteBranch{{Name: "main", RemoteHeadSHA: "abc"}},
			UpdatedAt:         validated,
			PRCatalogComplete: true,
		},
	}
	for i := 0; i < worktrees; i++ {
		id := fmt.Sprintf("w-%d", i)
		status := &domain.WorkingTreeStatus{Branch: "b", HeadSHA: "sha", Modified: 2, Files: []domain.FileStatus{{Path: "SENTINEL_FILE_PATH"}}, Conflicted: []string{"SENTINEL_FILE_PATH"}}
		local.Worktrees = append(local.Worktrees, domain.Worktree{ID: id, Path: "/repo/" + id, Branch: fmt.Sprintf("feature/%d", i), HeadSHA: "sha", Status: status, StatusError: &domain.StateError{Code: "status_failed", Message: "SENTINEL_ERROR_MESSAGE"}})
		snapshot.WorktreeState[id] = browserWorktreeState{
			PullRequest: &githubdomain.PullRequest{Number: i + 1, Title: "t", Body: "SENTINEL_PR_BODY SENTINEL_PATCH"},
			CI:          browserCIState{Status: "passed", CheckedSHA: "sha", Checks: []githubdomain.Check{{Name: "ci", Status: "completed", Conclusion: "success"}}, Freshness: browserFreshness{State: "error", UpdatedAt: &validated, Error: &browserStateError{Code: "x", Message: "SENTINEL_ERROR_MESSAGE"}}},
		}
	}
	for i := 0; i < prs; i++ {
		snapshot.Remote.PullRequests = append(snapshot.Remote.PullRequests, githubdomain.PullRequest{Number: i + 1, Title: fmt.Sprintf("Change %d", i), Body: "SENTINEL_PR_BODY", State: "open", Head: fmt.Sprintf("feature/%d", i)})
	}
	snapshot.Local = &local
	return snapshot
}

func decodeWarmProjects(t *testing.T, data []byte) []warmProject {
	t.Helper()
	var doc warmDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	out := make([]warmProject, 0, len(doc.Projects))
	for _, raw := range doc.Projects {
		var project warmProject
		if err := json.Unmarshal(raw, &project); err != nil {
			t.Fatal(err)
		}
		out = append(out, project)
	}
	return out
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buffer
}

func TestWarmStartDocumentKeepsMetadataSubset(t *testing.T) {
	logs := captureLog(t)
	snapshot := warmTestSnapshot(t, 3, 4)
	data, skipped, err := encodeWarmDocument("ns", []warmExport{{id: "repo", path: "/repo", snapshot: snapshot}}, "", time.Now())
	if err != nil || skipped != 0 {
		t.Fatalf("encode: skipped %d, %v", skipped, err)
	}
	store := newWarmStore(t.TempDir(), "ns")
	if err := store.save(data); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range warmSentinels {
		if bytes.Contains(onDisk, []byte(sentinel)) || strings.Contains(logs.String(), sentinel) {
			t.Fatalf("%s reached the warm-start document or logs", sentinel)
		}
	}
	doc, reason := store.load()
	if reason != "" {
		t.Fatalf("load: %s", reason)
	}
	projects := decodeWarmProjects(t, marshalWarmTest(t, doc))
	if len(projects) != 1 {
		t.Fatalf("projects = %d", len(projects))
	}
	project := projects[0]
	if project.Local == nil || len(project.Local.State.Worktrees) != 3 || project.Local.State.Worktrees[0].Status.Modified != 2 || project.Local.State.Worktrees[0].StatusError.Code != "status_failed" {
		t.Fatalf("local = %+v", project.Local)
	}
	if len(project.Local.State.Groups) != 1 || project.Local.Metadata["w-0"].MergeTargetBranch != "main" || project.Local.State.Remotes[0].FullName != "acme/repo" {
		t.Fatalf("identities, groups or preferences lost: %+v", project.Local)
	}
	provider := project.Provider
	if provider == nil || len(provider.PullRequests) != 4 || !provider.CatalogComplete || provider.Worktrees["w-1"].PullRequest.Number != 2 || provider.Worktrees["w-1"].CI.Status != "passed" || provider.Worktrees["w-1"].CI.ValidatedAt == nil {
		t.Fatalf("provider = %+v", provider)
	}
}

func marshalWarmTest(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWarmStartSkipsNeverValidatedProjects(t *testing.T) {
	snapshot := warmTestSnapshot(t, 1, 0)
	snapshot.Freshness["local"] = browserFreshness{State: "loading"}
	data, _, err := encodeWarmDocument("ns", []warmExport{{id: "repo", snapshot: snapshot}}, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if projects := decodeWarmProjects(t, data); len(projects) != 0 {
		t.Fatalf("unvalidated project saved: %+v", projects)
	}
}

func TestWarmStartLimitsKeepPriorityAndRecentProjects(t *testing.T) {
	small := warmTestSnapshot(t, 1, 0)
	now := time.Now()
	var exports []warmExport
	for i := 0; i < warmStartMaxProjects+10; i++ {
		exports = append(exports, warmExport{id: fmt.Sprintf("p%03d", i), lastUsed: now.Add(time.Duration(i) * time.Minute), snapshot: small})
	}
	// The oldest project is the priority one: it is kept regardless.
	data, skipped, err := encodeWarmDocument("ns", exports, "p000", now)
	if err != nil {
		t.Fatal(err)
	}
	projects := decodeWarmProjects(t, data)
	if len(projects) != warmStartMaxProjects || skipped != 10 {
		t.Fatalf("kept %d, skipped %d", len(projects), skipped)
	}
	if projects[0].ID != "p000" || projects[1].ID != fmt.Sprintf("p%03d", warmStartMaxProjects+9) {
		t.Fatalf("order = %s, %s", projects[0].ID, projects[1].ID)
	}
	kept := map[string]bool{}
	for _, project := range projects {
		kept[project.ID] = true
	}
	if kept["p001"] || kept["p010"] || !kept["p011"] {
		t.Fatalf("least recently used projects were not the ones evicted")
	}

	// A project over the per-project limit is skipped whole, never truncated.
	huge := warmTestSnapshot(t, 1, 0)
	huge.Remote.PullRequests = []githubdomain.PullRequest{{Number: 1, Title: strings.Repeat("x", warmStartMaxProject)}}
	data, skipped, err = encodeWarmDocument("ns", []warmExport{{id: "huge", snapshot: huge}, {id: "ok", snapshot: small}}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	projects = decodeWarmProjects(t, data)
	if skipped != 1 || len(projects) != 1 || projects[0].ID != "ok" {
		t.Fatalf("oversized project: skipped %d, kept %+v", skipped, projects)
	}
}

func TestWarmStartLoadRejectsUnusableDocuments(t *testing.T) {
	cases := map[string]func(path string){
		warmReasonMissing: func(string) {},
		warmReasonCorrupt: func(path string) { _ = os.WriteFile(path, []byte(`{"schema":1,"namespace":"ns","projects":[`), 0o600) },
		warmReasonUnsupported: func(path string) {
			_ = os.WriteFile(path, []byte(`{"schema":99,"namespace":"ns","projects":[]}`), 0o600)
		},
		warmReasonNamespace: func(path string) {
			_ = os.WriteFile(path, []byte(`{"schema":1,"namespace":"other","projects":[]}`), 0o600)
		},
		warmReasonOversized: func(path string) {
			file, _ := os.Create(path)
			_ = file.Truncate(warmStartMaxDocument + 1)
			_ = file.Close()
		},
		warmReasonUnreadable: func(path string) { _ = os.Mkdir(path, 0o700) },
	}
	for want, prepare := range cases {
		t.Run(want, func(t *testing.T) {
			store := newWarmStore(t.TempDir(), "ns")
			if err := os.MkdirAll(store.dir, 0o700); err != nil {
				t.Fatal(err)
			}
			prepare(store.path())
			if doc, reason := store.load(); doc != nil || reason != want {
				t.Fatalf("load = %v, %q; want %q", doc, reason, want)
			}
		})
	}
}

func TestWarmStartFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	store := newWarmStore(filepath.Join(t.TempDir(), "warm-start"), "ns")
	if err := store.save([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{store.dir: 0o700, store.path(): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestWarmStartFailedReplacementKeepsPreviousDocument(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced")
	}
	store := newWarmStore(t.TempDir(), "ns")
	previous, _, err := encodeWarmDocument("ns", []warmExport{{id: "repo", snapshot: warmTestSnapshot(t, 1, 1)}}, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.save(previous); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.dir, 0o700) })
	if err := store.save([]byte(`{"schema":1,"namespace":"ns","projects":[]}`)); err == nil {
		t.Fatal("save into a read-only directory succeeded")
	}
	doc, reason := store.load()
	if reason != "" || len(doc.Projects) != 1 {
		t.Fatalf("previous document lost: %v %q", doc, reason)
	}
}

func TestWarmStartSaveSkipsWhileAnotherWriterHoldsTheLock(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := procstore.TryLock(filepath.Join(store.dir, warmStartLockFile))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	if err := store.save([]byte(`{}`)); !errors.Is(err, procstore.ErrLocked) {
		t.Fatalf("save under a held lock = %v", err)
	}
}

func TestWarmStartWriterCoalescesAndFlushesOnClose(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	var mu sync.Mutex
	exports := 0
	snapshot := warmTestSnapshot(t, 1, 1)
	writer := newWarmWriter(store, func() ([]warmExport, string) {
		mu.Lock()
		exports++
		mu.Unlock()
		return []warmExport{{id: "repo", snapshot: cloneSnapshot(snapshot)}}, "repo"
	}, time.Now)
	writer.idle, writer.limit = time.Hour, time.Hour
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				writer.notify()
			}
		}()
	}
	wg.Wait()
	writer.Close(5 * time.Second)
	writer.Close(time.Second) // idempotent
	mu.Lock()
	defer mu.Unlock()
	if exports != 1 {
		t.Fatalf("800 notifications produced %d saves, want one coalesced save on close", exports)
	}
	if doc, reason := store.load(); reason != "" || len(doc.Projects) != 1 {
		t.Fatalf("flushed document: %v %q", doc, reason)
	}
}

func TestWarmStartWriterSavesWithinTheMaximumDelay(t *testing.T) {
	store := newWarmStore(t.TempDir(), "ns")
	snapshot := warmTestSnapshot(t, 1, 0)
	writer := newWarmWriter(store, func() ([]warmExport, string) {
		return []warmExport{{id: "repo", snapshot: cloneSnapshot(snapshot)}}, ""
	}, time.Now)
	defer writer.Close(time.Second)
	writer.idle, writer.limit = 50*time.Millisecond, 200*time.Millisecond
	// Continuous commits never leave the writer idle; the maximum delay still
	// forces a save.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		writer.notify()
		writer.mu.Lock()
		saves := writer.saves
		writer.mu.Unlock()
		if saves > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no save while commits kept arriving")
}

func TestWarmStartConcurrentCommitsRemovalAndShutdown(t *testing.T) {
	registry := &syncTestRegistry{entries: map[string]projectServices{}}
	for _, id := range []string{"a", "b"} {
		root := t.TempDir()
		registry.entries[id] = projectServices{info: ProjectInfo{ID: id, Path: root, Available: true}, daemon: &syncTestDaemon{root: root}}
	}
	syncer := newStateSync(registry, newEventHub())
	store := newWarmStore(t.TempDir(), "ns")
	syncer.enableWarmStart(store)
	syncer.warm.idle, syncer.warm.limit = time.Millisecond, 5*time.Millisecond
	syncer.ReconcileCatalog()
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				syncer.commit(id, "local", func(snapshot *browserSnapshot) {
					now := time.Now().UTC()
					snapshot.Local = &domain.RepositoryState{ID: id, Worktrees: []domain.Worktree{{ID: fmt.Sprintf("%s-%d", id, i), Branch: "main"}}}
					snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &now}
				})
			}
		}(id)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		registry.remove("b")
		syncer.ReconcileCatalog()
	}()
	wg.Wait()
	syncer.closeWarm(5 * time.Second)
	doc, reason := store.load()
	if reason != "" {
		t.Fatalf("load: %s", reason)
	}
	projects := decodeWarmProjects(t, marshalWarmTest(t, doc))
	if len(projects) != 1 || projects[0].ID != "a" || projects[0].Local.State.Worktrees[0].ID != "a-199" {
		t.Fatalf("saved projects = %+v", projects)
	}
}
