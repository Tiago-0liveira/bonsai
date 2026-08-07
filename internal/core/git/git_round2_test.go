package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirty(t *testing.T) {
	dir := gitInit(t)
	if d, err := Dirty(dir); err != nil || d {
		t.Fatalf("clean repo: Dirty = %v, err %v", d, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d, err := Dirty(dir); err != nil || !d {
		t.Fatalf("after write: Dirty = %v, err %v", d, err)
	}
}

func TestLastCommitUnix(t *testing.T) {
	dir := gitInit(t)
	u, err := LastCommitUnix(dir)
	if err != nil {
		t.Fatal(err)
	}
	if u <= 0 {
		t.Errorf("LastCommitUnix = %d, want > 0", u)
	}
}

func TestMergedBranches(t *testing.T) {
	dir := gitInit(t)
	// Create and merge a branch into main.
	runGit(t, dir, "checkout", "-b", "done")
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "done work")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "merge", "done")

	// An unmerged branch should be excluded.
	runGit(t, dir, "checkout", "-b", "wip")
	if err := os.WriteFile(filepath.Join(dir, "h.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "wip")
	runGit(t, dir, "checkout", "main")

	merged, err := MergedBranches(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	hasDone, hasWip := false, false
	for _, b := range merged {
		if b == "done" {
			hasDone = true
		}
		if b == "wip" {
			hasWip = true
		}
		if b == "main" {
			t.Errorf("base branch main should be excluded")
		}
	}
	if !hasDone {
		t.Errorf("expected 'done' in merged branches: %v", merged)
	}
	if hasWip {
		t.Errorf("unmerged 'wip' should not appear: %v", merged)
	}
}

func TestDiffBase(t *testing.T) {
	dir := gitInit(t)
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "change f")

	out, err := DiffBase(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" || out == "(no changes vs main)" {
		t.Errorf("expected a diff vs main, got %q", out)
	}
}
