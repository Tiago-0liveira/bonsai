package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@t.t")
	runGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestListWorktrees(t *testing.T) {
	main := gitInit(t)
	wtPath := filepath.Join(t.TempDir(), "feature")
	runGit(t, main, "worktree", "add", "-b", "feature", wtPath)

	trees, err := ListWorktrees(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %+v", len(trees), trees)
	}
	if !trees[0].IsMain {
		t.Errorf("first worktree should be main")
	}
	if trees[0].Branch != "main" {
		t.Errorf("main branch = %q, want main", trees[0].Branch)
	}
	var found bool
	for _, w := range trees {
		if w.Branch == "feature" {
			found = true
		}
	}
	if !found {
		t.Errorf("feature worktree not parsed: %+v", trees)
	}
}

func TestRepoRoot(t *testing.T) {
	main := gitInit(t)
	root, err := RepoRoot(main)
	if err != nil {
		t.Fatal(err)
	}
	// macOS temp dirs symlink through /private; compare resolved paths.
	got, _ := filepath.EvalSymlinks(root)
	want, _ := filepath.EvalSymlinks(main)
	if got != want {
		t.Errorf("RepoRoot = %q, want %q", got, want)
	}
}

func TestRemoveWorktreeClean(t *testing.T) {
	main := gitInit(t)
	wtPath := filepath.Join(t.TempDir(), "feature")
	runGit(t, main, "worktree", "add", "-b", "feature", wtPath)

	if err := RemoveWorktree(main, wtPath); err != nil {
		t.Fatalf("clean removal failed: %v", err)
	}
	trees, err := ListWorktrees(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 1 {
		t.Errorf("worktree still listed after removal: %+v", trees)
	}
}

func TestRemoveWorktreeDirty(t *testing.T) {
	main := gitInit(t)
	wtPath := filepath.Join(t.TempDir(), "feature")
	runGit(t, main, "worktree", "add", "-b", "feature", wtPath)
	if err := os.WriteFile(filepath.Join(wtPath, "f.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RemoveWorktree(main, wtPath)
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("dirty removal: err = %v, want ErrWorktreeDirty", err)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Errorf("worktree should still exist after refused removal: %v", statErr)
	}

	if err := ForceRemoveWorktree(main, wtPath); err != nil {
		t.Fatalf("force removal of dirty worktree failed: %v", err)
	}
	trees, listErr := ListWorktrees(main)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(trees) != 1 {
		t.Errorf("worktree still listed after force removal: %+v", trees)
	}
}

func TestCommonDirAndGitDir(t *testing.T) {
	main := gitInit(t)
	wtPath := filepath.Join(t.TempDir(), "feature")
	runGit(t, main, "worktree", "add", "-b", "feature", wtPath)

	mainCommon, err := CommonDir(main)
	if err != nil {
		t.Fatalf("CommonDir(main): %v", err)
	}
	wtCommon, err := CommonDir(wtPath)
	if err != nil {
		t.Fatalf("CommonDir(wtPath): %v", err)
	}
	if mainCommon != wtCommon {
		t.Errorf("expected common dir to match: main=%s wt=%s", mainCommon, wtCommon)
	}

	mainGitDir, err := GitDir(main)
	if err != nil {
		t.Fatalf("GitDir(main): %v", err)
	}
	wtGitDir, err := GitDir(wtPath)
	if err != nil {
		t.Fatalf("GitDir(wtPath): %v", err)
	}
	if mainGitDir == wtGitDir {
		t.Errorf("expected distinct git dirs for main and worktree: %s", mainGitDir)
	}
	if !filepath.IsAbs(mainCommon) || !filepath.IsAbs(wtGitDir) {
		t.Errorf("expected absolute paths: common=%s gitDir=%s", mainCommon, wtGitDir)
	}
}

func TestCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	can := CanonicalPath(dir)
	if !filepath.IsAbs(can) {
		t.Errorf("expected absolute canonical path: %s", can)
	}
}
