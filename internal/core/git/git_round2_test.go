package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusSummary(t *testing.T) {
	dir := gitInit(t)
	s, err := StatusSummaryOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Dirty() || s.Total() != 0 {
		t.Fatalf("clean repo: %+v", s)
	}

	// Untracked file.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Modified tracked file.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err = StatusSummaryOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Untracked != 1 || s.Modified != 1 || s.Staged != 0 {
		t.Errorf("want 1 untracked + 1 modified, got %+v", s)
	}

	// Stage both.
	runGit(t, dir, "add", "-A")
	s, err = StatusSummaryOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Staged != 2 || s.Modified != 0 || s.Untracked != 0 {
		t.Errorf("want 2 staged, got %+v", s)
	}
	if len(s.Lines) != 2 {
		t.Errorf("want 2 raw lines, got %v", s.Lines)
	}
}

func TestLastCommit(t *testing.T) {
	dir := gitInit(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "fix: subject with colon: and spaces")

	c, err := LastCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "fix: subject with colon: and spaces" {
		t.Errorf("subject = %q", c.Subject)
	}
	if c.Author != "t" {
		t.Errorf("author = %q, want t", c.Author)
	}
	if c.When.IsZero() || c.When.Unix() <= 0 {
		t.Errorf("When = %v, want a real time", c.When)
	}

	// A repo with no commits errors.
	empty := t.TempDir()
	runGit(t, empty, "init", "-b", "main")
	if _, err := LastCommit(empty); err == nil {
		t.Errorf("expected error on repo without commits")
	}
}

func TestStashCount(t *testing.T) {
	dir := gitInit(t)
	n, err := StashCount(dir)
	if err != nil || n != 0 {
		t.Fatalf("StashCount = %d, err %v; want 0", n, err)
	}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "stash", "push", "-m", "s")
	}
	n, err = StashCount(dir)
	if err != nil || n != 2 {
		t.Fatalf("StashCount = %d, err %v; want 2", n, err)
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
