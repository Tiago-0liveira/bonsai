package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type executorTestDaemon struct {
	*syncTestDaemon
	executor *gitbridge.Executor
}

func (d *executorTestDaemon) Git(c gitbridge.Command) (*gitbridge.Result, error) {
	return d.GitContext(context.Background(), c)
}
func (d *executorTestDaemon) GitContext(ctx context.Context, c gitbridge.Command) (*gitbridge.Result, error) {
	result := d.executor.Execute(ctx, c)
	return &result, nil
}

type managementTestRegistry struct {
	*syncTestRegistry
	local *gitlocal.Service
}

func (r *managementTestRegistry) Worktree(ctx context.Context, id string) (projectServices, bool) {
	trees, err := r.local.ListWorktrees(ctx, localRepositoryID)
	if err != nil {
		return projectServices{}, false
	}
	for _, tree := range trees {
		if tree.ID == id {
			return r.Default(), true
		}
	}
	return projectServices{}, false
}

func managementServer(t *testing.T, records []*procstore.Record) (*Server, *gitlocal.Service) {
	t.Helper()
	root := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	svc, err := gitlocal.New([]gitlocal.Config{{ID: localRepositoryID, Root: root, WorktreeRoot: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := gitstore.Open(filepath.Join(t.TempDir(), "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := gitstore.Open(filepath.Join(t.TempDir(), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := &executorTestDaemon{syncTestDaemon: &syncTestDaemon{root: root, records: records}, executor: &gitbridge.Executor{Local: svc, Journal: journal}}
	project := projectServices{info: ProjectInfo{ID: "repo", Path: root, Available: true}, state: metadata, daemon: d, github: &syncTestGitHub{}}
	registry := &managementTestRegistry{syncTestRegistry: &syncTestRegistry{entries: map[string]projectServices{"repo": project}}, local: svc}
	s := newTestServer(t)
	s.registry = registry
	s.stateSync = newStateSync(registry, s.eventHub)
	s.stateSync.ReconcileCatalog()
	return s, svc
}

func managementRequest(t *testing.T, s *Server, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "http://127.0.0.1:7001"+path, strings.NewReader(body))
	r.Header.Set("Origin", ProductionBrowserOrigin)
	r.Header.Set("X-Bonsai-Session", session.Token)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestWorktreeCreateAndDeleteHTTPIdempotencyDirtyProtectionAndMetadata(t *testing.T) {
	s, svc := managementServer(t, nil)
	body := `{"mode":"new","branch":"feature","base":"main"}`
	first := managementRequest(t, s, http.MethodPost, "/api/projects/repo/worktrees", "create", body)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	second := managementRequest(t, s, http.MethodPost, "/api/projects/repo/worktrees", "create", body)
	if second.Code != 200 || second.Body.String() != first.Body.String() {
		t.Fatal(second.Code, second.Body.String())
	}
	var created struct{ Result domain.Worktree }
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Result.ID
	w := managementRequest(t, s, http.MethodPatch, "/api/worktrees/"+id+"/metadata", "meta", `{"tag":"feat"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(created.Result.Path, "keep.txt"), []byte("untracked"), 0600); err != nil {
		t.Fatal(err)
	}
	w = managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+id, "dirty", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "dirty_worktree") {
		t.Fatal(w.Code, w.Body.String())
	}
	removed := managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+id, "discard", `{"confirm_discard":true}`)
	if removed.Code != 200 {
		t.Fatal(removed.Code, removed.Body.String())
	}
	repeated := managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+id, "discard", `{"confirm_discard":true}`)
	if repeated.Code != 200 || repeated.Body.String() != removed.Body.String() {
		t.Fatal(repeated.Code, repeated.Body.String())
	}
	branches, err := svc.ListBranches(context.Background(), localRepositoryID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range branches {
		found = found || b.Name == "feature"
	}
	if !found {
		t.Fatal("deleted branch")
	}
	err = s.registry.Default().state.View(func(data gitstore.Data) error {
		if _, ok := data["worktree_metadata"][id]; ok {
			t.Error("stale metadata survived removal")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	trees, err := svc.ListWorktrees(context.Background(), localRepositoryID)
	if err != nil {
		t.Fatal(err)
	}
	w = managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+trees[0].ID, "main", `{"confirm_discard":true}`)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestWorktreeDeletionRequiresStoppingTrackedProcesses(t *testing.T) {
	s, svc := managementServer(t, nil)
	wt, err := svc.CreateWorktree(context.Background(), domain.CreateWorktreeRequest{RepositoryID: localRepositoryID, Mode: "new", Branch: "worker", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	s.registry.Default().daemon.(*executorTestDaemon).records = []*procstore.Record{{ID: 1, Worktree: wt.Path, Status: procstore.StatusRunning}}
	w := managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+wt.ID, "running", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "processes_running") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatal("removed active worktree", err)
	}
}

func TestMissingWorktreeDeletionClearsMetadataAndRemainsReplayable(t *testing.T) {
	s, svc := managementServer(t, nil)
	ctx := context.Background()
	wt, err := svc.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: localRepositoryID, Mode: "new", Branch: "missing", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	w := managementRequest(t, s, http.MethodPatch, "/api/worktrees/"+wt.ID+"/metadata", "missing-meta", `{"tag":"feat"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := os.RemoveAll(filepath.Dir(wt.Path)); err != nil {
		t.Fatal(err)
	}
	removed := managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+wt.ID, "missing-remove", `{}`)
	if removed.Code != 200 {
		t.Fatal(removed.Code, removed.Body.String())
	}
	repeated := managementRequest(t, s, http.MethodDelete, "/api/worktrees/"+wt.ID, "missing-remove", `{}`)
	if repeated.Code != 200 || repeated.Body.String() != removed.Body.String() {
		t.Fatal(repeated.Code, repeated.Body.String())
	}
	trees, err := svc.ListWorktrees(ctx, localRepositoryID)
	if err != nil || len(trees) != 1 || !trees[0].Main {
		t.Fatal(trees, err)
	}
	if err := s.registry.Default().state.View(func(data gitstore.Data) error {
		if _, ok := data["worktree_metadata"][wt.ID]; ok {
			t.Error("missing worktree metadata survived removal")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	branches, err := svc.ListBranches(ctx, localRepositoryID)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range branches {
		if branch.Name == "missing" {
			return
		}
	}
	t.Fatal("removed the local branch")
}
