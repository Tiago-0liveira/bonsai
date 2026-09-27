package local

import (
	"context"
	"errors"
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
	os.WriteFile(filepath.Join(dir, "a\n b.txt"), []byte("new\n"), 0600)
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
	if e = s.Stage(ctx, id, []string{"a\n b.txt"}, false); e != nil {
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
