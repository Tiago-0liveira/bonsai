package playgroundbackend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/sessionlink"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	localgit "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// gitEnv keeps every Git command off the real home and system configuration.
func gitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
}

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is a temp repository on branch trunk with one commit.
type repo struct {
	t    *testing.T
	main string
}

func newRepo(t *testing.T, branch string) *repo {
	t.Helper()
	gitEnv(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "init", "-b", branch)
	sh(t, dir, "config", "user.email", "t@t.t")
	sh(t, dir, "config", "user.name", "tester")
	sh(t, dir, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(dir, "base.txt"), "one\ntwo\nthree\n")
	sh(t, dir, "add", "-A")
	sh(t, dir, "commit", "-m", "init")
	return &repo{t: t, main: dir}
}

func (r *repo) commit(dir, msg string) string {
	sh(r.t, dir, "add", "-A")
	sh(r.t, dir, "commit", "-m", msg)
	return sh(r.t, dir, "rev-parse", "HEAD")
}

// worktree adds a linked worktree on a new branch.
func (r *repo) worktree(branch string) string {
	p, err := filepath.EvalSymlinks(r.t.TempDir())
	if err != nil {
		r.t.Fatal(err)
	}
	p = filepath.Join(p, branch)
	sh(r.t, r.main, "worktree", "add", "-b", branch, p)
	return p
}

type fakeSessions struct {
	mu   sync.Mutex
	list []agentsapi.Session
	err  error
}

func (f *fakeSessions) List(context.Context) ([]agentsapi.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentsapi.Session(nil), f.list...), f.err
}

func session(id, dir, project string) agentsapi.Session {
	return agentsapi.Session{Summary: agentterminal.Summary{ID: id, ProjectID: project}, WorkDir: dir}
}

type fakeAttempts struct {
	byID  map[string]launch.Attempt
	err   error
	calls int
}

func (f *fakeAttempts) Get(_ context.Context, id string) (launch.Attempt, error) {
	f.calls++
	if f.err != nil {
		return launch.Attempt{}, f.err
	}
	a, ok := f.byID[id]
	if !ok {
		return launch.Attempt{}, launch.ErrNotFound
	}
	return a, nil
}

type fakeGitHub struct {
	mu        sync.Mutex
	prs       []github.PullRequest
	prsErr    error
	details   map[int]github.PullRequestDetail
	detailErr error
	checks    []github.Check
	checksErr error
	calls     []string
	filter    github.PRFilter
}

func (f *fakeGitHub) PullRequests(_ context.Context, repo string, fl github.PRFilter) ([]github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "list:"+repo)
	f.filter = fl
	return f.prs, f.prsErr
}

func (f *fakeGitHub) PullRequest(_ context.Context, repo string, n int) (github.PullRequestDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "get:"+repo)
	if f.detailErr != nil {
		return github.PullRequestDetail{}, f.detailErr
	}
	d, ok := f.details[n]
	if !ok {
		return github.PullRequestDetail{}, errors.New("no such pull request")
	}
	return d, nil
}

func (f *fakeGitHub) Checks(_ context.Context, repo, sha string) ([]github.Check, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "checks:"+repo+"@"+sha)
	return f.checks, f.checksErr
}

type fakeGarden struct {
	view  gardenapi.WorkView
	err   error
	calls int
}

func (f *fakeGarden) Work(context.Context) (gardenapi.WorkView, error) {
	f.calls++
	return f.view, f.err
}

func workWith(local garden.LocalRepoID, source gardensync.Source, numbers ...int64) gardenapi.WorkView {
	snap := gardensync.Snapshot{LocalRepoID: local, Source: source}
	for _, n := range numbers {
		snap.Items = append(snap.Items, gardensync.Item{Issue: garden.Issue{ID: garden.IssueID(n * 100), Number: n}})
	}
	return gardenapi.WorkView{Snapshot: snap}
}

// rig is a backend over a real temp repository with fake sessions and GitHub.
type rig struct {
	*repo
	b        *Backend
	sessions *fakeSessions
	attempts *fakeAttempts
	gh       *fakeGitHub
	garden   *fakeGarden
	links    *sessionlink.FileStore
	remotes  []coregit.Remote
	tick     time.Time
}

func newRig(t *testing.T, branch string) *rig {
	t.Helper()
	r := &rig{repo: newRepo(t, branch), sessions: &fakeSessions{}, attempts: &fakeAttempts{byID: map[string]launch.Attempt{}}, gh: &fakeGitHub{details: map[int]github.PullRequestDetail{}}, garden: &fakeGarden{}}
	r.links = &sessionlink.FileStore{Path: filepath.Join(t.TempDir(), "links.json")}
	r.remotes = []coregit.Remote{{Name: "origin", URL: "https://token@github.com/acme/widgets.git"}}
	r.tick = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	b, err := newBackend(r.main, Deps{
		Sessions: r.sessions, Attempts: r.attempts, GitHub: r.gh, Garden: r.garden, Links: r.links,
		Git: runGit, Trees: coregit.ListWorktreesContext,
		Remotes: func(context.Context) ([]coregit.Remote, error) { return r.remotes, nil },
		Now:     func() time.Time { return r.tick },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.b = b
	return r
}

// add registers a session running in dir, recorded under this project.
func (r *rig) add(id, dir string) agentsapi.Session {
	s := session(id, dir, r.b.project)
	r.sessions.list = append(r.sessions.list, s)
	return s
}

type coregitWorktree = coregit.Worktree

func worktreeID(path string) string { return localgit.ID(repositoryID, path) }
