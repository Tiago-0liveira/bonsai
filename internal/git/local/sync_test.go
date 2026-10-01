package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func withOrigin(t *testing.T) (*Service, string, string) {
	t.Helper()
	s, dir, _ := setup(t)
	bare := t.TempDir()
	git(t, bare, "init", "--bare")
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-u", "origin", "main")
	return s, dir, bare
}

func TestRepositorySyncNarrowFetchPruneAndFreshness(t *testing.T) {
	s, dir, bare := withOrigin(t)
	ctx := context.Background()
	git(t, dir, "branch", "available")
	git(t, dir, "push", "origin", "available")
	git(t, dir, "update-ref", "-d", "refs/remotes/origin/available")
	git(t, dir, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
	before := strings.TrimSpace(git(t, dir, "config", "remote.origin.fetch"))
	first, err := s.SyncRepository(ctx, "repo", domain.PullPolicy{FastForwardOnly: true})
	if err != nil || first.Fetch.State != "ready" || first.Pull.State != "ready" || first.Fetch.CompletedAt == nil {
		t.Fatal(first, err)
	}
	branches, err := s.ListBranches(ctx, "repo")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range branches {
		if b.Ref == "refs/remotes/origin/available" {
			found = b.LastCommitAt != nil && b.RemoteName == "origin"
		}
	}
	if !found {
		t.Fatalf("narrow refspec omitted available branch: %+v", branches)
	}
	if strings.TrimSpace(git(t, dir, "config", "remote.origin.fetch")) != before {
		t.Fatal("rewrote configuration")
	}
	git(t, bare, "update-ref", "-d", "refs/heads/available")
	second, err := s.SyncRepository(ctx, "repo", domain.PullPolicy{FastForwardOnly: true})
	if err != nil || second.Fetch.CompletedAt == nil || !second.Fetch.CompletedAt.After(*first.Fetch.CompletedAt) {
		t.Fatal(second, err)
	}
	if _, err := ref(ctx, dir, "refs/remotes/origin/available"); err == nil {
		t.Fatal("deleted remote branch was not pruned")
	}
}

func TestRepositorySyncGuardedPull(t *testing.T) {
	for _, reason := range []string{"dirty_worktree", "detached_head", "no_upstream", "upstream_missing", "git_operation_in_progress", "diverged", "fast_forward"} {
		t.Run(reason, func(t *testing.T) {
			s, dir, bare := withOrigin(t)
			before := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
			switch reason {
			case "dirty_worktree":
				if err := os.WriteFile(filepath.Join(dir, "dirty"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "detached_head":
				git(t, dir, "checkout", "--detach")
			case "no_upstream":
				git(t, dir, "branch", "--unset-upstream")
			case "upstream_missing":
				git(t, dir, "config", "branch.main.merge", "refs/heads/gone")
			case "git_operation_in_progress":
				if err := os.WriteFile(filepath.Join(dir, ".git", "MERGE_HEAD"), []byte(before+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "diverged", "fast_forward":
				other := t.TempDir()
				git(t, other, "clone", "--branch", "main", bare, ".")
				git(t, other, "config", "user.name", "Other")
				git(t, other, "config", "user.email", "other@example.com")
				git(t, other, "commit", "--allow-empty", "-m", "remote")
				git(t, other, "push", "origin", "main")
				if reason == "diverged" {
					git(t, dir, "commit", "--allow-empty", "-m", "local")
					before = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
				}
			}
			outcome, err := s.SyncRepository(context.Background(), "repo", domain.PullPolicy{FastForwardOnly: true})
			if err != nil || outcome.Fetch.State != "ready" {
				t.Fatal(outcome, err)
			}
			after := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
			if reason == "fast_forward" {
				if outcome.Pull.State != "ready" || before == after {
					t.Fatal(outcome, before, after)
				}
			} else if outcome.Pull.State != "skipped" || outcome.Pull.Reason != reason || before != after {
				t.Fatal(outcome, before, after)
			}
		})
	}
}

func TestSyncWithoutRemoteAndFetchFailure(t *testing.T) {
	s, dir, _ := setup(t)
	result, err := s.SyncRepository(context.Background(), "repo", domain.PullPolicy{FastForwardOnly: true})
	if err != nil || result.Fetch.Reason != "no_remote" {
		t.Fatal(result, err)
	}
	git(t, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	result, err = s.SyncRepository(context.Background(), "repo", domain.PullPolicy{FastForwardOnly: true})
	if err != nil || result.Fetch.State != "error" || result.Pull.Reason != "fetch_failed" {
		t.Fatal(result, err)
	}
	state, err := s.Repository(context.Background(), "repo")
	if err != nil || len(state.Worktrees) != 1 || len(state.Branches) != 1 {
		t.Fatal(state, err)
	}
}

func TestRemoteCreationAttachesExistingBranchAndRejectsCheckedOut(t *testing.T) {
	s, dir, _ := withOrigin(t)
	git(t, dir, "branch", "tracking", "main")
	request := domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "remote", Branch: "tracking", Base: "origin/main"}
	w, err := s.CreateWorktree(context.Background(), request)
	if err != nil || w.Branch != "tracking" {
		t.Fatal(w, err)
	}
	if _, err := s.CreateWorktree(context.Background(), request); domain.Code(err) != "busy" {
		t.Fatal(err)
	}
}
