package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

func TestOnBranchesOpensModal(t *testing.T) {
	cases := []struct {
		kind branchesKind
		want modals.Kind
	}{
		{branchesForRebase, modals.KindBranches},
		{branchesForCreateExisting, modals.KindCreateExisting},
	}
	for _, c := range cases {
		m := renderModel()
		nm, _ := m.Update(branchesMsg{kind: c.kind, branches: []string{"main", "feat"}})
		model := nm.(Model)
		if model.modal == nil || model.modal.Kind() != c.want {
			t.Errorf("kind %v: want modal kind %v", c.kind, c.want)
		}
	}
}

func TestOnBranchesError(t *testing.T) {
	m := renderModel()
	nm, _ := m.Update(branchesMsg{err: errors.New("boom")})
	model := nm.(Model)
	if model.modal != nil {
		t.Error("error path should not open a modal")
	}
	if model.status == "" {
		t.Error("error path should set the status bar")
	}
}

func TestMergedTargetsFrom(t *testing.T) {
	m := testModel()
	m.cfg = &config.Config{Upstream: "origin/main"}
	m.statuses = map[string]git.StatusSummary{"/wt/dirty": {Modified: 1}}
	m.prByBranch = map[string]gh.PR{"feat-a": {Number: 5}}
	m.worktrees = []git.Worktree{
		{Path: "/wt/main", Branch: "main", IsMain: true},
		{Path: "/wt/detached", Branch: "(detached)"},
		{Path: "/wt/dirty", Branch: "feat-dirty"},
		{Path: "/wt/unmerged", Branch: "feat-unmerged"},
		{Path: "/wt/a", Branch: "feat-a"},
	}

	targets := m.mergedTargetsFrom(map[string]string{"feat-dirty": "d1", "feat-a": "a1"})
	if len(targets) != 1 {
		t.Fatalf("want 1 target (main/detached/dirty/unmerged excluded), got %d: %+v", len(targets), targets)
	}
	got := targets[0]
	if got.path != "/wt/a" || got.branch != "feat-a" || got.upstream != "origin/main" || got.prNumber != 5 {
		t.Errorf("target fields wrong: %+v", got)
	}
}

func TestOnPruneCandidates(t *testing.T) {
	newModel := func(confirm bool) Model {
		m := renderModel()
		m.cfg = &config.Config{Upstream: "origin/main", ConfirmDestructive: confirm}
		m.worktrees = []git.Worktree{{Path: "/wt/a", Branch: "feat-a"}}
		return m
	}

	// Error from the scan: status note, no modal.
	nm, _ := newModel(true).Update(pruneCandidatesMsg{err: errors.New("boom")})
	model := nm.(Model)
	if model.modal != nil || model.status == "" {
		t.Error("scan error should set status and open no modal")
	}

	// No merged candidates: status note, no modal.
	nm, _ = newModel(true).Update(pruneCandidatesMsg{merged: map[string]string{}})
	model = nm.(Model)
	if model.modal != nil || model.status == "" {
		t.Error("empty candidates should set status and open no modal")
	}

	// Targets with confirmation enabled: confirm modal + pending action.
	nm, _ = newModel(true).Update(pruneCandidatesMsg{merged: map[string]string{"feat-a": "a1"}})
	model = nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfirm {
		t.Error("targets + ConfirmDestructive should open the confirm modal")
	}
	if model.pendingConfirm == nil {
		t.Error("targets + ConfirmDestructive should set pendingConfirm")
	}

	// Targets with confirmation disabled: action returned directly.
	nm, cmd := newModel(false).Update(pruneCandidatesMsg{merged: map[string]string{"feat-a": "a1"}})
	model = nm.(Model)
	if model.modal != nil {
		t.Error("ConfirmDestructive=false should not open a modal")
	}
	if cmd == nil {
		t.Error("ConfirmDestructive=false should return the prune command")
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestKindPruneForceThreadsThrough(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, main, "init", "-q", "-b", "main")
	runGitCmd(t, main, "config", "user.email", "t@t.t")
	runGitCmd(t, main, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, main, "add", "-A")
	runGitCmd(t, main, "commit", "-qm", "init")
	wtPath := filepath.Join(base, "repo-feat")
	runGitCmd(t, main, "worktree", "add", "-b", "feat", wtPath)
	if err := os.WriteFile(filepath.Join(wtPath, "f.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Upstream: "origin/main"}
	m := New(main, cfg, &config.State{})
	m.width, m.height = 100, 40
	m.worktrees = []git.Worktree{{Path: wtPath, Branch: "feat"}}
	m.rebuildItems()

	// Without force the dirty worktree survives with a clear error.
	_, cmd := m.Update(modals.SubmitMsg{Kind: modals.KindPrune, Value: "confirm"})
	if cmd == nil {
		t.Fatal("no command returned for KindPrune")
	}
	done, ok := cmd().(opDoneMsg)
	if !ok {
		t.Fatalf("prune command returned %T, want opDoneMsg", done)
	}
	if !errors.Is(done.err, git.ErrWorktreeDirty) {
		t.Fatalf("confirm prune of dirty worktree: err = %v, want ErrWorktreeDirty", done.err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("dirty worktree destroyed without force: %v", err)
	}

	// With force it is removed.
	_, cmd = m.Update(modals.SubmitMsg{Kind: modals.KindPrune, Value: "force"})
	done, ok = cmd().(opDoneMsg)
	if !ok {
		t.Fatalf("prune command returned %T, want opDoneMsg", done)
	}
	if done.err != nil {
		t.Fatalf("force prune failed: %v", done.err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree should be gone after force prune, stat err = %v", err)
	}
}

// pruneRepo builds main plus a "feat" worktree whose commit is merged into main.
func pruneRepo(t *testing.T) (main, wt string) {
	t.Helper()
	base := t.TempDir()
	main = filepath.Join(base, "repo")
	wt = filepath.Join(base, "wt")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, main, "init", "-q", "-b", "main")
	runGitCmd(t, main, "config", "user.email", "t@t.t")
	runGitCmd(t, main, "config", "user.name", "t")
	runGitCmd(t, main, "commit", "--allow-empty", "-qm", "base")
	runGitCmd(t, main, "worktree", "add", "-q", "-b", "feat", wt)
	runGitCmd(t, wt, "commit", "--allow-empty", "-qm", "merged work")
	runGitCmd(t, main, "merge", "-q", "--ff-only", "feat")
	return main, wt
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestVerifyMergedBindsBranchNameToCommit(t *testing.T) {
	main, wt := pruneRepo(t)
	tip := gitOut(t, main, "rev-parse", "feat")
	names := map[string]bool{"feat": true, "missing": true}

	if got := verifyMerged(main, "main", names, nil); got["feat"] != tip || len(got) != 1 {
		t.Fatalf("merged tip not verified: %v", got)
	}

	// A historical merged PR with this head name must not vouch for new work.
	runGitCmd(t, wt, "commit", "--allow-empty", "-qm", "new unmerged work")
	if got := verifyMerged(main, "main", names, map[string]string{"feat": tip}); len(got) != 0 {
		t.Fatalf("branch with new commits accepted: %v", got)
	}

	// Squash merge: the PR head commit is not an ancestor of base but is the tip.
	newTip := gitOut(t, main, "rev-parse", "feat")
	if got := verifyMerged(main, "main", names, map[string]string{"feat": newTip}); got["feat"] != newTip {
		t.Fatalf("squash-merged tip rejected: %v", got)
	}
}

func TestBulkPruneKeepsBranchWhoseTipMoved(t *testing.T) {
	main, wt := pruneRepo(t)
	verified := gitOut(t, main, "rev-parse", "feat")
	runGitCmd(t, wt, "commit", "--allow-empty", "-qm", "new unmerged work")

	targets := []pruneTarget{{path: wt, branch: "feat", upstream: "main", sha: verified}}
	done := bulkPrune(main, targets, nil)().(opDoneMsg)
	if done.err == nil || !strings.Contains(done.err.Error(), "kept feat") {
		t.Fatalf("want a kept-branch error, got %v", done.err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed although its tip moved: %v", err)
	}
	runGitCmd(t, main, "show-ref", "--verify", "-q", "refs/heads/feat")
}

func TestBulkPruneRemovesVerifiedBranch(t *testing.T) {
	main, wt := pruneRepo(t)
	verified := gitOut(t, main, "rev-parse", "feat")
	done := bulkPrune(main, []pruneTarget{{path: wt, branch: "feat", upstream: "main", sha: verified}}, nil)().(opDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if err := exec.Command("git", "-C", main, "show-ref", "--verify", "-q", "refs/heads/feat").Run(); err == nil {
		t.Error("verified merged branch was not deleted")
	}
}
