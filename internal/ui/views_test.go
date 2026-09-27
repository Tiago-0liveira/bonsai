package ui

import (
	"testing"

	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

func TestViewRegistryRoundTripsLegacyTabs(t *testing.T) {
	for _, tab := range []rightTab{tabLog, tabProcs, tabDiff, tabPR, tabInspect, tabChecks} {
		id := viewForTab(tab)
		got, ok := tabForView(id)
		if !ok || got != tab {
			t.Fatalf("tab %v -> view %q -> tab %v,%v", tab, id, got, ok)
		}
	}
	if _, ok := tabForView(viewWorktrees); ok {
		t.Fatal("worktrees must not map to a legacy workspace tab")
	}
}

func TestTabCyclesPanesInTreeOrder(t *testing.T) {
	m := renderModel()
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{}
	m.initPaneRuntime()

	want := []paneID{"pane-worktrees", "pane-log", "pane-processes", "pane-worktrees"}
	if m.focusedPane != want[0] {
		t.Fatalf("initial pane = %q, want %q", m.focusedPane, want[0])
	}
	for i := 1; i < len(want); i++ {
		m.toggleFocus()
		if m.focusedPane != want[i] {
			t.Fatalf("cycle %d pane = %q, want %q", i, m.focusedPane, want[i])
		}
	}
}

func TestShiftTabCyclesViewsOnlyInsideFocusedPane(t *testing.T) {
	m := renderModel()
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{
		"pane-worktrees": viewWorktrees,
		"pane-log":       viewLog,
		"pane-processes": viewProcesses,
	}
	m.initPaneRuntime()
	m.focusPane("pane-log")

	if !m.cycleFocusedPaneView() {
		t.Fatal("expected pane-log to have another view")
	}
	if got := m.activeView("pane-log"); got != viewInspect {
		t.Fatalf("pane-log active = %q, want inspect", got)
	}
	if got := m.activeView("pane-processes"); got != viewProcesses {
		t.Fatalf("cycling pane-log changed pane-processes to %q", got)
	}
	if m.focusedPane != "pane-log" {
		t.Fatalf("cycling views changed focused pane to %q", m.focusedPane)
	}
}

func TestActivateViewFocusesOwningPane(t *testing.T) {
	m := renderModel()
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{}
	m.initPaneRuntime()

	if !m.activateView(viewChecks) {
		t.Fatal("checks should be activatable for branch selection")
	}
	if m.focusedPane != "pane-processes" {
		t.Fatalf("focused pane = %q, want pane-processes", m.focusedPane)
	}
	if got := m.activeView("pane-processes"); got != viewChecks {
		t.Fatalf("active view = %q, want checks", got)
	}
}

func TestUnavailableActiveViewFallsForwardWithoutChangingLayout(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{
		WT: git.Worktree{Path: "/w/main", Branch: "main", IsMain: true},
	}})
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{
		"pane-worktrees": viewWorktrees,
		"pane-log":       viewDiff,
		"pane-processes": viewPR,
	}
	m.initPaneRuntime()
	before := m.dynamicLayout.persisted()

	m.normalizePaneActives()

	if got := m.activeView("pane-log"); got != viewLog {
		t.Fatalf("unavailable Diff fell to %q, want log", got)
	}
	if got := m.activeView("pane-processes"); got != viewProcesses {
		t.Fatalf("unavailable PR fell to %q, want processes", got)
	}
	after := m.dynamicLayout.persisted()
	if !persistedTreeEqual(before.Root, after.Root) {
		t.Fatal("runtime availability fallback mutated persisted layout membership")
	}
}

func persistedTreeEqual(a, b interface{ GetType() string }) bool {
	return false
}
