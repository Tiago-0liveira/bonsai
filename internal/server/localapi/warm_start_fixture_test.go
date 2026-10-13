package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// The warm-start fixture: three deterministic projects served through the real
// server wiring (newServer) with fake daemons and a fake GitHub. Nothing touches
// a real repository or the network. "alpha" is the active project.
const (
	warmFixtureAddress     = "127.0.0.1:7013"
	warmFixtureActive      = "alpha"
	warmFixturePageSize    = 100
	warmFixtureBackgroundN = 5
)

var warmFixtureProjects = []string{"alpha", "beta", "gamma"}

// warmGates holds fake reads open until released. A missing gate is open.
type warmGates struct {
	mu   sync.Mutex
	held map[string]chan struct{}
}

func (g *warmGates) hold(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held == nil {
		g.held = map[string]chan struct{}{}
	}
	if g.held[name] == nil {
		g.held[name] = make(chan struct{})
	}
}

func (g *warmGates) release(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ch := g.held[name]; ch != nil {
		close(ch)
		delete(g.held, name)
	}
}

func (g *warmGates) wait(ctx context.Context, name string) error {
	g.mu.Lock()
	ch := g.held[name]
	g.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

func warmBranch(i int) string               { return fmt.Sprintf("feature/%03d", i) }
func warmHead(project string, i int) string { return fmt.Sprintf("%s-head-%03d", project, i) }

// warmDaemon serves a generated repository: the main worktree plus linked ones.
type warmDaemon struct {
	project   string
	root      string
	worktrees int
	gates     *warmGates
	mu        sync.Mutex
	commands  []string
}

func (d *warmDaemon) state(withStatus bool) domain.RepositoryState {
	fullName := "acme/" + d.project
	main := domain.Worktree{ID: gitlocal.ID(localRepositoryID, d.root), RepositoryID: localRepositoryID, Path: d.root, Branch: "main", HeadSHA: d.project + "-main", Main: true}
	branches := []domain.Branch{
		{Name: "main", LocalHeadSHA: main.HeadSHA, Upstream: "origin/main"},
		{Name: "origin/main", Remote: true, LocalRemoteRefSHA: main.HeadSHA},
	}
	status := func(branch, head string) *domain.WorkingTreeStatus {
		if !withStatus {
			return nil
		}
		return &domain.WorkingTreeStatus{Branch: branch, HeadState: "branch", HeadSHA: head, Upstream: "origin/" + branch, LocalRemoteRefSHA: head, DivergenceAvailable: true, Files: []domain.FileStatus{}, Conflicted: []string{}}
	}
	main.Status = status("main", main.HeadSHA)
	trees := []domain.Worktree{main}
	for i := 0; i < d.worktrees-1; i++ {
		path := filepath.Join(d.root, fmt.Sprintf("wt-%03d", i))
		branch, head := warmBranch(i), warmHead(d.project, i)
		trees = append(trees, domain.Worktree{ID: gitlocal.ID(localRepositoryID, path), RepositoryID: localRepositoryID, Path: path, Branch: branch, HeadSHA: head, Status: status(branch, head)})
		branches = append(branches,
			domain.Branch{Name: branch, LocalHeadSHA: head, Upstream: "origin/" + branch},
			domain.Branch{Name: "origin/" + branch, Remote: true, LocalRemoteRefSHA: head},
		)
	}
	return domain.RepositoryState{
		ID:            localRepositoryID,
		DefaultBranch: "main",
		Branches:      branches,
		Worktrees:     trees,
		Remotes:       []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: "acme", Repository: d.project, FullName: fullName}},
	}
}

func (d *warmDaemon) GitContext(ctx context.Context, command gitbridge.Command) (*gitbridge.Result, error) {
	d.mu.Lock()
	d.commands = append(d.commands, command.Type)
	d.mu.Unlock()
	var payload any
	switch command.Type {
	case "git.inventory":
		payload = d.state(false)
	case "git.repository.refresh":
		if err := d.gates.wait(ctx, "local"); err != nil {
			return nil, err
		}
		payload = d.state(true)
	default:
		return &gitbridge.Result{ID: command.ID, Error: &domain.Error{Code: "unsupported", Message: "the warm-start fixture does not serve " + command.Type}}, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &gitbridge.Result{ID: command.ID, Payload: raw}, nil
}
func (d *warmDaemon) Git(command gitbridge.Command) (*gitbridge.Result, error) {
	return d.GitContext(context.Background(), command)
}
func (d *warmDaemon) List() ([]*procstore.Record, error) { return nil, nil }
func (d *warmDaemon) ListContext(context.Context) ([]*procstore.Record, error) {
	return nil, nil
}
func (d *warmDaemon) Restart(int) (*procstore.Record, error) { return nil, nil }
func (d *warmDaemon) Kill(int, bool, string) ([]int, error)  { return nil, nil }
func (d *warmDaemon) Logs(int, bool, int, string, bool, func(string) error) error {
	return nil
}

// warmGitHub answers every project's repository from generated data. Open PRs
// 0..linked-1 belong to linked worktrees; the rest have no worktree.
type warmGitHub struct {
	*syncTestGitHub
	gates *warmGates
	// prs and worktrees per repository full name.
	prs       map[string]int
	worktrees map[string]int
	mu        sync.Mutex
	pages     map[string][]int
}

func (g *warmGitHub) project(repo string) string { return strings.TrimPrefix(repo, "acme/") }

func (g *warmGitHub) Repository(ctx context.Context, repo string) (githubdomain.RemoteRepository, error) {
	if err := g.gates.wait(ctx, "catalog"); err != nil {
		return githubdomain.RemoteRepository{}, err
	}
	return githubdomain.RemoteRepository{ID: 1, FullName: repo, DefaultBranch: "main"}, nil
}

func (g *warmGitHub) Branches(ctx context.Context, repo string) ([]githubdomain.RemoteBranch, error) {
	if err := g.gates.wait(ctx, "catalog"); err != nil {
		return nil, err
	}
	project := g.project(repo)
	out := []githubdomain.RemoteBranch{{Name: "main", RemoteHeadSHA: project + "-main"}}
	for i := 0; i < g.worktrees[repo]-1; i++ {
		out = append(out, githubdomain.RemoteBranch{Name: warmBranch(i), RemoteHeadSHA: warmHead(project, i)})
	}
	return out, nil
}

func (g *warmGitHub) pull(repo string, i int) githubdomain.PullRequest {
	project := g.project(repo)
	head, sha := fmt.Sprintf("orphan/%03d", i), fmt.Sprintf("%s-orphan-%03d", project, i)
	if i < g.worktrees[repo]-1 {
		head, sha = warmBranch(i), warmHead(project, i)
	}
	updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute)
	return githubdomain.PullRequest{
		Number: i + 1, Title: fmt.Sprintf("Change %d in %s", i+1, project), Body: "Fixture description.",
		State: "open", Head: head, HeadRepository: repo, Base: "main", HeadSHA: sha,
		URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, i+1), Author: "fixture",
		CreatedAt: updated, UpdatedAt: updated, NodeID: fmt.Sprintf("PR_%s_%d", project, i+1),
	}
}

func (g *warmGitHub) PullRequestPage(ctx context.Context, repo string, _ githubdomain.PRFilter, page int) (githubdomain.PullRequestPage, error) {
	if err := g.gates.wait(ctx, "catalog"); err != nil {
		return githubdomain.PullRequestPage{}, err
	}
	g.mu.Lock()
	if g.pages == nil {
		g.pages = map[string][]int{}
	}
	g.pages[repo] = append(g.pages[repo], page)
	g.mu.Unlock()
	total := g.prs[repo]
	out := githubdomain.PullRequestPage{Items: []githubdomain.PullRequest{}}
	for i := (page - 1) * warmFixturePageSize; i < total && i < page*warmFixturePageSize; i++ {
		out.Items = append(out.Items, g.pull(repo, i))
	}
	if page*warmFixturePageSize < total {
		out.NextPage = page + 1
	}
	return out, nil
}

func (g *warmGitHub) PullRequests(ctx context.Context, repo string, filter githubdomain.PRFilter) ([]githubdomain.PullRequest, error) {
	var out []githubdomain.PullRequest
	for page := 1; page != 0; {
		batch, err := g.PullRequestPage(ctx, repo, filter, page)
		if err != nil {
			return nil, err
		}
		out = append(out, batch.Items...)
		page = batch.NextPage
	}
	return out, nil
}

func (g *warmGitHub) Checks(ctx context.Context, _ string, sha string) ([]githubdomain.Check, error) {
	if err := g.gates.wait(ctx, "checks"); err != nil {
		return nil, err
	}
	conclusion := "success"
	if n, err := strconv.Atoi(sha[len(sha)-3:]); err == nil && n%7 == 3 {
		conclusion = "failure"
	}
	return []githubdomain.Check{{ID: 1, Name: "ci", Status: "completed", Conclusion: conclusion}}, nil
}

// warmFixture owns the current server and swaps it on restart; the listener
// and the browser-facing origin stay the same, as with a restarted API.
type warmFixture struct {
	t     *testing.T
	dist  string
	base  string
	gates *warmGates

	mu        sync.Mutex
	worktrees int
	prs       int
	removed   map[string]bool
	server    *Server
	handler   http.Handler
	cancel    context.CancelFunc
	runDone   chan struct{}
	hijacked  map[net.Conn]bool
	daemons   map[string]*warmDaemon
	github    *warmGitHub
}

func newWarmFixture(t *testing.T, dist string) *warmFixture {
	return &warmFixture{t: t, dist: dist, base: t.TempDir(), gates: &warmGates{}, worktrees: 25, prs: 50, removed: map[string]bool{}, hijacked: map[net.Conn]bool{}}
}

func (f *warmFixture) registry() *syncTestRegistry {
	entries := map[string]projectServices{}
	f.daemons = map[string]*warmDaemon{}
	f.github = &warmGitHub{syncTestGitHub: &syncTestGitHub{}, gates: f.gates, prs: map[string]int{}, worktrees: map[string]int{}}
	for _, name := range warmFixtureProjects {
		if f.removed[name] {
			continue
		}
		worktrees, prs := warmFixtureBackgroundN, 3
		if name == warmFixtureActive {
			worktrees, prs = f.worktrees, f.prs
		}
		root := filepath.Join(f.base, "projects", name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			f.t.Fatal(err)
		}
		metadata, err := gitstore.Open(filepath.Join(f.base, name+"-metadata.json"))
		if err != nil {
			f.t.Fatal(err)
		}
		daemon := &warmDaemon{project: name, root: root, worktrees: worktrees, gates: f.gates}
		f.daemons[name] = daemon
		f.github.prs["acme/"+name] = prs
		f.github.worktrees["acme/"+name] = worktrees
		entries[name] = projectServices{
			info:   ProjectInfo{ID: name, RootID: "fixture", Name: name, Path: root, Available: true, WorkspaceID: "fixture", FullName: "acme/" + name, DefaultBranch: "main"},
			state:  metadata,
			daemon: daemon,
			github: f.github,
		}
	}
	return &syncTestRegistry{entries: entries}
}

// start replaces the running server with a new one: a new epoch, new
// sessions and empty in-memory caches, like a restarted API process.
func (f *warmFixture) start() {
	if err := f.restart(""); err != nil {
		f.t.Fatal(err)
	}
}

func (f *warmFixture) restart(cache string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopLocked()
	if cache != "" {
		if err := damageWarmCache(f.cachePath(), cache); err != nil {
			return err
		}
	}
	s, err := newServer(f.config(), f.registry(), f.rootsPath())
	if err != nil {
		return err
	}
	s.stateSync.watchProject = func(ctx context.Context, _ ProjectInfo) { <-ctx.Done() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.stateSync.Run(ctx)
	}()
	f.server, f.handler, f.cancel, f.runDone = s, s.Handler(), cancel, done
	return nil
}

func (f *warmFixture) rootsPath() string { return filepath.Join(f.base, "project-roots.json") }

func (f *warmFixture) cachePath() string {
	cfg := f.config()
	return newWarmStore(cfg.SnapshotCacheDir, warmStartNamespace(f.rootsPath(), cfg.RepoDir, cfg.SecurityMode)).path()
}

func (f *warmFixture) config() Config {
	return Config{Address: warmFixtureAddress, UI: os.DirFS(f.dist), SnapshotCacheDir: filepath.Join(f.base, "warm-start")}
}

func (f *warmFixture) stopLocked() {
	if f.server == nil {
		return
	}
	f.cancel()
	<-f.runDone
	// A graceful shutdown: the last commits reach the warm-start cache.
	f.server.stateSync.closeWarm(warmStartShutdownFlush)
	f.server.agents.manager.Close(true, 0)
	// Open event sockets belong to the old process.
	for conn := range f.hijacked {
		_ = conn.Close()
		delete(f.hijacked, conn)
	}
	f.server = nil
}

func (f *warmFixture) connState(conn net.Conn, state http.ConnState) {
	if state != http.StateHijacked {
		return
	}
	f.mu.Lock()
	f.hijacked[conn] = true
	f.mu.Unlock()
}

func (f *warmFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/__fixture/") {
		f.control(w, r)
		return
	}
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	handler.ServeHTTP(w, r)
}

func (f *warmFixture) components(r *http.Request) []string {
	var out []string
	for _, name := range strings.Split(r.URL.Query().Get("components"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// control is the test-only API the warm-start spec drives. It exists only in
// this test binary.
func (f *warmFixture) control(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	switch strings.TrimPrefix(r.URL.Path, "/__fixture/") {
	case "reset":
		f.mu.Lock()
		f.worktrees, f.prs = 25, 50
		if n, err := strconv.Atoi(query.Get("worktrees")); err == nil && n > 0 {
			f.worktrees = n
		}
		if n, err := strconv.Atoi(query.Get("prs")); err == nil && n >= 0 {
			f.prs = n
		}
		f.removed = map[string]bool{}
		f.mu.Unlock()
		for _, name := range []string{"local", "catalog", "checks"} {
			f.gates.release(name)
		}
		// A new process with the new sizes and no saved state.
		if err := f.restart("missing"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "hold":
		for _, name := range f.components(r) {
			f.gates.hold(name)
		}
	case "release":
		for _, name := range f.components(r) {
			f.gates.release(name)
		}
	case "restart":
		// Holds named here apply from the new process's first read; cache
		// damages the saved document between the two processes.
		for _, name := range f.components(r) {
			f.gates.hold(name)
		}
		if err := f.restart(query.Get("cache")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case "settle":
		// Validate every project live, as a browser's first connection would,
		// so the next restart finds a complete cache.
		if err := f.settle(); err != nil {
			http.Error(w, err.Error(), http.StatusGatewayTimeout)
			return
		}
	case "remove":
		f.mu.Lock()
		f.removed[query.Get("project")] = true
		f.mu.Unlock()
	default:
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	epoch := ""
	if f.server != nil {
		epoch = f.server.stateSync.epoch
	}
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"epoch": epoch, "active": warmFixtureActive})
}

func (f *warmFixture) settle() error {
	f.mu.Lock()
	s := f.server
	f.mu.Unlock()
	s.stateSync.RefreshAll(refreshAll, false)
	deadline := time.Now().Add(30 * time.Second)
	for _, info := range s.registry.List() {
		for {
			snapshot, _ := s.stateSync.CachedSnapshot(info.ID)
			if warmFixtureSettled(snapshot) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s did not validate: %+v", info.ID, snapshot.Freshness)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return nil
}

func warmFixtureSettled(snapshot browserSnapshot) bool {
	for _, component := range []string{"local", "provider", "processes"} {
		if freshness := snapshot.Freshness[component]; freshness.State != "ready" || freshness.Source != "" {
			return false
		}
	}
	if snapshot.Local == nil || len(snapshot.WorktreeState) != len(snapshot.Local.Worktrees) {
		return false
	}
	for _, state := range snapshot.WorktreeState {
		if state.CI.Freshness.State != "ready" {
			return false
		}
	}
	return true
}

// damageWarmCache replaces the saved document for the cold-path tests.
func damageWarmCache(path, mode string) error {
	switch mode {
	case "missing":
		err := os.Remove(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	case "corrupt":
		return os.WriteFile(path, []byte(`{"schema":1,"projects":[{"id":`), 0o600)
	case "unsupported":
		return os.WriteFile(path, []byte(`{"schema":999,"namespace":"x","projects":[]}`), 0o600)
	case "oversized":
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		defer file.Close()
		return file.Truncate(warmStartMaxDocument + 1)
	}
	return fmt.Errorf("unknown cache mode %q", mode)
}

func waitForE2EBundle(t *testing.T) string {
	t.Helper()
	dist, err := filepath.Abs(filepath.Join("..", "..", "..", "web", "dist-e2e"))
	if err != nil {
		t.Fatal(err)
	}
	// The e2e bundle is built by the first Playwright web server.
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if _, err := os.Stat(filepath.Join(dist, "index.html")); err == nil {
			return dist
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s/index.html not built", dist)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestWarmStartBrowserFixture serves the e2e bundle on 127.0.0.1:7013 for
// web/e2e/warm-start.spec.ts.
func TestWarmStartBrowserFixture(t *testing.T) {
	if os.Getenv("BONSAI_WARM_START_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	dist := waitForE2EBundle(t)
	// BONSAI_WARM_START_UI=dist serves the production build instead, for the
	// final paint numbers (build it first with `pnpm -C web build`).
	if os.Getenv("BONSAI_WARM_START_UI") == "dist" {
		dist = filepath.Join(filepath.Dir(dist), "dist")
	}
	t.Setenv("HOME", t.TempDir())
	f := newWarmFixture(t, dist)
	f.start()
	listener, err := net.Listen("tcp", warmFixtureAddress)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: f, ReadHeaderTimeout: time.Second, ConnState: f.connState}
	go func() { <-ctx.Done(); _ = server.Close() }()
	if err = server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.stopLocked()
	f.mu.Unlock()
}
