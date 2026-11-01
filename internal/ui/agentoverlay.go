package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

const accountsUnavailable = "agents: accounts are unavailable"

// agentOverlay is the active agent view inside the tab. At most
// one field is set. The live stream keeps pumping underneath: opening an
// overlay never touches the attachment.
type agentOverlay struct {
	launch   *launchDialog
	accounts *accounts.Model
	quick    *accounts.Quick
	sessions *sessionsPicker
}

// agentExecDoneMsg reports that a terminal flow (sign in, add account) ended.
type agentExecDoneMsg struct{ err error }

// agentStoppedMsg is the answer to a stop request.
type agentStoppedMsg struct {
	name string
	err  error
}

// leaveAgentTerminal drops terminal mode before something else takes the
// keyboard. The stream stays attached; returning needs enter.
func (m *Model) leaveAgentTerminal() {
	if v := m.agents.view; v != nil && v.control {
		m.leaveTerminalMode(v)
	}
}

// agentOverlayReady checks what every opener needs and sets the status when
// it is missing.
func (m *Model) agentOverlayReady() bool {
	if !m.agents.active() {
		m.setStatus(unsupportedNotice, false)
		return false
	}
	if m.agents.api.Accounts == nil {
		m.setStatus(accountsUnavailable, false)
		return false
	}
	return true
}

// selectedAgentAccount is the account of the selected session, if any.
func (m Model) selectedAgentAccount() agents.AccountID {
	if wt, ok := m.selectedWorktree(); ok {
		if s, _, ok := m.selectedSession(cleanPath(wt.Path)); ok {
			return s.AccountID
		}
	}
	return ""
}

func (m *Model) setAgentOverlay(o agentOverlay) {
	m.rightTab = tabAgents
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	m.agents.overlay = &o
	m.sizeAgentOverlay()
}

func (m Model) openAgentLaunch() (tea.Model, tea.Cmd) {
	if !m.agentOverlayReady() {
		return m, nil
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	m.leaveAgentTerminal()
	m.agents.launchSeq++
	target := launchTarget{
		worktree: git.ID(webRepositoryID, wt.Path),
		workDir:  wt.Path,
		branch:   branchOf(wt),
	}
	d := newLaunchDialog(m.agents.launchSeq, m.agents.api.Accounts, target, m.selectedAgentAccount(), m.agents.usage, m.agentGlyphsASCII())
	// The last-used account is only a fallback: until the user moves the
	// cursor, the account with the most headroom wins once usage is known.
	d.accountPinned = false
	d.sessions = m.agents.sessions
	d.changeWorktree = m.keys.LaunchChangeWorktree
	for _, tree := range m.worktrees {
		d.targets = append(d.targets, launchTarget{worktree: git.ID(webRepositoryID, tree.Path), workDir: tree.Path, branch: branchOf(tree)})
	}
	d.SetSize(m.agentPaneSize())
	m.setAgentOverlay(agentOverlay{launch: &d})
	return m, d.Init()
}

func (m Model) openAgentAccounts() (tea.Model, tea.Cmd) {
	if !m.agentOverlayReady() {
		return m, nil
	}
	m.leaveAgentTerminal()
	return m.showAccounts(m.selectedAgentAccount(), accounts.TabOverview)
}

func (m Model) showAccounts(initial agents.AccountID, tab accounts.Tab) (tea.Model, tea.Cmd) {
	if m.layoutKind == LayoutRedesign && m.workspace == wsWorktrees {
		return m.worktreesAccounts(initial, tab)
	}
	am := accounts.New(m.agents.api.Accounts, accounts.Options{ASCII: m.agentGlyphsASCII(), Initial: initial, Tab: tab, Usage: m.agents.usage})
	am.SetSize(m.agentPaneSize())
	m.setAgentOverlay(agentOverlay{accounts: &am})
	return m, am.Init()
}

func (m Model) openAgentUsage() (tea.Model, tea.Cmd) {
	if !m.agentOverlayReady() {
		return m, nil
	}
	m.leaveAgentTerminal()
	if m.layoutKind == LayoutRedesign && m.workspace == wsWorktrees {
		return m.worktreesAccounts(m.selectedAgentAccount(), accounts.TabUsage)
	}
	q := accounts.NewQuick(m.agents.api.Accounts, m.selectedAgentAccount(), m.agentGlyphsASCII()).WithUsage(m.agents.usage)
	q.SetSize(m.agentPaneSize())
	m.setAgentOverlay(agentOverlay{quick: &q})
	return m, q.Init()
}

// sizeAgentOverlay keeps child views within the Agents tab body.
func (m *Model) sizeAgentOverlay() {
	o := m.agents.overlay
	if o == nil {
		return
	}
	switch {
	case o.launch != nil:
		o.launch.SetSize(m.agentPaneSize())
	case o.accounts != nil:
		o.accounts.SetSize(m.agentPaneSize())
	case o.quick != nil:
		o.quick.SetSize(m.agentPaneSize())
	case o.sessions != nil:
		o.sessions.SetSize(m.agentPaneSize())
	}
}

// overlayUpdate gives a key or a backend result to the open overlay. Results
// for an overlay that was closed or swapped find nobody (or a model that
// ignores them) and are dropped.
func (m Model) overlayUpdate(msg tea.Msg) (Model, tea.Cmd) {
	o := m.agents.overlay
	if o == nil {
		return m, nil
	}
	var cmd tea.Cmd
	switch {
	case o.launch != nil:
		var d launchDialog
		d, cmd = o.launch.Update(msg)
		m.setAgentOverlay(agentOverlay{launch: &d})
	case o.accounts != nil:
		var am accounts.Model
		am, cmd = o.accounts.Update(msg)
		m.setAgentOverlay(agentOverlay{accounts: &am})
	case o.quick != nil:
		var q accounts.Quick
		q, cmd = o.quick.Update(msg)
		m.setAgentOverlay(agentOverlay{quick: &q})
	case o.sessions != nil:
		var sp sessionsPicker
		sp, cmd = o.sessions.Update(msg)
		m.setAgentOverlay(agentOverlay{sessions: &sp})
	}
	return m, cmd
}

// closeAgentOverlay closes whatever is open. Usage may have moved while it was
// open, so the header refreshes.
func (m Model) closeAgentOverlay() (tea.Model, tea.Cmd) {
	m.agents.overlay = nil
	cmd := m.requestAgentUsage()
	return m, cmd
}

// agentOverlayView draws the open view inside the Agents tab.
func (m Model) agentOverlayView(w, h int) string {
	o := m.agents.overlay
	var s string
	switch {
	case o.launch != nil:
		s = o.launch.View()
	case o.accounts != nil:
		s = o.accounts.View()
	case o.quick != nil:
		s = o.quick.View()
	case o.sessions != nil:
		s = o.sessions.View()
	}
	return lipgloss.Place(w, h, lipgloss.Left, lipgloss.Top, m.chrome(s))
}

// onAgentOverlayMsg handles the messages overlays send to their parent. handled
// is false for anything else.
func (m Model) onAgentOverlayMsg(msg tea.Msg) (handled bool, nm tea.Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case accounts.CloseMsg:
		nm, cmd = m.closeAgentOverlay()
		return true, nm, cmd
	case accounts.OpenAccountsMsg:
		if m.agents.overlay == nil || m.agents.overlay.quick == nil {
			return true, m, nil
		}
		nm, cmd = m.showAccounts(msg.Account, accounts.TabUsage)
		return true, nm, cmd
	case accounts.ExecMsg:
		if msg.Cmd == nil {
			return true, m, nil
		}
		// Sign in and add-account are interactive CLI flows: suspend the TUI,
		// give them the real terminal, and come back to a refreshed overlay.
		return true, m, tea.ExecProcess(msg.Cmd, func(err error) tea.Msg { return agentExecDoneMsg{err: err} })
	case agentExecDoneMsg:
		if msg.err != nil {
			m.setStatus("agents: "+sanitizeAgentText(msg.err.Error()), true)
		}
		var ocmd tea.Cmd
		m, ocmd = m.overlayUpdate(accounts.ReloadMsg{})
		return true, m, tea.Batch(ocmd, m.relistAgents())
	case sessionsPickMsg:
		m.agents.overlay = nil
		nm, cmd = m.onAgentsPick(msg.path, msg.id)
		return true, nm, cmd
	case sessionsNewMsg:
		nm, cmd = m.openAgentLaunch()
		return true, nm, cmd
	case launchSubmitMsg:
		nm, cmd = m.onLaunchSubmit(msg)
		return true, nm, cmd
	case launchStartedMsg:
		nm, cmd = m.onLaunchStarted(msg)
		return true, nm, cmd
	case launchSavedMsg:
		if msg.err != nil {
			m.setStatus("agents: could not save defaults: "+sanitizeAgentText(msg.err.Error()), true)
		}
		return true, m, nil
	case agentStoppedMsg:
		if msg.err != nil {
			m.setStatus("agents: could not stop "+msg.name+": "+sanitizeAgentText(msg.err.Error()), true)
		}
		return true, m, m.relistAgents()
	}
	return false, m, nil
}

// --- launch ---

func (m Model) onLaunchSubmit(msg launchSubmitMsg) (tea.Model, tea.Cmd) {
	o := m.agents.overlay
	if o == nil || o.launch == nil || o.launch.id != msg.dlg {
		return m, nil
	}
	req := msg.req
	req.Cols, req.Rows = m.agentAreaSize()
	if req.Cols <= 0 || req.Rows <= 0 {
		// Nothing laid out yet (tiny pane): the writer resizes to the real
		// area as soon as terminal mode starts.
		req.Cols, req.Rows = 80, 24
	}
	return m, startAgentCmd(m.agents.ctx, m.agents.api.Sessions, m.repoDir, msg, req)
}

func startAgentCmd(ctx context.Context, s agentsapi.Sessions, repoDir string, sub launchSubmitMsg, req agentsapi.StartRequest) tea.Cmd {
	return func() tea.Msg {
		// ProjectID resolves symlinks (file IO), so it is computed here, not in Update.
		req.ProjectID = config.ProjectID(repoDir)
		sess, err := s.Start(ctx, req)
		return launchStartedMsg{dlg: sub.dlg, session: sess, account: sub.account, save: sub.save, workDir: req.WorkDir, err: err}
	}
}

func saveDefaultsCmd(a agentsapi.Accounts, id agents.AccountID, changes map[string]*string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := launchCtx()
		defer cancel()
		return launchSavedMsg{err: a.UpdateDefaults(ctx, id, changes)}
	}
}

func (m Model) onLaunchStarted(msg launchStartedMsg) (tea.Model, tea.Cmd) {
	o := m.agents.overlay
	open := o != nil && o.launch != nil && o.launch.id == msg.dlg
	if msg.err != nil {
		if open {
			d := o.launch.failed(msg.err)
			m.setAgentOverlay(agentOverlay{launch: &d})
		} else {
			m.setStatus("agents: could not start: "+sanitizeAgentText(msg.err.Error()), true)
		}
		return m, nil
	}
	if !open {
		m.setStatus("agents: started "+sanitizeAgentText(sessionName(msg.session)), false)
	}
	var cmds []tea.Cmd
	// Defaults are saved only after the agent really started, so a failed
	// start never changes what the account launches with next time.
	if msg.save != nil && m.agents.api.Accounts != nil {
		cmds = append(cmds, saveDefaultsCmd(m.agents.api.Accounts, msg.account, msg.save))
	}
	if open {
		m.agents.overlay = nil
		path := cleanPath(msg.workDir)
		m.list.SelectByPath(msg.workDir)
		if wt, ok := m.selectedWorktree(); ok && cleanPath(wt.Path) == path {
			m.agents.selected[path] = msg.session.ID
			m.agents.enterAfter, m.agents.enterPath = msg.session.ID, path
			nm, c := m.openAgentsTab()
			m = nm.(Model)
			cmds = append(cmds, c)
		}
	}
	// The list that follows holds the new session; the live view for it then
	// attaches as the writer (see syncAgentView).
	cmds = append(cmds, m.relistAgents())
	m.agents.enterSeq = m.agents.seq
	return m, tea.Batch(cmds...)
}

// --- stop ---

// askStopAgent confirms, then stops the selected session. The prompt always
// shows: ending an agent loses its work in progress, which the
// confirm-destructive setting does not cover.
func (m Model) askStopAgent() (tea.Model, tea.Cmd) {
	if !m.agents.active() {
		m.setStatus(unsupportedNotice, false)
		return m, nil
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	s, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok || !s.Active() {
		m.setStatus("agents: no running agent to stop", false)
		return m, nil
	}
	m.leaveAgentTerminal()
	name := sessionName(s)
	m.pendingConfirm = &confirmState{action: stopAgentCmd(m.agents.ctx, m.agents.api.Sessions, s.ID, name)}
	modal := modals.NewConfirm(kindAgentStop, fmt.Sprintf("Stop %s?", name))
	modal.SetBody("The agent process ends.")
	modal.SetInline(true)
	modal.SetSize(m.agentPaneSize())
	m.modal = &modal
	return m, nil
}

func stopAgentCmd(ctx context.Context, s agentsapi.Sessions, id, name string) tea.Cmd {
	return func() tea.Msg { return agentStoppedMsg{name: name, err: s.Stop(ctx, id)} }
}

func (m Model) askStopAllAgents() (tea.Model, tea.Cmd) {
	if !m.agents.active() {
		return m, nil
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, s := range m.orderedSessions(cleanPath(wt.Path)) {
		if s.Active() {
			cmds = append(cmds, stopAgentCmd(m.agents.ctx, m.agents.api.Sessions, s.ID, sessionName(s)))
		}
	}
	if len(cmds) == 0 {
		m.setStatus("agents: no running sessions to stop", false)
		return m, nil
	}
	m.leaveAgentTerminal()
	m.pendingConfirm = &confirmState{action: tea.Batch(cmds...)}
	modal := modals.NewConfirm(kindAgentStop, fmt.Sprintf("Stop all %d sessions in %s?", len(cmds), branchOf(wt)))
	modal.SetBody("All agent processes in this worktree end.")
	modal.SetInline(true)
	modal.SetSize(m.agentPaneSize())
	m.modal = &modal
	return m, nil
}
