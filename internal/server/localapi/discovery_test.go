package localapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
)

func gitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
func repoFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, path, "init", "-b", "main")
	gitFixture(t, path, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
func TestProjectIDMatchesGitReportedMainPath(t *testing.T) {
	repo := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	trees, err := coregit.ListWorktreesContext(context.Background(), repo)
	if err != nil || len(trees) == 0 {
		t.Fatal(trees, err)
	}
	if got, want := config.ProjectID(trees[0].Path), config.ProjectID(repo); got != want {
		t.Fatalf("git-reported main path changed project id: git=%q caller=%q gitID=%q callerID=%q", trees[0].Path, repo, got, want)
	}
}

func TestProjectIDMatchesGitReportedMainPathThroughNativeAlias(t *testing.T) {
	repo := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	alias := ""
	if runtime.GOOS == "windows" {
		alias = strings.ToUpper(repo)
		if _, err := os.Stat(alias); err != nil {
			t.Fatalf("case-variant repository path unavailable: %v", err)
		}
	} else {
		alias = filepath.Join(t.TempDir(), "repo-alias")
		if err := os.Symlink(repo, alias); err != nil {
			t.Skipf("symlink alias unavailable: %v", err)
		}
	}
	trees, err := coregit.ListWorktreesContext(context.Background(), alias)
	if err != nil || len(trees) == 0 {
		t.Fatal(trees, err)
	}
	if got, want := config.ProjectID(trees[0].Path), config.ProjectID(alias); got != want {
		t.Fatalf("native alias changed project id: git=%q alias=%q gitID=%q aliasID=%q", trees[0].Path, alias, got, want)
	}
}

func TestProjectIDMatchesGitReportedMainPathFromLinkedWorktree(t *testing.T) {
	repo := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	linked := filepath.Join(t.TempDir(), "linked")
	gitFixture(t, repo, "worktree", "add", "-b", "linked-id-test", linked)
	trees, err := coregit.ListWorktreesContext(context.Background(), linked)
	if err != nil || len(trees) == 0 {
		t.Fatal(trees, err)
	}
	if got, want := config.ProjectID(trees[0].Path), config.ProjectID(repo); got != want {
		t.Fatalf("linked worktree reported a different main project id: git=%q main=%q gitID=%q mainID=%q", trees[0].Path, repo, got, want)
	}
}

func TestDiscoveryOverlappingRootsClonesAndLinkedWorktrees(t *testing.T) {
	root := t.TempDir()
	a := repoFixture(t, filepath.Join(root, "team", "one"))
	b := repoFixture(t, filepath.Join(root, "two"))
	for _, repo := range []string{a, b} {
		gitFixture(t, repo, "remote", "add", "origin", "https://github.com/example/same.git")
	}
	linked := filepath.Join(t.TempDir(), "linked")
	gitFixture(t, a, "worktree", "add", "-b", "linked", linked)
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := config.UpdateProjectRoots(path, "one", 0, root, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = config.UpdateProjectRoots(path, "two", cfg.Revision, filepath.Join(root, "team"), "")
	if err != nil {
		t.Fatal(err)
	}
	r := newProjectRegistry(path, a)
	if _, err = r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 2 {
		t.Fatal(r.List())
	}
	p, ok := r.Lookup(config.ProjectID(a))
	if !ok || p.info.RootID != config.PathID("root", filepath.Dir(a)) {
		t.Fatalf("project lookup failed: path=%q id=%q project=%+v projects=%+v", a, config.ProjectID(a), p.info, r.List())
	}
	trees, err := coregit.ListWorktreesContext(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	linkedInfo, err := os.Stat(linked)
	if err != nil {
		t.Fatal(err)
	}
	linkedID := ""
	for _, tree := range trees {
		treeInfo, statErr := os.Stat(tree.Path)
		if !tree.Bare && statErr == nil && os.SameFile(linkedInfo, treeInfo) {
			linkedID = local.ID("local", tree.Path)
			break
		}
	}
	if linkedID == "" {
		t.Fatalf("linked worktree missing from git listing: linked=%q trees=%+v", linked, trees)
	}
	if owner, ok := r.Worktree(context.Background(), linkedID); !ok || owner.info.ID != p.info.ID {
		t.Fatalf("git-listed worktree id did not resolve owner: id=%q owner=%+v ok=%v", linkedID, owner.info, ok)
	}
	before := r.List()
	if changed, err := r.Refresh(context.Background()); changed || err != nil {
		t.Fatal(changed, err)
	}
	if r.List()[0].ID != before[0].ID {
		t.Fatal("unstable IDs")
	}
	// A selected linked worktree can discover a main repository outside all roots.
	linkedSettings := filepath.Join(t.TempDir(), "settings.json")
	if _, err = config.UpdateProjectRoots(linkedSettings, "linked", 0, linked, ""); err != nil {
		t.Fatal(err)
	}
	only := newProjectRegistry(linkedSettings, b)
	if _, err = only.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(only.List()) != 1 || only.List()[0].Path != a {
		t.Fatal(only.List())
	}
	if only.Default().daemon != nil {
		t.Fatal("picked arbitrary default")
	}
}
func TestDiscoveryLimitsMissingAndCancellation(t *testing.T) {
	root := t.TempDir()
	repoFixture(t, filepath.Join(root, "a", "b", "c", "d", "included"))
	repoFixture(t, filepath.Join(root, "node_modules", "hidden"))
	repoFixture(t, filepath.Join(root, "visible"))
	gitFixture(t, root, "init", "--bare", "bare.git")
	canonical, _ := config.CanonicalDirectory(root)
	scan := scanRoot(context.Background(), config.ProjectRoot{ID: "root", Path: canonical})
	if len(scan.repos) != 1 || !scan.diagnostic.Truncated || len(scan.diagnostic.Messages) < 2 {
		t.Fatal(scan)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out := scanRoot(ctx, config.ProjectRoot{Path: root}); out.complete {
		t.Fatal("cancelled scan marked complete")
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := config.UpdateProjectRoots(path, "add", 0, filepath.Join(root, "visible"), "")
	if err != nil {
		t.Fatal(err)
	}
	r := newProjectRegistry(path, filepath.Join(root, "visible"))
	if _, err = r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(filepath.Join(root, "visible"), filepath.Join(root, "offline")); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 || r.List()[0].Available {
		t.Fatal(r.List())
	}
	if _, err = config.UpdateProjectRoots(path, "remove", cfg.Revision, "", cfg.Roots[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 0 {
		t.Fatal(r.List())
	}
	if _, err = os.Stat(filepath.Join(root, "offline", ".git")); err != nil {
		t.Fatal("root removal changed files", err)
	}
}
func TestDiscoveryRootItselfAndNoSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	repo := repoFixture(t, filepath.Join(root, "repo"))
	scan := scanRoot(context.Background(), config.ProjectRoot{Path: repo})
	if len(scan.repos) != 1 {
		t.Fatal(scan)
	}
	alias := filepath.Join(root, "cycle")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	root, _ = config.CanonicalDirectory(root)
	scan = scanRoot(context.Background(), config.ProjectRoot{Path: root})
	if len(scan.repos) != 1 || scan.diagnostic.Truncated {
		t.Fatal(scan)
	}
}

func TestDiscoveryDiscardsChangedGeneration(t *testing.T) {
	root := repoFixture(t, filepath.Join(t.TempDir(), "repo"))
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := config.UpdateProjectRoots(path, "add", 0, root, "")
	if err != nil {
		t.Fatal(err)
	}
	r := newProjectRegistry(path, root)
	started, release := make(chan struct{}), make(chan struct{})
	r.scan = func(ctx context.Context, root config.ProjectRoot) rootScan {
		close(started)
		<-release
		return scanRoot(ctx, root)
	}
	done := make(chan error, 1)
	go func() { _, err := r.Refresh(context.Background()); done <- err }()
	<-started
	if _, err = config.UpdateProjectRoots(path, "remove", cfg.Revision, "", cfg.Roots[0].ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-done; err == nil {
		t.Fatal("published obsolete scan")
	}
	if len(r.List()) != 0 {
		t.Fatal(r.List())
	}
}
