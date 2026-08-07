package ui

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

func testModel() Model {
	return Model{
		worktrees:  nil,
		metrics:    map[string]git.Metrics{},
		prByBranch: map[string]gh.PR{},
		lastCommit: map[string]int64{},
	}
}

func TestSortedWorktreesMainFirst(t *testing.T) {
	m := testModel()
	m.worktrees = []git.Worktree{
		{Path: "/a", Branch: "aaa"},
		{Path: "/main", Branch: "main", IsMain: true},
		{Path: "/z", Branch: "zzz"},
	}
	m.sort = sortName
	got := m.sortedWorktrees()
	if !got[0].IsMain {
		t.Fatalf("main should be first, got %q", got[0].Branch)
	}
	if got[1].Branch != "aaa" || got[2].Branch != "zzz" {
		t.Errorf("name sort wrong: %q %q", got[1].Branch, got[2].Branch)
	}
}

func TestSortedWorktreesByDirty(t *testing.T) {
	m := testModel()
	m.statuses = map[string]git.StatusSummary{}
	m.worktrees = []git.Worktree{
		{Path: "/main", Branch: "main", IsMain: true},
		{Path: "/clean", Branch: "clean"},
		{Path: "/messy", Branch: "messy"},
	}
	m.statuses["/messy"] = git.StatusSummary{Modified: 2, Untracked: 1}
	m.statuses["/clean"] = git.StatusSummary{}
	m.sort = sortDirty
	got := m.sortedWorktrees()
	if !got[0].IsMain {
		t.Fatalf("main first, got %q", got[0].Branch)
	}
	if got[1].Branch != "messy" || got[2].Branch != "clean" {
		t.Errorf("dirty sort wrong: %q %q", got[1].Branch, got[2].Branch)
	}
}

func TestSortedWorktreesByAhead(t *testing.T) {
	m := testModel()
	m.worktrees = []git.Worktree{
		{Path: "/main", Branch: "main", IsMain: true},
		{Path: "/low", Branch: "low"},
		{Path: "/high", Branch: "high"},
	}
	m.metrics["/low"] = git.Metrics{Ahead: 1}
	m.metrics["/high"] = git.Metrics{Ahead: 9}
	m.sort = sortAhead
	got := m.sortedWorktrees()
	if !got[0].IsMain {
		t.Fatalf("main first, got %q", got[0].Branch)
	}
	if got[1].Branch != "high" || got[2].Branch != "low" {
		t.Errorf("ahead sort wrong: %q %q", got[1].Branch, got[2].Branch)
	}
}
