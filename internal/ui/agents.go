package ui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Option customizes a Model at construction.
type Option func(*Model)

// WithAgents injects the agent backend. A backend without Sessions, or whose
// Sessions are unsupported on this host, leaves every agent feature off.
func WithAgents(b agentsapi.Backend) Option {
	return func(m *Model) {
		if b.Sessions == nil {
			return
		}
		// Kept even when unsupported so the tab can explain why it is idle.
		m.agents.api = b
		m.agents.selected = map[string]string{}
		m.agents.usage = map[agents.AccountID]usageEntry{}
		m.agents.ack = map[string]bool{}
		if !b.Sessions.Supported() {
			return
		}
		m.agents.ctx, m.agents.cancel = context.WithCancel(context.Background())
		// Init issues the first list with seq 1.
		m.agents.seq = 1
		m.agents.subscribed = true // Init subscribes
		m.agents.usageTickGen = 1
	}
}

// agentState is the TUI's view of the agent sessions.
type agentState struct {
	api    agentsapi.Backend
	ctx    context.Context // owned by the model; cancelled on quit
	cancel context.CancelFunc

	sessions []agentsapi.Session
	// byTree maps a cleaned worktree path to indices into sessions.
	byTree map[string][]int
	// seq is the last list request issued, applied the last one applied; a
	// result at or below applied is older than what is shown and is dropped.
	seq, applied uint64
	subscribed   bool // a Changes subscription is live or pending; cleared if Changes fails
	err          string
	// ack holds failed session IDs the user has already seen, so their "!"
	// stops showing.
	ack map[string]bool

	// selected remembers each worktree's chosen session for the TUI's lifetime.
	selected map[string]string
	// usage is the last usage per account; usageSeq numbers requests and
	// usageAccount is the account the latest one asked for.
	usage        map[agents.AccountID]usageEntry
	usageSeq     uint64
	usageAccount agents.AccountID
	// usageTickGen makes duplicate Init timers stale after one re-arms.
	usageTickGen uint64
	// pickIDs maps picker item text to its session; pickPath is the worktree
	// the open picker belongs to.
	pickIDs  map[string]string
	pickPath string

	// view is the one live attachment; attachSeq numbers streams for its token.
	view      *agentView
	attachSeq uint64
}

// enabled: a backend is present (the tab will exist). active: it is also
// supported here, so lists, the subscription and glyphs run.
func (a agentState) enabled() bool { return a.api.Sessions != nil }
func (a agentState) active() bool  { return a.enabled() && a.ctx != nil }

// stop ends the change subscription. TUI exit never stops the holders.
func (a agentState) stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

type agentsListMsg struct {
	seq      uint64
	sessions []agentsapi.Session
	byTree   map[string][]int
	err      error
}

type agentsSubscribedMsg struct {
	ch  <-chan struct{}
	err error
}

type agentsChangedMsg struct{ ch <-chan struct{} }

// agentsClosedMsg reports that the change channel ended; nothing re-arms it.
type agentsClosedMsg struct{}

// listAgentsCmd lists sessions of this project and matches them to the given
// worktree paths. The matching resolves symlinks, which is file IO, so it runs
// here and never in Update/View.
func listAgentsCmd(ctx context.Context, s agentsapi.Sessions, repoDir string, paths []string, seq uint64) tea.Cmd {
	return func() tea.Msg {
		all, err := s.List(ctx)
		if err != nil {
			return agentsListMsg{seq: seq, err: err}
		}
		project := config.ProjectID(repoDir)
		mine := make([]agentsapi.Session, 0, len(all))
		for _, ss := range all {
			if ss.ProjectID == project {
				mine = append(mine, ss)
			}
		}
		return agentsListMsg{seq: seq, sessions: mine, byTree: indexSessions(paths, mine)}
	}
}

// webRepositoryID is the repository ID the web API and daemon pass to
// local.ID, so stored WorktreeIDs are computed from it (not from the repo path).
const webRepositoryID = "local"

// indexSessions groups session indices by worktree path. A session belongs to
// the worktree whose real path equals its WorkDir; the stored worktree ID is
// the fallback for holders recorded under another spelling of the path.
func indexSessions(paths []string, sessions []agentsapi.Session) map[string][]int {
	resolve := func(p string) string {
		p = filepath.Clean(p)
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Clean(r)
		}
		return p
	}
	byTree := map[string][]int{}
	if len(sessions) == 0 {
		return byTree
	}
	dirs := make([]string, len(sessions))
	for i, s := range sessions {
		if s.WorkDir != "" {
			dirs[i] = resolve(s.WorkDir)
		}
	}
	for _, p := range paths {
		key := filepath.Clean(p)
		rp, id := resolve(p), git.ID(webRepositoryID, p)
		for i, s := range sessions {
			if (dirs[i] != "" && dirs[i] == rp) || s.WorktreeID == id {
				byTree[key] = append(byTree[key], i)
			}
		}
	}
	return byTree
}

func subscribeAgentsCmd(ctx context.Context, s agentsapi.Sessions) tea.Cmd {
	return func() tea.Msg {
		ch, err := s.Changes(ctx)
		return agentsSubscribedMsg{ch: ch, err: err}
	}
}

// waitAgentsCmd blocks for one change signal; the handler re-issues it.
func waitAgentsCmd(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return agentsClosedMsg{}
		}
		return agentsChangedMsg{ch: ch}
	}
}

func (m Model) worktreePaths() []string {
	paths := make([]string, len(m.worktrees))
	for i, t := range m.worktrees {
		paths[i] = t.Path
	}
	return paths
}

// relistAgents issues a new list request with the next sequence number.
func (m *Model) relistAgents() tea.Cmd {
	if !m.agents.active() {
		return nil
	}
	m.agents.seq++
	return listAgentsCmd(m.agents.ctx, m.agents.api.Sessions, m.repoDir, m.worktreePaths(), m.agents.seq)
}

// initAgents is the Init half: first list plus the change subscription.
func (m Model) initAgents() tea.Cmd {
	if !m.agents.active() {
		return nil
	}
	return tea.Batch(
		listAgentsCmd(m.agents.ctx, m.agents.api.Sessions, m.repoDir, nil, 1),
		subscribeAgentsCmd(m.agents.ctx, m.agents.api.Sessions),
		tickAgentUsage(m.agents.usageTickGen),
	)
}

// resubscribeAgents retries a failed Changes subscription.
func (m *Model) resubscribeAgents() tea.Cmd {
	if !m.agents.active() || m.agents.subscribed {
		return nil
	}
	m.agents.subscribed = true
	return subscribeAgentsCmd(m.agents.ctx, m.agents.api.Sessions)
}

func (m Model) onAgentsList(msg agentsListMsg) (tea.Model, tea.Cmd) {
	if msg.seq <= m.agents.applied {
		return m, nil
	}
	m.agents.applied = msg.seq
	if msg.err != nil {
		// Keep the last good list; only a successful result replaces it.
		m.agents.err = msg.err.Error()
		return m, nil
	}
	m.agents.err = ""
	m.agents.sessions = msg.sessions
	m.agents.byTree = msg.byTree
	m.refreshAgentBadges()
	// New or ended sessions can change which account the open tab shows.
	if m.rightTab == tabAgents {
		if wt, ok := m.selectedWorktree(); ok {
			if s, _, ok := m.selectedSession(cleanPath(wt.Path)); ok && s.AccountID != m.agents.usageAccount {
				cmd := m.requestAgentUsage()
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m Model) onAgentsSubscribed(msg agentsSubscribedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// Retried on the next worktree refresh.
		m.agents.subscribed = false
		m.agents.err = msg.err.Error()
		return m, nil
	}
	return m, waitAgentsCmd(msg.ch)
}

func (m Model) onAgentsChanged(msg agentsChangedMsg) (tea.Model, tea.Cmd) {
	// Hoisted: relistAgents bumps m.agents.seq, and the order in which Go reads
	// m for the return versus runs the call is unspecified.
	list := m.relistAgents()
	return m, tea.Batch(list, waitAgentsCmd(msg.ch))
}

// agentGlyphsASCII reports the glyph preference.
func (m Model) agentGlyphsASCII() bool { return m.state.Prefs.AgentGlyphs == "ascii" }

// refreshAgentBadges rebuilds the per-worktree glyph text and hands it to the
// list. Called when sessions or the glyph preference change.
func (m *Model) refreshAgentBadges() {
	if !m.agents.active() {
		return
	}
	ascii := m.agentGlyphsASCII()
	badges := make(map[string]string, len(m.agents.byTree))
	for path, idx := range m.agents.byTree {
		if b := m.agentBadge(idx, ascii); b != "" {
			badges[path] = b
		}
	}
	m.list.SetAgentBadges(func(path string) string { return badges[filepath.Clean(path)] })
}

var agentProviderOrder = []agents.ProviderID{"claude", "codex", "antigravity"}

// agentBadge renders one worktree's glyphs: active sessions per provider with
// a count, then "!" for failed sessions the user has not acknowledged.
func (m Model) agentBadge(idx []int, ascii bool) string {
	counts := map[agents.ProviderID]int{}
	failed := false
	for _, i := range idx {
		s := m.agents.sessions[i]
		switch {
		case s.Active():
			counts[s.Provider]++
		case s.State == "failed" && !m.agents.ack[s.ID]:
			failed = true
		}
	}
	var parts []string
	seen := map[agents.ProviderID]bool{}
	add := func(p agents.ProviderID) {
		if n := counts[p]; n > 0 && !seen[p] {
			seen[p] = true
			parts = append(parts, theme.ProviderStyle(p).Render(theme.AgentBadge(p, n, ascii)))
		}
	}
	for _, p := range agentProviderOrder {
		add(p)
	}
	// Unknown providers still count, after the known ones, in a stable order.
	for _, i := range idx {
		add(m.agents.sessions[i].Provider)
	}
	if failed {
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.Current.Warning).Render("!"))
	}
	return strings.Join(parts, " ")
}
