package local

import (
	"context"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s: %v", args, b, e)
	}
	return string(b)
}
func setup(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0600)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	s, e := New([]Config{{ID: "repo", Root: dir, WorktreeRoot: t.TempDir()}})
	if e != nil {
		t.Fatal(e)
	}
	trees, e := s.ListWorktrees(context.Background(), "repo")
	if e != nil {
		t.Fatal(e)
	}
	if len(trees) != 1 {
		t.Fatalf("expected one main worktree, got %d", len(trees))
	}
	return s, dir, trees[0].ID
}
func TestStatusFilesBoundaries(t *testing.T) {
	s, dir, id := setup(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("edit\n"), 0600)
	const stagedPath = "a b.txt"
	if e := os.WriteFile(filepath.Join(dir, stagedPath), []byte("new\n"), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := s.Status(ctx, id)
	if e != nil || !st.Dirty || st.Modified != 1 || st.Untracked != 1 {
		t.Fatalf("%+v %v", st, e)
	}
	for _, p := range []string{"../secret", ".git/config", "/etc/passwd", "foo/../../secret"} {
		if _, e = s.ReadFile(ctx, id, p); e == nil {
			t.Fatalf("allowed %q", p)
		}
	}
	os.Symlink(".git/config", filepath.Join(dir, "secret"))
	if _, e = s.ReadFile(ctx, id, "secret"); e == nil {
		t.Fatal("allowed symlink")
	}
	if _, e = s.Status(ctx, "other"); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	if e = s.Stage(ctx, id, []string{stagedPath}, false); e != nil {
		t.Fatal(e)
	}
	c, e := s.Commit(ctx, id, "added file")
	if e != nil || c.SHA == "" {
		t.Fatal(c, e)
	}
	st, _ = s.Status(ctx, id)
	if st.Modified != 1 {
		t.Fatal("commit staged unrelated working changes")
	}
}
func TestParseStatusPreservesNewlinePath(t *testing.T) {
	st, e := parseStatus("? a\n b.txt\x00")
	if e != nil {
		t.Fatal(e)
	}
	if st.Untracked != 1 || len(st.Files) != 1 || st.Files[0].Path != "a\n b.txt" {
		t.Fatalf("%+v", st)
	}
}

func TestWorktreesAndConflictRecovery(t *testing.T) {
	s, dir, id := setup(t)
	ctx := context.Background()
	wt, e := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "feature/test", Base: "main"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "existing", Branch: "feature/test"}); !errors.Is(e, domain.ErrBusy) {
		t.Fatal(e)
	}
	_, path, _ := s.target(ctx, wt.ID)
	os.WriteFile(filepath.Join(path, "file.txt"), []byte("feature\n"), 0600)
	if e = s.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: wt.ID}); !errors.Is(e, domain.ErrDirty) {
		t.Fatal(e)
	}
	git(t, path, "add", ".")
	git(t, path, "commit", "-m", "feature")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0600)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "main")
	op, e := s.Merge(ctx, wt.ID, "main")
	if e != nil || op.State != "conflict" || !op.CanAbort || len(op.ConflictedPaths) != 1 {
		t.Fatalf("%+v %v", op, e)
	}
	if _, e = s.Abort(ctx, id, op.ID); e == nil {
		t.Fatal("cross-worktree recovery")
	}
	op, e = s.Abort(ctx, wt.ID, op.ID)
	if e != nil || op.State != "cancelled" {
		t.Fatal(op, e)
	}
	if e = s.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: wt.ID}); e != nil {
		t.Fatal(e)
	}
}
func TestRemoteRefsStayLocal(t *testing.T) {
	s, dir, _ := setup(t)
	bare := t.TempDir()
	git(t, bare, "init", "--bare")
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-u", "origin", "main")
	wt, e := s.CreateWorktree(context.Background(), domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "remote", Branch: "tracking", Base: "origin/main"})
	if e != nil {
		t.Fatal(e)
	}
	if wt.Status.Upstream != "origin/main" || wt.Status.LocalRemoteRefSHA == "" {
		t.Fatal(wt)
	}
	branches, e := s.ListBranches(context.Background(), "repo")
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range branches {
		if b.RemoteHeadSHA != "" {
			t.Fatal("invented GitHub state")
		}
	}
}

func TestDynamicWorktreePlacementAndSymlinkContainment(t *testing.T) {
	_, repo, _ := setup(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := config.UpdateProjectRoots(path, "add", 0, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New([]Config{{ID: "local", Root: repo, WithWorktreeRoot: func(ctx context.Context, create func(string) error) error {
		return config.WithBrowserWorktreeRoot(ctx, path, repo, create)
	}}})
	if err != nil {
		t.Fatal(err)
	}
	create := func(branch string) error {
		_, err := svc.CreateWorktree(context.Background(), domain.CreateWorktreeRequest{RepositoryID: "local", Mode: "new", Branch: branch, Base: "main"})
		return err
	}
	if err := create("one"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".bonsai.yaml"), []byte("worktree:\n  root: "+filepath.ToSlash(outside)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := create("outside"); err == nil {
		t.Fatal("allowed unconfigured destination")
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err == nil {
		if err := os.WriteFile(filepath.Join(repo, ".bonsai.yaml"), []byte("worktree:\n  root: escape/new-child\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := create("symlink"); err == nil {
			t.Fatal("allowed symlink escape")
		}
		if _, err := os.Stat(filepath.Join(outside, "new-child")); !os.IsNotExist(err) {
			t.Fatal("created escaped directory", err)
		}
	}
	if _, err := config.UpdateProjectRoots(path, "remove", cfg.Revision, "", cfg.Roots[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := create("removed"); err == nil {
		t.Fatal("used stale settings")
	}
	trees, err := svc.ListWorktrees(context.Background(), "local")
	if err != nil || len(trees) != 2 {
		t.Fatal(trees, err)
	}
}

func TestStatusHeadStatesAndDivergenceAvailability(t *testing.T) {
	s, dir, id := setup(t)
	ctx := context.Background()

	git(t, dir, "checkout", "--detach")
	detached, err := s.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detached.HeadState != "detached" || detached.HeadSHA == "" {
		t.Fatalf("detached status = %+v", detached)
	}

	git(t, dir, "checkout", "main")
	bare := filepath.Join(t.TempDir(), "remote.git")
	git(t, filepath.Dir(bare), "init", "--bare", filepath.Base(bare))
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-u", "origin", "main")
	if err := os.WriteFile(filepath.Join(dir, "ahead.txt"), []byte("ahead\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "ahead.txt")
	git(t, dir, "commit", "-m", "ahead")
	ahead, err := s.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !ahead.DivergenceAvailable || ahead.Ahead != 1 || ahead.Behind != 0 || ahead.LocalRemoteRefSHA == "" {
		t.Fatalf("ahead status = %+v", ahead)
	}

	git(t, dir, "update-ref", "-d", "refs/remotes/origin/main")
	gone, err := s.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Upstream != "origin/main" || gone.DivergenceAvailable || gone.LocalRemoteRefSHA != "" || gone.Ahead != 0 || gone.Behind != 0 {
		t.Fatalf("gone-upstream status = %+v", gone)
	}
}

func TestStatusUnbornHead(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "newborn")
	s, err := New([]Config{{ID: "repo", Root: dir, WorktreeRoot: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	trees, err := s.ListWorktrees(context.Background(), "repo")
	if err != nil || len(trees) != 1 {
		t.Fatal(trees, err)
	}
	status, err := s.Status(context.Background(), trees[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.HeadState != "unborn" || status.HeadSHA != "" || status.Branch != "newborn" {
		t.Fatalf("unborn status = %+v", status)
	}
}

func TestRepositoryKeepsSiblingWorktreesWhenOneStatusFails(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	worktree, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "feature/missing", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, missingPath, err := s.target(ctx, worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(missingPath); err != nil {
		t.Fatal(err)
	}
	state, err := s.Repository(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v", state.Worktrees)
	}
	var failed, healthy int
	for _, tree := range state.Worktrees {
		if tree.ID == worktree.ID {
			if tree.StatusError == nil || tree.Status != nil {
				t.Fatalf("missing worktree status = %+v", tree)
			}
			failed++
		} else if tree.Status != nil {
			healthy++
		}
	}
	if failed != 1 || healthy != 1 {
		t.Fatalf("partial repository state = %+v", state.Worktrees)
	}
}

func TestSanitizeRemoteIdentityStripsCredentialsAndUnsupportedHosts(t *testing.T) {
	https := sanitizeRemoteIdentity("origin", "https://token:secret@github.com/acme/widgets.git")
	if https.Host != "github.com" || https.FullName != "acme/widgets" || https.Owner != "acme" || https.Repository != "widgets" {
		t.Fatalf("https identity = %+v", https)
	}
	ssh := sanitizeRemoteIdentity("fork", "git@github.com:contributor/widgets.git")
	if ssh.FullName != "contributor/widgets" || ssh.Host != "github.com" {
		t.Fatalf("ssh identity = %+v", ssh)
	}
	unsupported := sanitizeRemoteIdentity("origin", "https://user:secret@gitlab.example/acme/widgets.git")
	if unsupported.Host != "gitlab.example" || unsupported.FullName != "" {
		t.Fatalf("unsupported identity = %+v", unsupported)
	}
}
