package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func renderModel() Model {
	m := testModel()
	m.width, m.height = 100, 40
	m.keys = newKeyMap(nil)
	m.diffFileContent = map[string]string{}
	m.dynamicLayout = defaultDynamicLayout()
	m.paneLayout = defaultLayoutSpec()
	m.paneActive = map[paneID]viewID{}
	m.viewTerms = newViewTerminals()
	m.procs = &procView{}
	m.initPaneRuntime()
	return m
}

func TestRenderPRDetail(t *testing.T) {
	m := renderModel()
	m.prExpandDesc = true
	m.prExpandCommits = true
	d := gh.PRDetail{
		Number: 12, Title: "Add login", State: "OPEN", IsDraft: true,
		Author: "alice", Base: "main", Head: "feat", Mergeable: "CONFLICTING",
		Labels: []string{"enh"}, Reviewers: []string{"carol"},
		Body: "line one\nline two", Additions: 40, Deletions: 3, ChangedFiles: 5,
		Commits:  []gh.Commit{{OID: "abcdef1234", Headline: "do it", Author: "alice"}},
		Reviews:  []gh.Review{{Author: "carol", State: "APPROVED", Body: "lgtm", SubmittedAt: "2026-01-03T00:00:00Z"}},
		Comments: []gh.TimelineItem{{Author: "dan", Body: "nit", CreatedAt: "2026-01-02T00:00:00Z"}},
	}
	out := m.renderPRDetail(d, []gh.Check{{Name: "build", Bucket: "pass"}})
	for _, want := range []string{"#12", "Add login", "OPEN", "conflicting", "Commits", "abcdef1", "Activity", "carol", "build"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderPRDetail missing %q\n%s", want, out)
		}
	}
}

func TestRenderPRDetailCollapsedDescription(t *testing.T) {
	m := renderModel()
	m.prExpandDesc = false
	d := gh.PRDetail{Number: 1, Title: "t", State: "OPEN", Body: "secret detail\nmore"}
	out := m.renderPRDetail(d, nil)
	if strings.Contains(out, "more") {
		t.Errorf("collapsed description should hide later lines:\n%s", out)
	}
	if !strings.Contains(out, "expand") {
		t.Errorf("collapsed description should show expand hint:\n%s", out)
	}
}

func TestRenderDiff(t *testing.T) {
	m := renderModel()
	m.diffBase = "origin/main"
	m.diffFiles = []git.DiffFile{
		{Path: "a.go", Add: 10, Del: 2},
		{Path: "b.bin", Add: -1, Del: -1},
	}
	m.diffCursor = 0
	out := m.renderDiff()
	for _, want := range []string{"2 files changed", "a.go", "+10", "binary"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDiff missing %q\n%s", want, out)
		}
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	m := renderModel()
	m.diffBase = "origin/main"
	out := m.renderDiff()
	if !strings.Contains(out, "no changes") {
		t.Errorf("empty diff should say no changes: %q", out)
	}
}

func TestRenderInspect(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}}})
	m.inspectPath = "/w/feat"
	m.inspect = inspectorData{
		status:   git.StatusSummary{Modified: 2, Untracked: 1},
		commit:   git.HeadCommit{Subject: "fix: thing", Author: "ana", When: time.Now().Add(-2 * time.Hour)},
		commitOK: true,
		diskKB:   42 * 1024,
		diskOK:   true,
		stashes:  2,
		base:     "origin/main",
		files:    []git.DiffFile{{Path: "a.go", Add: 3, Del: 1}},
	}
	out := m.renderInspect()
	for _, want := range []string{"feat", "2 modified", "1 untracked", "fix: thing", "ana", "2h ago", "42.0 MB", "2 stash", "+3", "main"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderInspect missing %q\n%s", want, out)
		}
	}
}

func TestRenderChecks(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}}})
	m.prByBranch = map[string]gh.PR{}
	m.ciPath = "/w/feat"
	m.ciRuns = []gh.Run{
		{Name: "fix: thing", Workflow: "ci", Event: "push", Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
		{Name: "wip", Workflow: "ci", Event: "pull_request", Status: "in_progress"},
	}
	out := m.renderChecks()
	for _, want := range []string{"Runs · feat", "fix: thing", "wip", "1h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderChecks missing %q\n%s", want, out)
		}
	}

	m.ciRuns = nil
	m.ciErr = "boom"
	if out := m.renderChecks(); !strings.Contains(out, "gh unavailable") {
		t.Errorf("error state missing:\n%s", out)
	}
	m.ciErr = ""
	if out := m.renderChecks(); !strings.Contains(out, "no workflow runs") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

func TestRenderInspectCleanNoCommit(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/main", Branch: "main", IsMain: true}}})
	m.inspectPath = "/w/main"
	m.inspect = inspectorData{}
	out := m.renderInspect()
	for _, want := range []string{"clean", "no commits yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderInspect missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "Diff vs") {
		t.Errorf("main worktree should not show a diff section:\n%s", out)
	}
}

func TestPaneTabStripNeverWraps(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{
		{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}, PR: 12},
	})
	m.prByBranch = map[string]gh.PR{"feat": {Number: 12}}

	testWidths := []int{10, 15, 20, 25, 30, 40, 50, 60, 75, 80, 100, 120}
	views := []viewID{viewLog, viewProcesses, viewInspect, viewDiff, viewChecks, viewPR}

	for _, w := range testWidths {
		for _, id := range views {
			m.activateView(id)
			pane, _ := m.dynamicLayout.paneContaining(id)
			strip := m.paneTabStrip(pane, w)
			if strings.Contains(strip, "\n") {
				t.Fatalf("paneTabStrip(%d) view %v contains newline: %q", w, id, strip)
			}
			if visualW := lipgloss.Width(strip); visualW > w {
				t.Fatalf("paneTabStrip(%d) view %v width %d > %d: %q", w, id, visualW, w, strip)
			}
			if h := lipgloss.Height(strip); h > 1 {
				t.Fatalf("paneTabStrip(%d) view %v height %d > 1", w, id, h)
			}
		}
	}
}

func TestActiveViewVisibleInConstrainedPaneStrip(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{
		{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}, PR: 12},
	})
	m.prByBranch = map[string]gh.PR{"feat": {Number: 12}}

	for _, id := range []viewID{viewLog, viewProcesses, viewInspect, viewDiff, viewChecks, viewPR} {
		if !m.activateView(id) {
			t.Fatalf("could not activate %s", id)
		}
		pane, _ := m.dynamicLayout.paneContaining(id)
		strip := m.paneTabStrip(pane, 35)
		label := strings.TrimSpace(paneViewLabel(id, true))
		if !strings.Contains(strip, label) {
			t.Errorf("active view %s not visible in strip: %q", id, strip)
		}
	}
}

func TestDefaultDynamicGeometryMatchesLegacy(t *testing.T) {
	m := renderModel()
	m.width, m.height = 100, 40
	got := m.resolvedDynamicPaneLayout()

	work, ok := got.pane("pane-worktrees")
	if !ok {
		t.Fatal("missing worktrees pane")
	}
	workspace, ok := got.pane("pane-workspace")
	if !ok {
		t.Fatal("missing workspace pane")
	}
	if work.Rect.W != 35 || workspace.Rect.W != 65 {
		t.Fatalf("default widths = %d/%d, want 35/65", work.Rect.W, workspace.Rect.W)
	}
	if work.Rect.X != 0 || workspace.Rect.X != 35 {
		t.Fatalf("default positions = worktrees x=%d workspace x=%d", work.Rect.X, workspace.Rect.X)
	}
}

func dynamicLayoutVariants() []dynamicLayout {
	return []dynamicLayout{
		migrateLegacyLayout(layoutSpec{
			Axis: axisHorizontal, Order: [2]paneID{paneWorktrees, paneWorkspace}, WorktreesPercent: 35,
		}),
		migrateLegacyLayout(layoutSpec{
			Axis: axisHorizontal, Order: [2]paneID{paneWorkspace, paneWorktrees}, WorktreesPercent: 35,
		}),
		migrateLegacyLayout(layoutSpec{
			Axis: axisVertical, Order: [2]paneID{paneWorktrees, paneWorkspace}, WorktreesPercent: 35,
		}),
		migrateLegacyLayout(layoutSpec{
			Axis: axisVertical, Order: [2]paneID{paneWorkspace, paneWorktrees}, WorktreesPercent: 35,
		}),
		threePaneTestLayout(),
	}
}

func threePaneTestLayout() dynamicLayout {
	return dynamicLayout{Root: &layoutTreeNode{
		Axis:  axisHorizontal,
		Ratio: 35,
		First: &layoutTreeNode{Pane: &paneSpec{
			ID: "pane-worktrees", Views: []viewID{viewWorktrees},
		}},
		Second: &layoutTreeNode{
			Axis:  axisVertical,
			Ratio: 50,
			First: &layoutTreeNode{Pane: &paneSpec{
				ID: "pane-log", Views: []viewID{viewLog, viewInspect, viewDiff},
			}},
			Second: &layoutTreeNode{Pane: &paneSpec{
				ID: "pane-processes", Views: []viewID{viewProcesses, viewChecks, viewPR},
			}},
		},
	}}
}

func TestViewNeverExceedsTerminalAcrossDynamicLayouts(t *testing.T) {
	termSizes := [][2]int{{80, 24}, {100, 40}, {60, 20}, {120, 30}, {40, 15}}

	for _, sz := range termSizes {
		w, h := sz[0], sz[1]
		for _, layout := range dynamicLayoutVariants() {
			m := renderModel()
			m.width, m.height = w, h
			m.dynamicLayout = layout
			m.paneActive = map[paneID]viewID{}
			m.initPaneRuntime()
			m.list = worktreelist.New()
			m.list.SetItems([]worktreelist.Item{
				{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}, PR: 12},
			})
			m.prByBranch = map[string]gh.PR{"feat": {Number: 12}}
			m.ready = true
			m.layout()

			view := m.View()
			if actualH := lipgloss.Height(view); actualH > h {
				t.Fatalf("View() size %dx%d height=%d exceeds %d:\n%s", w, h, actualH, h, view)
			} else if actualH != h {
				t.Fatalf("View() size %dx%d height=%d, want %d", w, h, actualH, h)
			}
			if actualW := lipgloss.Width(view); actualW > w {
				t.Fatalf("View() size %dx%d width=%d exceeds %d:\n%s", w, h, actualW, w, view)
			}
		}
	}
}

func TestMultipleViewsRenderSimultaneously(t *testing.T) {
	m := renderModel()
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{
		"pane-worktrees": viewWorktrees,
		"pane-log":       viewLog,
		"pane-processes": viewProcesses,
	}
	m.initPaneRuntime()
	m.ready = true
	m.layout()
	m.viewTerm(viewLog).SetContent("LOG-SENTINEL")
	m.viewTerm(viewProcesses).SetContent("PROC-SENTINEL")

	out := m.View()
	for _, want := range []string{"LOG-SENTINEL", "PROC-SENTINEL"} {
		if !strings.Contains(out, want) {
			t.Fatalf("simultaneous view missing %q:\n%s", want, out)
		}
	}
}

func TestPerViewScrollStateIsIndependent(t *testing.T) {
	m := renderModel()
	m.dynamicLayout = threePaneTestLayout()
	m.paneActive = map[paneID]viewID{
		"pane-worktrees": viewWorktrees,
		"pane-log":       viewLog,
		"pane-processes": viewProcesses,
	}
	m.initPaneRuntime()
	m.layout()

	logTerm := m.viewTerm(viewLog)
	procTerm := m.viewTerm(viewProcesses)
	logTerm.SetContent(strings.Repeat("log line\n", 200))
	procTerm.SetContent(strings.Repeat("proc line\n", 200))
	logTerm.GotoTop()
	procTerm.GotoTop()

	next, _ := logTerm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	*logTerm = next
	if logTerm.ScrollPercent() <= 0 {
		t.Fatal("log viewport did not scroll")
	}
	if procTerm.ScrollPercent() > 0 {
		t.Fatalf("process viewport moved with log: %.2f", procTerm.ScrollPercent())
	}
}

func TestMouseWheelRoutesByDynamicPane(t *testing.T) {
	for _, layout := range dynamicLayoutVariants() {
		m := procModel()
		m.width, m.height = 100, 40
		m.dynamicLayout = layout
		m.paneActive = map[paneID]viewID{}
		m.initPaneRuntime()
		if !m.activateView(viewProcesses) {
			t.Fatal("processes unavailable")
		}
		m.layout()
		term := m.viewTerm(viewProcesses)
		term.SetContent(strings.Repeat("a line of output\n", 200))
		term.GotoTop()

		resolved := m.resolvedDynamicPaneLayout()
		procPane, _ := m.dynamicLayout.paneContaining(viewProcesses)
		rp, _ := resolved.pane(procPane.ID)
		wheel := tea.MouseMsg{
			X: rp.Rect.X + rp.Rect.W/2, Y: rp.Rect.Y + rp.Rect.H/2,
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		}
		nm, _ := m.onMouse(wheel)
		got := nm.(Model)
		if got.viewTerm(viewProcesses).ScrollPercent() <= 0 {
			t.Fatalf("layout=%+v: wheel over Processes did not scroll", layout.Root)
		}

		got.viewTerm(viewProcesses).GotoTop()
		workPane, _ := got.dynamicLayout.paneContaining(viewWorktrees)
		workRect, _ := got.resolvedDynamicPaneLayout().pane(workPane.ID)
		wheel = tea.MouseMsg{
			X: workRect.Rect.X + workRect.Rect.W/2, Y: workRect.Rect.Y + workRect.Rect.H/2,
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		}
		nm, _ = got.onMouse(wheel)
		afterModel := nm.(Model)
		if after := afterModel.viewTerm(viewProcesses).ScrollPercent(); after > 0 {
			t.Fatalf("layout=%+v: wheel over Worktrees scrolled Processes %.2f", layout.Root, after)
		}
	}
}

func TestMouseWheelSuppressionAcrossDynamicLayouts(t *testing.T) {
	for _, layout := range dynamicLayoutVariants() {
		base := procModel()
		base.width, base.height = 100, 40
		base.dynamicLayout = layout
		base.paneActive = map[paneID]viewID{}
		base.initPaneRuntime()
		base.activateView(viewProcesses)
		base.layout()
		base.viewTerm(viewProcesses).SetContent(strings.Repeat("a line of output\n", 200))
		procPane, _ := base.dynamicLayout.paneContaining(viewProcesses)
		rp, _ := base.resolvedDynamicPaneLayout().pane(procPane.ID)
		wheel := tea.MouseMsg{
			X: rp.Rect.X + rp.Rect.W/2, Y: rp.Rect.Y + rp.Rect.H/2,
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		}

		mouseOff := base
		mouseOff.mouseOff = true
		mouseOff.viewTerm(viewProcesses).GotoTop()
		nm, _ := mouseOff.onMouse(wheel)
		mouseOffModel := nm.(Model)
		if got := mouseOffModel.viewTerm(viewProcesses).ScrollPercent(); got > 0 {
			t.Fatalf("mouseOff allowed process scroll: %.2f", got)
		}

		withPrefs := base
		pm := prefs.Model{}
		withPrefs.prefs = &pm
		withPrefs.viewTerm(viewProcesses).GotoTop()
		nm, _ = withPrefs.onMouse(wheel)
		prefsModel := nm.(Model)
		if got := prefsModel.viewTerm(viewProcesses).ScrollPercent(); got > 0 {
			t.Fatalf("preferences overlay allowed process scroll: %.2f", got)
		}

		withModal := base
		modal := modals.Model{}
		withModal.modal = &modal
		withModal.viewTerm(viewProcesses).GotoTop()
		nm, _ = withModal.onMouse(wheel)
		modalModel := nm.(Model)
		if got := modalModel.viewTerm(viewProcesses).ScrollPercent(); got > 0 {
			t.Fatalf("modal allowed process scroll: %.2f", got)
		}
	}
}

func TestLayoutRecomputePreservesPaneFocusActiveViewAndSelection(t *testing.T) {
	m := renderModel()
	m.width, m.height = 100, 40
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{
		{WT: git.Worktree{Path: "/w/first", Branch: "first"}},
		{WT: git.Worktree{Path: "/w/second", Branch: "second"}},
	})
	m.activateView(viewInspect)
	before, ok := m.selectedWorktree()
	if !ok {
		t.Fatal("expected selected worktree")
	}
	focused := m.focusedPane

	m.dynamicLayout = migrateLegacyLayout(layoutSpec{
		Axis: axisVertical, Order: [2]paneID{paneWorkspace, paneWorktrees}, WorktreesPercent: 65,
	})
	m.initPaneRuntime()
	m.layout()

	after, ok := m.selectedWorktree()
	if !ok || after.Path != before.Path {
		t.Fatalf("layout recompute changed selection: before=%+v after=%+v ok=%v", before, after, ok)
	}
	if m.focusedPane != focused {
		t.Fatalf("layout recompute changed surviving focused pane: %q -> %q", focused, m.focusedPane)
	}
	if got := m.activeView(focused); got != viewInspect {
		t.Fatalf("layout recompute changed active view: %q", got)
	}
}
