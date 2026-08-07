package config

import (
	"path/filepath"
	"testing"
)

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
