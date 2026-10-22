package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

type wtSub int

const (
	wtCheckouts wtSub = iota
	wtProcesses
	wtBranches
	wtPRs
	wtSubCount
)

var wtSubNames = [wtSubCount]string{"Checkouts", "Processes", "Branches", "Pull requests"}

// worktreesState owns presentation only. Git/process data and the shared agent
// index remain on the root while the legacy route is supported (plan 12).
type worktreesState struct {
	started                   bool
	initCmd                   tea.Cmd
	sub                       wtSub
	menu                      bool
	branches                  []string
	branchLoaded              bool
	branchErr, prErr, readErr string
	selected                  [wtSubCount]string
	index                     [wtSubCount]int
	query                     [wtSubCount]string
	searching                 bool
	search                    textinput.Model
	before                    string
	generations               map[string]uint64
	// Documents retain their viewport when their tab/worktree is revisited.
	docs      map[string]wtDocument
	returning bool
	returnWS  workspace
}
type wtDocument struct {
	offset           int
	follow, atBottom bool
}
type wtRow struct {
	id, title, desc, path string
	procID, pr            int
	branch                string
}
type wtReadMsg struct {
	key    string
	gen    uint64
	msg    tea.Msg
	prompt bool
	path   string
}

const branchesForWorkspace branchesKind = 2

// worktreeRead orders each independent read. Older replies, including failures,
// cannot overwrite a newer result. The legacy route keeps its existing messages.
func (m *Model) worktreeRead(k string, cmd tea.Cmd) tea.Cmd {
	if m.layoutKind != LayoutRedesign {
		return cmd
	}
	if m.wt.generations == nil {
		m.wt.generations = map[string]uint64{}
	}
	m.wt.generations[k]++
	gen := m.wt.generations[k]
	return func() tea.Msg { return wtReadMsg{key: k, gen: gen, msg: cmd()} }
}
func (m *Model) readWorktrees(force bool) tea.Cmd {
	return m.worktreeRead("trees", loadWorktrees(m.repoDir, force))
}
func (m *Model) readLog(path string) tea.Cmd { return m.worktreeRead("log", loadLog(path)) }
func (m *Model) readBranches(kind branchesKind, path string) tea.Cmd {
	if kind != branchesForWorkspace {
		return m.worktreePromptRead(fmt.Sprintf("branches:%d", kind), loadBranches(kind, path))
	}
	return m.worktreeRead(fmt.Sprintf("branches:%d", kind), loadBranches(kind, path))
}

// Prompt reads are scoped to the checkout that requested them. A late result
// may warm data, but cannot open a mutation dialog for a different selection.
func (m *Model) worktreePromptRead(key string, cmd tea.Cmd) tea.Cmd {
	if m.layoutKind != LayoutRedesign {
		return cmd
	}
	read := m.worktreeRead(key, cmd)
	path := m.currentPath()
	return func() tea.Msg { msg := read().(wtReadMsg); msg.prompt, msg.path = true, path; return msg }
}

func (m Model) worktreesEnter() (Model, tea.Cmd) {
	m.list.SetEmbedded(true)
	m.layout()
	if m.wt.started {
		return m, nil
	}
	m.wt.started = true
	m.rightTab = tabInspect
	// Exactly one process tick and PR tick survive workspace round-trips. Session
	// reads/subscriptions are shared with Playground, rather than started again.
	var sessions tea.Cmd
	m, sessions = m.playgroundEnter()
	read := m.readWorktrees(false)
	return m, tea.Batch(read, indexFiles(m.repoDir), tickProc(), tickPRs(), sessions)
}
func (m Model) worktreesSetSub(sub wtSub) (Model, tea.Cmd) {
	m.worktreesRememberDocument()
	m.wt.sub = (sub + wtSubCount) % wtSubCount
	m.focus = focusList
	m.list.Focus()
	m.term.Blur()
	var cmd tea.Cmd
	switch m.wt.sub {
	case wtProcesses:
		m.rightTab = tabProcs
	case wtPRs:
		m.rightTab = tabPR
	default:
		m.rightTab = tabInspect
	}
	if m.wt.sub == wtBranches {
		cmd = m.readBranches(branchesForWorkspace, m.repoDir)
	}
	m.worktreesReconcile()
	var detail tea.Cmd
	if m.width >= 100 && (m.wt.sub == wtCheckouts || m.currentPath() != "") {
		detail = m.reloadRightPane()
	}
	return m, tea.Batch(cmd, detail)
}
func (m Model) worktreesRows() []wtRow {
	var rows []wtRow
	switch m.wt.sub {
	case wtProcesses:
		for _, w := range m.worktrees {
			for _, p := range m.procs.List(w.Path) {
				rows = append(rows, wtRow{id: strconv.Itoa(p.ID), title: fmt.Sprintf("#%d %s", p.ID, labelOf(p)), desc: p.Status + " · " + w.Branch, path: w.Path, procID: p.ID})
			}
		}
	case wtBranches:
		for _, b := range m.wt.branches {
			row := wtRow{id: b, title: b, branch: b, desc: "No checkout · Enter to create one"}
			for _, w := range m.worktrees {
				if w.Branch == b {
					row.path = w.Path
					row.desc = w.Path
					break
				}
			}
			rows = append(rows, row)
		}
	case wtPRs:
		for n, p := range m.prByNumber {
			row := wtRow{id: strconv.Itoa(n), title: fmt.Sprintf("#%d %s", n, p.Title), branch: p.Head, pr: n, desc: p.State + " · " + p.Head}
			if p.IsDraft {
				row.desc = "Draft · " + row.desc
			}
			for _, w := range m.worktrees {
				if w.Branch == p.Head || w.Branch == fmt.Sprintf("pr-%d", n) {
					row.path = w.Path
					break
				}
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].pr > rows[j].pr })
	}
	q := strings.ToLower(strings.TrimSpace(m.wt.query[m.wt.sub]))
	if q == "" {
		return rows
	}
	var filtered []wtRow
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.title+" "+r.desc), q) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
func (m Model) worktreesRow() (wtRow, bool) {
	for _, r := range m.worktreesRows() {
		if r.id == m.wt.selected[m.wt.sub] {
			return r, true
		}
	}
	return wtRow{}, false
}
func (m *Model) worktreesReconcile() {
	if m.wt.sub == wtCheckouts {
		return
	}
	rows := m.worktreesRows()
	s := m.wt.sub
	if len(rows) == 0 {
		m.wt.selected[s] = ""
		m.wt.index[s] = 0
		return
	}
	idx := min(m.wt.index[s], len(rows)-1)
	for i, r := range rows {
		if r.id == m.wt.selected[s] {
			idx = i
			break
		}
	}
	m.wt.index[s], m.wt.selected[s] = idx, rows[idx].id
	m.worktreesSelectRow(rows[idx])
}
func (m *Model) worktreesSelectRow(r wtRow) {
	if r.path == "" {
		return
	}
	m.list.SelectByPath(r.path)
	if r.procID > 0 {
		m.activeProc[r.path] = r.procID
	}
}
func (m Model) worktreesMove(delta int) (Model, tea.Cmd) {
	rows := m.worktreesRows()
	s := m.wt.sub
	if len(rows) == 0 {
		return m, nil
	}
	m.worktreesRememberDocument()
	idx := max(0, min(m.wt.index[s]+delta, len(rows)-1))
	m.wt.index[s], m.wt.selected[s] = idx, rows[idx].id
	m.worktreesSelectRow(rows[idx])
	if m.width >= 100 && rows[idx].path != "" {
		return m, m.reloadRightPane()
	}
	return m, nil
}
func (m Model) worktreesCapturing() bool {
	return m.workspace == wsWorktrees && (m.agents.overlay != nil || m.wt.searching || m.procSearchActive || m.list.SettingFilter())
}
func (m Model) worktreesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.agents.overlay != nil {
		return m.overlayUpdate(msg)
	}
	if m.list.SettingFilter() {
		return m.forwardToPane(msg)
	}
	if m.procSearchActive {
		return m.updateProcSearch(msg)
	}
	if m.wt.searching {
		switch msg.String() {
		case "esc":
			m.wt.query[m.wt.sub] = m.wt.before
			m.wt.searching = false
		case "enter":
			m.wt.searching = false
		default:
			var cmd tea.Cmd
			m.wt.search, cmd = m.wt.search.Update(msg)
			m.wt.query[m.wt.sub] = m.wt.search.Value()
			m.worktreesReconcile()
			return m, cmd
		}
		m.worktreesReconcile()
		return m, nil
	}
	k := msg.String()
	if k == "esc" {
		if m.focus == focusTerminal {
			m.worktreesRememberDocument()
			m.toggleFocus()
		} else if !m.wt.menu {
			m.wt.menu = true
		} else {
			m.shellFocus = shellNav
		}
		return m, nil
	}
	if m.wt.menu {
		switch k {
		case "enter", "down", "j":
			m.wt.menu = false
		case "tab", "right", "l":
			return m.worktreesSetSub(m.wt.sub + 1)
		case "shift+tab", "left", "h":
			return m.worktreesSetSub(m.wt.sub - 1)
		}
		return m, nil
	}
	if m.focus == focusList {
		switch {
		case key.Matches(msg, m.keys.Tab):
			return m.worktreesSetSub(m.wt.sub + 1)
		case key.Matches(msg, m.keys.ShiftTab):
			return m.worktreesSetSub(m.wt.sub - 1)
		case k == "enter":
			return m.worktreesInspect()
		case m.wt.sub != wtCheckouts && (k == "up" || k == "k"):
			return m.worktreesMove(-1)
		case m.wt.sub != wtCheckouts && (k == "down" || k == "j"):
			return m.worktreesMove(1)
		case m.wt.sub != wtCheckouts && key.Matches(msg, m.keys.Filter):
			m.wt.before = m.wt.query[m.wt.sub]
			m.wt.search = textinput.New()
			m.wt.search.SetValue(m.wt.before)
			m.wt.searching = true
			return m, m.wt.search.Focus()
		}
	}
	// An unchecked branch/PR must never act on the previously selected checkout.
	if m.wt.sub != wtCheckouts {
		if r, ok := m.worktreesRow(); !ok || r.path == "" {
			switch {
			case key.Matches(msg, m.keys.Create):
				return m.openCreateSourceModal()
			case key.Matches(msg, m.keys.Refresh):
				return m.worktreesRefresh()
			}
			return m, nil
		}
	}
	if m.wt.sub == wtProcesses && m.focus == focusList {
		switch {
		case key.Matches(msg, m.keys.Kill), key.Matches(msg, m.keys.Restart), key.Matches(msg, m.keys.Prune), key.Matches(msg, m.keys.SetPolicy), key.Matches(msg, m.keys.MultiView), key.Matches(msg, m.keys.RenameProc), key.Matches(msg, m.keys.Create):
			if handled, nm, cmd := m.procTabKey(msg); handled {
				next := nm.(Model)
				next.worktreesSyncProcess()
				return next, cmd
			}
		}
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.worktreesRefresh()
	}
	m.worktreesRememberDocument()
	oldKey := m.worktreesDocumentKey()
	nm, cmd := m.onKey(msg)
	next := nm.(Model)
	next.worktreesSyncProcess()
	next.layout()
	if next.worktreesDocumentKey() == oldKey {
		next.worktreesRememberDocument()
	}
	return next, cmd
}
func (m Model) worktreesRefresh() (tea.Model, tea.Cmd) {
	var branch tea.Cmd
	if m.wt.sub == wtBranches {
		branch = m.readBranches(branchesForWorkspace, m.repoDir)
	}
	read := m.readWorktrees(true)
	return m, tea.Batch(read, branch)
}
func (m Model) worktreesInspect() (tea.Model, tea.Cmd) {
	m.worktreesRememberDocument()
	if m.wt.sub != wtCheckouts {
		r, ok := m.worktreesRow()
		if !ok {
			return m, nil
		}
		if r.path == "" {
			var action tea.Cmd
			if r.pr > 0 {
				action = createWorktree(m.repoDir, "pr", "", r.pr, m.cfg)
			} else {
				action = createWorktree(m.repoDir, "existing", r.branch, 0, m.cfg)
			}
			// Creation is explicit even when destructive confirmations are disabled.
			m.pendingConfirm = &confirmState{action: action}
			d := modals.NewConfirm(modals.KindConfirm, "Create checkout for "+r.title+"?")
			branch := r.branch
			if r.pr > 0 {
				branch = fmt.Sprintf("pr-%d", r.pr)
			}
			d.SetBody(fmt.Sprintf("Create checkout at %s\nRun %d creation hook(s). Existing files and branches remain ownership-checked.", m.cfg.WorktreePath(m.repoDir, branch), len(m.cfg.CreateHooks())))
			d.SetSize(m.width, m.height)
			m.modal = &d
			return m, nil
		}
		m.worktreesSelectRow(r)
	}
	if _, ok := m.selectedWorktree(); !ok {
		return m, nil
	}
	var nm tea.Model
	var cmd tea.Cmd
	switch m.wt.sub {
	case wtProcesses:
		m.rightTab = tabProcs
		m.focus = focusTerminal
		m.refreshProcPane()
		nm = m
	case wtPRs:
		nm, cmd = m.openPRTab()
	default:
		nm, cmd = m.openInspectTab()
	}
	next := nm.(Model)
	next.wt.menu = false
	next.focus = focusTerminal
	next.list.Blur()
	next.term.Focus()
	next.layout()
	return next, cmd
}
func (m *Model) worktreesRememberDocument() {
	if m.layoutKind != LayoutRedesign {
		return
	}
	if m.wt.docs == nil {
		m.wt.docs = map[string]wtDocument{}
	}
	m.wt.docs[m.worktreesDocumentKey()] = wtDocument{offset: m.term.YOffset(), follow: m.term.Following(), atBottom: m.term.AtBottom()}
}
func (m Model) worktreesDocumentKey() string {
	if m.rightTab == tabProcs {
		return fmt.Sprintf("%s:%d:%d:%v", m.currentPath(), m.rightTab, m.activeProc[m.currentPath()], m.procMultiSel[m.currentPath()])
	}
	return fmt.Sprintf("%s:%d", m.currentPath(), m.rightTab)
}
func (m *Model) worktreesRestoreDocument() {
	if d, ok := m.wt.docs[m.worktreesDocumentKey()]; ok {
		m.term.SetFollow(d.follow)
		if d.follow && d.atBottom {
			m.term.GotoBottom()
		} else {
			m.term.SetYOffset(d.offset)
		}
	}
}

// Worktrees hands the exact existing session to Playground and observes it.
func (m Model) worktreesPlayground() (tea.Model, tea.Cmd) {
	w, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	s, _, ok := m.selectedSession(cleanPath(w.Path))
	if !ok {
		return m, nil
	}
	m.worktreesRememberDocument()
	m.wt.returning, m.wt.returnWS = true, wsPlayground
	m, cmd := m.selectWorkspace(wsPlayground)
	m.pg.sub, m.pg.all, m.pg.query, m.pg.group = pgSubSessions, true, "", false
	m.pg.sel = s.ID
	found := false
	for _, r := range m.pg.recs {
		if r.Session.ID == s.ID {
			found = true
		}
	}
	if !found {
		m.pg.recs = append(m.pg.recs, pgRec{Session: s, Branch: w.Branch, InTree: true})
	}
	m.playgroundReconcile()
	m.shellFocus = shellBody
	m, observe := m.pgObserve()
	return m, tea.Batch(cmd, observe)
}
func (m Model) worktreesAccounts(id agents.AccountID, tab accounts.Tab) (tea.Model, tea.Cmd) {
	m.worktreesRememberDocument()
	m.wt.returning, m.wt.returnWS = true, wsAccounts
	m, cmd := m.selectWorkspace(wsAccounts)
	m.shellFocus = shellBody
	if !m.acct.present {
		return m, cmd
	}
	am, show := m.acct.model.Show(id, tab)
	m.acct.model = am
	return m, tea.Batch(cmd, show)
}
func (m Model) worktreesReturn() (Model, tea.Cmd) {
	m.wt.returning = false
	m, cmd := m.selectWorkspace(wsWorktrees)
	m.shellFocus = shellBody
	m.worktreesRestoreDocument()
	return m, cmd
}

// worktreesMsg shares the domain handlers with the legacy route, without a
// second model, event loop, backend or set of background timers.
func (m Model) worktreesMsg(msg tea.Msg) (bool, tea.Model, tea.Cmd) {
	if !m.wt.started {
		switch msg.(type) {
		case worktreesMsg, metricsMsg, fileIndexMsg, logMsg, scriptsMsg, commitPreviewMsg, prsMsg, branchesMsg, pruneCandidatesMsg, prMapMsg, prDetailMsg, checksMsg, statusMsg, activityMsg, diffMsg, inspectorMsg, runsMsg, fileDiffMsg, prCreatedMsg, opDoneMsg, procTickMsg, prTickMsg:
			return true, m, nil
		}
	}
	switch msg.(type) {
	case launchSubmitMsg, launchStartedMsg, launchSavedMsg, agentStoppedMsg:
		_, nm, cmd := m.onAgentOverlayMsg(msg)
		return true, nm, cmd
	}
	if v, ok := msg.(usagestore.Result); ok {
		nm, root := m.onUsageResult(v)
		m = nm.(Model)
		var body tea.Cmd
		if m.acct.started {
			m.acct.model, body = m.acct.model.Update(v)
		}
		return true, m, tea.Batch(root, body)
	}
	switch v := msg.(type) {
	case branchesMsg:
		if v.kind == branchesForWorkspace {
			if v.err != nil {
				m.wt.branchErr = v.err.Error()
			} else {
				m.wt.branches = v.branches
				m.wt.branchLoaded = true
				m.wt.branchErr = ""
			}
			m.worktreesReconcile()
			var detail tea.Cmd
			if m.workspace == wsWorktrees && m.width >= 100 && m.wt.sub == wtBranches && m.currentPath() != "" {
				detail = m.reloadRightPane()
			}
			return true, m, detail
		}
	case worktreesMsg:
		if v.err != nil {
			m.wt.readErr = v.err.Error()
		} else {
			m.wt.readErr = ""
		}
	case prMapMsg:
		if v.err != nil {
			m.wt.prErr = v.err.Error()
		} else {
			m.wt.prErr = ""
		}
	case logMsg:
		if v.path != "" && v.path != m.currentPath() {
			return true, m, nil
		}
	}
	switch msg.(type) {
	case worktreesMsg, metricsMsg, fileIndexMsg, logMsg, scriptsMsg, commitPreviewMsg, prsMsg, branchesMsg, pruneCandidatesMsg, prMapMsg, prDetailMsg, checksMsg, statusMsg, activityMsg, diffMsg, inspectorMsg, runsMsg, fileDiffMsg, prCreatedMsg, opDoneMsg, procTickMsg, prTickMsg, modals.MultiSubmitMsg,
		agentAttachedMsg, agentRetryMsg, agentFramesMsg, agentInputMsg, agentInputSentMsg, agentTookMsg, agentResizeTickMsg, agentUsageTickMsg, paneAccountsMsg, paneTickMsg, usagestore.Observation:
		oldPath := m.currentPath()
		nm, cmd := m.update(msg)
		next := nm.(Model)
		if _, ok := msg.(modals.MultiSubmitMsg); ok {
			next.worktreesSyncProcess()
		}
		next.worktreesReconcile()
		if next.workspace == wsWorktrees && next.width >= 100 && next.currentPath() != oldPath && next.currentPath() != "" {
			cmd = tea.Batch(cmd, next.reloadRightPane())
		}
		next.worktreesRestoreDocument()
		return true, next, cmd
	}
	if m.workspace == wsWorktrees || m.agents.overlay != nil {
		if handled, nm, cmd := m.onAgentOverlayMsg(msg); handled {
			return true, nm, cmd
		}
	}
	if m.agents.overlay != nil {
		nm, cmd := m.overlayUpdate(msg)
		return true, nm, cmd
	}
	return false, m, nil
}

func (m Model) worktreesModalKind(k modals.Kind) bool {
	switch k {
	case modals.KindConfigConfirm:
		return m.pendingCfg != nil
	case modals.KindCopyFile, modals.KindScripts, modals.KindRunCommand, modals.KindAliases, modals.KindYank, modals.KindBranches, modals.KindCommit, modals.KindPrune, modals.KindNewAlias, modals.KindNewAliasCmd, modals.KindMergeStrategy, modals.KindReviewApprove, modals.KindReviewChanges, modals.KindReviewComment, modals.KindPRTitle, modals.KindPRBody, modals.KindUpdateBase, modals.KindConfirm, modals.KindBulkPrune, modals.KindCreateSource, modals.KindCreateNew, modals.KindCreateExisting, modals.KindCreatePR, modals.KindProcesses, modals.KindSetPolicy, modals.KindRenameProc, modals.KindConfigChoice, modals.KindConfigValue, modals.KindPrefEditor, kindAgentStop:
		return true
	case modals.KindKeymap:
		return m.workspace == wsWorktrees
	}
	return false
}
func (m Model) worktreesModalSubmit(msg modals.SubmitMsg) (tea.Model, tea.Cmd) {
	nm, cmd := m.onModalSubmit(msg)
	next := nm.(Model)
	if msg.Kind == modals.KindProcesses {
		next.shellFocus = shellBody
		next.worktreesSyncProcess()
	}
	next.layout()
	return next, cmd
}

func (m Model) worktreesGarden() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	session, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok || session.Task == nil {
		return m, nil
	}
	rec, ok := pgTask(m.garden.board.collection, session.Task)
	if !ok || m.garden.api == nil {
		m.status = "The linked Issue is not loaded in Garden"
		return m, nil
	}
	m.worktreesRememberDocument()
	m.wt.returning, m.wt.returnWS = true, wsGarden
	m, cmd := m.selectWorkspace(wsGarden)
	m.shellFocus = shellBody
	m.garden.sub = subBoard
	m.garden.board.opened = rec.Item.Issue.ID
	m.garden.board.docScroll = 0
	return m, cmd
}

func (m *Model) worktreesSyncProcess() {
	if m.wt.sub != wtProcesses || m.rightTab != tabProcs {
		return
	}
	if wt, ok := m.selectedWorktree(); ok {
		if id := m.activeProc[wt.Path]; id > 0 {
			m.wt.selected[wtProcesses] = strconv.Itoa(id)
		}
	}
	m.worktreesReconcile()
}

func (m Model) worktreesMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mouseOff || m.modal != nil || m.agents.overlay != nil || msg.Y < shellHeaderRows+settingsSubmenuRows || msg.Y >= m.height-shellFooterRows {
		return m, nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = 1
	default:
		return m, nil
	}
	list := m.focus == focusList
	if m.width >= 100 {
		list = msg.X < m.width*35/100
	}
	if list {
		if m.wt.sub != wtCheckouts {
			return m.worktreesMove(delta)
		}
		oldPath := m.currentPath()
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		if m.currentPath() != oldPath {
			cmd = tea.Batch(cmd, m.reloadRightPane())
		}
		return m, cmd
	}
	// An agent owns its own terminal. Wheel events only scroll Bonsai's log and
	// document viewport, never the holder or an invisible pane under it.
	if m.rightTab == tabAgents {
		return m, nil
	}
	var cmd tea.Cmd
	m.term, cmd = m.term.Update(msg)
	m.worktreesRememberDocument()
	return m, cmd
}
