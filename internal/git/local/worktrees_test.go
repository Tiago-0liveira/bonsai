package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func TestRemoveMissingWorktreeRegistration(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_discard", true: "with_discard"}[discard], func(t *testing.T) {
			s, root, mainID := setup(t)
			ctx := context.Background()
			parent := filepath.Join(t.TempDir(), "orch")
			missing := filepath.Join(parent, "00-contracts")
			git(t, root, "worktree", "add", "-b", "feature/orchestrate-v1", missing)
			sibling, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "sibling", Base: "main"})
			if err != nil {
				t.Fatal(err)
			}
			head := git(t, root, "rev-parse", "feature/orchestrate-v1")
			// Resolve the ID while the directory still exists, as a client
			// would before the worktree is removed outside Bonsai.
			registered, err := s.ListWorktrees(ctx, "repo")
			if err != nil {
				t.Fatal(err)
			}
			var id string
			for _, tree := range registered {
				if tree.Branch == "feature/orchestrate-v1" {
					id = tree.ID
				}
			}
			if id == "" {
				t.Fatal("missing registered worktree")
			}
			if err := os.RemoveAll(parent); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Status(ctx, id); err == nil {
				t.Fatal("status should still reject a missing directory")
			}
			if err := s.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: id, ConfirmDiscard: discard}); err != nil {
				t.Fatal(err)
			}
			trees, err := s.ListWorktrees(ctx, "repo")
			if err != nil || len(trees) != 2 {
				t.Fatal(trees, err)
			}
			for _, tree := range trees {
				if tree.ID != mainID && tree.ID != sibling.ID {
					t.Fatalf("unexpected registration: %+v", tree)
				}
			}
			if got := git(t, root, "rev-parse", "feature/orchestrate-v1"); got != head {
				t.Fatalf("branch changed: %s != %s", got, head)
			}
			if _, err := os.Stat(sibling.Path); err != nil {
				t.Fatal("sibling directory affected", err)
			}
		})
	}
}

func TestMissingWorktreeRemovalKeepsLocksAndProcessProtection(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	wt, err := s.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: "repo", Mode: "new", Branch: "locked", Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "worktree", "lock", "--reason", "external disk", wt.Path)
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	req := domain.RemoveWorktreeRequest{WorktreeID: wt.ID, ConfirmDiscard: true}
	if err := s.RemoveWorktree(ctx, req); err == nil {
		t.Fatal("removed a locked registration")
	}
	git(t, root, "worktree", "unlock", wt.Path)
	r, err := s.repo("repo")
	if err != nil {
		t.Fatal(err)
	}
	r.BeforeRemove = func(_ context.Context, path string) error {
		if path != wt.Path {
			t.Fatalf("unexpected process check path: %s", path)
		}
		return domain.E("processes_running", "Stop running processes before deleting this worktree")
	}
	if err := s.RemoveWorktree(ctx, req); domain.Code(err) != "processes_running" {
		t.Fatal(err)
	}
	trees, err := s.ListWorktrees(ctx, "repo")
	if err != nil || len(trees) != 2 || !trees[1].Missing {
		t.Fatal(trees, err)
	}
	if err := s.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: "unknown"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
