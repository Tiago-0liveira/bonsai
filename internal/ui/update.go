package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	corenotify "github.com/Tiago-0liveira/bonsai/internal/core/notify"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

// Update routes messages: layout, data results, the process tick, an active
// modal, or global keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.onResize(msg), nil

	case worktreesMsg:
		return m.onWorktrees(msg)

	case metricsMsg:
		return m.onMetrics(msg), nil

	case fileIndexMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		m.fileIndex = msg.files
		return m, nil

	case logMsg:
		// Log failures (e.g. a branch with no commits yet) are non-fatal: show a
		// note in the output pane rather than flooding the help bar.
		m.logContent = msg.content
		if msg.err != nil || msg.content == "" {
			m.logContent = "(no commits yet)"
		}
		if m.rightTab == tabLog {
			m.term.SetTitle("Git Log")
			m.term.SetFollow(false)
			m.term.SetContent(m.logContent)
		}
		return m, nil

	case scriptsMsg:
		return m.onScripts(msg)

	case commitPreviewMsg:
		return m.onCommitPreview(msg)

	case prsMsg:
		return m.onPRs(msg)

	case branchesMsg:
		return m.onBranches(msg)

	case pruneCandidatesMsg:
		return m.onPruneCandidates(msg)

	case prMapMsg:
		return m.onPRMap(msg)

	case prDetailMsg:
		return m.onPRDetail(msg)

	case checksMsg:
		return m.onChecks(msg)

	case dirtyMsg:
		m.dirty[msg.path] = msg.dirty
		m.rebuildItems()
		return m, nil

	case activityMsg:
		m.lastCommit[msg.path] = msg.unix
		m.rebuildItems()
		return m, nil

	case diffMsg:
		return m.onDiff(msg)

	case fileDiffMsg:
		if msg.path == m.diffPath {
			if msg.err != nil {
				m.diffFileContent[msg.file] = "(diff unavailable: " + msg.err.Error() + ")"
			} else if strings.TrimSpace(msg.content) == "" {
				m.diffFileContent[msg.file] = "(no textual diff)"
			} else {
				m.diffFileContent[msg.file] = msg.content
			}
			// Feed the open diff modal if it is waiting on this file.
			if m.modal != nil && m.modal.Kind() == modals.KindDiffFile && msg.file == m.diffModalFile {
				m.modal.SetScrollContent(m.diffFileContent[msg.file])
			}
		}
		return m, nil

	case prCreatedMsg:
		return m.onPRCreated(msg)

	case opDoneMsg:
		return m.onOpDone(msg)

	case procTickMsg:
		return m.onProcTick()

	case prTickMsg:
		return m.onPRTick()

	case modals.SubmitMsg:
		return m.onModalSubmit(msg)

	case modals.CancelMsg:
		m.modal = nil
		m.pendingConfirm = nil
		return m, nil

	case tea.KeyMsg:
		// An open modal owns all key input.
		if m.modal != nil {
			nm, cmd := m.modal.Update(msg)
			m.modal = &nm
			return m, cmd
		}
		return m.onKey(msg)
	}

	// Forward remaining messages to the focused pane.
	return m.forwardToPane(msg)
}

func (m Model) onResize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.help.Width = msg.Width
	m.layout()
	m.ready = true
	return m
}

func (m Model) onWorktrees(msg worktreesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.worktrees = msg.trees
	m.rebuildItems()

	// Fetch metrics per branch, connected PRs, dirty state, activity, and the log
	// for the selection.
	cmds := []tea.Cmd{fetchPRs(m.repoDir)}
	for _, t := range msg.trees {
		cmds = append(cmds, loadDirty(t.Path), loadActivity(t.Path))
		if t.Branch != "" && t.Branch != "(detached)" {
			cmds = append(cmds, loadMetrics(t.Path, t.Branch, m.cfg.Upstream))
		}
	}
	if wt, ok := m.selectedWorktree(); ok {
		cmds = append(cmds, loadLog(wt.Path))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) onMetrics(msg metricsMsg) Model {
	if msg.err != nil {
		return m
	}
	m.metrics[msg.path] = msg.metrics
	m.rebuildItems()
	return m
}

// onPRMap records PRs (any state) keyed by head branch and redecorates rows.
// gh errors are silent — no gh, no badges. When a branch carries several PRs
// an open one wins over merged/closed ones. Because a PR carries its real base
// branch, ahead/behind metrics for PR-backed branches are recomputed here
// against that base rather than the global upstream.
func (m Model) onPRMap(msg prMapMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	m.prByBranch = make(map[string]gh.PR, len(msg.prs))
	for _, pr := range msg.prs {
		if cur, ok := m.prByBranch[pr.Head]; ok && cur.State == gh.StateOpen && pr.State != gh.StateOpen {
			continue
		}
		m.prByBranch[pr.Head] = pr
	}
	m.rebuildItems()

	var cmds []tea.Cmd
	for _, t := range m.worktrees {
		if t.Branch == "" || t.Branch == "(detached)" {
			continue
		}
		if pr, ok := m.prByBranch[t.Branch]; ok && pr.Number > 0 {
			if pr.Base != "" {
				cmds = append(cmds, loadMetrics(t.Path, t.Branch, m.upstreamFor(t.Branch)))
			}
			cmds = append(cmds, loadChecks(m.repoDir, t.Path, pr.Number))
		}
	}
	return m, tea.Batch(cmds...)
}

// onPRDetail caches a loaded PR and, if the PR tab is showing, renders it.
func (m Model) onPRDetail(msg prDetailMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.prPaneErr = msg.err.Error()
	} else {
		m.prPaneErr = ""
		m.prDetail[msg.detail.Number] = msg.detail
	}
	if m.rightTab == tabPR {
		m.refreshPRPane()
	}
	return m, nil
}

// onChecks records a PR's CI rollup + checks, notifies on a rollup transition,
// and redecorates rows.
func (m Model) onChecks(msg checksMsg) (tea.Model, tea.Cmd) {
	prev := m.checkRollup[msg.path]
	m.checkRollup[msg.path] = msg.rollup
	if msg.number > 0 {
		m.prChecks[msg.number] = msg.checks
	}
	if m.cfg.Notifications.CI && prev != "" && msg.rollup != "" && prev != msg.rollup {
		notify("bonsai: CI "+msg.rollup, fmt.Sprintf("PR #%d checks: %s", msg.number, msg.rollup))
	}
	m.rebuildItems()
	if m.rightTab == tabPR {
		m.refreshPRPane()
	}
	return m, nil
}

// upstreamFor returns the ref a branch's ahead/behind metrics and base-branch
// label should compare against: a connected PR's real base (as <remote>/<base>)
// when known, otherwise the configured global upstream.
func (m Model) upstreamFor(branch string) string {
	if pr, ok := m.prByBranch[branch]; ok && pr.Base != "" {
		return config.RemoteOf(m.cfg.Upstream) + "/" + pr.Base
	}
	return m.cfg.Upstream
}

// rebuildItems repopulates the list from the current worktrees plus any known
// per-path metrics, PRs, dirty state and CI rollups, applying the current sort
// (main pinned first) while preserving the highlighted row.
func (m *Model) rebuildItems() {
	trees := m.sortedWorktrees()
	items := make([]worktreelist.Item, len(trees))
	for i, t := range trees {
		it := worktreelist.Item{WT: t}
		if mtr, ok := m.metrics[t.Path]; ok {
			it.Metrics = mtr
			it.HasMetrics = true
		}
		if pr, ok := m.prByBranch[t.Branch]; ok {
			it.PR = pr.Number
			it.PRState = pr.State
		}
		it.Dirty = m.dirty[t.Path]
		it.Checks = m.checkRollup[t.Path]
		it.Running = m.countRunning(t.Path)
		items[i] = it
	}
	m.list.SetItems(items)
	m.list.SetTitle("Worktrees · " + m.sort.label())
}

// sortedWorktrees returns the worktrees ordered by the active sort mode, with the
// main worktree always first.
func (m *Model) sortedWorktrees() []git.Worktree {
	trees := make([]git.Worktree, len(m.worktrees))
	copy(trees, m.worktrees)
	sort.SliceStable(trees, func(a, b int) bool {
		ta, tb := trees[a], trees[b]
		if ta.IsMain != tb.IsMain {
			return ta.IsMain // main first
		}
		switch m.sort {
		case sortAhead:
			return m.metrics[ta.Path].Ahead > m.metrics[tb.Path].Ahead
		case sortBehind:
			return m.metrics[ta.Path].Behind > m.metrics[tb.Path].Behind
		case sortPR:
			return m.prByBranch[ta.Branch].Number > m.prByBranch[tb.Branch].Number
		case sortActivity:
			return m.lastCommit[ta.Path] > m.lastCommit[tb.Path]
		default:
			return ta.Branch < tb.Branch
		}
	})
	return trees
}

// runCommandLabel is the sentinel scripts-modal entry that starts the ad-hoc
// "run any command" flow.
const runCommandLabel = "＋ run command…"

func (m Model) onScripts(msg scriptsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "scripts: " + msg.err.Error()
		return m, nil
	}
	m.scriptRun = msg.runCmd

	names := msg.scripts
	// The sentinel entry is always present (even with no manager) so any command
	// can be run; typed text filters the detected scripts below it.
	filter := func(q string) []string {
		out := []string{runCommandLabel}
		q = strings.ToLower(strings.TrimSpace(q))
		for _, n := range names {
			if q == "" || strings.Contains(strings.ToLower(n), q) {
				out = append(out, n)
			}
		}
		return out
	}

	title := "Run script"
	if msg.manager != "" {
		title = "Run " + msg.manager + " script"
	}
	modal := modals.NewFuzzy(modals.KindScripts, title, filter, filter(""))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

func (m Model) onOpDone(msg opDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.label + ": " + msg.err.Error()
	} else {
		m.status = msg.label + ": ok"
	}
	// Refresh worktrees + log after any mutating op.
	return m, loadWorktrees(m.repoDir)
}

func (m Model) onProcTick() (tea.Model, tea.Cmd) {
	m.checkProcTransitions()
	// Refresh the list badges only when the running-process counts changed.
	if sig := m.runningCountSig(); sig != m.runningSig {
		m.runningSig = sig
		m.rebuildItems()
	}
	if m.rightTab == tabProcs {
		m.refreshProcPane()
	}
	return m, tickProc()
}

// countRunning returns the number of processes currently running in path.
func (m Model) countRunning(path string) int {
	n := 0
	for _, p := range m.procs.List(path) {
		if p.Status() == "running" {
			n++
		}
	}
	return n
}

// runningCountSig is a compact signature of per-worktree running counts, used to
// detect when list badges need rebuilding.
func (m Model) runningCountSig() string {
	var b strings.Builder
	for _, t := range m.worktrees {
		if n := m.countRunning(t.Path); n > 0 {
			fmt.Fprintf(&b, "%s=%d;", t.Path, n)
		}
	}
	return b.String()
}

// checkProcTransitions fires a desktop notification when a process moves from
// running to done/failed (once per process), if enabled in config.
func (m *Model) checkProcTransitions() {
	if !m.cfg.Notifications.Process {
		return
	}
	for _, p := range m.procs.All() {
		key := p.Path + "#" + strconv.Itoa(p.ID)
		st := p.Status()
		prev := m.seenProcStatus[key]
		m.seenProcStatus[key] = st
		if prev == "running" && (st == "done" || st == "failed") {
			notify("bonsai: process "+st, fmt.Sprintf("#%d %s", p.ID, p.Label))
		}
	}
}

// notify is a thin wrapper over the core notify package.
func notify(title, body string) { corenotify.Send(title, body) }

// onPRTick re-checks PR states periodically so a merge done outside bonsai
// flips the row badge to MERGED without a restart or manual refresh.
func (m Model) onPRTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(fetchPRs(m.repoDir), tickPRs())
}

// activeProcess returns the process currently selected for display under path,
// falling back to the most recently spawned one.
func (m Model) activeProcess(path string) (*coreexec.Process, bool) {
	if id, ok := m.activeProc[path]; ok {
		if p, found := m.procs.GetByID(path, id); found {
			return p, true
		}
	}
	return m.procs.Latest(path)
}

// refreshProcPane loads the selected process's output into the scrolling area of
// the Processes tab. The process list + keybind hint are rendered separately as a
// footer pinned to the bottom of the pane (see renderProcFooter / View). Output
// tail-follows so new lines stay visible.
func (m *Model) refreshProcPane() {
	m.term.SetTitle("Processes")
	m.term.SetFollow(true)
	wt, ok := m.selectedWorktree()
	if !ok {
		m.term.SetContent("")
		return
	}
	if len(m.procs.List(wt.Path)) == 0 {
		m.term.SetContent("")
		return
	}
	sel, selOK := m.activeProcess(wt.Path)
	if selOK {
		m.activeProc[wt.Path] = sel.ID
		m.term.SetContent(sel.Output())
		return
	}
	m.term.SetContent("")
}

// selectedProcID returns the selected process id for path.
func (m Model) selectedProcID(path string) (int, bool) {
	if p, ok := m.activeProcess(path); ok {
		return p.ID, true
	}
	return 0, false
}

// moveProcSel moves the process selection for path by delta.
func (m *Model) moveProcSel(path string, delta int) {
	procs := m.procs.List(path)
	if len(procs) == 0 {
		return
	}
	idx := 0
	cur := m.activeProc[path]
	for i, p := range procs {
		if p.ID == cur {
			idx = i
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(procs) {
		idx = len(procs) - 1
	}
	m.activeProc[path] = procs[idx].ID
}

// onKey handles global keybindings when no modal is open.
func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the worktree filter input is active, the list owns every key.
	if m.focus == focusList && m.list.SettingFilter() {
		return m.forwardToPane(msg)
	}
	// On the Processes tab (focused), process-control keys win over the globals.
	if m.rightTab == tabProcs && m.focus == focusTerminal {
		if handled, nm, cmd := m.procTabKey(msg); handled {
			return nm, cmd
		}
	}
	// On the PR tab (focused), PR-action keys win over the globals.
	if m.rightTab == tabPR && m.focus == focusTerminal {
		if handled, nm, cmd := m.prTabKey(msg); handled {
			return nm, cmd
		}
	}
	// On the Diff tab (focused), file navigation/expand keys win over the globals.
	if m.rightTab == tabDiff && m.focus == focusTerminal {
		if handled, nm, cmd := m.diffTabKey(msg); handled {
			return nm, cmd
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.procs.KillAll()
		return m, tea.Quit

	case key.Matches(msg, m.keys.Tab):
		m.toggleFocus()
		return m, nil

	case key.Matches(msg, m.keys.ShiftTab):
		return m.cycleRightTab()

	case key.Matches(msg, m.keys.Refresh):
		return m, loadWorktrees(m.repoDir)

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		return m.openShell()

	case key.Matches(msg, m.keys.Create):
		return m.openCreateSourceModal()

	case key.Matches(msg, m.keys.ViewProcs):
		return m.toggleProcsTab()

	case key.Matches(msg, m.keys.LogTab):
		return m.openLogTab()

	case key.Matches(msg, m.keys.PRTab):
		return m.openPRTab()

	case key.Matches(msg, m.keys.DiffTab):
		return m.openDiffTab()

	case key.Matches(msg, m.keys.Sort):
		m.sort = (m.sort + 1) % 5
		m.rebuildItems()
		return m, nil

	case key.Matches(msg, m.keys.Fetch):
		if wt, ok := m.selectedWorktree(); ok {
			m.status = "fetching…"
			return m, gitFetch(wt.Path)
		}
		return m, nil

	case key.Matches(msg, m.keys.Update):
		return m.openUpdateBaseModal()

	case key.Matches(msg, m.keys.CreatePR):
		return m.openCreatePR()

	case key.Matches(msg, m.keys.BulkPrune):
		return m.openBulkPruneModal()

	case key.Matches(msg, m.keys.CopyFile):
		return m.openCopyModal()

	case key.Matches(msg, m.keys.Scripts):
		if wt, ok := m.selectedWorktree(); ok {
			return m, loadScripts(wt.Path)
		}
		return m, nil

	case key.Matches(msg, m.keys.Aliases):
		return m.openAliasModal()

	case key.Matches(msg, m.keys.Pull):
		if wt, ok := m.selectedWorktree(); ok {
			m.status = "pulling…"
			return m, gitPull(wt.Path)
		}
		return m, nil

	case key.Matches(msg, m.keys.Push):
		if wt, ok := m.selectedWorktree(); ok {
			m.status = "pushing…"
			return m, gitPush(wt.Path)
		}
		return m, nil

	case key.Matches(msg, m.keys.Commit):
		return m.openCommitModal()

	case key.Matches(msg, m.keys.Rebase):
		return m.openRebaseModal()

	case key.Matches(msg, m.keys.Prune):
		return m.openPruneModal()
	}

	return m.forwardToPane(msg)
}

// forwardToPane sends a message to whichever pane is focused.
func (m Model) forwardToPane(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focus == focusList {
		prevPath := selectedPath(m.list)
		m.list, cmd = m.list.Update(msg)
		// Reload the active tab's content when the highlighted worktree changes.
		if newPath := selectedPath(m.list); newPath != "" && newPath != prevPath {
			return m, tea.Batch(cmd, m.reloadRightPane())
		}
		return m, cmd
	}
	m.term, cmd = m.term.Update(msg)
	return m, cmd
}

func selectedPath(l worktreelist.Model) string {
	if it, ok := l.Selected(); ok {
		return it.WT.Path
	}
	return ""
}

func (m *Model) toggleFocus() {
	if m.focus == focusList {
		m.focus = focusTerminal
		m.list.Blur()
		m.term.Focus()
	} else {
		m.focus = focusList
		m.term.Blur()
		m.list.Focus()
	}
}

// --- Modal openers ---

func (m Model) openShell() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	c := coreexec.ShellCmd(wt.Path)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return opDoneMsg{label: "shell", err: err}
	})
}

func (m Model) openCopyModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	if wt.IsMain {
		m.status = "select a target worktree (not main) to copy into"
		return m, nil
	}
	files := m.fileIndex
	rank := func(p string) int { return m.state.CopyRank(p) }
	filter := func(q string) []string { return fs.Search(files, q, rank) }
	modal := modals.NewFuzzy(modals.KindCopyFile, "Copy file to worktree", filter, fs.Search(files, "", rank))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// addAliasLabel is the sentinel menu entry that starts the new-alias flow.
const addAliasLabel = "＋ new alias"

func (m Model) openAliasModal() (tea.Model, tea.Cmd) {
	names := []string{addAliasLabel}
	for _, a := range append(m.cfg.Aliases, m.state.SortedAliases()...) {
		names = append(names, a.Name)
	}
	modal := modals.NewSelect(modals.KindAliases, "Aliases (enter to run)", names)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

func (m Model) openCommitModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	// Fetch the status first; the modal opens when commitPreviewMsg arrives.
	return m, loadCommitPreview(wt.Path)
}

// onCommitPreview opens the commit modal seeded with the working-tree status.
func (m Model) onCommitPreview(msg commitPreviewMsg) (tea.Model, tea.Cmd) {
	body := msg.status
	switch {
	case msg.err != nil:
		body = "(status unavailable: " + msg.err.Error() + ")"
	case strings.TrimSpace(body) == "":
		body = "(working tree clean)"
	}
	modal := modals.NewInput(modals.KindCommit, "Commit message", "describe your change")
	modal.SetBody(body)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

func (m Model) openRebaseModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	m.status = "loading branches…"
	return m, loadBranches(branchesForRebase, wt.Path)
}

// onBranches opens the branch select modal for the flow that requested the
// list once the branches arrive.
func (m Model) onBranches(msg branchesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "branches: " + msg.err.Error()
		return m, nil
	}
	var modal modals.Model
	switch msg.kind {
	case branchesForRebase:
		modal = modals.NewSelect(modals.KindBranches, "Rebase onto", msg.branches)
	case branchesForCreateExisting:
		modal = modals.NewSelect(modals.KindCreateExisting, "Existing branch", msg.branches)
	default:
		return m, nil
	}
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// Create-source option labels.
const (
	createSourceNew      = "New branch"
	createSourceExisting = "Existing branch"
	createSourcePR       = "Pull request"
)

func (m Model) openCreateSourceModal() (tea.Model, tea.Cmd) {
	modal := modals.NewSelect(modals.KindCreateSource, "Create worktree from",
		[]string{createSourceNew, createSourceExisting, createSourcePR})
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// toggleProcsTab switches the right pane between the Git Log and Processes tabs.
// Selecting Processes moves focus to the right pane.
func (m Model) toggleProcsTab() (tea.Model, tea.Cmd) {
	if m.rightTab == tabProcs {
		m.rightTab = tabLog
		m.term.SetTitle("Git Log")
		m.term.SetFollow(false)
		m.term.SetContent(m.logContent)
		return m, nil
	}
	m.rightTab = tabProcs
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	m.refreshProcPane()
	return m, nil
}

// openLogTab switches the right pane to the Git Log tab for the selection.
func (m Model) openLogTab() (tea.Model, tea.Cmd) {
	m.rightTab = tabLog
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	m.term.SetTitle("Git Log")
	wt, ok := m.selectedWorktree()
	if !ok {
		m.term.SetFollow(false)
		m.term.SetContent(m.logContent)
		return m, nil
	}
	return m, loadLog(wt.Path)
}

// cycleRightTab advances to the next visible right-pane tab, focusing the right
// pane and loading the tab's content.
func (m Model) cycleRightTab() (tea.Model, tea.Cmd) {
	tabs := m.visibleTabs()
	if len(tabs) == 0 {
		return m, nil
	}
	idx := 0
	for i, t := range tabs {
		if t == m.rightTab {
			idx = i
			break
		}
	}
	m.rightTab = tabs[(idx+1)%len(tabs)]
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	cmd := m.reloadRightPane()
	return m, cmd
}

// tabVisible reports whether tab t is currently available for the selection.
func (m Model) tabVisible(t rightTab) bool {
	
	for _, v := range m.visibleTabs() {
		if v == t {
			return true
		}
	}
	return false
}

// reloadRightPane refreshes the active tab's content for the current selection,
// falling back to the Git Log tab if the active tab is no longer visible.
func (m *Model) reloadRightPane() tea.Cmd {
	if !m.tabVisible(m.rightTab) {
		m.rightTab = tabLog
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return nil
	}
	switch m.rightTab {
	case tabPR:
		m.refreshPRPane()
		if n, isPR := m.prForBranch(wt.Branch); isPR {
			return loadPRDetail(m.repoDir, n)
		}
		return nil
	case tabDiff:
		return m.enterDiff(wt)
	case tabProcs:
		m.refreshProcPane()
		return nil
	default:
		m.term.SetTitle("Git Log")
		return loadLog(wt.Path)
	}
}

// openPRTab focuses the PR detail tab for the selected worktree's PR.
func (m Model) openPRTab() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	n, isPR := m.prForBranch(wt.Branch)
	if !isPR {
		m.status = "no PR connected to this worktree"
		return m, nil
	}
	m.rightTab = tabPR
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	m.refreshPRPane()
	return m, loadPRDetail(m.repoDir, n)
}

// openDiffTab focuses the diff-vs-base tab for the selected worktree.
func (m Model) openDiffTab() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	if wt.IsMain {
		m.status = "diff vs base is unavailable on the main worktree"
		return m, nil
	}
	m.rightTab = tabDiff
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	cmd := m.enterDiff(wt)
	return m, cmd
}

// enterDiff shows the diff tab for wt, resetting per-file state when the diff
// belongs to a different worktree than the one currently cached.
func (m *Model) enterDiff(wt git.Worktree) tea.Cmd {
	m.term.SetTitle("Diff · " + wt.Branch)
	base := m.upstreamFor(wt.Branch)
	if m.diffPath != wt.Path {
		m.diffPath = wt.Path
		m.diffBase = base
		m.diffFiles = nil
		m.diffCursor = 0
		m.diffFileContent = map[string]string{}
		m.term.SetFollow(false)
		m.term.SetContent("(loading diff…)")
	} else {
		m.refreshDiffPane()
	}
	return loadDiff(wt.Path, base)
}

// onDiff records the changed-file list and renders the diff tab.
func (m Model) onDiff(msg diffMsg) (tea.Model, tea.Cmd) {
	if msg.path != m.diffPath {
		return m, nil // stale (selection moved on)
	}
	if msg.err != nil {
		m.diffFiles = nil
		if m.rightTab == tabDiff {
			m.term.SetTitle("Diff")
			m.term.SetContent("(diff unavailable: " + msg.err.Error() + ")")
		}
		return m, nil
	}
	m.diffFiles = msg.files
	if m.diffCursor >= len(m.diffFiles) {
		m.diffCursor = 0
	}
	if m.rightTab == tabDiff {
		m.refreshDiffPane()
	}
	return m, nil
}

// refreshDiffPane renders the changed-file list into the viewport.
func (m *Model) refreshDiffPane() {
	m.term.SetFollow(false)
	m.term.SetContent(m.renderDiff())
}

// diffTabKey handles navigation/expand keys while the Diff tab is focused.
func (m Model) diffTabKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if len(m.diffFiles) == 0 {
		return false, m, nil
	}
	switch msg.String() {
	case "up", "ctrl+k", "k":
		if m.diffCursor > 0 {
			m.diffCursor--
		}
		m.refreshDiffPane()
		m.term.EnsureVisible(diffHeaderLines + m.diffCursor)
		return true, m, nil
	case "down", "ctrl+j", "j":
		if m.diffCursor < len(m.diffFiles)-1 {
			m.diffCursor++
		}
		m.refreshDiffPane()
		m.term.EnsureVisible(diffHeaderLines + m.diffCursor)
		return true, m, nil
	case "enter", " ":
		f := m.diffFiles[m.diffCursor].Path
		content := "(loading…)"
		if c, ok := m.diffFileContent[f]; ok {
			content = c
		}
		modal := modals.NewScroll(modals.KindDiffFile, "Diff · "+f, content)
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		m.diffModalFile = f
		if _, cached := m.diffFileContent[f]; !cached {
			return true, m, loadFileDiff(m.diffPath, m.diffBase, f)
		}
		return true, m, nil
	}
	return false, m, nil
}

// refreshPRPane renders the PR detail tab from the cached PRDetail + checks.
func (m *Model) refreshPRPane() {
	m.term.SetTitle("PR")
	m.term.SetFollow(false)
	wt, ok := m.selectedWorktree()
	if !ok {
		m.term.SetContent("(no worktree selected)")
		return
	}
	n, isPR := m.prForBranch(wt.Branch)
	if !isPR {
		m.term.SetContent("(no PR connected)")
		return
	}
	d, ok := m.prDetail[n]
	if !ok {
		if m.prPaneErr != "" {
			m.term.SetContent("PR #" + strconv.Itoa(n) + ": " + m.prPaneErr)
		} else {
			m.term.SetContent("(loading PR #" + strconv.Itoa(n) + "…)")
		}
		return
	}
	m.term.SetContent(m.renderPRDetail(d, m.prChecks[n]))
}

// prTabKey handles PR-action keys while the PR tab is focused. The bool reports
// whether the key was consumed.
func (m Model) prTabKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return false, m, nil
	}
	n, isPR := m.prForBranch(wt.Branch)
	if !isPR {
		return false, m, nil
	}

	switch msg.String() {
	case "a": // approve
		modal := modals.NewInput(modals.KindReviewApprove, "Approve PR #"+strconv.Itoa(n), "optional comment")
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return true, m, nil
	case "c": // request changes
		modal := modals.NewInput(modals.KindReviewChanges, "Request changes on PR #"+strconv.Itoa(n), "what needs changing")
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return true, m, nil
	case "m": // merge (choose strategy)
		modal := modals.NewSelect(modals.KindMergeStrategy, "Merge PR #"+strconv.Itoa(n)+" via", []string{"merge", "squash", "rebase"})
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return true, m, nil
	case "x": // close
		title := "Close PR #" + strconv.Itoa(n) + "?"
		nm, cmd := m.confirm(title, "", prClose(m.repoDir, n))
		return true, nm, cmd
	case "O": // reopen
		return true, m, prReopen(m.repoDir, n)
	case "y": // ready for review
		return true, m, prReady(m.repoDir, n)
	case "enter", " ": // toggle description
		m.prExpandDesc = !m.prExpandDesc
		m.refreshPRPane()
		return true, m, nil
	case "t": // toggle commits
		m.prExpandCommits = !m.prExpandCommits
		m.refreshPRPane()
		return true, m, nil
	}
	return false, m, nil
}

// confirm runs action after a yes/no prompt, or immediately when confirmation is
// disabled in config.
func (m Model) confirm(title, body string, action tea.Cmd) (tea.Model, tea.Cmd) {
	if !m.cfg.ConfirmDestructive {
		return m, action
	}
	m.pendingConfirm = &confirmState{action: action}
	modal := modals.NewConfirm(modals.KindConfirm, title)
	if body != "" {
		modal.SetBody(body)
	}
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// openUpdateBaseModal asks whether to rebase or merge the base into the branch.
func (m Model) openUpdateBaseModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok || wt.IsMain {
		return m, nil
	}
	modal := modals.NewSelect(modals.KindUpdateBase, "Update from "+config.BaseBranch(m.upstreamFor(wt.Branch))+" via", []string{"rebase", "merge"})
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// openCreatePR starts the create-pull-request flow for the selected worktree.
func (m Model) openCreatePR() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok || wt.IsMain {
		m.status = "select a feature worktree to open a PR"
		return m, nil
	}
	if _, isPR := m.prForBranch(wt.Branch); isPR {
		m.status = "this worktree already has a PR"
		return m, nil
	}
	modal := modals.NewInput(modals.KindPRTitle, "New PR title", wt.Branch)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// openBulkPruneModal kicks off an async scan for merged worktrees; the confirm
// appears when the candidates arrive (onPruneCandidates).
func (m Model) openBulkPruneModal() (tea.Model, tea.Cmd) {
	m.status = "scanning for merged worktrees…"
	return m, loadPruneCandidates(m.repoDir, config.BaseBranch(m.cfg.Upstream))
}

// onPruneCandidates filters the scan result against live model state and
// confirms removal of the merged targets.
func (m Model) onPruneCandidates(msg pruneCandidatesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "prune scan: " + msg.err.Error()
		return m, nil
	}
	targets := m.mergedTargetsFrom(msg.merged)
	if len(targets) == 0 {
		m.status = "no merged worktrees to prune"
		return m, nil
	}
	var b strings.Builder
	for _, t := range targets {
		b.WriteString("• " + t.branch + "\n")
	}
	action := bulkPrune(m.repoDir, targets, m.cfg.DeleteHooks())
	return m.confirm(fmt.Sprintf("Prune %d merged worktree(s)?", len(targets)), strings.TrimRight(b.String(), "\n"), action)
}

// mergedTargetsFrom returns non-main, clean worktrees whose branch appears in
// merged (the union of merged PR heads and locally-merged branches).
func (m Model) mergedTargetsFrom(merged map[string]bool) []pruneTarget {
	var targets []pruneTarget
	for _, t := range m.worktrees {
		if t.IsMain || t.Branch == "" || t.Branch == "(detached)" {
			continue
		}
		if m.dirty[t.Path] {
			continue
		}
		if !merged[t.Branch] {
			continue
		}
		prNum, _ := m.prForBranch(t.Branch)
		targets = append(targets, pruneTarget{
			path: t.Path, branch: t.Branch, upstream: m.upstreamFor(t.Branch), prNumber: prNum,
		})
	}
	return targets
}

// onPRCreated reports the outcome of creating a PR and refreshes the PR map.
func (m Model) onPRCreated(msg prCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "create PR: " + msg.err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("opened PR #%d", msg.number)
	return m, fetchPRs(m.repoDir)
}

// procTabKey handles process-control keys while the Processes tab is focused.
// The bool reports whether the key was consumed.
func (m Model) procTabKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return false, m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Kill):
		if id, ok := m.selectedProcID(wt.Path); ok {
			m.procs.KillByID(wt.Path, id)
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.Restart):
		if id, ok := m.selectedProcID(wt.Path); ok {
			if p, err := m.procs.Restart(wt.Path, id); err == nil {
				m.activeProc[wt.Path] = p.ID
			} else {
				m.status = "restart: " + err.Error()
			}
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.Prune): // x removes a finished process
		if id, ok := m.selectedProcID(wt.Path); ok {
			m.procs.Remove(wt.Path, id)
			if p, ok := m.procs.Latest(wt.Path); ok {
				m.activeProc[wt.Path] = p.ID
			} else {
				delete(m.activeProc, wt.Path)
			}
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.Create): // n starts a new command
		return true, m, loadScripts(wt.Path)

	case msg.String() == "up" || msg.String() == "ctrl+k":
		m.moveProcSel(wt.Path, -1)
		m.refreshProcPane()
		return true, m, nil

	case msg.String() == "down" || msg.String() == "ctrl+j":
		m.moveProcSel(wt.Path, 1)
		m.refreshProcPane()
		return true, m, nil
	}

	return false, m, nil
}

func (m Model) onPRs(msg prsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = "PRs: " + msg.err.Error()
		return m, nil
	}
	if len(msg.prs) == 0 {
		m.status = "no open pull requests"
		return m, nil
	}
	m.prs = msg.prs
	items := make([]string, len(msg.prs))
	for i, pr := range msg.prs {
		items[i] = fmt.Sprintf("#%d %s", pr.Number, pr.Title)
	}
	modal := modals.NewSelect(modals.KindCreatePR, "Checkout pull request", items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

func (m Model) openPruneModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	if wt.IsMain {
		m.status = "cannot prune the main worktree"
		return m, nil
	}

	prNum, isPR := m.prForBranch(wt.Branch)
	if pr, ok := m.prByBranch[wt.Branch]; ok && pr.State != gh.StateOpen {
		isPR = false // already merged or closed: nothing left to merge
	}
	mergeLabel := fmt.Sprintf("merge PR #%d to %s", prNum, config.BaseBranch(m.upstreamFor(wt.Branch)))

	steps := []string{}
	if len(m.cfg.DeleteHooks()) > 0 {
		steps = append(steps, "run on_worktree_delete")
	}
	steps = append(steps, "delete worktree", "delete branch")

	modal := modals.NewPrune(modals.KindPrune, "Prune worktree "+wt.Branch+"?", mergeLabel, steps, isPR)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onModalSubmit routes a confirmed modal value to the right core operation.
func (m Model) onModalSubmit(msg modals.SubmitMsg) (tea.Model, tea.Cmd) {
	m.modal = nil

	// These kinds do not require an existing selection.
	switch msg.Kind {
	case modals.KindConfirm:
		if m.pendingConfirm != nil {
			action := m.pendingConfirm.action
			m.pendingConfirm = nil
			return m, action
		}
		return m, nil

	case modals.KindPRTitle:
		if msg.Value == "" {
			m.status = "empty PR title"
			return m, nil
		}
		m.pendingPRTitle = msg.Value
		modal := modals.NewInput(modals.KindPRBody, "PR description (optional)", "what and why")
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return m, nil

	case modals.KindCreateSource:
		return m.onCreateSource(msg.Value)
	case modals.KindCreateNew:
		if msg.Value == "" {
			m.status = "empty branch name"
			return m, nil
		}
		m.status = "creating " + msg.Value
		return m, createWorktree(m.repoDir, "new", msg.Value, 0, m.cfg)
	case modals.KindCreateExisting:
		m.status = "creating worktree for " + msg.Value
		return m, createWorktree(m.repoDir, "existing", msg.Value, 0, m.cfg)
	case modals.KindCreatePR:
		num := parseLeadingNum(msg.Value)
		m.status = fmt.Sprintf("creating worktree for PR #%d", num)
		return m, createWorktree(m.repoDir, "pr", "", num, m.cfg)

	case modals.KindNewAlias:
		if msg.Value == "" {
			m.status = "empty alias name"
			return m, nil
		}
		m.pendingAlias = msg.Value
		modal := modals.NewInput(modals.KindNewAliasCmd, "Command for alias "+msg.Value, "npm run dev")
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return m, nil

	case modals.KindNewAliasCmd:
		if msg.Value == "" || m.pendingAlias == "" {
			m.status = "empty alias command"
			return m, nil
		}
		if err := m.state.AddAlias(config.Alias{Name: m.pendingAlias, Command: msg.Value}); err != nil {
			m.status = "add alias: " + err.Error()
		} else {
			m.status = "added alias " + m.pendingAlias
		}
		m.pendingAlias = ""
		return m, nil
	}

	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}

	switch msg.Kind {
	case modals.KindCopyFile:
		m.status = "copying " + msg.Value
		return m, copyFile(m.repoDir, wt.Path, msg.Value, m.state.RecordCopy)

	case modals.KindScripts:
		if msg.Value == runCommandLabel {
			modal := modals.NewInput(modals.KindRunCommand, "Run command in "+wt.Branch, "zed .")
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			return m, nil
		}
		cmd := m.scriptRun[msg.Value]
		if cmd == "" {
			cmd = msg.Value // fallback: run the entry literally
		}
		return m.spawn(wt.Path, msg.Value, cmd), nil

	case modals.KindRunCommand:
		if msg.Value == "" {
			m.status = "empty command"
			return m, nil
		}
		return m.spawn(wt.Path, msg.Value, msg.Value), nil

	case modals.KindAliases:
		if msg.Value == addAliasLabel {
			modal := modals.NewInput(modals.KindNewAlias, "New alias name", "dev")
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			return m, nil
		}
		if cmd := m.aliasCommand(msg.Value); cmd != "" {
			prNum, _ := m.prForBranch(wt.Branch)
			vars := config.HookVars(m.repoDir, wt.Path, wt.Branch, m.upstreamFor(wt.Branch), prNum)
			return m.spawn(wt.Path, msg.Value, coreexec.ExpandVars(cmd, vars)), nil
		}
		return m, nil

	case modals.KindBranches:
		c := git.RebaseCmd(wt.Path, msg.Value)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return opDoneMsg{label: "rebase", err: err}
		})

	case modals.KindCommit:
		if msg.Value == "" {
			m.status = "empty commit message"
			return m, nil
		}
		m.status = "committing…"
		return m, gitCommit(wt.Path, msg.Value)

	case modals.KindPrune:
		merge := strings.Contains(msg.Value, "merge")
		force := strings.Contains(msg.Value, "force")
		prNum, _ := m.prForBranch(wt.Branch)
		if merge {
			m.status = fmt.Sprintf("merging PR #%d then pruning %s", prNum, wt.Branch)
		} else {
			m.status = "pruning " + wt.Branch
		}
		return m, pruneWorktree(m.repoDir, wt.Path, wt.Branch, m.upstreamFor(wt.Branch), m.cfg.DeleteHooks(), merge, force, prNum)

	case modals.KindPRBody:
		base := config.BaseBranch(m.upstreamFor(wt.Branch))
		title := m.pendingPRTitle
		m.pendingPRTitle = ""
		m.status = "creating PR…"
		return m, createPR(m.repoDir, wt.Path, title, msg.Value, base, false)

	case modals.KindMergeStrategy:
		if n, isPR := m.prForBranch(wt.Branch); isPR {
			strat := msg.Value
			m.status = "merging PR #" + strconv.Itoa(n) + " (" + strat + ")"
			return m.confirm("Merge PR #"+strconv.Itoa(n)+" via "+strat+"?", "", prMerge(m.repoDir, n, strat))
		}
		return m, nil

	case modals.KindReviewApprove:
		if n, isPR := m.prForBranch(wt.Branch); isPR {
			return m, prReview(m.repoDir, n, "approve", msg.Value)
		}
		return m, nil

	case modals.KindReviewChanges:
		if msg.Value == "" {
			m.status = "request-changes needs a comment"
			return m, nil
		}
		if n, isPR := m.prForBranch(wt.Branch); isPR {
			return m, prReview(m.repoDir, n, "request-changes", msg.Value)
		}
		return m, nil

	case modals.KindUpdateBase:
		base := m.upstreamFor(wt.Branch)
		var c = git.RebaseCmd(wt.Path, base)
		label := "rebase"
		if msg.Value == "merge" {
			c = git.MergeCmd(wt.Path, base)
			label = "merge base"
		}
		run := func() tea.Cmd {
			return tea.ExecProcess(c, func(err error) tea.Msg {
				return opDoneMsg{label: label, err: err}
			})
		}()
		return m.confirm("Update "+wt.Branch+" from "+config.BaseBranch(base)+" ("+msg.Value+")?", "", run)
	}
	return m, nil
}

// spawn starts a background process, marks it active for its worktree, and sets
// a status line.
func (m Model) spawn(path, label, command string) Model {
	p, err := m.procs.Spawn(path, label, command)
	if err != nil {
		m.status = "spawn: " + err.Error()
		return m
	}
	m.activeProc[path] = p.ID
	m.status = "running " + label
	return m
}

// onCreateSource advances the worktree-creation flow after the source type is
// chosen.
func (m Model) onCreateSource(choice string) (tea.Model, tea.Cmd) {
	switch choice {
	case createSourceNew:
		modal := modals.NewInput(modals.KindCreateNew, "New branch name", "feature/awesome")
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return m, nil
	case createSourceExisting:
		m.status = "loading branches…"
		return m, loadBranches(branchesForCreateExisting, m.repoDir)
	case createSourcePR:
		m.status = "loading pull requests…"
		return m, loadPRs(m.repoDir)
	}
	return m, nil
}

// parseLeadingNum extracts the integer following a leading '#', e.g. "#42 foo"
// yields 42. Returns 0 when absent.
func parseLeadingNum(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return n
}

// prForBranch resolves a branch's PR number, preferring one discovered via gh
// (any branch) and falling back to a "pr-N" branch name.
func (m Model) prForBranch(branch string) (int, bool) {
	if pr, ok := m.prByBranch[branch]; ok && pr.Number > 0 {
		return pr.Number, true
	}
	return prNumberOfBranch(branch)
}

// prNumberOfBranch extracts N from a "pr-N" branch name.
func prNumberOfBranch(branch string) (int, bool) {
	if after, found := strings.CutPrefix(branch, "pr-"); found {
		if n, err := strconv.Atoi(after); err == nil {
			return n, true
		}
	}
	return 0, false
}

// aliasCommand resolves an alias name to its command from config then state;
// user (state) aliases shadow config-file aliases of the same name.
func (m Model) aliasCommand(name string) string {
	command, ok := config.ResolveAlias(name, m.cfg.Aliases, m.state.SortedAliases())
	if !ok {
		return ""
	}
	return command
}
