package ui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// pgState is the Playground workspace: a flat, selectable list of this
// repository's agent sessions. It reads m.agents (backend, ordering counters,
// usage store, failure acknowledgements) and the Garden collection that is
// already loaded, and starts nothing: selecting a row never starts, stops,
// attaches to or refreshes anything.
type pgState struct {
	recs   []pgRec // last good scoped list, in stable order
	loaded bool
	// stale: the newest list failed (or could not verify the worktrees), so recs
	// is the last good one. Only a successful result, empty included, replaces it.
	stale  bool
	errMsg string

	sub   pgSub
	all   bool // Sessions also lists ended sessions
	group bool // nest sessions under their Issue

	sel    string // selected session ID: the one thing the cursor follows
	selIdx int    // where it was, for the deterministic neighbor when it leaves
	top    int    // first display unit in view

	searching bool
	search    textinput.Model
	query     string
	before    string

	subscribed bool
	initCmd    tea.Cmd
	// usageAsked: accounts whose stored snapshot was already read once.
	usageAsked map[agents.AccountID]bool
	// trees lists this repository's worktrees; a seam for tests.
	trees func(ctx context.Context, dir string) ([]git.Worktree, error)
	api   playgroundapi.Backend // changes, PR and CI reads; nil leaves them unavailable

	// The Changes screen and the manual links (plan 10.3); see playground_changes.go.
	rev     pgReview
	chg     pgChanges
	links   pgCall[playgroundapi.LinksView]
	linkMap map[string]playgroundapi.Link // links by session ID, from the last good read
	link    pgLinkFlow

	// The terminal: the Playground borrows the one live attachment (m.agents.view)
	// while it owns it. See playground_terminal.go.
	own      bool          // the Playground holds m.agents.view
	open     bool          // the terminal fills the body (Observe, or Control while view.control)
	pending  string        // session a debounced observe attach is waiting for
	gen      uint64        // debounce generation: older ticks are ignored
	takeFor  string        // session the takeover confirmation was opened for
	debounce time.Duration // zero means pgAttachDebounce; a seam for tests
}

type (
	pgListMsg struct {
		seq    uint64
		recs   []pgRec
		byTree map[string][]int // resolved in the command; Update performs no filesystem reads
		// degraded: the worktrees could not be listed, so only project-ID matches are present.
		degraded bool
		err      error
	}
	pgSubscribedMsg struct {
		ch  <-chan struct{}
		err error
	}
	pgChangedMsg struct{ ch <-chan struct{} }
	pgClosedMsg  struct{}
)

func newPgState() pgState {
	return pgState{trees: git.ListWorktreesContext, usageAsked: map[agents.AccountID]bool{}}
}

// listPlaygroundCmd reads every holder record and keeps this repository's. The
// worktree list and the symlink resolution are file IO, so they run here and
// never in Update or View.
func listPlaygroundCmd(ctx context.Context, s agentsapi.Sessions, trees func(context.Context, string) ([]git.Worktree, error), repoDir string, seq uint64) tea.Cmd {
	return func() tea.Msg {
		all, err := s.List(ctx)
		if err != nil {
			return pgListMsg{seq: seq, err: err}
		}
		resolve := func(p string) string {
			p = filepath.Clean(p)
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return filepath.Clean(r)
			}
			return p
		}
		list, terr := trees(ctx, repoDir)
		var pt []pgTree
		for _, w := range list {
			if w.Bare {
				continue
			}
			pt = append(pt, pgTree{ID: git.ID(webRepositoryID, w.Path), Path: w.Path, Resolved: resolve(w.Path), Branch: w.Branch})
		}
		recs := pgScope(all, config.ProjectID(repoDir), pt, resolve)
		sessions := make([]agentsapi.Session, 0, len(recs))
		paths := make([]string, 0, len(list))
		for _, rec := range recs {
			sessions = append(sessions, rec.Session)
		}
		for _, tree := range list {
			if !tree.Bare {
				paths = append(paths, tree.Path)
			}
		}
		return pgListMsg{seq: seq, recs: recs, byTree: indexSessions(paths, sessions), degraded: terr != nil}
	}
}

func pgSubscribeCmd(ctx context.Context, s agentsapi.Sessions) tea.Cmd {
	return func() tea.Msg {
		ch, err := s.Changes(ctx)
		return pgSubscribedMsg{ch: ch, err: err}
	}
}

func pgWaitCmd(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return pgClosedMsg{}
		}
		return pgChangedMsg{ch: ch}
	}
}

// playgroundEnter is the entry read: one list (cheap and local) and, once, the
// change subscription that keeps it fresh. It starts nothing else.
func (m Model) playgroundEnter() (Model, tea.Cmd) {
	if !m.agents.active() {
		return m, nil
	}
	list := m.playgroundRelist()
	var sub tea.Cmd
	if !m.pg.subscribed {
		m.pg.subscribed = true
		sub = pgSubscribeCmd(m.agents.ctx, m.agents.api.Sessions)
	}
	links := m.pgLinksRead()
	return m, tea.Batch(list, sub, links)
}

// playgroundRelist numbers a new list request on the shared agents counters, so
// an older answer can never overwrite a newer one.
func (m *Model) playgroundRelist() tea.Cmd {
	m.agents.seq++
	return listPlaygroundCmd(m.agents.ctx, m.agents.api.Sessions, m.pg.trees, m.repoDir, m.agents.seq)
}

// playgroundMsg handles the Playground's own replies; handled is false for any
// other message.
func (m Model) playgroundMsg(msg tea.Msg) (bool, Model, tea.Cmd) {
	if handled, nm, cmd := m.pgReviewMsg(msg); handled {
		return true, nm, cmd
	}
	switch msg := msg.(type) {
	case pgReadMsg[playgroundapi.ChangesView], pgReadMsg[playgroundapi.PRView], pgReadMsg[playgroundapi.ChecksView],
		pgReadMsg[playgroundapi.FileDiffView], pgReadMsg[playgroundapi.LinksView], pgLinkMsg:
		nm, cmd := m.pgChangesMsg(msg)
		return true, nm, cmd
	case pgAttachTickMsg:
		if msg.gen != m.pg.gen || m.pg.pending == "" || m.pg.pending != m.pgWanted() {
			return true, m, nil // the selection moved on while the tick ran
		}
		return true, m, m.pgAttach(m.pg.pending, false)
	case agentAttachedMsg, agentRetryMsg, agentFramesMsg, agentInputMsg, agentInputSentMsg, agentTookMsg, agentResizeTickMsg:
		if !m.pg.own {
			return false, m, nil
		}
		nm, cmd := m.pgTerminalMsg(msg)
		return true, nm, cmd
	case pgListMsg:
		nm, cmd := m.onPlaygroundList(msg)
		return true, nm, cmd
	case pgSubscribedMsg:
		if msg.err != nil {
			m.pg.subscribed = false // retried the next time the workspace is entered
			return true, m, nil
		}
		return true, m, pgWaitCmd(msg.ch)
	case pgChangedMsg:
		if !m.agents.active() {
			return true, m, nil
		}
		list := m.playgroundRelist()
		return true, m, tea.Batch(list, pgWaitCmd(msg.ch))
	case pgClosedMsg:
		m.pg.subscribed = false
		return true, m, nil
	}
	return false, m, nil
}

func (m Model) onPlaygroundList(msg pgListMsg) (Model, tea.Cmd) {
	if msg.seq <= m.agents.applied {
		return m, nil // older than what is shown
	}
	m.agents.applied = msg.seq
	p := &m.pg
	if msg.err != nil || (msg.degraded && p.loaded) {
		// Keep the last good list; a failure never empties it.
		p.stale = true
		m.agents.err = "Session list stale"
		if msg.err != nil {
			p.errMsg = pgClean(msg.err.Error(), 100)
		} else {
			p.errMsg = "worktrees could not be verified"
		}
		return m, nil
	}
	p.recs, p.loaded, p.stale, p.errMsg = msg.recs, true, msg.degraded, ""
	if msg.degraded {
		p.errMsg = "worktrees could not be verified"
	}
	m.agents.err = ""
	m.agents.sessions = nil
	for _, r := range msg.recs {
		m.agents.sessions = append(m.agents.sessions, r.Session)
	}
	if m.agents.enterAfter != "" && msg.seq >= m.agents.enterSeq && !hasSession(m.agents.sessions, m.agents.enterAfter) {
		m.agents.enterAfter = ""
	}
	m.agents.byTree = msg.byTree
	m.refreshAgentBadges()
	if o := m.agents.overlay; o != nil && o.sessions != nil {
		o.sessions.setSessions(m.orderedSessions(o.sessions.path))
	}
	m.playgroundReconcile()
	if m.pg.chg.open && m.pg.sel != m.pg.chg.session {
		m.pg.chg.open, m.pg.chg.diffOpen = false, false // the session left the list
	}
	return m, m.playgroundStoredUsage()
}

// playgroundRows is the list as currently filtered, in stable order.
func (m Model) playgroundRows() []pgRow {
	return pgBuild(m.pg.recs, m.garden.board.collection, m.agents.ack, pgOpts{sub: m.pg.sub, all: m.pg.all, text: m.pg.query, group: m.pg.group, links: m.pg.linkMap})
}

// playgroundReconcile keeps the selection on its session ID and the window on it.
func (m *Model) playgroundReconcile() {
	rows := m.playgroundRows()
	m.pg.sel, m.pg.selIdx = pgReconcile(rows, m.pg.sel, m.pg.selIdx)
	m.pg.top = m.pgScroll(rows)
}

// playgroundSelected returns the selected row.
func (m Model) playgroundSelected() (pgRow, bool) {
	for _, r := range m.playgroundRows() {
		if r.ID == m.pg.sel {
			return r, true
		}
	}
	return pgRow{}, false
}

// playgroundStoredUsage reads the stored usage snapshot of each account that
// has sessions here and no data yet. UsageStored never calls a provider, and
// each account is asked once, so navigating can never cause a refresh.
func (m *Model) playgroundStoredUsage() tea.Cmd {
	acc := m.agents.api.Accounts
	if acc == nil || !m.agents.active() {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range m.pg.recs {
		id := r.Session.AccountID
		if id == "" || m.pg.usageAsked[id] || m.agents.usage[id].Have {
			continue
		}
		m.pg.usageAsked[id] = true
		read := usagestore.Stored(m.agents.ctx, acc, id)
		cmds = append(cmds, func() tea.Msg {
			if r, ok := read().(usagestore.Result); ok {
				return usagestore.Observation(r)
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

// playgroundEnterBody gives the body the keyboard.
func (m Model) playgroundEnterBody() (Model, tea.Cmd) {
	m.shellFocus = shellBody
	if m.pg.searching {
		m.shellFocus = shellInput
	}
	return m, nil
}

// playgroundSetSub switches the submenu entry; the selection stays on its
// session when that session is also listed there.
func (m Model) playgroundSetSub(s pgSub) Model {
	m.pg.sub = (s + pgSubCount) % pgSubCount
	m.playgroundReconcile()
	return m
}

func (m Model) playgroundKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.pg
	k := msg.String()
	if p.searching {
		switch k {
		case "esc":
			p.query, p.searching = p.before, false
			m.shellFocus = shellBody
		case "enter":
			p.searching = false
			m.shellFocus = shellBody
		default:
			var cmd tea.Cmd
			p.search, cmd = p.search.Update(msg)
			p.query = p.search.Value()
			m.playgroundReconcile()
			return m, cmd
		}
		m.playgroundReconcile()
		return m, nil
	}
	if p.rev.open {
		return m.pgReviewKey(msg)
	}
	if p.chg.open {
		return m.pgChangesKey(msg)
	}
	if p.open {
		return m.pgOpenKey(msg)
	}
	switch {
	case k == "r" && p.sub != pgSubUsage:
		return m.pgReviewOpen()
	case k == "c" && p.sub != pgSubUsage:
		return m.pgChangesOpen()
	case k == "esc":
		m.shellFocus = shellNav
	case k == "enter":
		nm, cmd := m.pgEnter()
		return nm, cmd
	case k == "v":
		nm, cmd := m.pgObserve()
		return nm, cmd
	case k == "up" || k == "k":
		m.playgroundMove(-1)
	case k == "down" || k == "j":
		m.playgroundMove(1)
	case key.Matches(msg, m.keys.Tab) || k == "right":
		m = m.playgroundSetSub(p.sub + 1)
	case k == "shift+tab" || k == "left":
		m = m.playgroundSetSub(p.sub - 1)
	case k == "/" && p.sub != pgSubUsage:
		return m.playgroundStartSearch()
	case k == "a" && p.sub == pgSubSessions:
		p.all = !p.all
		m.playgroundReconcile()
	case k == "g" && p.sub != pgSubUsage:
		p.group = !p.group
		m.playgroundReconcile()
	case k == "x":
		if r, ok := m.playgroundSelected(); ok && r.Rec.Session.State == "failed" && m.agents.ack != nil && !m.agents.ack[r.ID] {
			m.agents.ack[r.ID] = true // a map shared with every copy of the model, like the legacy tab
			m.playgroundReconcile()
		}
	case k == "i":
		return m.playgroundOpenIssue()
	case k == "o":
		return m.openAccounts(accounts.TabOverview)
	}
	return m, nil
}

// playgroundMove moves the cursor by one session in display order.
func (m *Model) playgroundMove(d int) {
	rows := m.playgroundRows()
	if len(rows) == 0 {
		return
	}
	_, i := pgReconcile(rows, m.pg.sel, m.pg.selIdx)
	i = max(0, min(i+d, len(rows)-1))
	m.pg.sel, m.pg.selIdx = rows[i].ID, i
	m.pg.top = m.pgScroll(rows)
}

func (m Model) playgroundStartSearch() (Model, tea.Cmd) {
	p := &m.pg
	p.before = p.query
	p.search = textinput.New()
	// Search handles keys only, so a static cursor avoids unhandled blink messages.
	p.search.Cursor.SetMode(cursor.CursorStatic)
	p.search.SetValue(p.query)
	p.search.CharLimit = 256
	p.search.Focus()
	p.searching = true
	m.shellFocus = shellInput // the shell can no longer read q, ?, ctrl+k or tab as commands
	return m, nil
}

// playgroundOpenIssue opens the linked Issue in the Garden board. It uses only
// the already-loaded collection; with no such record it does nothing.
func (m Model) playgroundOpenIssue() (Model, tea.Cmd) {
	r, ok := m.playgroundSelected()
	g := &m.garden
	if !ok || !r.Task || g.api == nil || !g.hasStatus || !g.status.Caps.Read || !g.board.loaded {
		return m, nil
	}
	rec, ok := pgTask(g.board.collection, r.Ref)
	if !ok {
		return m, nil
	}
	m, cmd := m.selectWorkspace(wsGarden)
	m.shellFocus = shellBody
	g = &m.garden
	g.sub = subBoard
	g.board.selection.Focus(g.board.collection, rec.Item.Issue.ID)
	g.board.opened, g.board.section, g.board.docScroll = rec.Item.Issue.ID, 0, 0
	return m, cmd
}
