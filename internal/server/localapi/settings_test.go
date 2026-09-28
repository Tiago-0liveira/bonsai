package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

func authorizedRequest(t *testing.T, s *Server, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7001"+path, bytes.NewReader(raw))
	req.Header.Set("Origin", ProductionBrowserOrigin)
	req.Header.Set("X-Bonsai-Session", session.Token)
	req.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}
func TestRootSettingsAPIRevisionsReplayAndSecurity(t *testing.T) {
	s := newTestServer(t)
	root := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	get := authorizedRequest(t, s, "GET", "/api/settings/project-roots", "", nil)
	if get.Code != 200 {
		t.Fatal(get.Body.String())
	}
	var cfg rootSettingsResponse
	json.Unmarshal(get.Body.Bytes(), &cfg)
	if len(cfg.Roots) != 0 || len(cfg.Suggestions) == 0 {
		t.Fatal(cfg)
	}
	body := map[string]any{"path": root, "revision": 0}
	if w := authorizedRequest(t, s, "POST", "/api/settings/project-roots", "", body); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := authorizedRequest(t, s, "POST", "/api/settings/project-roots", "unknown", map[string]any{"path": root, "revision": 0, "unexpected": true}); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w := authorizedRequest(t, s, "POST", "/api/settings/project-roots", "add", body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &cfg)
	if replay := authorizedRequest(t, s, "POST", "/api/settings/project-roots", "add", body); replay.Code != 200 {
		t.Fatal(replay.Body.String())
	}
	if stale := authorizedRequest(t, s, "POST", "/api/settings/project-roots", "second-browser", body); stale.Code != 409 {
		t.Fatal(stale.Code)
	}
	if len(s.registry.List()) != 1 {
		t.Fatal(s.registry.List())
	}
	// The launch repo is not configured: unscoped routes must not choose this project.
	if legacy := authorizedRequest(t, s, "GET", "/api/processes", "", nil); legacy.Code != 404 {
		t.Fatal(legacy.Code)
	}
	if unknown := authorizedRequest(t, s, "PATCH", "/api/worktrees/made-up/metadata", "patch", map[string]string{"tag": "bad"}); unknown.Code != 404 {
		t.Fatal(unknown.Code)
	}
	if removed := authorizedRequest(t, s, "DELETE", "/api/settings/project-roots/"+cfg.Roots[0].ID, "remove", map[string]int{"revision": 1}); removed.Code != 200 {
		t.Fatal(removed.Body.String())
	}
	if len(s.registry.List()) != 0 {
		t.Fatal("retained removed project")
	}
	// Same configuration file, separate API instance: reconciliation observes edits.
	other, err := New(Config{RepoDir: s.repoDir, Address: s.expectedHost, BrowserOrigin: s.browserOrigin, ProjectRootsPath: s.rootsPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.UpdateProjectRoots(s.rootsPath, "external", 2, root, ""); err != nil {
		t.Fatal(err)
	}
	if err = other.Reconcile(context.Background()); err != nil || len(other.registry.List()) != 1 {
		t.Fatal(err, other.registry.List())
	}
}

type routingDaemon struct {
	calls     []gitbridge.Command
	restarted []int
	name      string
}

func (d *routingDaemon) Git(c gitbridge.Command) (*gitbridge.Result, error) {
	d.calls = append(d.calls, c)
	raw, _ := json.Marshal([]domain.Worktree{{ID: "tree", RepositoryID: "local"}})
	if c.Type == "git.repository.refresh" {
		raw, _ = json.Marshal(domain.RepositoryState{ID: "local", Worktrees: []domain.Worktree{{ID: "tree", RepositoryID: "local"}}})
	}
	return &gitbridge.Result{ID: c.ID, Payload: raw}, nil
}
func (d *routingDaemon) List() ([]*procstore.Record, error) { return []*procstore.Record{}, nil }
func (d *routingDaemon) Restart(id int) (*procstore.Record, error) {
	d.restarted = append(d.restarted, id)
	return &procstore.Record{}, nil
}
func (d *routingDaemon) Kill(id int, _ bool, _ string) ([]int, error) { return []int{id}, nil }
func (d *routingDaemon) Logs(_ int, _ bool, _ int, _ string, _ bool, fn func(string) error) error {
	return fn(d.name)
}
func TestProjectRoutingAndLegacyMetadataPreservation(t *testing.T) {
	parent := t.TempDir()
	a := repoFixture(t, filepath.Join(parent, "one"))
	b := repoFixture(t, filepath.Join(parent, "two"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if _, err := config.UpdateProjectRoots(settings, "add", 0, parent, ""); err != nil {
		t.Fatal(err)
	}
	legacy, err := gitstore.Open(filepath.Join(a, ".bonsai", "local-api-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	id := local.ID("local", a)
	if err = legacy.Update(func(data gitstore.Data) error {
		gitstore.Put(data, "github_commands", "audit", githubAudit{State: "done", Hash: "kept"})
		return gitstore.Put(data, "worktree_metadata", id, worktreeMetadata{WorktreeID: id, RepositoryID: "local", Tag: "old", MergeTargetBranch: "main"})
	}); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{RepoDir: a, Address: "127.0.0.1:7001", BrowserOrigin: ProductionBrowserOrigin, ProjectRootsPath: settings})
	if err != nil {
		t.Fatal(err)
	}
	r := s.registry.(*discoveredProjectRegistry)
	daemons := map[string]*routingDaemon{}
	for _, info := range r.List() {
		p, _ := r.Lookup(info.ID)
		d := &routingDaemon{name: info.Name}
		daemons[info.ID] = d
		p.daemon = d
		r.entries[info.ID] = p
	}
	aID, bID := config.PathID("project", a), config.PathID("project", b)
	for _, project := range []string{aID, bID} {
		w := authorizedRequest(t, s, "GET", "/api/projects/"+project+"/worktrees", "", nil)
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(project)) {
			t.Fatal(w.Code, w.Body.String())
		}
		if daemons[project].calls[0].RepositoryID != "local" {
			t.Fatal("changed daemon ID")
		}
		w = authorizedRequest(t, s, "POST", "/api/projects/"+project+"/processes/1/restart", "restart", nil)
		if w.Code != 200 || len(daemons[project].restarted) != 1 {
			t.Fatal(w.Code)
		}
	}
	if w := authorizedRequest(t, s, "GET", "/api/repository", "", nil); w.Code != 200 || len(daemons[aID].calls) != 2 || len(daemons[bID].calls) != 1 {
		t.Fatal(w.Code, "wrong legacy owner")
	}
	if w := authorizedRequest(t, s, "PATCH", "/api/worktrees/"+id+"/metadata", "metadata", map[string]string{"tag": "new"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	legacy.View(func(data gitstore.Data) error {
		meta, _ := gitstore.Get[worktreeMetadata](data, "worktree_metadata", id)
		audit, ok := gitstore.Get[githubAudit](data, "github_commands", "audit")
		if meta.RepositoryID != "local" || meta.Tag != "new" || meta.MergeTargetBranch != "main" || !ok || audit.Hash != "kept" {
			t.Fatal(meta, audit)
		}
		return nil
	})
	before := len(daemons[aID].calls) + len(daemons[bID].calls)
	for _, path := range []string{"/api/projects/missing/worktrees", "/api/worktrees/missing/files", "/api/projects/missing/processes"} {
		if w := authorizedRequest(t, s, http.MethodGet, path, "", nil); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	// Catalog enumeration never contacts a daemon or provider.
	if w := authorizedRequest(t, s, "GET", "/api/projects", "", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if before != len(daemons[aID].calls)+len(daemons[bID].calls) {
		t.Fatal("catalog made daemon calls")
	}
}

func TestPublicPayloadPreservesNonRepositoryIDs(t *testing.T) {
	raw := json.RawMessage(`{"id":"local","content":"local","repository_id":"local"}`)
	out, err := publicGitPayload("git.file.read", "project-v1-test", raw)
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatal(string(out), err)
	}
	out, err = publicGitPayload("git.worktree.create", "project-v1-test", raw)
	if err != nil {
		t.Fatal(err)
	}
	var tree domain.Worktree
	if err = json.Unmarshal(out, &tree); err != nil || tree.ID != "local" || tree.RepositoryID != "project-v1-test" {
		t.Fatal(tree, err)
	}
}
