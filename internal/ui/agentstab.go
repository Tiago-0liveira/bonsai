package ui

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// kindAgentStop is the stop confirmation. It lives here, not in the modals
// package, because Kind is only an identifier the parent routes on.
const kindAgentStop modals.Kind = "agent-stop"

const (
	usageRefresh      = 60 * time.Second
	minTerminalRows   = 5
	stripNameCells    = 12
	unsupportedNotice = "Interactive agents need a Unix host (Linux or macOS)."
)

type agentUsageTickMsg struct{ gen uint64 }

func tickAgentUsage(gen uint64) tea.Cmd {
	return tea.Tick(usageRefresh, func(time.Time) tea.Msg { return agentUsageTickMsg{gen: gen} })
}

// fetchUsage begins a shared-store fetch for the account. lim bounds provider
// calls in flight for callers that fetch many accounts at once.
func (m *Model) fetchUsage(id agents.AccountID, lim usagestore.Limiter) tea.Cmd {
	var seq uint64
	m.agents.usage, seq = m.agents.usage.Begin(id)
	return usagestore.Fetch(m.agents.ctx, m.agents.api.Accounts, lim, id, seq, false)
}

// --- selection ---

// stripSessions are the sessions the strip numbers for a worktree: the active
// ones oldest first (so numbers stay put as new agents start), plus the
// selected session when it has ended and was picked from the history.
func (m Model) stripSessions(path string) []agentsapi.Session {
	var active []agentsapi.Session
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.Active() || !m.agents.ack[s.ID] {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		for _, i := range m.agents.byTree[path] {
			active = append(active, m.agents.sessions[i])
		}
	}
	sort.SliceStable(active, func(a, b int) bool { return active[a].CreatedAt.Before(active[b].CreatedAt) })
	if id, ok := m.agents.selected[path]; ok {
		for _, s := range active {
			if s.ID == id {
				return active
			}
		}
		if s, ok := m.sessionByID(path, id); ok {
			active = append(active, s)
		}
	}
	return active
}

func (m Model) sessionByID(path, id string) (agentsapi.Session, bool) {
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.ID == id {
			return s, true
		}
	}
	return agentsapi.Session{}, false
}

// selectedSession is the remembered session of a worktree, else its most
// recently started active one.
func (m Model) selectedSession(path string) (agentsapi.Session, int, bool) {
	strip := m.stripSessions(path)
	if id, ok := m.agents.selected[path]; ok {
		for i, s := range strip {
			if s.ID == id {
				return s, i, true
			}
		}
	}
	best := -1
	for i, s := range strip {
		if best < 0 || s.CreatedAt.After(strip[best].CreatedAt) {
			best = i
		}
	}
	if best < 0 {
		return agentsapi.Session{}, 0, false
	}
	return strip[best], best, true
}

// selectStripIndex remembers strip entry i for the worktree.
func (m *Model) selectStripIndex(path string, i int) bool {
	strip := m.stripSessions(path)
	if i < 0 || i >= len(strip) {
		return false
	}
	m.agents.selected[path] = strip[i].ID
	return true
}

// --- usage ---

// requestAgentUsage fetches usage for the selected session's account through
// the shared store; the answer for an account the user already left still
// lands on that account's own entry.
func (m *Model) requestAgentUsage() tea.Cmd {
	if !m.agents.active() || m.agents.api.Accounts == nil {
		return nil
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return nil
	}
	s, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok {
		return nil
	}
	m.agents.usageAccount = s.AccountID
	return m.fetchUsage(s.AccountID, nil)
}

// onUsageResult applies a fetch or stored read to the shared store and hands
// fetch results to the open overlay, whose own store ignores answers it did
// not ask for.
func (m Model) onUsageResult(r usagestore.Result) (tea.Model, tea.Cmd) {
	var applied bool
	m.agents.usage, applied = m.agents.usage.Apply(r)
	if applied {
		m.layout()
	}
	if r.Stored || m.agents.overlay == nil {
		return m, nil
	}
	m, cmd := m.overlayUpdate(r)
	return m, cmd
}

// onUsageObserved takes in a result an overlay applied, so the strip and the
// pane show it without fetching again.
func (m Model) onUsageObserved(o usagestore.Observation) (tea.Model, tea.Cmd) {
	var applied bool
	m.agents.usage, applied = m.agents.usage.Apply(usagestore.Result(o))
	if applied {
		m.layout()
	}
	return m, nil
}

func (m Model) onAgentUsageTick(msg agentUsageTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.agents.usageTickGen {
		return m, nil
	}
	m.agents.usageTickGen++
	next := tickAgentUsage(m.agents.usageTickGen)
	if m.rightTab != tabAgents {
		return m, next
	}
	cmd := m.requestAgentUsage()
	return m, tea.Batch(cmd, next)
}

// --- keys & actions ---

func (m Model) openAgentsTab() (tea.Model, tea.Cmd) {
	m.rightTab = tabAgents
	m.layout()
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	cmd := m.requestAgentUsage()
	return m, cmd
}

// agentsTabKey handles Agents-tab keys while that tab is focused. They take
// precedence over the globals that share their letters (n, a, u, m).
func (m Model) agentsTabKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	s := msg.String()
	switch {
	case m.layoutKind == LayoutRedesign && key.Matches(msg, m.keys.AgentPlayground):
		nm, cmd := m.worktreesPlayground()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentRename):
		nm, cmd := m.openRenameSelected()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentStopAll):
		nm, cmd := m.askStopAllAgents()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentAllRepo):
		nm, cmd := m.openRepoSessions()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentAll):
		if !m.agents.active() {
			m.setStatus(unsupportedNotice, false)
			return true, m, nil
		}
		nm, cmd := m.openAgentsPicker()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentAttach):
		// A failed or dropped attach retries first; otherwise enter takes control.
		if cmd := m.retryAgentAttach(); cmd != nil {
			return true, m, cmd
		}
		handled, cmd := m.enterTerminalMode()
		return handled, m, cmd
	case key.Matches(msg, m.keys.AgentQuickNew):
		nm, cmd := m.openAgentTask(false)
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentFork):
		nm, cmd := m.openAgentTask(true)
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentNew):
		nm, cmd := m.openAgentLaunch()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentAccounts):
		nm, cmd := m.openAgentAccounts()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentUsage):
		nm, cmd := m.openAgentUsage()
		return true, nm, cmd
	case key.Matches(msg, m.keys.AgentStop):
		nm, cmd := m.askStopAgent()
		return true, nm, cmd
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9', s == "[", s == "]":
		wt, ok := m.selectedWorktree()
		if !ok || !m.agents.active() {
			return true, m, nil
		}
		path := cleanPath(wt.Path)
		_, cur, _ := m.selectedSession(path)
		n := len(m.stripSessions(path))
		target := cur
		switch s {
		case "[":
			target = cur - 1
		case "]":
			target = cur + 1
		default:
			target = int(s[0] - '1')
		}
		if n > 0 && (s == "[" || s == "]") {
			target = (target + n) % n
		}
		if !m.selectStripIndex(path, target) {
			return true, m, nil
		}
		cmd := m.requestAgentUsage()
		return true, m, cmd
	}
	return false, m, nil
}

// --- picker ---

func (m Model) openAgentsPicker() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	path := cleanPath(wt.Path)
	// Opening history acknowledges the ended sessions shown there.
	m.ackFailedAgents(path)
	m.leaveAgentTerminal()
	p := newSessionsPicker(branchOf(wt), m.agentGlyphsASCII())
	p.path = path
	p.keys = m.keys
	p.setSessions(m.orderedSessions(path))
	m, peek := m.peekPicker(&p)
	m.setAgentOverlay(agentOverlay{sessions: &p})
	return m, peek
}

// ackFailedAgents marks ended sessions as seen and refreshes their counts.
func (m *Model) ackFailedAgents(path string) {
	changed := false
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; !s.Active() && !m.agents.ack[s.ID] {
			m.agents.ack[s.ID] = true
			changed = true
		}
	}
	if changed {
		m.refreshAgentBadges()
	}
}

// onAgentsPick selects the chosen session and acknowledges its failure mark.
func (m Model) onAgentsPick(path, id string) (tea.Model, tea.Cmd) {
	if path == "" {
		return m, nil
	}
	if wt, ok := m.selectedWorktree(); !ok || cleanPath(wt.Path) != cleanPath(path) {
		if !m.list.SelectByPath(path) {
			m.list, _ = m.list.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if !m.list.SelectByPath(path) {
				m.setStatus("agents: worktree is hidden by the current filter or no longer exists", false)
				return m, nil
			}
		}
	}
	m.rightTab = tabAgents
	m.focus = focusTerminal
	m.list.Blur()
	m.term.Focus()
	m.layout()
	m.agents.selected[cleanPath(path)] = id
	m.agents.ack[id] = true
	m.refreshAgentBadges()
	cmd := m.requestAgentUsage()
	return m, cmd
}

// stateMarkStyled is the picker's status mark, colored like the status:
// exited gets a check, failed a cross, every live status a dot.
func stateMarkStyled(key string) string {
	mark := "●"
	switch key {
	case "exited":
		mark = "✓"
	case "failed":
		mark = "✗"
	}
	return lipgloss.NewStyle().Foreground(statusColor(key)).Render(mark)
}

func branchOf(wt git.Worktree) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	return filepath.Base(wt.Path)
}

func cleanPath(p string) string { return filepath.Clean(p) }

// openRepoSessions uses the same ordered history and filter as the local picker.
func (m Model) openRepoSessions() (tea.Model, tea.Cmd) {
	if !m.agents.active() {
		m.setStatus(unsupportedNotice, false)
		return m, nil
	}
	m.leaveAgentTerminal()
	p := newSessionsPicker("Repository", m.agentGlyphsASCII())
	p.repo, p.keys = true, m.keys
	m.populateRepoPicker(&p)
	for _, ss := range p.sessions {
		if !ss.Active() {
			m.agents.ack[ss.ID] = true
		}
	}
	m.refreshAgentBadges()
	m, peek := m.peekPicker(&p)
	m.setAgentOverlay(agentOverlay{sessions: &p})
	return m, peek
}

func (m Model) nextAgentAttention() (tea.Model, tea.Cmd) {
	if !m.agents.active() {
		m.setStatus("No sessions need attention", false)
		return m, nil
	}
	var candidates []sessionsPickMsg
	for _, wt := range m.worktrees {
		path := cleanPath(wt.Path)
		for _, s := range m.orderedSessions(path) {
			if m.sessionNeedsAttention(s) {
				candidates = append(candidates, sessionsPickMsg{path, s.ID})
			}
		}
	}
	if len(candidates) == 0 {
		m.setStatus("No sessions need attention", false)
		return m, nil
	}
	next := 0
	for i, c := range candidates {
		if c.id == m.agents.attentionID {
			next = (i + 1) % len(candidates)
			break
		}
	}
	c := candidates[next]
	m.agents.attentionID = c.id
	return m.onAgentsPick(c.path, c.id)
}

func (m Model) populateRepoPicker(p *sessionsPicker) {
	p.paths, p.branches = map[string]string{}, map[string]string{}
	for _, s := range m.agents.sessions {
		p.paths[s.ID] = cleanPath(s.WorkDir)
		p.branches[s.ID] = filepath.Base(s.WorkDir)
	}
	for _, wt := range m.worktrees {
		for _, i := range m.agents.byTree[cleanPath(wt.Path)] {
			s := m.agents.sessions[i]
			p.paths[s.ID] = cleanPath(wt.Path)
			p.branches[s.ID] = branchOf(wt)
		}
	}
	list := append([]agentsapi.Session(nil), m.agents.sessions...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Active() != list[j].Active() {
			return list[i].Active()
		}
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	p.setSessions(list)
}
