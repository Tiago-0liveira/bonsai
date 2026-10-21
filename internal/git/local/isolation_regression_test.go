package local

import (
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewRegressionCleanupRejectsChangedHooks(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	if err := os.WriteFile(filepath.Join(r.root, ".bonsai.yaml"), []byte("hooks:\n  on_worktree_delete:\n    - newly-unconfirmed-hook\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: c})
	if err == nil || r.hookCalls != 0 {
		t.Fatalf("changed cleanup config: err=%v hook calls=%d", err, r.hookCalls)
	}
}
func TestReviewRegressionRejectsAdditionalPushURL(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	git(t, r.root, "remote", "set-url", "--add", "--push", "origin", "https://github.com/owner/repo.git")
	git(t, r.root, "remote", "set-url", "--add", "--push", "origin", "https://github.com/foreign/repo.git")
	p, err := r.service.PreviewIsolation(context.Background(), r.request)
	if err == nil {
		t.Fatalf("accepted foreign second push URL: proposal=%+v", p)
	}
}
func TestReviewRegressionCleanupPreservesAssumeUnchanged(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	git(t, c.Proposal.Path, "update-index", "--assume-unchanged", "file.txt")
	if err := os.WriteFile(filepath.Join(c.Proposal.Path, "file.txt"), []byte("user edits hidden from status"), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := r.service.InspectIsolation(context.Background(), c.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	err = r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: current})
	if _, e := os.Stat(filepath.Join(c.Proposal.Path, "file.txt")); os.IsNotExist(e) {
		t.Fatalf("erased user edits: inspect Clean=%v removal err=%v", current.Clean, err)
	}
}

func TestFinalRegressionRevokedRootCleanup(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	path, err := config.ProjectRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	roots, err := config.ReadProjectRoots(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.UpdateProjectRoots(path, "revoke-root", roots.Revision, "", roots.Roots[0].ID); err != nil {
		t.Fatal(err)
	}
	check, err := r.service.InspectIsolation(context.Background(), c.Proposal)
	if err != nil {
		return
	}
	if err = r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: check}); err == nil {
		t.Fatal("cleanup removed worktree after its authorized project root was revoked")
	}
}
func TestFinalRegressionSameRepositoryMultipleURLs(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	git(t, r.root, "remote", "set-url", "--add", "--push", "origin", "https://github.com/owner/repo.git")
	git(t, r.root, "remote", "set-url", "--add", "--push", "origin", "git@github.com:owner/repo.git")
	if _, err := r.service.PreviewIsolation(context.Background(), r.request); err != nil {
		t.Fatal(err)
	}
}
func TestFinalRegressionNormalIndexClean(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	if !c.Clean {
		t.Fatal("normal freshly created worktree considered dirty")
	}
}
func TestFinalRegressionChangedDeleteHooksAtFinalGuard(t *testing.T) {
	r := newFakeLaunchIsolationRig(t)
	c := r.create(t)
	repo, err := r.service.repo("local")
	if err != nil {
		t.Fatal(err)
	}
	repo.BeforeRemove = func(context.Context, string) error {
		return os.WriteFile(filepath.Join(r.root, ".bonsai.yaml"), []byte("hooks:\n  on_worktree_delete:\n    - new-unconfirmed-delete-hook\n"), 0600)
	}
	err = r.service.RemoveIsolation(context.Background(), domain.IsolationRemoveRequest{Check: c})
	if r.hookCalls > 0 {
		t.Fatalf("ran changed unconfirmed delete hook after final fingerprint check: calls=%d err=%v", r.hookCalls, err)
	}
}
