package git

import (
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
