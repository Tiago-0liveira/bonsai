package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

func wtModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := shellModel(t, config.Prefs{NoWorkspaceMemory: true}, nil)
	m.workspace, m.shellFocus, m.wt.started = wsWorktrees, shellBody, true
	m.procs = &procView{recs: []*procstore.Record{
		{ID: 1, Worktree: "/repo/feature", Label: "api", Status: procstore.StatusRunning, LastURL: "http://localhost:3000"},
		{ID: 2, Worktree: "/repo/main", Label: "web", Status: procstore.StatusDone},
	}}
	m.worktrees = []git.Worktree{{Path: "/repo/main", Branch: "main", IsMain: true}, {Path: "/repo/feature", Branch: "feature"}}
	m.statuses["/repo/feature"] = git.StatusSummary{Modified: 2, Untracked: 1}
	m.metrics["/repo/feature"] = git.Metrics{Ahead: 3, Behind: 1}
	m.prByBranch["feature"] = gh.PR{Number: 67, Head: "feature", Title: "Usage", State: gh.StateOpen}
	m.prByNumber = map[int]gh.PR{67: m.prByBranch["feature"], 69: {Number: 69, Head: "remote-only", Title: "Reconnect", State: gh.StateOpen}}
	m.wt.branches = []string{"main", "feature", "unchecked"}
	m.wt.branchLoaded = true
	m.list.SetEmbedded(true)
	m.rebuildItems()
	m.list.SelectByPath("/repo/feature")
	m = sized(t, m, w, h)
	m.layout()
	return m
}
func wtPress(t *testing.T, m Model, k string) Model {
	t.Helper()
	nm, _ := send(t, m, keyOf(k))
	return nm
}
func TestWorktreesSubmenusCompactAndWide(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 10}} {
		for sub := wtSub(0); sub < wtSubCount; sub++ {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], wtSubNames[sub]), func(t *testing.T) {
				m := wtModel(t, size[0], size[1])
				m.wt.sub = sub
				m.worktreesReconcile()
				m.layout()
				out := m.View()
				assertBounded(t, "worktrees", out, size[0], size[1])
				if got := len(strings.Split(out, "\n")); got != size[1] {
					t.Fatalf("height=%d", got)
				}
				if size[1] >= 24 {
					for _, s := range wtSubNames {
						mustContain(t, "submenu", ansi.Strip(out), s)
					}
				}
				if sub != wtCheckouts {
					rows := m.worktreesRows()
					if len(rows) == 0 {
						t.Fatal("expected live backend rows")
					}
				}
			})
		}
	}
	m := wtModel(t, 80, 24)
	if m.focus != focusList {
		t.Fatal("initial focus")
	}
	m = wtPress(t, m, "enter")
	if m.focus != focusTerminal || m.rightTab != tabInspect {
		t.Fatal("Enter must inspect at compact width")
	}
	m, _ = send(t, m, inspectorMsg{path: "/repo/feature", data: inspectorData{statusKnown: true, status: git.StatusSummary{Modified: 2}}})
	mustContain(t, "compact inspector", ansi.Strip(m.View()), "Working tree", "2 modified")
	m = wtPress(t, m, "esc")
	if m.focus != focusList {
		t.Fatal("Esc returns to list")
	}
	m = wtPress(t, m, "esc")
	if !m.wt.menu {
		t.Fatal("Esc returns to submenu")
	}
	m = wtPress(t, m, "tab")
	if m.wt.sub != wtProcesses {
		t.Fatal("Tab selects Processes")
	}
	m = wtPress(t, m, "esc")
	if m.shellFocus != shellNav {
		t.Fatal("Esc returns to workspace tabs")
	}
}
func TestWorktreesUncheckedRowsNeverTargetPreviousCheckout(t *testing.T) {
	m := wtModel(t, 80, 24)
	m.wt.sub = wtBranches
	m.wt.selected[wtBranches] = "unchecked"
	m.worktreesReconcile()
	if _, ok := m.selectedWorktree(); ok {
		t.Fatal("unchecked branch borrowed the old checkout")
	}
	m = wtPress(t, m, "x")
	if m.modal != nil {
		t.Fatal("prune targeted a previous checkout")
	}
	m = wtPress(t, m, "enter")
	if m.modal == nil || m.pendingConfirm == nil {
		t.Fatal("checkout creation requires confirmation")
	}
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindConfirm})
	if m.pendingConfirm != nil || m.modal != nil {
		t.Fatal("cancel retained a deferred creation")
	}
	m.wt.sub = wtPRs
	m.wt.selected[wtPRs] = "69"
	m.worktreesReconcile()
	if _, ok := m.selectedWorktree(); ok {
		t.Fatal("PR with no checkout borrowed previous checkout")
	}
	if ok, _ := needFeatureWorktree(m); ok {
		t.Fatal("palette enabled mutation for an unchecked PR")
	}
}
func TestWorktreesSelectionsUseStableBackendIdentities(t *testing.T) {
	m := wtModel(t, 120, 35)
	m.sort = sortDirty
	m.rebuildItems()
	if m.currentPath() != "/repo/feature" {
		t.Fatal("sort lost checkout identity")
	}
	m.wt.sub = wtProcesses
	m.wt.selected[wtProcesses] = "1"
	m.worktreesReconcile()
	if m.currentPath() != "/repo/feature" || m.activeProc[m.currentPath()] != 1 {
		t.Fatal("process selection lost owner or ID")
	}
	m.procs.recs = append([]*procstore.Record{{ID: 3, Worktree: "/repo/main", Label: "worker"}}, m.procs.recs...)
	m.worktreesReconcile()
	if m.wt.selected[wtProcesses] != "1" {
		t.Fatal("refresh moved process selection")
	}
	m.wt.sub = wtPRs
	m.wt.selected[wtPRs] = "67"
	m.worktreesReconcile()
	if m.currentPath() != "/repo/feature" {
		t.Fatal("PR not scoped to its checkout")
	}
	delete(m.prByNumber, 67)
	m.worktreesReconcile()
	if m.wt.selected[wtPRs] != "69" {
		t.Fatal("removed PR did not select a deterministic neighbor")
	}
}
func TestWorktreesReadsRejectLateResultsAndKeepLastGood(t *testing.T) {
	m := wtModel(t, 80, 24)
	read := func(branches []string, err error) tea.Cmd {
		return func() tea.Msg { return branchesMsg{kind: branchesForWorkspace, branches: branches, err: err} }
	}
	old := m.worktreeRead("branches:2", read([]string{"old"}, nil))
	newer := m.worktreeRead("branches:2", read([]string{"new"}, nil))
	m, _ = send(t, m, newer())
	m, _ = send(t, m, old())
	if strings.Join(m.wt.branches, " ") != "new" {
		t.Fatal("late result overwrote newer branches")
	}
	failed := m.worktreeRead("branches:2", read(nil, errors.New("offline")))
	m, _ = send(t, m, failed())
	if len(m.wt.branches) != 1 || m.wt.branchErr == "" {
		t.Fatal("failure cleared branches or omitted stale reason")
	}
	m, _ = send(t, m, logMsg{path: "/repo/main", content: "wrong checkout"})
	if m.logContent == "wrong checkout" {
		t.Fatal("late log switched scope")
	}
	m.rightTab = tabDiff
	m.diffPath = "/repo/feature"
	m.diffFiles = []git.DiffFile{{Path: "keep.go", Add: 3}}
	m, _ = send(t, m, diffMsg{path: m.diffPath, err: errors.New("offline")})
	if len(m.diffFiles) != 1 {
		t.Fatal("diff failure discarded last good data")
	}
	m, _ = send(t, m, diffMsg{path: m.diffPath})
	if len(m.diffFiles) != 0 {
		t.Fatal("successful empty diff did not clear list")
	}
	m.prChecks[67] = []gh.Check{{Name: "CI"}}
	m.checkRollup["/repo/feature"] = "pass"
	m, _ = send(t, m, checksMsg{path: "/repo/feature", number: 67, err: errors.New("offline")})
	if len(m.prChecks[67]) != 1 || m.checkRollup["/repo/feature"] != "pass" {
		t.Fatal("CI failure cleared last good checks")
	}
}
func TestWorktreesProcessSearchAndAgentFocusAreSeparate(t *testing.T) {
	m := wtModel(t, 80, 24)
	m.rightTab = tabProcs
	m.focus = focusTerminal
	m.layout()
	m = wtPress(t, m, "/")
	if !m.procSearchActive || m.currentShellFocus() != shellInput {
		t.Fatal("output search must capture keys")
	}
	for _, k := range []string{"q", ",", "a", "n", "?"} {
		m = wtPress(t, m, k)
	}
	if m.modal != nil || m.workspace != wsWorktrees || m.rightTab != tabProcs {
		t.Fatal("search keys escaped to the shell or agent")
	}
	m = wtPress(t, m, "enter")
	if m.procSearchActive || m.procSearch[m.currentPath()] != "q,an?" {
		t.Fatal("process filter not retained")
	}
	m.term.SetContent(strings.Repeat("log output\n", 100))
	m.term.GotoTop()
	before := m.term.YOffset()
	m = wtPress(t, m, "j")
	if m.term.YOffset() <= before || m.terminalTypingMode() {
		t.Fatal("process scrolling entered agent terminal input")
	}
	m.worktreesRememberDocument()
	m, _ = m.selectWorkspace(wsSettings)
	m, _ = m.selectWorkspace(wsWorktrees)
	if m.procSearch[m.currentPath()] != "q,an?" || m.rightTab != tabProcs || m.term.YOffset() == 0 {
		t.Fatal("round-trip lost process query/tab/scroll")
	}
}
func TestWorktreesDestructiveModalsUseExistingPreviews(t *testing.T) {
	m := wtModel(t, 120, 35)
	m = wtPress(t, m, "x")
	if m.modal == nil || m.modal.Kind() != modals.KindPrune {
		t.Fatal("prune did not use the existing options dialog")
	}
	mustContain(t, "prune", ansi.Strip(m.modal.View()), "delete worktree", "delete branch")
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindPrune})
	m = wtPress(t, m, "n")
	if m.modal == nil || m.modal.Kind() != modals.KindCreateSource {
		t.Fatal("new checkout flow missing")
	}
	m, _ = send(t, m, modals.SubmitMsg{Kind: modals.KindCreateSource, Value: createSourceNew})
	if m.modal == nil || m.modal.Kind() != modals.KindCreateNew {
		t.Fatal("shared modal submission was swallowed by shell")
	}
}
func TestWorktreesExactSessionHandoffStartsInObserveAndReturns(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fb.Sessions.Add(e.sess("session-exact", "claude", "work", "running", 1), e.sess("other-session", "codex", "personal", "running", 2))
	e.open(t)
	e.m = pgDrain(t, e.m, e.m.pgRelease())
	e.m, _ = e.m.selectWorkspace(wsWorktrees)
	e.m.wt.started = true
	e.m.worktrees = []git.Worktree{{Path: e.tree, Branch: "feature"}}
	e.m.rebuildItems()
	e.m.list.SelectByPath(e.tree)
	e.m.shellFocus = shellBody
	e.m.rightTab = tabAgents
	e.m.focus = focusTerminal
	e.m.agents.byTree = indexSessions(e.m.worktreePaths(), e.m.agents.sessions)
	e.m.agents.selected[cleanPath(e.tree)] = "session-exact"
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, keyOf("O"))
	e.m = pgDrain(t, e.m, cmd)
	if e.m.workspace != wsPlayground || e.m.pg.sel != "session-exact" || !e.m.pg.open || e.m.pgTerminalMode() {
		t.Fatal("handoff lost exact session or entered Control")
	}
	last := lastAttach(e)
	if last.ID != "session-exact" || !last.Opts.ReadOnly {
		t.Fatalf("handoff attachment: %+v", last)
	}
	e.key(t, "esc")
	e.key(t, "esc")
	e.key(t, "esc")
	if e.m.workspace != wsWorktrees || e.m.currentPath() != e.tree || e.m.rightTab != tabAgents {
		t.Fatal("Playground return lost original location")
	}
	for _, s := range e.fb.Sessions.Streams("session-exact") {
		if len(s.Inputs()) != 0 {
			t.Fatal("navigation wrote to an agent")
		}
	}
}
func TestWorktreesAccountHandoffKeepsIdentityAndReturnDocument(t *testing.T) {
	m := accountShellOpen(t, stableAccountsFake(), nil)
	m.workspace, m.shellFocus, m.wt.started = wsWorktrees, shellBody, true
	m.worktrees = []git.Worktree{{Path: "/repo/feature", Branch: "feature"}}
	m.procs = &procView{}
	m.rebuildItems()
	m.rightTab = tabInspect
	m.focus = focusTerminal
	m.layout()
	m.term.SetContent(strings.Repeat("document\n", 100))
	m.term.SetYOffset(12)
	nm, cmd := m.worktreesAccounts(agents.AccountID("x1"), accounts.TabUsage)
	m = nm.(Model)
	m, _ = accountShellDrain(t, m, cmd)
	if m.workspace != wsAccounts || !strings.Contains(m.accountsWhere(), "personal") {
		t.Fatal("account identity was lost")
	}
	for i := 0; i < 3 && m.workspace == wsAccounts; i++ {
		m = accountShellKeys(t, m, "esc")
	}
	if m.workspace != wsWorktrees || m.currentPath() != "/repo/feature" || m.term.YOffset() != 12 {
		t.Fatalf("return lost document: ws=%v path=%s offset=%d", m.workspace, m.currentPath(), m.term.YOffset())
	}
}

func TestWorktreesPRRowRetainsExactNumberForSharedHead(t *testing.T) {
	m := wtModel(t, 120, 35)
	m.prByBranch["feature"] = gh.PR{Number: 70, Head: "feature", State: gh.StateOpen}
	m.prByNumber[70] = m.prByBranch["feature"]
	m.wt.sub = wtPRs
	m.wt.selected[wtPRs] = "67"
	m.worktreesReconcile()
	if n, ok := m.prForBranch("feature"); !ok || n != 67 {
		t.Fatalf("selected PR became latest head PR: %d", n)
	}
}
func TestWorktreesProcessArrowSelectionSurvivesTicks(t *testing.T) {
	m := wtModel(t, 120, 35)
	m.procs.recs = append(m.procs.recs, &procstore.Record{ID: 3, Worktree: "/repo/feature", Label: "worker"})
	m.wt.sub = wtProcesses
	m.wt.selected[wtProcesses] = "1"
	m.worktreesReconcile()
	m = wtPress(t, m, "enter")
	m = wtPress(t, m, "down")
	if m.activeProc["/repo/feature"] != 3 || m.wt.selected[wtProcesses] != "3" {
		t.Fatal("detail selection did not update process row identity")
	}
	m, _ = send(t, m, procTickMsg{})
	if m.activeProc["/repo/feature"] != 3 || m.wt.selected[wtProcesses] != "3" {
		t.Fatal("process tick reset detail selection")
	}
}

func TestWorktreesLatePromptCannotActOnAnotherCheckout(t *testing.T) {
	m := wtModel(t, 120, 35)
	read := m.worktreePromptRead("commit-preview", func() tea.Msg { return commitPreviewMsg{status: "original checkout"} })
	m.list.SelectByPath("/repo/main")
	m, _ = send(t, m, read())
	if m.modal != nil {
		t.Fatal("late commit preview opened on another checkout")
	}
	read = m.worktreePromptRead("commit-preview", func() tea.Msg { return commitPreviewMsg{status: "main checkout"} })
	m, _ = m.selectWorkspace(wsSettings)
	m, _ = send(t, m, read())
	if m.modal != nil {
		t.Fatal("late read reopened a dialog after leaving Worktrees")
	}
}
func TestWorktreesAgentInputOwnsKeysAndASCIILeavesOutputIntact(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.fb.Sessions.Add(e.sess("exact", "claude", "work", "running", 1))
	read := e.m.playgroundRelist()
	e.m = pgDrain(t, e.m, read)
	e.m.wt.started = true
	e.m.procs = &procView{}
	e.m.worktrees = []git.Worktree{{Path: e.tree, Branch: "feature"}}
	e.m.rebuildItems()
	e.m.agents.byTree = indexSessions(e.m.worktreePaths(), e.m.agents.sessions)
	e.m, _ = e.m.selectWorkspace(wsWorktrees)
	e.m.work.AgentGlyphs = "ascii"
	e.m.shellFocus, e.m.focus, e.m.rightTab = shellBody, focusTerminal, tabAgents
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, nil)
	e.m = plannerRun(t, e.m, cmd)
	e.m = plannerPress(t, e.m, "enter")
	st := e.stream(t, "exact")
	e.frames(t, st, status("running"), out("agent glyph ●"))
	if !e.m.terminalTypingMode() {
		t.Fatal("Enter did not give the selected agent control")
	}
	mustContain(t, "agent output", ansi.Strip(e.m.View()), "agent glyph ●", "FOCUS: Agent input")
	mustNotContain(t, "terminal hints", ansi.Strip(e.m.View()), "ctrl+k commands")
	for _, k := range []tea.KeyMsg{runeKey("q"), runeKey("a"), runeKey("n"), {Type: tea.KeyCtrlC}, {Type: tea.KeyRunes, Runes: []rune{'4'}, Alt: true}} {
		e.press(t, k)
	}
	e.pump(t)
	if len(st.Inputs()) != 5 || e.m.workspace != wsWorktrees || e.m.modal != nil {
		t.Fatal("terminal keys escaped to shell or agent actions")
	}
	e.press(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if e.m.terminalTypingMode() || st.Closed() {
		t.Fatal("Ctrl+] should only return the keyboard")
	}
	e.m = plannerPress(t, e.m, "a")
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions == nil {
		t.Fatal("a lost its scoped all-sessions meaning")
	}
	e.m = plannerPress(t, e.m, "esc", "n")
	if e.m.agents.overlay == nil || e.m.agents.overlay.launch == nil {
		t.Fatal("n lost its scoped agent-launch meaning")
	}
	e.m = plannerPress(t, e.m, "esc", "v")
	if e.m.rightTab != tabProcs || e.m.terminalTypingMode() {
		t.Fatal("process logs inherited agent keyboard capture")
	}
	before := len(st.Inputs())
	e.m = plannerPress(t, e.m, "j")
	if len(st.Inputs()) != before {
		t.Fatal("process navigation wrote to holder")
	}
	sessions, err := e.fb.Sessions.List(context.Background())
	if err != nil || len(sessions) != 1 || !sessions[0].Active() {
		t.Fatal("leaving agent detail stopped its holder")
	}
}

func TestWorktreesBackgroundReadsKeepSubmenuScopeWhileAway(t *testing.T) {
	m := wtModel(t, 120, 35)
	m.wt.sub = wtProcesses
	m.wt.selected[wtProcesses] = "1"
	m.worktreesReconcile()
	// A Checkouts filter can hide the process's owner. The submenu's stable
	// owner still scopes background log/PR updates while another workspace is open.
	m.list.SelectByPath("/repo/main")
	m, _ = m.selectWorkspace(wsSettings)
	if m.currentPath() != "/repo/feature" {
		t.Fatal("background process reads borrowed hidden Checkouts selection")
	}
	m.wt.sub = wtPRs
	m.wt.selected[wtPRs] = "67"
	m.worktreesReconcile()
	m.prByBranch["feature"] = gh.PR{Number: 70, Head: "feature", State: gh.StateOpen}
	if n, ok := m.prForBranch("feature"); !ok || n != 67 {
		t.Fatalf("background PR update lost selected identity: %d", n)
	}
}

func TestWorktreesWheelUsesWorkspaceGeometryAndCaptureBoundaries(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := wtModel(t, width, 24)
			m.rightTab = tabLog
			m.focus = focusTerminal
			m.layout()
			m.term.SetContent(strings.Repeat("log\n", 100))
			wheel := tea.MouseMsg{X: width - 3, Y: shellHeaderRows + settingsSubmenuRows + 3, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
			m, _ = send(t, m, wheel)
			if m.term.YOffset() == 0 {
				t.Fatal("detail wheel did not scroll actual shell viewport")
			}
			offset := m.term.YOffset()
			m.mouseOff = true
			m, _ = send(t, m, wheel)
			if m.term.YOffset() != offset {
				t.Fatal("disabled mouse still scrolled")
			}
			m.mouseOff = false
			wheel.Y = 1
			m, _ = send(t, m, wheel)
			if m.term.YOffset() != offset {
				t.Fatal("header wheel scrolled body")
			}
			wheel.Y = shellHeaderRows + settingsSubmenuRows + 3
			m.rightTab = tabAgents
			m, _ = send(t, m, wheel)
			if m.term.YOffset() != offset {
				t.Fatal("agent wheel reached hidden log viewport")
			}
		})
	}
	m := wtModel(t, 120, 35)
	m.wt.sub = wtProcesses
	m.worktreesReconcile()
	m, _ = send(t, m, tea.MouseMsg{X: 2, Y: 9, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.wt.index[wtProcesses] != 1 {
		t.Fatal("wheel did not move scoped process rows")
	}
}
