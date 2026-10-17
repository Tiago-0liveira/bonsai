package ui

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// kindAgents is the session picker modal. It lives here, not in the modals
// package, because Kind is only an identifier the parent routes on.
const (
	kindAgents    modals.Kind = "agents"
	kindAgentStop modals.Kind = "agent-stop"
)

const (
	usageRefresh      = 60 * time.Second
	minTerminalRows   = 5
	stripNameCells    = 12
	pickerNameCells   = 28
	unsupportedNotice = "Interactive agents need a Unix host (Linux or macOS)."
)

// usageEntry is the last usage shown for an account. A failed refresh keeps
// the last good data and marks it stale instead of clearing it.
type usageEntry struct {
	usage agentsapi.Usage
	have  bool
	stale bool
	err   string
}

type agentUsageMsg struct {
	seq     uint64
	account agents.AccountID
	usage   agentsapi.Usage
	err     error
}

type agentUsageTickMsg struct{ gen uint64 }

func tickAgentUsage(gen uint64) tea.Cmd {
	return tea.Tick(usageRefresh, func(time.Time) tea.Msg { return agentUsageTickMsg{gen: gen} })
}

func fetchAgentUsage(a agentsapi.Accounts, seq uint64, account agents.AccountID) tea.Cmd {
	return func() tea.Msg {
		u, err := a.Usage(context.Background(), account, false)
		return agentUsageMsg{seq: seq, account: account, usage: u, err: err}
	}
}

// --- selection ---

// stripSessions are the sessions the strip numbers for a worktree: the active
// ones oldest first (so numbers stay put as new agents start), plus the
// selected session when it has ended and was picked from the history.
func (m Model) stripSessions(path string) []agentsapi.Session {
	var active []agentsapi.Session
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.Active() {
			active = append(active, s)
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

// requestAgentUsage fetches usage for the selected session's account. Each
// request supersedes the previous one, so a late answer for an account the
// user already left cannot overwrite what is shown.
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
	m.agents.usageSeq++
	m.agents.usageAccount = s.AccountID
	return fetchAgentUsage(m.agents.api.Accounts, m.agents.usageSeq, s.AccountID)
}

func (m Model) onAgentUsage(msg agentUsageMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.agents.usageSeq || msg.account != m.agents.usageAccount {
		return m, nil
	}
	e := m.agents.usage[msg.account]
	if msg.err != nil {
		e.stale, e.err = true, msg.err.Error()
	} else {
		e = usageEntry{usage: msg.usage, have: true, stale: msg.usage.Stale, err: msg.usage.Err}
	}
	m.agents.usage[msg.account] = e
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
	case key.Matches(msg, m.keys.AgentAll):
		if !m.agents.active() {
			m.status = unsupportedNotice
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
	m.agents.pickPath = path
	m.agents.pickIDs = map[string]string{}
	// Opening the list is seeing it: failed sessions of this worktree list
	// their ✗ there, so the worktree row's "!" has done its job.
	m.ackFailedAgents(path)
	build := func(query string) []string { return m.agentPickerItems(path, query) }
	modal := modals.NewPicker(kindAgents, "AGENTS IN "+branchOf(wt), build, build(""),
		"/ Search    Enter Open    n New    Esc Close", "No agents in this worktree yet", "n")
	modal.SetInline(true)
	modal.SetSize(m.agentPaneSize())
	m.modal = &modal
	return m, nil
}

// ackFailedAgents marks the failed sessions of a worktree as seen and refreshes
// the "!" badges.
func (m *Model) ackFailedAgents(path string) {
	changed := false
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.State == "failed" && !m.agents.ack[s.ID] {
			m.agents.ack[s.ID] = true
			changed = true
		}
	}
	if changed {
		m.refreshAgentBadges()
	}
}

// agentPickerItems lists a worktree's sessions as one flat list: running
// first, then ended, each newest first, with the name column aligned. It also
// records which session each item text stands for.
func (m Model) agentPickerItems(path, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	var running, ended []agentsapi.Session
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.Active() {
			running = append(running, s)
		} else {
			ended = append(ended, s)
		}
	}
	sort.SliceStable(running, func(a, b int) bool { return running[a].CreatedAt.After(running[b].CreatedAt) })
	sort.SliceStable(ended, func(a, b int) bool { return ended[a].CreatedAt.After(ended[b].CreatedAt) })
	all := append(running, ended...)

	nameW := 0
	for _, s := range all {
		nameW = max(nameW, min(ansi.StringWidth(sessionName(s)), pickerNameCells))
	}
	var items []string
	for _, s := range all {
		name := ansi.Truncate(sessionName(s), pickerNameCells, "…")
		plain := name + " " + providerName(s.Provider) + " " + s.ProfileName
		if q != "" && !strings.Contains(strings.ToLower(plain), q) {
			continue
		}
		text := m.chrome(stateMarkStyled(s.State) + " " + m.agentGlyph(s.Provider) + " " + fit(name, nameW) + "  " +
			dimText(providerName(s.Provider)+" · "+s.ProfileName))
		if prev, dup := m.agents.pickIDs[text]; dup && prev != s.ID {
			text += " #" + s.ID
		}
		m.agents.pickIDs[text] = s.ID
		items = append(items, text)
	}
	return items
}

// onAgentsPick selects the chosen session and acknowledges its failure mark.
func (m Model) onAgentsPick(value string) (tea.Model, tea.Cmd) {
	id, ok := m.agents.pickIDs[value]
	if !ok {
		return m, nil
	}
	m.agents.selected[m.agents.pickPath] = id
	m.agents.ack[id] = true
	m.refreshAgentBadges()
	cmd := m.requestAgentUsage()
	return m, cmd
}

// stateMarkStyled is the picker's state mark: running green, exited dim,
// failed red.
func stateMarkStyled(state string) string {
	mark, c := "●", theme.Current.Success
	switch state {
	case "exited":
		mark, c = "✓", theme.Current.Dim
	case "failed":
		mark, c = "✗", theme.Current.Danger
	case "starting", "stopping":
		c = theme.Current.Warning
	}
	return lipgloss.NewStyle().Foreground(c).Render(mark)
}

func branchOf(wt git.Worktree) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	return filepath.Base(wt.Path)
}

func cleanPath(p string) string { return filepath.Clean(p) }
