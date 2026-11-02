package ui

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"strings"
	"testing"
)

func TestAttentionSortAndPruneGuard(t *testing.T) {
	e := newAgentsEnv(t)
	e.m.agents.sessions = []agentsapi.Session{{Summary: agentterminal.Summary{ID: "live", Name: "working-session", State: "running"}}, {Summary: agentterminal.Summary{ID: "failed", Name: "failed-session", State: "failed"}}}
	e.m.agents.byTree = map[string][]int{e.wtA: {0}, e.wtB: {1}}
	e.m.sort = sortDirty
	e.m.cycleWorktreeSort()
	if e.m.sort != sortAttention {
		t.Fatal(e.m.sort)
	}
	got := e.m.sortedWorktrees()
	if got[1].Path != e.wtB {
		t.Fatalf("attention order %+v", got)
	}
	targets := e.m.mergedTargetsFrom(map[string]string{"feat-a": "a", "feat-b": "b"})
	if len(targets) != 1 || targets[0].path != e.wtB {
		t.Fatalf("targets %+v", targets)
	}
	e.m.list.SelectByPath(e.wtA)
	nm, _ := e.m.openPruneModal()
	m := nm.(Model)
	if m.modal == nil || !strings.Contains(m.modal.View(), "working-session") {
		t.Fatal("missing named warning")
	}
	e.m.sort = sortAttention
	e.m.cycleWorktreeSort()
	if e.m.sort != sortName {
		t.Fatal(e.m.sort)
	}
	e.m.agents.api.Sessions = nil
	e.m.sort = sortDirty
	e.m.cycleWorktreeSort()
	if e.m.sort != sortName {
		t.Fatal("disabled agents offered attention")
	}
}

func TestCreateSelectsNewWorktreeBeforeAgentLaunch(t *testing.T) {
	e := newAgentsEnv(t)
	e.m.createStartAgent = true
	path := e.repo + "-new"
	nm, _ := e.m.onOpDone(opDoneMsg{label: "create new", createdPath: path, startAgent: true})
	m := nm.(Model)
	trees := append(append([]git.Worktree(nil), m.worktrees...), git.Worktree{Path: path, Branch: "new"})
	nm, _ = m.onWorktrees(worktreesMsg{trees: trees})
	m = nm.(Model)
	wt, ok := m.selectedWorktree()
	if !ok || wt.Path != path || m.agents.overlay == nil || m.agents.overlay.launch == nil {
		t.Fatal("new target launch form missing")
	}
	if m.createdAgentPath != "" {
		t.Fatal("pending launch not consumed")
	}
}

func TestCreateUsesSubmittedAgentChoice(t *testing.T) {
	for _, start := range []bool{false, true} {
		e := newAgentsEnv(t)
		e.m.createStartAgent = !start
		nm, _ := e.m.onOpDone(opDoneMsg{createdPath: e.wtA, startAgent: start})
		if got := nm.(Model).createdAgentPath != ""; got != start {
			t.Fatalf("submitted %v got %v", start, got)
		}
	}
}
