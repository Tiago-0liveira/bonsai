package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadForWorktreeConfigWins(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "repo")
	wt := filepath.Join(base, "repo-wt")

	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(main, "init", "-b", "main")
	runGit(main, "config", "user.email", "t@t.t")
	runGit(main, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(main, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(main, "add", ".")
	runGit(main, "commit", "-m", "init")
	runGit(main, "worktree", "add", wt, "-b", "wt")

	if err := os.WriteFile(filepath.Join(main, ".bonsai.yaml"), []byte("upstream: origin/from-main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".bonsai.yaml"), []byte("upstream: origin/from-worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(wt)
	cfg, err := LoadFor(main)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != "origin/from-worktree" {
		t.Fatalf("LoadFor from worktree: upstream = %q, want origin/from-worktree", cfg.Upstream)
	}

	if err := os.Remove(filepath.Join(wt, ".bonsai.yaml")); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadFor(main)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != "origin/from-main" {
		t.Fatalf("LoadFor fallback: upstream = %q, want origin/from-main", cfg.Upstream)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, []byte("upstream: origin/dev\ntheme:\n  preset: nord\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream != "origin/dev" || cfg.Theme.Preset != "nord" {
		t.Fatalf("LoadFile = upstream %q, preset %q", cfg.Upstream, cfg.Theme.Preset)
	}

	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("LoadFile on missing file: expected error, got nil")
	}
}

func TestWorktreePathDefault(t *testing.T) {
	c := &Config{}
	repo := "/home/u/projects/bonsai"
	got := c.WorktreePath(repo, "feature/login")
	want := filepath.Join("/home/u/projects", "bonsai-feature-login")
	if got != want {
		t.Fatalf("WorktreePath = %q, want %q", got, want)
	}
}

func TestWorktreePathTemplateAndRoot(t *testing.T) {
	c := &Config{Worktree: Worktree{Root: "/tmp/wt", PathTemplate: "{repo}/{branch}"}}
	got := c.WorktreePath("/home/u/bonsai", "feat/x")
	want := filepath.Join("/tmp/wt", "bonsai", "feat-x")
	if got != want {
		t.Fatalf("WorktreePath = %q, want %q", got, want)
	}
}

func TestWorktreePathRelativeRoot(t *testing.T) {
	c := &Config{Worktree: Worktree{Root: "../trees"}}
	got := c.WorktreePath("/home/u/bonsai", "main")
	want := filepath.Join("/home/u/bonsai", "../trees", "bonsai-main")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("WorktreePath = %q, want %q", got, want)
	}
}

func TestRemoteOf(t *testing.T) {
	cases := map[string]string{
		"origin/main":      "origin",
		"upstream/develop": "upstream",
		"origin/feature/x": "origin",
		"main":             "origin",
		"":                 "origin",
	}
	for in, want := range cases {
		if got := RemoteOf(in); got != want {
			t.Fatalf("RemoteOf(%q) = %q, want %q", in, got, want)
		}
	}
}
