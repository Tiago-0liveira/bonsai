package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
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
			m.term.SetContent(m.logContent)
		}
		return m, nil

	case scriptsMsg:
		return m.onScripts(msg)

	case commitPreviewMsg:
		return m.onCommitPreview(msg)

	case prsMsg:
		return m.onPRs(msg)

	case prMapMsg:
		return m.onPRMap(msg)

	case opDoneMsg:
		return m.onOpDone(msg)

	case procTickMsg:
		return m.onProcTick()

	case modals.SubmitMsg:
		return m.onModalSubmit(msg)

	case modals.CancelMsg:
		m.modal = nil
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

	// Fetch metrics per branch, connected PRs, and the log for the selection.
	cmds := []tea.Cmd{fetchPRs(m.repoDir)}
	for _, t := range msg.trees {
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

// onPRMap records open PRs keyed by head branch and redecorates rows. gh errors
// are silent — no gh, no badges. Because a PR carries its real base branch,
// ahead/behind metrics for PR-backed branches are recomputed here against that
// base rather than the global upstream.
func (m Model) onPRMap(msg prMapMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	m.prByBranch = make(map[string]gh.PR, len(msg.prs))
	for _, pr := range msg.prs {
		m.prByBranch[pr.Head] = pr
	}
	m.rebuildItems()

	var cmds []tea.Cmd
	for _, t := range m.worktrees {
		if t.Branch == "" || t.Branch == "(detached)" {
			continue
		}
		if pr, ok := m.prByBranch[t.Branch]; ok && pr.Base != "" {
			cmds = append(cmds, loadMetrics(t.Path, t.Branch, m.upstreamFor(t.Branch)))
		}
	}
	return m, tea.Batch(cmds...)
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
// per-path metrics and connected PRs, preserving the highlighted row.
func (m *Model) rebuildItems() {
	items := make([]worktreelist.Item, len(m.worktrees))
	for i, t := range m.worktrees {
		it := worktreelist.Item{WT: t}
		if mtr, ok := m.metrics[t.Path]; ok {
			it.Metrics = mtr
			it.HasMetrics = true
		}
		if pr, ok := m.prByBranch[t.Branch]; ok {
			it.PR = pr.Number
		}
		items[i] = it
	}
	m.list.SetItems(items)
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
	if m.rightTab == tabProcs {
		m.refreshProcPane()
	}
	return m, tickProc()
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

// refreshProcPane renders the Processes tab: a colored process list for the
// selected worktree followed by the selected process's output.
func (m *Model) refreshProcPane() {
	m.term.SetTitle("Processes")
	wt, ok := m.selectedWorktree()
	if !ok {
		m.term.SetContent("(no worktree selected)")
		return
	}
	procs := m.procs.List(wt.Path)
	if len(procs) == 0 {
		m.term.SetContent(procEmptyHint)
		return
	}

	sel, selOK := m.activeProcess(wt.Path)
	if selOK {
		m.activeProc[wt.Path] = sel.ID
	}

	var b strings.Builder
	for _, p := range procs {
		cursor := "  "
		if selOK && p.ID == sel.ID {
			cursor = procAccent.Render("› ")
		}
		b.WriteString(cursor + procLine(p) + "\n")
	}
	b.WriteString("\n" + procDim.Render(procHint) + "\n\n")
	if selOK {
		b.WriteString(sel.Output())
	}
	m.term.SetContent(b.String())
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

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.procs.KillAll()
		return m, tea.Quit

	case key.Matches(msg, m.keys.Tab), key.Matches(msg, m.keys.ShiftTab):
		m.toggleFocus()
		return m, nil

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
		// Reload the log when the highlighted worktree changes.
		if newPath := selectedPath(m.list); newPath != "" && newPath != prevPath {
			return m, tea.Batch(cmd, loadLog(newPath))
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
	branches, err := git.ListBranches(wt.Path)
	if err != nil {
		m.status = "branches: " + err.Error()
		return m, nil
	}
	modal := modals.NewSelect(modals.KindBranches, "Rebase onto", branches)
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

	// Worktree-creation kinds do not require an existing selection.
	switch msg.Kind {
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
		merge := msg.Value == "merge"
		prNum, _ := m.prForBranch(wt.Branch)
		if merge {
			m.status = fmt.Sprintf("merging PR #%d then pruning %s", prNum, wt.Branch)
		} else {
			m.status = "pruning " + wt.Branch
		}
		return m, pruneWorktree(m.repoDir, wt.Path, wt.Branch, m.upstreamFor(wt.Branch), m.cfg.DeleteHooks(), merge, prNum)
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
		branches, err := git.ListBranches(m.repoDir)
		if err != nil {
			m.status = "branches: " + err.Error()
			return m, nil
		}
		modal := modals.NewSelect(modals.KindCreateExisting, "Existing branch", branches)
		modal.SetSize(m.width, m.height)
		m.modal = &modal
		return m, nil
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
	if strings.HasPrefix(branch, "pr-") {
		if n, err := strconv.Atoi(strings.TrimPrefix(branch, "pr-")); err == nil {
			return n, true
		}
	}
	return 0, false
}

// aliasCommand resolves an alias name to its command from config then state.
func (m Model) aliasCommand(name string) string {
	for _, a := range m.cfg.Aliases {
		if a.Name == name {
			return a.Command
		}
	}
	for _, a := range m.state.Aliases {
		if a.Name == name {
			return a.Command
		}
	}
	return ""
}
