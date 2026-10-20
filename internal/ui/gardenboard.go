package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// gardenLayout is how the one approved-work selection is drawn; Board and Table
// share the snapshot, query, selection and detail, never a second store.
type gardenLayout int

const (
	layoutBoard gardenLayout = iota
	layoutTable
)

type gardenBoardState struct {
	snapshot           gardensync.Snapshot
	loaded             bool
	query              work.Query
	collection         work.Collection
	selection          work.Selection
	layout             gardenLayout
	sort               work.Sort
	tableScroll        int
	rows               []work.Record // Rows(sort), rebuilt only when the collection or sort changes
	tableRow           int           // deletion anchor: the selection's last index in rows
	opened             garden.IssueID
	section, docScroll int
	searching          bool
	search             textinput.Model
	beforeSearch       work.Query
	beforeSelection    work.Selection
	notice             string
	memoryLoaded       bool
	rememberedView     string
	saveGen            uint64
}
type gardenWorkMsg struct {
	seq  uint64
	view gardenapi.WorkView
	err  error
}
type gardenViewSavedMsg struct {
	gen uint64
	err error
}

func (m Model) gardenLoadWork() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() {
		return m, nil
	}
	ctx, seq := g.begin(busyWork)
	api := g.api
	return m, func() tea.Msg { v, err := api.Work(ctx); return gardenWorkMsg{seq, v, err} }
}
func (m Model) onGardenWork(msg gardenWorkMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	b := &g.board
	if fail != nil {
		g.err = fail
		b.snapshot.Source = gardensync.SourceStale
		b.notice = "Refresh failed; retained last good board."
		return m, nil
	}
	s := msg.view.Stamp
	if s.GardenID != g.status.Target.RepoID || s.Epoch != g.status.Stamp.Epoch || !g.gate.Accept(s) {
		g.err = staleReply
		return m, nil
	}
	snap := msg.view.Snapshot
	// The backend owns last-good replacement. Never accept a snapshot stamped
	// for a different clone/epoch even if its enclosing reply is current.
	if snap.GardenID.Repo != 0 && (uint64(snap.GardenID.Repo) != s.GardenID || snap.LocalRepoID.String() != s.LocalRepoID || snap.Epoch != s.Epoch) {
		g.err = staleReply
		return m, nil
	}
	b.snapshot, b.loaded = snap, true
	b.notice = ""
	if snap.Source == gardensync.SourceStale {
		b.notice = "Refresh incomplete; showing last good board."
		if snap.SuccessfulAt.IsZero() {
			b.notice = "No successful read yet; approved work is unknown. Refresh to retry."
		}
	}
	b.reselect()
	g.drafts.pendingErr = msg.view.PendingCapturesErr
	if g.drafts.pendingErr == nil {
		g.drafts.pending = msg.view.PendingCaptures
	}
	g.drafts.reselect(snap)
	g.drafts.checkRemote(snap)
	m.gardenBoardVisible()
	m.gardenDraftsVisible()
	m.gardenClampDraftDocument()
	m.gardenClampDocument()
	g.status.LastSync = snap.SuccessfulAt
	g.status.IssueCount = len(snap.Items)
	g.status.Sync = gardensync.Status(gardensync.AssociationView{State: gardensync.AssociationState(g.status.State)}, snap).State
	cmd := m.gardenRememberView()
	if b.opened == 0 && g.planrev.stage != planNone {
		m = m.planClose()
		g = &m.garden
	}
	if b.opened == 0 && g.planner.stage != plannerNone {
		// The Issue left this view under its planner screen: the screen goes with it, the session keeps running.
		var rel tea.Cmd
		m, rel = m.gardenPlannerClose()
		cmd = tea.Batch(cmd, rel)
	}
	return m, cmd
}
func (b *gardenBoardState) reselect() {
	b.collection = work.Select(b.snapshot.Items, b.query, b.snapshot.Findings...)
	b.rows = b.collection.Rows(b.sort)
	if b.layout == layoutTable {
		if notice := b.tableFocus(); notice != "" {
			b.notice = notice
		}
	} else if reason := b.selection.Reconcile(b.collection); reason != "" {
		b.notice = reason
	}
	if b.opened != 0 {
		if _, _, ok := b.collection.Find(b.opened); !ok {
			b.opened = 0
			b.docScroll = 0
			b.notice = "Opened issue left this view; returned to " + b.layoutName() + "."
		}
	}
}
func (b *gardenBoardState) layoutName() string {
	if b.layout == layoutTable {
		return "Table"
	}
	return "Board"
}

// rowIndex is where the shared selection sits in the cached Table order, or -1.
// The selection ID is the only identity; tableRow just anchors a deletion.
func (b *gardenBoardState) rowIndex() int {
	if b.selection.ID == 0 {
		return -1
	}
	return slices.IndexFunc(b.rows, func(r work.Record) bool { return r.Item.Issue.ID == b.selection.ID })
}

// tableFocus makes the Table cursor follow the shared selection. When the
// selected Issue vanished it lands on the row now at its old index (the previous
// row if it was last) and returns a notice saying which.
func (b *gardenBoardState) tableFocus() string {
	if i := b.rowIndex(); i >= 0 {
		b.selection.Focus(b.collection, b.selection.ID)
		b.tableRow = i
		return ""
	}
	left := b.selection.ID != 0
	was := b.tableRow
	b.selection.ID = 0
	b.tableRow = max(0, min(was, len(b.rows)-1))
	if len(b.rows) == 0 {
		if left {
			return "Selected issue left this view; no matching work remains."
		}
		return ""
	}
	b.selection.Focus(b.collection, b.rows[b.tableRow].Item.Issue.ID)
	switch {
	case !left:
		return ""
	case was >= len(b.rows):
		return "Selected issue left this view; selected the previous row."
	}
	return "Selected issue left this view; selected the row now in its place."
}
func (b *gardenBoardState) setLayout(l gardenLayout) {
	b.layout = l
	if l == layoutTable {
		b.tableFocus()
	}
}
func (m *Model) gardenRememberView() tea.Cmd {
	if m.state == nil {
		return nil
	}
	view := "setup"
	if m.gardenScreen() == scrBoard {
		view = "board"
		if m.garden.board.layout == layoutTable {
			view = "table"
		}
	}
	if m.garden.board.rememberedView == view {
		return nil
	}
	m.garden.board.rememberedView = view
	m.garden.board.saveGen++
	gen, st, dir := m.garden.board.saveGen, m.state, m.repoDir
	return func() tea.Msg { return gardenViewSavedMsg{gen, st.PersistGardenView(gen, dir, view)} }
}

func gardenBoardColumns(width int) int {
	if width < 120 {
		return 4
	}
	return int(work.ColumnCount)
}
func gardenBoardCapacity(rows int) int { return max(1, (rows-5)/6) }
func (m Model) gardenBoardRows() int {
	return m.height - shellHeaderRows - shellFooterRows - settingsSubmenuRows
}
func (m *Model) gardenBoardVisible() {
	b := &m.garden.board
	b.selection.EnsureVisible(b.collection, gardenBoardCapacity(m.gardenBoardRows()), gardenBoardColumns(m.width))
	b.tableScroll = tableScrollFor(b.tableScroll, b.rowIndex(), gardenTableCapacity(m.gardenBoardRows()), len(b.collection.Records))
}
func (m Model) gardenStartSearch() (Model, tea.Cmd) {
	b := &m.garden.board
	b.beforeSearch = b.query
	b.beforeSelection = b.selection
	b.search = textinput.New()
	// Search handles keys only, so a static cursor avoids scheduling unhandled blink messages.
	b.search.Cursor.SetMode(cursor.CursorStatic)
	b.search.SetValue(b.query.Text)
	b.search.CharLimit = 256
	b.search.Focus()
	b.searching = true
	m.shellFocus = shellInput
	return m, nil
}
func (m Model) gardenBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &m.garden.board
	k := msg.String()
	if b.searching {
		switch k {
		case "esc":
			b.query = b.beforeSearch
			b.selection = b.beforeSelection
			b.searching = false
			m.shellFocus = shellBody
			b.reselect()
		case "enter":
			b.searching = false
			m.shellFocus = shellBody
		default:
			var cmd tea.Cmd
			b.search, cmd = b.search.Update(msg)
			b.query.Text = b.search.Value()
			b.reselect()
			m.gardenBoardVisible()
			return m, cmd
		}
		b.notice = ""
		m.gardenBoardVisible()
		return m, nil
	}
	if m.garden.planrev.stage != planNone {
		if b.opened != 0 {
			return m.gardenPlanKey(msg)
		}
		// The Issue left this view under the plan screen: drop the screen with it.
		m = m.planClose()
		b = &m.garden.board
	}
	if m.garden.planner.stage != plannerNone {
		if b.opened != 0 {
			return m.gardenPlannerKey(msg)
		}
		// The Issue left this view under the planner screen: drop the screen with it.
		var rel tea.Cmd
		m, rel = m.gardenPlannerClose()
		b = &m.garden.board
		if key.Matches(msg, m.keys.Refresh) {
			nm, cmd := m.gardenLoadWork()
			return nm, tea.Batch(rel, cmd)
		}
		return m, rel
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenLoadWork()
	}
	if b.opened != 0 {
		switch k {
		case "p":
			return m.gardenOpenPlanner()
		case "r":
			return m.gardenOpenPlanReview()
		case "esc", "backspace":
			b.opened = 0
			b.docScroll = 0
		case "left", "h":
			b.section = max(0, b.section-1)
			b.docScroll = 0
		case "right", "l":
			b.section = min(len(gardenDocumentSections)-1, b.section+1)
			b.docScroll = 0
		case "down", "j":
			b.docScroll++
		case "up", "k":
			b.docScroll = max(0, b.docScroll-1)
		case "pgdown":
			b.docScroll += max(1, m.height-12)
		case "pgup":
			b.docScroll = max(0, b.docScroll-max(1, m.height-12))
		case "home":
			b.docScroll = 0
		case "end":
			b.docScroll = 1 << 20
		}
		m.gardenClampDocument()
		return m, nil
	}
	if key.Matches(msg, m.keys.GardenView) {
		if b.layout == layoutBoard {
			b.setLayout(layoutTable)
		} else {
			b.setLayout(layoutBoard)
		}
		m.gardenBoardVisible()
		cmd := m.gardenRememberView()
		return m, cmd
	}
	switch k {
	case "esc":
		m = m.gardenBack(scrBoard)
		cmd := m.gardenRememberView()
		return m, cmd
	case "enter":
		if b.selection.ID != 0 {
			b.opened = b.selection.ID
			b.section, b.docScroll = 0, 0
		}
	case "/":
		return m.gardenStartSearch()
	case "t":
		types := []garden.ItemType{"", garden.TypeFeature, garden.TypeBug, garden.TypeChore, garden.TypeResearch}
		idx := 0
		for i, v := range types {
			if v == b.query.Type {
				idx = (i + 1) % len(types)
				break
			}
		}
		b.query.Type = types[idx]
		b.reselect()
	case "p":
		values := []string{"", "P0", "P1", "P2", "P3", "?"}
		idx := 0
		for i, v := range values {
			if v == b.query.Priority {
				idx = (i + 1) % len(values)
				break
			}
		}
		b.query.Priority = values[idx]
		b.reselect()
	case "x":
		b.query = work.Query{}
		b.reselect()
	default:
		if key.Matches(msg, m.keys.Tab) && m.canToggleSub() {
			m = m.gardenToggleSub()
			cmd := m.gardenRememberView()
			return m, cmd
		}
		if b.layout == layoutTable {
			m.gardenTableKey(k)
		} else {
			m.gardenBoardMove(k)
		}
	}
	m.gardenBoardVisible()
	return m, nil
}

func (m *Model) gardenBoardMove(k string) {
	b := &m.garden.board
	switch k {
	case "left", "h":
		b.selection.Move(b.collection, -1, 0)
	case "right", "l":
		b.selection.Move(b.collection, 1, 0)
	case "up", "k":
		b.selection.Move(b.collection, 0, -1)
	case "down", "j":
		b.selection.Move(b.collection, 0, 1)
	case "pgup":
		b.selection.Move(b.collection, 0, -gardenBoardCapacity(m.gardenBoardRows()))
	case "pgdown":
		b.selection.Move(b.collection, 0, gardenBoardCapacity(m.gardenBoardRows()))
	}
}

func (m Model) gardenBoardHints() []shellHint {
	b := m.garden.board
	if m.planActive() {
		return m.gardenPlanHints()
	}
	if m.plannerActive() {
		return m.gardenPlannerHints()
	}
	if b.opened != 0 {
		return []shellHint{{"esc", b.layoutName()}, {"p", "request plan"}, {"r", "review plans"}, {"←/→", "section"}, {"↑/↓", "scroll"}, {"pgup/dn", "page"}, {m.keys.Refresh.Help().Key, "refresh"}}
	}
	view := m.keys.GardenView.Help().Key
	h := []shellHint{{"←/→", "state"}, {"↑/↓", "item"}, {"enter", "open"}, {"/", "search"}, {"t/p", "type/priority"}, {"x", "clear"}, {view, "table"}, {m.keys.Refresh.Help().Key, "refresh"}, {"esc", "parent"}}
	if b.layout == layoutTable {
		h = []shellHint{{"↑/↓", "select"}, {"enter", "open"}, {"/", "search"}, {"t/p", "type/priority"}, {"o", "sort"}, {"x", "clear"}, {view, "board"}, {m.keys.Refresh.Help().Key, "refresh"}, {"esc", "parent"}}
	}
	if m.canToggleSub() {
		h = append(h, shellHint{m.keys.Tab.Help().Key, "submenu"})
	}
	return h
}
func (m Model) gardenBoardStatus() string {
	b := m.garden.board
	state := "synced"
	if !b.loaded {
		state = "not loaded"
	}
	if b.snapshot.Source == gardensync.SourceStale || m.garden.err != nil {
		state = "stale"
	}
	for _, p := range b.snapshot.Problems {
		if p.Code == "offline" {
			state = "offline / stale"
		}
		if p.Code == "auth-required" || p.Code == "permission-denied" {
			state = "access revoked / stale"
		}
	}
	if m.garden.err != nil && m.garden.err.Code == gardenapi.CodeOffline {
		state = "offline / stale"
	}
	if m.garden.busy == busyWork {
		state = "refreshing"
	}
	last := "never"
	if !b.snapshot.SuccessfulAt.IsZero() {
		last = b.snapshot.SuccessfulAt.Local().Format(time.DateTime)
	}
	return state + " · sync " + last
}
func (m Model) gardenBoardIdentity() string {
	b := m.garden.board
	id := b.selection.ID
	if b.opened != 0 {
		id = b.opened
	}
	if r, _, ok := b.collection.Find(id); ok {
		return fmt.Sprintf("#%d  %s / %s", r.Item.Issue.Number, boardTitle(r.Item.Issue.Title), r.Column)
	}
	if b.layout == layoutTable {
		return "Table / empty"
	}
	return "Board / " + b.selection.Column.String() + " / empty"
}
func (m Model) gardenBoardSaved(msg gardenViewSavedMsg) Model {
	if msg.err != nil && !errors.Is(msg.err, config.ErrSuperseded) && msg.gen == m.garden.board.saveGen {
		m.garden.board.notice = "Could not remember Garden view."
		m.garden.board.rememberedView = ""
	}
	return m
}

var gardenDocumentSections = []string{"Overview", "Specification", "Questions", "Plans", "Execution"}

func (m *Model) gardenClampDocument() {
	width := m.width
	if width >= 120 {
		width -= 26
	}
	lines := m.gardenDocument(width)
	m.garden.board.docScroll = max(0, min(m.garden.board.docScroll, max(0, len(lines)-max(1, m.height-12))))
}
func gardenIssueBody(body string) string {
	if strings.HasPrefix(body, "<!-- bonsai:garden-metadata/v1 ") {
		_, body, _ = strings.Cut(body, "\n")
	}
	return boardSafe(body)
}
