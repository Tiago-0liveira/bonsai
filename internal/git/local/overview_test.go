package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

// setupWithOrigin returns a service whose main branch tracks a bare origin and
// that has n extra worktrees, each on its own pushed, tracking branch.
func setupWithOrigin(t *testing.T, n int) (*Service, string) {
	t.Helper()
	s, dir, _ := setup(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, filepath.Dir(bare), "init", "--bare", filepath.Base(bare))
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-u", "origin", "main")
	ctx := context.Background()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("feature/%d", i)
		if _, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: name, Base: "main"}); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "push", "-u", "origin", name)
	}
	return s, dir
}

func gitSpawns() int64 {
	g, _ := trace.Counts()
	return g
}

func TestRepositoryStatusPoolIsBoundedAndKeepsOrder(t *testing.T) {
	s, _ := setupWithOrigin(t, 5)
	ctx := context.Background()
	original, originalSize := overviewStatus, statusPoolSize
	defer func() { overviewStatus, statusPoolSize = original, originalSize }()

	var inFlight, peak atomic.Int32
	overviewStatus = func(ctx context.Context, dir string, refs *refIndex) (domain.WorkingTreeStatus, error) {
		now := inFlight.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		defer inFlight.Add(-1)
		return original(ctx, dir, refs)
	}
	for _, size := range []int{1, 3} {
		peak.Store(0)
		statusPoolSize = func() int { return size }
		state, err := s.Repository(ctx, "repo")
		if err != nil {
			t.Fatal(err)
		}
		if got := int(peak.Load()); got != size {
			t.Fatalf("pool size %d: peak concurrency = %d", size, got)
		}
		listed, err := s.ListWorktrees(ctx, "repo")
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Worktrees) != len(listed) {
			t.Fatalf("worktrees = %d, want %d", len(state.Worktrees), len(listed))
		}
		for i, tree := range state.Worktrees {
			if tree.ID != listed[i].ID {
				t.Fatalf("worktree %d out of order", i)
			}
			if tree.Status == nil || tree.Status.Branch != tree.Branch {
				t.Fatalf("worktree %s status = %+v", tree.Branch, tree.Status)
			}
		}
	}
}

func TestRepositoryOverviewSpawnsOneGitProcessPerWorktree(t *testing.T) {
	const extra = 4
	s, _ := setupWithOrigin(t, extra)
	ctx := context.Background()
	before := gitSpawns()
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	spawned := gitSpawns() - before
	worktrees := int64(extra + 1)
	if int64(len(state.Worktrees)) != worktrees {
		t.Fatalf("worktrees = %d", len(state.Worktrees))
	}
	// for-each-ref, worktree list, remote and one get-url, plus exactly one
	// `git status` per worktree: no rev-parse, no log, no git-dir, no
	// symbolic-ref.
	if want := worktrees + 4; spawned != want {
		t.Fatalf("git spawns = %d, want %d", spawned, want)
	}
}

func TestOverviewMatchesDetailStatusWithoutExtraProcesses(t *testing.T) {
	s, dir := setupWithOrigin(t, 2)
	ctx := context.Background()
	// A local commit makes main ahead of its upstream.
	if err := os.WriteFile(filepath.Join(dir, "ahead.txt"), []byte("ahead\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "ahead.txt")
	git(t, dir, "commit", "-m", "ahead of origin")
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tree := range state.Worktrees {
		detail, err := s.Status(ctx, tree.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := tree.Status
		if got == nil {
			t.Fatalf("%s: no overview status", tree.Branch)
		}
		if got.LocalRemoteRefSHA != detail.LocalRemoteRefSHA || got.LocalRemoteRefSHA == "" {
			t.Fatalf("%s: upstream sha overview %q detail %q", tree.Branch, got.LocalRemoteRefSHA, detail.LocalRemoteRefSHA)
		}
		if got.DivergenceAvailable != detail.DivergenceAvailable || got.Ahead != detail.Ahead || got.Behind != detail.Behind {
			t.Fatalf("%s: divergence overview %+v detail %+v", tree.Branch, got, detail)
		}
		if got.LastCommit == nil || detail.LastCommit == nil || *got.LastCommit != *detail.LastCommit {
			t.Fatalf("%s: last commit overview %+v detail %+v", tree.Branch, got.LastCommit, detail.LastCommit)
		}
		if got.GitState != detail.GitState || got.Upstream != detail.Upstream {
			t.Fatalf("%s: overview %+v detail %+v", tree.Branch, got, detail)
		}
	}
}

func TestOverviewFallsBackToGitForDetachedGoneAndMovedRefs(t *testing.T) {
	s, dir := setupWithOrigin(t, 0)
	ctx := context.Background()

	git(t, dir, "checkout", "--detach")
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	detached := state.Worktrees[0].Status
	if detached == nil || detached.HeadState != "detached" || detached.LastCommit == nil || detached.LastCommit.Subject != "initial" {
		t.Fatalf("detached overview = %+v", detached)
	}
	git(t, dir, "checkout", "main")

	git(t, dir, "update-ref", "-d", "refs/remotes/origin/main")
	state, err = s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	gone := state.Worktrees[0].Status
	if gone == nil || gone.Upstream != "origin/main" || gone.DivergenceAvailable || gone.LocalRemoteRefSHA != "" || gone.Ahead != 0 || gone.Behind != 0 {
		t.Fatalf("gone-upstream overview = %+v", gone)
	}

	// A ref that moved after the index was built must not be reported as HEAD's
	// commit: the tip only answers for the exact HEAD SHA.
	refs := parseRefs("refs/heads/main\x00aaa\x00\x00\x00\x001700000000\x00old subject\x00Ann\n")
	if _, ok := refs.tipCommit("main", "bbb"); ok {
		t.Fatal("tip commit answered for a different HEAD")
	}
	if c, ok := refs.tipCommit("main", "aaa"); !ok || c.Subject != "old subject" || c.Author != "Ann" {
		t.Fatalf("tip commit = %+v %v", c, ok)
	}
}

func TestOverviewCountsUntrackedDirectoryOnceDetailKeepsAll(t *testing.T) {
	s, dir, id := setup(t)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, "scratch", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	overview := state.Worktrees[0].Status
	if overview == nil || !overview.Dirty || overview.Untracked != 1 || len(overview.Files) != 1 {
		t.Fatalf("overview = %+v", overview)
	}
	detail, err := s.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Dirty || detail.Untracked != 3 || len(detail.Files) != 3 {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestAbsoluteGitDirReadsDiskAndFallsBack(t *testing.T) {
	s, dir, _ := setup(t)
	ctx := context.Background()
	tree, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "feature/gitdir", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	want := func(path string) string {
		t.Helper()
		out := strings.TrimSpace(git(t, path, "rev-parse", "--absolute-git-dir"))
		resolved, err := filepath.EvalSymlinks(out)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	check := func(label, path string) {
		t.Helper()
		got, err := absoluteGitDir(ctx, path)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		resolved, err := filepath.EvalSymlinks(got)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if resolved != want(path) {
			t.Fatalf("%s: git dir = %q, want %q", label, resolved, want(path))
		}
	}

	check("main", dir)
	if _, ok := readGitDir(dir); !ok {
		t.Fatal("main worktree git dir should come from disk")
	}
	check("linked", tree.Path)
	if _, ok := readGitDir(tree.Path); !ok {
		t.Fatal("linked worktree git dir should come from disk")
	}

	// A relative gitdir is resolved against the worktree.
	gd, _ := readGitDir(tree.Path)
	rel, err := filepath.Rel(tree.Path, gd)
	if err != nil {
		t.Fatal(err)
	}
	dotGit := filepath.Join(tree.Path, ".git")
	original, err := os.ReadFile(dotGit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotGit, []byte("gitdir: "+rel+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, ok := readGitDir(tree.Path); !ok || got != filepath.Clean(gd) {
		t.Fatalf("relative gitdir = %q %v, want %q", got, ok, gd)
	}
	check("relative", tree.Path)

	// Unparseable or dangling pointers ask Git instead.
	for label, content := range map[string]string{"garbage": "not a pointer\n", "dangling": "gitdir: /does/not/exist\n"} {
		if err := os.WriteFile(dotGit, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, ok := readGitDir(tree.Path); ok {
			t.Fatalf("%s: should not resolve from disk", label)
		}
	}
	if err := os.WriteFile(dotGit, original, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryCacheServesTargetLookups(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	tree, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "feature/cache", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	r := s.repos["repo"]
	measure := func(fn func()) int64 {
		before := gitSpawns()
		fn()
		return gitSpawns() - before
	}
	lookup := func() {
		if _, _, err := s.target(ctx, tree.ID); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.ListWorktrees(ctx, "repo"); err != nil {
		t.Fatal(err)
	}
	if n := measure(lookup); n != 0 {
		t.Fatalf("warm lookup spawned %d git processes", n)
	}
	if n := measure(func() {
		if _, _, err := s.registeredTarget(ctx, tree.ID); err != nil {
			t.Fatal(err)
		}
	}); n != 1 {
		t.Fatalf("registeredTarget must list fresh, spawned %d", n)
	}

	r.inv.mu.Lock()
	r.inv.at = time.Now().Add(-2 * inventoryCacheTTL)
	r.inv.mu.Unlock()
	if n := measure(lookup); n != 1 {
		t.Fatalf("expired lookup spawned %d, want 1", n)
	}
	if n := measure(lookup); n != 0 {
		t.Fatalf("refilled lookup spawned %d", n)
	}

	r.inv.invalidate()
	if n := measure(lookup); n != 1 {
		t.Fatalf("invalidated lookup spawned %d, want 1", n)
	}
	// An ID missing from a warm cache is re-listed once before NotFound, so a
	// worktree created outside this service is still found.
	if _, _, err := s.target(ctx, "wt-missing"); err == nil {
		t.Fatal("unknown worktree resolved")
	}
	if err := s.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: tree.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.target(ctx, tree.ID); err == nil {
		t.Fatal("removed worktree still resolves from cache")
	}
}

func TestInventoryReturnsWorktreesBranchesAndRemotes(t *testing.T) {
	s, _ := setupWithOrigin(t, 1)
	ctx := context.Background()
	inv, err := s.Inventory(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Worktrees) != 2 || len(inv.Remotes) != 1 || inv.Remotes[0].Name != "origin" {
		t.Fatalf("inventory = %+v", inv)
	}
	names := map[string]bool{}
	for _, b := range inv.Branches {
		names[b.Name] = true
	}
	if !names["main"] || !names["feature/0"] || !names["origin/main"] {
		t.Fatalf("branches = %v", names)
	}
	for _, tree := range inv.Worktrees {
		if tree.Status != nil {
			t.Fatalf("inventory must not carry status: %+v", tree)
		}
	}
	if _, err := s.Inventory(ctx, "nope"); err == nil {
		t.Fatal("unknown repository accepted")
	}
}

func TestRepositoryStatusPoolRunsUnderRepositoryLock(t *testing.T) {
	s, _ := setupWithOrigin(t, 3)
	ctx := context.Background()
	original := overviewStatus
	defer func() { overviewStatus = original }()
	var mu sync.Mutex
	var lockedDuringStatus bool
	r := s.repos["repo"]
	overviewStatus = func(ctx context.Context, dir string, refs *refIndex) (domain.WorkingTreeStatus, error) {
		select {
		case r.gate <- struct{}{}:
			<-r.gate // acquired: the pool was running without the lock
		default:
			mu.Lock()
			lockedDuringStatus = true
			mu.Unlock()
		}
		return original(ctx, dir, refs)
	}
	if _, err := s.Repository(ctx, "repo"); err != nil {
		t.Fatal(err)
	}
	if !lockedDuringStatus {
		t.Fatal("statuses ran without holding the repository lock")
	}
}

func TestInventoryCacheIgnoresListsStartedBeforeInvalidation(t *testing.T) {
	var c inventoryCache
	gen := c.begin()
	c.invalidate() // a mutation lands while the list is still running
	c.store(gen, []core.Worktree{{Path: "/stale"}})
	if _, ok := c.load(); ok {
		t.Fatal("a list read before the invalidation was cached")
	}
	c.store(c.begin(), []core.Worktree{{Path: "/fresh"}})
	if trees, ok := c.load(); !ok || len(trees) != 1 || trees[0].Path != "/fresh" {
		t.Fatalf("fresh list not cached: %v %v", trees, ok)
	}
}

func TestStaleCachedWorktreeReportsNotFoundNotFilesystemError(t *testing.T) {
	s, dir, _ := setup(t)
	ctx := context.Background()
	tree, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "feature/gone", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListWorktrees(ctx, "repo"); err != nil {
		t.Fatal(err)
	}
	// Removed behind the service's back, inside the cache TTL.
	git(t, dir, "worktree", "remove", "--force", tree.Path)
	if _, _, err := s.target(ctx, tree.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("target after external removal = %v, want not found", err)
	}
}

func TestDefaultBranchComesFromOriginHeadWithoutSymbolicRef(t *testing.T) {
	s, dir := setupWithOrigin(t, 0)
	ctx := context.Background()
	git(t, dir, "branch", "trunk")
	git(t, dir, "push", "origin", "trunk")
	git(t, dir, "remote", "set-head", "origin", "trunk")
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	if state.DefaultBranch != "trunk" {
		t.Fatalf("default branch = %q, want trunk", state.DefaultBranch)
	}
}
