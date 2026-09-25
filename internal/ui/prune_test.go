package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	targets := m.mergedTargetsFrom(map[string]bool{"feat-dirty": true, "feat-a": true})
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
	nm, _ = newModel(true).Update(pruneCandidatesMsg{merged: map[string]bool{}})
	model = nm.(Model)
	if model.modal != nil || model.status == "" {
		t.Error("empty candidates should set status and open no modal")
	}

	// Targets with confirmation enabled: confirm modal + pending action.
	nm, _ = newModel(true).Update(pruneCandidatesMsg{merged: map[string]bool{"feat-a": true}})
	model = nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfirm {
		t.Error("targets + ConfirmDestructive should open the confirm modal")
	}
	if model.pendingConfirm == nil {
		t.Error("targets + ConfirmDestructive should set pendingConfirm")
	}

	// Targets with confirmation disabled: action returned directly.
	nm, cmd := newModel(false).Update(pruneCandidatesMsg{merged: map[string]bool{"feat-a": true}})
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
	// The prune safety probe needs a healthy AGYM response before Git removes a
	// worktree. Serve an empty run list so this test reaches the Git force path.
	binDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		bat := "@echo {\"protocol\":{\"major\":1,\"minor\":0},\"ok\":true,\"data\":[]}\r\n"
		if err := os.WriteFile(filepath.Join(binDir, "agym.bat"), []byte(bat), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(binDir, "agym"), []byte("#!/bin/sh\necho '{\"protocol\":{\"major\":1,\"minor\":0},\"ok\":true,\"data\":[]}'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
