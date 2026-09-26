package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	corenotify "github.com/Tiago-0liveira/bonsai/internal/core/notify"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Update routes messages: layout, data results, the process tick, an active
// modal, or global keys.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The right pane's viewport height depends on the active tab (the Processes
	// footer eats into it), so re-derive it before any message — scroll keys in
	// particular are computed against the viewport's own height and go nowhere
	// when it disagrees with the area on screen.
	if m.ready {
		m.layout()
	}
	switch msg := msg.(type) {
	case updateAvailableMsg:
		m.availableUpdate = msg.release
		return m, nil
	case updateInstalledMsg:
		m.updating = false
		m.availableUpdate.Tag = ""
		if msg.err != nil {
			m.err = fmt.Errorf("update failed: %w", msg.err)
		} else {
			m.status = "Updated to " + msg.tag + ". Restart Bonsai to use it."
		}
		return m, nil
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

	case statusMsg:
		m.statuses[msg.path] = msg.summary
		m.rebuildItems()
		return m, nil

	case activityMsg:
		m.lastCommit[msg.path] = msg.unix
		m.rebuildItems()
		return m, nil

	case diffMsg:
		return m.onDiff(msg)

	case inspectorMsg:
		return m.onInspector(msg)

	case runsMsg:
		return m.onRuns(msg)

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

	case configSavedMsg:
		return m.onConfigSaved(msg)

	case modals.SubmitMsg:
		return m.onModalSubmit(msg)

	case modals.MultiSubmitMsg:
		switch msg.Kind {
		case modals.KindQuit:
			return m.onQuitSubmit(msg.Selected)
		case modals.KindMultiView:
			return m.onMultiViewSubmit(msg.Selected)
		}
		m.modal = nil
		return m, nil

	case modals.CancelMsg:
		m.modal = nil
		m.pendingConfirm = nil
		m.pendingCfg = nil
		m.pendingCfgSub = ""
		return m, nil

	case prefs.SaveMsg:
		return m.applyPrefs(msg)

	case prefs.CloseMsg:
		m.prefs = nil
		return m, nil

	case prefs.JumpMsg:
		m.prefs = nil
		if msg.Target == "aliases" {
			return m.openAliasModal()
		}
		if msg.Target == "editor" {
			return m.openPrefEditorModal()
		}
		return m, nil

	case tea.MouseMsg:
		return m.onMouse(msg)

	case tea.KeyMsg:
		if m.updatePromptVisible() && msg.String() != "ctrl+c" {
			switch msg.String() {
			case "u":
				if !m.updating {
					m.updating = true
					return m, installUpdate(m.availableUpdate)
				}
			case "l", "esc":
				m.availableUpdate.Tag = ""
			}
			return m, nil
		}
		// An open overlay owns all key input.
		if m.prefs != nil {
			pm, cmd := m.prefs.Update(msg)
			m.prefs = &pm
			return m, cmd
		}
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

// onMouse scrolls the pane the pointer is over — the log on the right, the
// worktree list on the left — regardless of which one has keyboard focus, since
// aiming the wheel is how a mouse says "this one". Only wheel events are acted
// on: clicks and drags belong to the terminal's own text selection.
func (m Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mouseOff || m.prefs != nil || m.modal != nil {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
	default:
		return m, nil
	}
	var cmd tea.Cmd
	if leftW, _, _ := m.dims(); msg.X < leftW {
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	m.term, cmd = m.term.Update(msg)
	return m, cmd
}

func (m Model) onResize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	if m.prefs != nil {
		m.prefs.SetSize(msg.Width, msg.Height)
	}
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
	cmds := []tea.Cmd{fetchPRs(m.repoDir, m.ghCache, msg.force)}
	for _, t := range msg.trees {
		cmds = append(cmds, loadStatus(t.Path), loadActivity(t.Path))
		if t.Branch != "" && t.Branch != "(detached)" {
			cmds = append(cmds, loadMetrics(t.Path, t.Branch, m.cfg.Upstream))
		}
	}
	if wt, ok := m.selectedWorktree(); ok {
		cmds = append(cmds, loadLog(wt.Path))
		if m.rightTab == tabInspect {
			cmds = append(cmds, m.enterInspect(wt))
		}
		if m.rightTab == tabChecks && wt.Branch != "" && wt.Branch != "(detached)" {
			cmds = append(cmds, m.enterChecks(wt))
		}
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
	m.prByNumber = make(map[int]gh.PR, len(msg.prs))
	for _, pr := range msg.prs {
		m.prByNumber[pr.Number] = pr
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
			cmds = append(cmds, loadChecks(m.repoDir, m.ghCache, t.Path, pr.Number, msg.force))
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
		pr, ok := m.prByBranch[t.Branch]
		if !ok {
			// "pr-N" worktree branches match no head ref; resolve by number.
			if strings.HasPrefix(t.Branch, "pr-") {
				if n, err := strconv.Atoi(strings.TrimPrefix(t.Branch, "pr-")); err == nil {
					pr, ok = m.prByNumber[n]
				}
			}
		}
		if ok {
			it.PR = pr.Number
			it.PRState = pr.State
			it.PRDraft = pr.IsDraft
			it.PRReview = pr.ReviewDecision
		}
		it.Status = m.statuses[t.Path]
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
		case sortDirty:
			return m.statuses[ta.Path].Total() > m.statuses[tb.Path].Total()
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
	m.scriptDir = msg.runDir
	m.scriptExec = msg.runExec

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
	// Refresh worktrees + log after any mutating op. Force a gh refetch so
	// badges reflect the just-completed op instead of a cached state.
	return m, loadWorktrees(m.repoDir, true)
}

func (m Model) onProcTick() (tea.Model, tea.Cmd) {
	m.procs.refresh()
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

// countRunning returns the number of active processes currently running or recovering in path.
func (m Model) countRunning(path string) int {
	n := 0
	for _, p := range m.procs.List(path) {
		if procstore.IsActive(p.Status) {
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
// active to done/failed/lost (once per process), if enabled in config.
func (m *Model) checkProcTransitions() {
	if !m.cfg.Notifications.Process {
		return
	}
	for _, p := range m.procs.All() {
		key := p.Worktree + "#" + strconv.Itoa(p.ID)
		st := p.Status
		prev := m.seenProcStatus[key]
		m.seenProcStatus[key] = st
		// "stopped" (user kill) is intentionally silent.
		if procstore.IsActive(prev) && (st == procstore.StatusDone || st == procstore.StatusFailed || st == procstore.StatusLost) {
			notify("bonsai: process "+st, fmt.Sprintf("#%d %s", p.ID, p.Label))
		}
	}
}

// notify is a thin wrapper over the core notify package.
func notify(title, body string) { corenotify.Send(title, body) }

// onPRTick re-checks PR states periodically so a merge done outside bonsai
// flips the row badge to MERGED without a restart or manual refresh. Fresh
// cached results are reused, so only every other tick (TTL > tick interval)
// actually shells out to gh.
func (m Model) onPRTick() (tea.Model, tea.Cmd) {
	return m, tea.Batch(fetchPRs(m.repoDir, m.ghCache, false), tickPRs())
}

// activeProcess returns the process currently selected for display under path,
// falling back to the most recently spawned one.
func (m Model) activeProcess(path string) (*procstore.Record, bool) {
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
// tail-follows so new lines stay visible. When two or more processes are picked
// via the multi-view modal, their outputs are merged instead (see
// mergedProcOutput). Either way, an active search/filter query (see
// updateProcSearch) is applied before display.
func (m *Model) refreshProcPane() {
	m.term.SetTitle("Processes")
	m.term.SetFollow(true)
	// The footer's height depends on the process list, so re-size the viewport
	// before loading content: a viewport taller than the area it is drawn into
	// clamps its own scroll offset to 0 and nothing scrolls (see layout).
	m.layout()
	wt, ok := m.selectedWorktree()
	if !ok {
		m.term.SetContent("")
		return
	}
	if len(m.procs.List(wt.Path)) == 0 {
		m.term.SetContent("")
		return
	}
	query := m.procSearch[wt.Path]
	if ids := m.procMultiSel[wt.Path]; len(ids) > 1 {
		m.term.SetContent(m.mergedProcOutput(ids, query))
		return
	}
	sel, selOK := m.activeProcess(wt.Path)
	if selOK {
		m.activeProc[wt.Path] = sel.ID
		out := applyProcSearch(m.procs.Output(sel.ID), query)
		m.term.SetContent(renderProcLog(out, m.termInnerWidth()))
		return
	}
	m.term.SetContent("")
}

// mergedProcOutput builds the multi-view merged log: each selected process's
// output, in selection order, with every line prefixed by a color-tagged label
// (docker-compose-style) so lines from different processes stay easy to tell
// apart. Colors come from assignProcColor (deterministic, not re-rolled per
// refresh); tags come from procTag (user override or truncated label).
func (m Model) mergedProcOutput(ids []int, query string) string {
	var b strings.Builder
	for _, id := range ids {
		out := applyProcSearch(m.procs.Output(id), query)
		if out == "" {
			continue
		}
		// Delimiter rules are narrowed by the tag column so they still end at the
		// pane edge once prefixed.
		out = renderProcLog(out, max(m.termInnerWidth()-procTagMaxLen-2, 8))
		style := lipgloss.NewStyle().Foreground(assignProcColor(id)).Bold(true)
		prefix := style.Render(fmt.Sprintf("%-*s│ ", procTagMaxLen, m.procTag(id)))
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			b.WriteString(prefix)
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// applyProcSearch filters text down to the lines containing query
// (case-insensitive) and highlights every match. It is plain substring
// matching, not a regex engine, so it stays cheap enough to re-run on the full
// log every tick while a process is still writing output. An empty query is a
// no-op (returns text unchanged, unfiltered). Run delimiters survive the filter
// unconditionally, so hits stay attributable to the run that produced them.
func applyProcSearch(text, query string) string {
	if query == "" {
		return text
	}
	q := strings.ToLower(query)
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isMarkerLine(line) {
			out = append(out, line)
			continue
		}
		low := strings.ToLower(line)
		if !strings.Contains(low, q) {
			continue
		}
		out = append(out, highlightMatches(line, low, q))
	}
	return strings.Join(out, "\n")
}

// highlightMatches wraps every case-insensitive occurrence of q in line (low
// is line, already lower-cased by the caller) in the search-hit style.
func highlightMatches(line, low, q string) string {
	var b strings.Builder
	i := 0
	for {
		idx := strings.Index(low[i:], q)
		if idx < 0 {
			b.WriteString(line[i:])
			break
		}
		start, end := i+idx, i+idx+len(q)
		b.WriteString(line[i:start])
		b.WriteString(procSearchHit.Render(line[start:end]))
		i = end
	}
	return b.String()
}

// updateProcSearch routes keys to the inline process-output search box while
// it has focus (see keys.ProcSearch), swallowing everything except esc/enter,
// which close the box (the query itself is kept either way — clear it by
// deleting the text before closing).
func (m Model) updateProcSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.procSearchActive = false
		m.procSearchInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.procSearchInput, cmd = m.procSearchInput.Update(msg)
	if wt, ok := m.selectedWorktree(); ok {
		m.procSearch[wt.Path] = m.procSearchInput.Value()
		m.refreshProcPane()
	}
	return m, cmd
}

// isProcScrollKey reports whether msg is one of the bubbles viewport's default
// scroll bindings that would otherwise be swallowed by a global single-letter
// action (fetch/diff-tab/checks-tab/update-base) before ever reaching the
// terminal pane. See the Processes-tab gate in onKey.
func isProcScrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "j", "k", "u", "d", "b", "f", "ctrl+u", "ctrl+d", "ctrl+b", "ctrl+f", "pgup", "pgdown", " ", "home", "end":
		return true
	}
	return false
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
	// On the Processes tab (focused), the inline search box (if active), then
	// process-control keys, win over the globals. Remaining viewport-scroll keys
	// (j/k/u/d/b/f/…) are forwarded to the log viewport explicitly, since several
	// of them are also global single-letter actions (fetch/diff-tab/checks-tab/
	// update-base) that would otherwise swallow them before they ever scroll.
	if m.rightTab == tabProcs && m.focus == focusTerminal {
		if m.procSearchActive {
			return m.updateProcSearch(msg)
		}
		if handled, nm, cmd := m.procTabKey(msg); handled {
			return nm, cmd
		}
		if isProcScrollKey(msg) {
			return m.forwardToPane(msg)
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
	case key.Matches(msg, m.keys.Palette):
		return m.openPalette()

	case key.Matches(msg, m.keys.ProcModal):
		return m.openProcModal()

	case key.Matches(msg, m.keys.Quit):
		return m.quit()

	case key.Matches(msg, m.keys.Tab):
		m.toggleFocus()
		return m, nil

	case key.Matches(msg, m.keys.ShiftTab):
		return m.cycleRightTab()

	case key.Matches(msg, m.keys.Refresh):
		return m, loadWorktrees(m.repoDir, true)

	case key.Matches(msg, m.keys.Help):
		return m.openKeymap()

	case key.Matches(msg, m.keys.Prefs):
		return m.openPrefs(false)

	case key.Matches(msg, m.keys.Enter):
		return m.openShell()

	case key.Matches(msg, m.keys.Editor):
		return m.openEditor()

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

	case key.Matches(msg, m.keys.InspectTab):
		return m.openInspectTab()

	case key.Matches(msg, m.keys.ChecksTab):
		return m.openChecksTab()

	case key.Matches(msg, m.keys.Sort):
		m.sort = (m.sort + 1) % 6
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

	case key.Matches(msg, m.keys.Yank):
		return m.openYankModal()

	case key.Matches(msg, m.keys.Scripts):
		if wt, ok := m.selectedWorktree(); ok {
			return m, loadScripts(wt.Path, m.cfg.PkgMgr.SearchDepth)
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

// openEditor suspends the TUI into the configured editor (personal override,
// then .bonsai.yaml's editor, then $VISUAL/$EDITOR, then vi) rooted at the
// selected worktree.
func (m Model) openEditor() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	c := coreexec.EditorCmd(wt.Path, m.effectiveEditor())
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return opDoneMsg{label: "editor", err: err}
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

// Yank-menu option labels.
const (
	yankPathLabel   = "copy worktree path"
	yankBranchLabel = "copy branch name"
)

// openYankModal offers copying the selection's path, branch name, or connected
// PR URL to the clipboard.
func (m Model) openYankModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	m.yankTargets = map[string]string{yankPathLabel: wt.Path}
	items := []string{yankPathLabel}
	// On the Processes tab the log is what the user is looking at, so offer it
	// first — terminal text selection cannot reach the scrolled-off part.
	if m.rightTab == tabProcs {
		if id, ok := m.selectedProcID(wt.Path); ok {
			label := fmt.Sprintf("copy process #%d output", id)
			m.yankTargets[label] = m.plainProcOutput(id)
			items = append([]string{label}, items...)
		}
	}
	if wt.Branch != "" && wt.Branch != "(detached)" {
		m.yankTargets[yankBranchLabel] = wt.Branch
		items = append(items, yankBranchLabel)
		if pr, ok := m.prByBranch[wt.Branch]; ok && pr.URL != "" {
			label := fmt.Sprintf("copy PR URL (#%d)", pr.Number)
			m.yankTargets[label] = pr.URL
			items = append(items, label)
		}
	}
	modal := modals.NewSelect(modals.KindYank, "Yank", items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// plainProcOutput returns a process's full log as pasteable text: run delimiters
// become plain rules and every ANSI sequence the process emitted is stripped, so
// what lands on the clipboard is readable outside a terminal.
func (m Model) plainProcOutput(id int) string {
	lines := strings.Split(m.procs.Output(id), "\n")
	for i, line := range lines {
		if mk, ok := procstore.ParseMarker(line); ok {
			lines[i] = mk.Line(60)
			continue
		}
		lines[i] = ansi.Strip(line)
	}
	return strings.Join(lines, "\n")
}

// paletteEntry is one rendered command-palette row: the styled display string
// and the plain text used for filtering.
type paletteEntry struct {
	display string
	plain   string
}

// openPalette builds the command palette from live state. Each row shows its
// keybinding (if any) and the worktree/PR it would act on; rows that cannot run
// right now show the reason instead and stay inert on enter.
func (m Model) openPalette() (tea.Model, tea.Cmd) {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	m.paletteByLabel = map[string]paletteCmd{}

	// Entries grouped by section, preserving first-seen section order.
	var order []string
	bySection := map[string][]paletteEntry{}

	add := func(c paletteCmd) {
		plain, display := c.label, c.label
		ok, reason := true, ""
		if c.available != nil {
			ok, reason = c.available(m)
		}
		if !ok {
			plain += " (" + reason + ")"
			display += dim.Render("  (" + reason + ")")
		} else {
			if hint := c.binding.Help().Key; hint != "" {
				plain += " (" + hint + ")"
				display += dim.Render("  (" + hint + ")")
			}
			if c.scopeHint != nil {
				if scope := c.scopeHint(m); scope != "" {
					plain += " · " + scope
					display += dim.Render("  · " + scope)
				}
			}
		}
		if _, dup := m.paletteByLabel[display]; dup {
			return
		}
		m.paletteByLabel[display] = c
		sec := c.section
		if sec == "" {
			sec = "Other"
		}
		if _, seen := bySection[sec]; !seen {
			order = append(order, sec)
		}
		bySection[sec] = append(bySection[sec], paletteEntry{display: display, plain: plain})
	}
	for _, c := range m.paletteCommands() {
		add(c)
	}

	filter := func(q string) []modals.Section {
		q = strings.ToLower(strings.TrimSpace(q))
		out := make([]modals.Section, 0, len(order))
		for _, sec := range order {
			items := make([]string, 0, len(bySection[sec]))
			for _, e := range bySection[sec] {
				if q == "" || strings.Contains(strings.ToLower(e.plain), q) {
					items = append(items, e.display)
				}
			}
			out = append(out, modals.Section{Name: sec, Items: items})
		}
		return out
	}

	title := "Commands"
	if wt, ok := m.selectedWorktree(); ok && wt.Branch != "" {
		title += " · " + wt.Branch
	}
	modal := modals.NewSectionedFuzzy(modals.KindPalette, title, filter, filter(""))
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
		m.layout() // the Processes footer is gone: give its rows back to the viewport
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
	m.layout()
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
	m.layout() // the viewport's height is tab-dependent (Processes footer)
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
	case tabInspect:
		return m.enterInspect(wt)
	case tabChecks:
		return m.enterChecks(wt)
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
	m.layout()
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
	m.layout()
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

// onInspector records loaded inspector data, discarding stale results that
// belong to a worktree the cursor has since left.
func (m Model) onInspector(msg inspectorMsg) (tea.Model, tea.Cmd) {
	if msg.path != m.inspectPath {
		return m, nil // stale (selection moved on)
	}
	m.inspect = msg.data
	if m.rightTab == tabInspect {
		m.refreshInspectPane()
	}
	return m, nil
}

// openInspectTab focuses the inspector tab for the selected worktree.
func (m Model) openInspectTab() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	m.rightTab = tabInspect
	m.layout()
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	return m, m.enterInspect(wt)
}

// enterInspect shows the inspector for wt, showing cached data instantly when it
// belongs to the same worktree, and returns the load command.
func (m *Model) enterInspect(wt git.Worktree) tea.Cmd {
	m.term.SetTitle("Inspect · " + wt.Branch)
	base := ""
	if !wt.IsMain {
		base = m.upstreamFor(wt.Branch)
	}
	if m.inspectPath != wt.Path {
		m.inspectPath = wt.Path
		m.inspect = inspectorData{}
		m.term.SetFollow(false)
		m.term.SetContent("(loading…)")
	} else {
		m.refreshInspectPane()
	}
	return loadInspector(wt.Path, base)
}

// refreshInspectPane renders the cached inspector data into the viewport.
func (m *Model) refreshInspectPane() {
	m.term.SetFollow(false)
	m.term.SetContent(m.renderInspect())
}

// onRuns records branch workflow runs for the Checks tab, discarding stale
// results for a worktree the cursor has since left.
func (m Model) onRuns(msg runsMsg) (tea.Model, tea.Cmd) {
	if msg.path != m.ciPath {
		return m, nil // stale (selection moved on)
	}
	if msg.err != nil {
		m.ciErr = msg.err.Error()
		m.ciRuns = nil
	} else {
		m.ciErr = ""
		m.ciRuns = msg.runs
	}
	if m.rightTab == tabChecks {
		m.refreshChecksPane()
	}
	return m, nil
}

// openChecksTab focuses the CI Checks tab for the selected worktree's branch.
func (m Model) openChecksTab() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	if wt.Branch == "" || wt.Branch == "(detached)" {
		m.status = "checks need a branch — HEAD is detached"
		return m, nil
	}
	m.rightTab = tabChecks
	m.layout()
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	return m, m.enterChecks(wt)
}

// enterChecks shows the Checks tab for wt, keeping cached runs when they belong
// to the same worktree, and returns the load command.
func (m *Model) enterChecks(wt git.Worktree) tea.Cmd {
	m.term.SetTitle("Checks · " + wt.Branch)
	if m.ciPath != wt.Path {
		m.ciPath = wt.Path
		m.ciRuns = nil
		m.ciErr = ""
		m.term.SetFollow(false)
		m.term.SetContent("(loading…)")
	} else {
		m.refreshChecksPane()
	}
	return loadRuns(m.repoDir, wt.Path, wt.Branch)
}

// refreshChecksPane renders the cached workflow runs into the viewport.
func (m *Model) refreshChecksPane() {
	m.term.SetFollow(false)
	m.term.SetContent(m.renderChecks())
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
		if m.statuses[t.Path].Dirty() {
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
	return m, fetchPRs(m.repoDir, m.ghCache, true)
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
			m.procs.Kill(id)
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.Restart):
		if id, ok := m.selectedProcID(wt.Path); ok {
			if p, err := m.procs.Restart(id); err == nil {
				m.activeProc[wt.Path] = p.ID
			} else {
				m.status = "restart: " + err.Error()
			}
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.Prune): // x removes a finished process
		if id, ok := m.selectedProcID(wt.Path); ok {
			m.procs.Remove(id)
			if p, ok := m.procs.Latest(wt.Path); ok {
				m.activeProc[wt.Path] = p.ID
			} else {
				delete(m.activeProc, wt.Path)
			}
			m.refreshProcPane()
		}
		return true, m, nil

	case key.Matches(msg, m.keys.SetPolicy): // p opens the restart-policy picker
		if id, ok := m.selectedProcID(wt.Path); ok {
			nm, cmd := m.openPolicyModal(id)
			return true, nm, cmd
		}
		return true, m, nil

	case key.Matches(msg, m.keys.MultiView): // m picks processes to view together
		nm, cmd := m.openProcMultiViewModal()
		return true, nm, cmd

	case key.Matches(msg, m.keys.RenameProc): // L tags the selected process
		if id, ok := m.selectedProcID(wt.Path); ok {
			nm, cmd := m.openRenameProcModal(id)
			return true, nm, cmd
		}
		return true, m, nil

	case key.Matches(msg, m.keys.ProcSearch): // / opens the inline output search
		m.procSearchActive = true
		m.procSearchInput.SetValue(m.procSearch[wt.Path])
		m.procSearchInput.CursorEnd()
		m.procSearchInput.Focus()
		return true, m, nil

	case key.Matches(msg, m.keys.Create): // n starts a new command
		return true, m, loadScripts(wt.Path, m.cfg.PkgMgr.SearchDepth)

	case msg.String() == "g": // jump to the start of the log
		m.term.GotoTop()
		return true, m, nil

	case msg.String() == "G": // jump back to the live end and resume tailing
		m.term.GotoBottom()
		return true, m, nil

	case msg.String() == "up" || msg.String() == "ctrl+k":
		m.moveProcSel(wt.Path, -1)
		m.refreshProcPane()
		m.term.GotoBottom() // a different process: show its live tail, not the old offset
		return true, m, nil

	case msg.String() == "down" || msg.String() == "ctrl+j":
		m.moveProcSel(wt.Path, 1)
		m.refreshProcPane()
		m.term.GotoBottom() // a different process: show its live tail, not the old offset
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
	modal.SetMergeDefault(m.state.Prefs.PruneMerge)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onModalSubmit routes a confirmed modal value to the right core operation.
func (m Model) onModalSubmit(msg modals.SubmitMsg) (tea.Model, tea.Cmd) {
	m.modal = nil

	// These kinds do not require an existing selection.
	switch msg.Kind {
	case modals.KindKeymap:
		// Any selection in the key reference jumps to the key editor.
		return m.openPrefs(true)

	case modals.KindProcesses:
		return m.onProcModalSubmit(msg.Value)

	case modals.KindSetPolicy:
		return m.onPolicyModalSubmit(msg.Value)

	case modals.KindRenameProc:
		return m.onRenameProcSubmit(msg.Value)

	case modals.KindPalette:
		c, found := m.paletteByLabel[msg.Value]
		if !found {
			return m, nil
		}
		if c.available != nil {
			if ok, reason := c.available(m); !ok {
				m.status = reason
				return m, nil
			}
		}
		if c.run == nil {
			return m, nil
		}
		return c.run(m)

	case modals.KindConfirm:
		if m.pendingConfirm != nil {
			action := m.pendingConfirm.action
			m.pendingConfirm = nil
			return m, action
		}
		return m, nil

	case modals.KindConfigChoice:
		return m.onConfigChoice(msg.Value)

	case modals.KindConfigValue:
		return m.onConfigValue(msg.Value)

	case modals.KindConfigConfirm:
		return m.onConfigConfirm()

	case modals.KindPrefEditor:
		m.state.Prefs.Editor = msg.Value
		if err := m.state.Save(); err != nil {
			m.status = "editor: " + err.Error()
			return m, nil
		}
		if msg.Value == "" {
			m.status = "editor: cleared personal override"
		} else {
			m.status = "editor: " + msg.Value
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
	case modals.KindYank:
		if text := m.yankTargets[msg.Value]; text != "" {
			return m, copyToClipboard(text, "yank")
		}
		return m, nil

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
		if inv, ok := m.scriptExec[msg.Value]; ok {
			return m.spawnExec(wt.Path, msg.Value, inv), nil
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
	ownerPath, branch := m.worktreeOwner(path)
	p, err := m.procs.Spawn(path, branch, label, command)
	if err != nil {
		m.status = "spawn: " + err.Error()
		return m
	}
	m.activeProc[ownerPath] = p.ID
	m.status = "running " + label
	return m
}

func (m Model) spawnExec(ownerPath, label string, inv scriptInvocation) Model {
	_, branch := m.worktreeOwner(ownerPath)
	dir := inv.Dir
	if dir == "" {
		dir = ownerPath
	}
	p, err := m.procs.SpawnExec(ownerPath, branch, dir, label, inv.Program, inv.Args)
	if err != nil {
		m.status = "spawn: " + err.Error()
		return m
	}
	m.activeProc[ownerPath] = p.ID
	m.status = "running " + label
	return m
}

// branchForPath returns the branch containing path, including nested project
// directories inside a worktree.
func (m Model) branchForPath(path string) string {
	_, branch := m.worktreeOwner(path)
	return branch
}

func (m Model) worktreeOwner(path string) (string, string) {
	path = filepath.Clean(path)
	bestPath, bestBranch := path, ""
	bestLen := -1
	for _, t := range m.worktrees {
		root := filepath.Clean(t.Path)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && len(root) > bestLen {
			bestPath, bestBranch, bestLen = t.Path, t.Branch, len(root)
		}
	}
	return bestPath, bestBranch
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
