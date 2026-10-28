package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

func attentionEnv(t *testing.T, w, h int) (Model, *gardenfake.Backend) {
	t.Helper()
	fb := gardenFake(gardenapi.StateConnected)
	for i, stage := range []garden.Stage{garden.StageReady, garden.StageInProgress, garden.StageReady, garden.StageReady, garden.StageReview, garden.StageBlocked} {
		it := boardItem(t, i+1, stage, i == 3)
		if i%2 == 1 {
			it.Issue.Labels[1] = garden.FormatTypeLabel(garden.TypeBug)
			it.Issue.Labels[3] = "priority:p2"
		}
		fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, it)
	}
	fb.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{
		1: {Planner: &gardenapi.LocalRunSummary{Live: true, Activity: localAct(agentactivity.Idle, "")}},
		2: {Implementation: &gardenapi.LocalRunSummary{Live: true, Activity: localAct(agentactivity.Working, "")}},
		3: {Planner: &gardenapi.LocalRunSummary{Live: true, Activity: localAct(agentactivity.Waiting, "")}},
		6: {Implementation: &gardenapi.LocalRunSummary{State: "failed"}},
	}
	return gardenEnv(t, fb, config.Prefs{}, w, h), fb
}

func TestGardenAttentionCountsAndSharedFilters(t *testing.T) {
	m, fb := attentionEnv(t, 160, 48)
	reads := fb.Count("Work")
	m = pressAll(t, m, "a")
	if m.garden.board.layout != layoutAttention || len(m.garden.board.attn.records) != 4 {
		t.Fatal("wrong needs", rowIDs(m.garden.board.attn.records))
	}
	for _, keys := range [][]string{nil, {"t"}, {"p"}, {"x", "/", "T", "a", "s", "k", " ", "4", "enter"}, {"x", "t", "p"}} {
		m = pressAll(t, m, keys...)
		b := &m.garden.board
		b.notice = ""
		s := b.collection.Summarize()
		if s.NeedsYou != len(b.attn.records) || !slices.Equal(rowIDs(b.attn.records), rowIDs(work.NeedsYou(b.rows))) {
			t.Fatal("count diverged")
		}
		out := view(m)
		mustContain(t, "filtered attention counts", out, fmt.Sprintf("NEEDS YOU  %d", s.NeedsYou), fmt.Sprintf("Needs you %d", s.NeedsYou), fmt.Sprintf("%d of %d need you", s.NeedsYou, len(b.collection.Records)))
		head := ansi.Strip(m.gsTable(m.width, m.gardenBoardRows())[0])
		want := fmt.Sprintf("NEEDS YOU  %d", s.NeedsYou)
		if b.query.Active() {
			want += " · filtered"
		}
		mustContain(t, "attention head", head, want)
		mustNotContain(t, "attention denominator", head, " / "+fmt.Sprint(b.collection.Total))
		query := b.query
		m = pressAll(t, m, "v")
		if m.garden.board.layout != layoutTable || m.garden.board.query != query {
			t.Fatal("v lost shared query/layout")
		}
		m.garden.board.notice = ""
		mustContain(t, "summary", view(m), fmt.Sprintf("%d active", s.Active), fmt.Sprintf("%d need you", s.NeedsYou), fmt.Sprintf("%d blocked", s.Blocked))
		m = pressAll(t, m, "a")
	}
	if fb.Count("Work") != reads || fb.Writes() != 0 {
		t.Fatal("navigation read or wrote backend")
	}
}

func TestGardenAttentionStaleAndNonRequestsExcluded(t *testing.T) {
	m, _ := attentionEnv(t, 120, 35)
	b := &m.garden.board
	b.evidence = map[garden.IssueID]work.Evidence{
		1: {Planner: &work.RunEvidence{Live: true, Activity: work.ActivityUnknown}},
		2: {Implementation: &work.RunEvidence{Live: true, Activity: work.ActivityIdle}},
		3: {Stale: true, Planner: &work.RunEvidence{Live: true, Activity: work.ActivityNeedsInput, PlanReady: true}},
	}
	b.reselect()
	b.setLayout(layoutAttention)
	if got := rowIDs(b.attn.records); !slices.Equal(got, []garden.IssueID{4, 5, 6}) {
		t.Fatal(got)
	}
}

func TestGardenTableGroupingCollapseAndValueCopies(t *testing.T) {
	for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			m, fb := attentionEnv(t, 120, 35)
			// An unproven retained blocker belongs only to Unplaced.
			it := boardItem(t, 7, garden.StageBlocked, false)
			it.Verification.SupersededApprovals = slices.Clone(it.Verification.ValidApprovals)
			it.Verification.ValidApprovals = nil
			it.Verification.AcceptedSpecHash = ""
			fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, it)
			m = pressAll(t, m, "R")
			b := &m.garden.board
			b.setLayout(layout)
			b.selection.Focus(b.collection, 6)
			b.tableFocus()
			m = pressAll(t, m, "g")
			b = &m.garden.board
			v := b.tableView()
			if !v.grouped || b.selection.ID != 6 {
				t.Fatal("g changed identity")
			}
			seen := 0
			for _, l := range v.lines {
				if !l.header && l.rec.Item.Issue.ID == 6 {
					seen++
					if l.phase != work.Building {
						t.Fatal("blocker moved out of proven phase")
					}
				}
			}
			if seen != 1 || v.headerIndex(work.PhaseCount) < 0 {
				t.Fatal("duplicate blocker or missing Unplaced")
			}
			before := m
			m = pressAll(t, m, " ")
			b = &m.garden.board
			v = b.tableView()
			if !v.collapsed[work.Building] || !v.lines[v.line].header || b.selection.ID != 6 {
				t.Fatal("collapse lost identity/header")
			}
			mustContain(t, "collapse notice", b.notice, "Building collapsed; selected #106 is hidden inside.")
			if before.garden.board.tableView().collapsed[work.Building] {
				t.Fatal("collapse array aliased")
			}
			if len(before.garden.board.tableView().lines) == len(v.lines) {
				t.Fatal("old lines mutated")
			}
			m = pressAll(t, m, "enter")
			b = &m.garden.board
			v = b.tableView()
			if v.collapsed[work.Building] || v.lines[v.line].header || b.opened != 0 || b.selection.ID != 6 {
				t.Fatal("Enter on header did not expand to selected item")
			}
			m = tpress(t, m, "home", "down", "up")
			b = &m.garden.board
			v = b.tableView()
			id := b.selection.ID
			if !v.lines[v.line].header {
				t.Fatal("movement skipped header")
			}
			m = pressAll(t, m, "enter", "enter", "g")
			if m.garden.board.selection.ID != id || m.garden.board.tableView().grouped {
				t.Fatal("header/grouping changed identity")
			}
		})
	}
}

func TestGardenAttentionToggleAndFallback(t *testing.T) {
	m, _ := attentionEnv(t, 80, 24)
	id := m.garden.board.selection.ID
	m = pressAll(t, m, "a")
	if m.garden.board.selection.ID == id || !strings.Contains(m.garden.board.notice, "does not need you; showing #") {
		t.Fatal("missing fallback notice")
	}
	selected := m.garden.board.selection.ID
	m = pressAll(t, m, "a")
	if m.garden.board.layout != layoutBoard || m.garden.board.selection.ID != id {
		t.Fatal("a did not return to Board")
	}
	m = pressAll(t, m, "v", "a", "a")
	if m.garden.board.layout != layoutTable || m.garden.board.selection.ID != id {
		t.Fatal("a did not return to Table")
	}
	m = pressAll(t, m, "a", "v")
	if m.garden.board.layout != layoutTable {
		t.Fatal("v from attention did not open Table")
	}
	m = pressAll(t, m, "a")
	b := &m.garden.board
	b.query.Text = "Task 1 "
	b.reselect()
	if len(b.attn.records) != 0 || b.selection.ID != selected {
		t.Fatal("empty attention discarded shared selection")
	}
	m.gardenBoardVisible()
	mustContain(t, "empty", view(m), "(nothing matching this filter needs you)", "Needs you / empty")
	m = pressAll(t, m, "enter")
	if m.garden.board.opened != 0 {
		t.Fatal("empty attention opened hidden identity")
	}
}

func TestGardenAttentionRefreshAnchorErrorAndOverlay(t *testing.T) {
	m, fb := attentionEnv(t, 120, 35)
	m = pressAll(t, m, "a", "down")
	if m.garden.board.selection.ID != 4 {
		t.Fatal("fixture order")
	}
	// Remove this need, but keep the accepted issue in the shared collection.
	fb.WorkView.Snapshot.Items[3] = boardItem(t, 4, garden.StageReady, false)
	m = pressAll(t, m, "R")
	if m.garden.board.selection.ID != 5 || !strings.Contains(m.garden.board.notice, "selected #105") {
		t.Fatal("refresh failed to use in-place anchor", m.garden.board.notice)
	}
	m = pressAll(t, m, "g", "enter")
	opened, focus := m.garden.board.opened, m.shellFocus
	m = pressAll(t, m, "R")
	if m.garden.board.opened != opened || m.shellFocus != focus {
		t.Fatal("refresh stole overlay focus")
	}
	m = pressAll(t, m, "esc")
	before := m.garden.board.attn
	fb.WorkErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "Offline fixture"}
	m = pressAll(t, m, "R")
	if !reflect.DeepEqual(m.garden.board.attn, before) {
		t.Fatal("failed refresh replaced rows/cursor")
	}
	mustContain(t, "stale", view(m), "Offline fixture")
}

func TestGardenTablesReferenceRendersAndOverlayOrigins(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
			for _, grouped := range []bool{false, true} {
				for _, ascii := range []bool{false, true} {
					t.Run(fmt.Sprintf("%v/%d/group%t/ascii%t", size, layout, grouped, ascii), func(t *testing.T) {
						m, _ := attentionEnv(t, size[0], size[1])
						if ascii {
							m.work.AgentGlyphs = "ascii"
						}
						m.garden.board.setLayout(layout)
						if grouped {
							m = pressAll(t, m, "g")
						}
						m.gardenBoardVisible()
						m.garden.board.notice = ""
						name := m.garden.board.layoutName()
						mustContain(t, "chrome", view(m), name, "ISSUE", "TITLE", "STATE", "NEXT", "Group: ")
						if size[0] >= 120 {
							mustContain(t, "wide", view(m), "SIGNAL", "Enter ")
						}
						if size[0] == 160 {
							mustContain(t, "widest", view(m), "TYPE", "P2")
						}
						if grouped {
							mustContain(t, "groups", view(m), "Building", "Review")
						}
						assertBounded(t, "reference", m.View(), size[0], size[1])
						if ascii && len(fixtureFree(view(m))) > 0 {
							t.Fatal("non-ASCII chrome", string(fixtureFree(view(m))))
						}
						m = tpress(t, m, "end")
						b := m.garden.board
						m = pressAll(t, m, "enter")
						if m.garden.board.opened != b.selection.ID {
							t.Fatal("wrong overlay identity")
						}
						mustContain(t, "origin", strings.Join(m.gardenCrumbs(), " / "), "Garden / "+name+" / #")
						m = pressAll(t, m, "esc")
						if !reflect.DeepEqual(*m.garden.board.tableView(), *b.tableView()) || m.garden.board.selection.ID != b.selection.ID {
							t.Fatal("overlay lost origin state")
						}
					})
				}
			}
		}
	}
}

func TestGardenTablesMouseMatchesLines(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}} {
		for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
			m, _ := attentionEnv(t, size[0], size[1])
			m.garden.board.setLayout(layout)
			m = pressAll(t, m, "g")
			m = tpress(t, m, "home")
			v := m.garden.board.tableView()
			first := v.lines[0].phase
			click := func(line int) {
				t.Helper()
				msg := tea.MouseMsg{X: size[0] - 1, Y: shellHeaderRows + settingsSubmenuRows + 3 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
				next, cmd := m.gardenMouse(msg)
				m, _ = drainAll(t, next.(Model), cmd)
			}
			id := m.garden.board.selection.ID
			click(0)
			if !m.garden.board.tableView().collapsed[first] || m.garden.board.opened != 0 || m.garden.board.selection.ID != id {
				t.Fatal("header click did not collapse")
			}
			click(0)
			v = m.garden.board.tableView()
			want := v.lines[1].rec.Item.Issue.ID
			click(1)
			if m.garden.board.opened != want || m.garden.board.selection.ID != want {
				t.Fatal("item click did not open", want, m.garden.board.opened)
			}
			m = pressAll(t, m, "esc")
			if id, ok := m.gardenListHit(1, shellHeaderRows+settingsSubmenuRows+4); !ok || id != want {
				t.Fatal("item line must be clickable past the margin")
			}
			for _, line := range []int{3, 4} {
				if _, ok := m.gardenTableHit(0, shellHeaderRows+settingsSubmenuRows+line); ok {
					t.Fatal("shell margin is not a header or item hit")
				}
			}
			if _, ok := m.gardenTableHit(1, shellHeaderRows+settingsSubmenuRows+3+gardenTableCapacity(m.gardenBoardRows())); ok {
				t.Fatal("strip/footer clickable")
			}
		}
	}
}

func TestGardenCompactEveryExactStateAndNextActions(t *testing.T) {
	m, _ := attentionEnv(t, 80, 24)
	b := &m.garden.board
	// Rendering accepts the projection's full nine-state vocabulary, including
	// Reviewing which has no current backend placement producer.
	c := work.Collection{}
	for col := work.Column(0); col < work.ColumnCount; col++ {
		r := work.Record{Column: col, Phase: col.Phase(), Placed: col != work.Blocked}
		r.Item.Issue = garden.Issue{ID: garden.IssueID(col + 1), Number: int64(col + 101), Title: "A complete exact state"}
		c.Records = append(c.Records, r)
		c.Columns[col] = append(c.Columns[col], r)
		if r.Placed {
			c.Phases[r.Phase] = append(c.Phases[r.Phase], r)
		} else {
			c.Unplaced = append(c.Unplaced, r)
		}
	}
	b.collection = c
	b.rows = c.Rows(work.SortState)
	b.rebuildTables()
	b.setLayout(layoutTable)
	for _, key := range []string{"home", "down", "down", "down", "down", "down", "down", "down", "down", "up", "pgup", "end", "home"} {
		m = tpress(t, m, key)
		b = &m.garden.board
		v := b.tableView()
		if v.line < v.scroll || v.line >= v.scroll+gardenTableCapacity(m.gardenBoardRows()) {
			t.Fatal("selected state invisible")
		}
		for _, line := range m.gsTable(80, m.gardenBoardRows()) {
			if ansi.StringWidth(line) > 80 {
				t.Fatal("overflow")
			}
			for _, l := range v.lines[v.scroll:min(len(v.lines), v.scroll+gardenTableCapacity(m.gardenBoardRows()))] {
				if strings.Contains(ansi.Strip(line), fmt.Sprintf("#%d ", l.rec.Item.Issue.Number)) && !strings.Contains(ansi.Strip(line), boardState(l.rec)) {
					t.Fatal("state truncated", line)
				}
			}
		}
	}
	for _, tc := range []struct {
		r           work.Record
		short, long string
	}{
		{work.Record{Column: work.Blocked}, "Resolve", "Resolve blocker"},
		{work.Record{Column: work.HumanReview}, "Review", "Review changes"},
		{work.Record{Column: work.PlanReview, Accepted: true}, "Review", "Review plan"},
		{work.Record{Evidence: work.Evidence{Planner: &work.RunEvidence{Live: true, Activity: work.ActivityNeedsInput}}}, "Answer", "Answer the agent"},
		{work.Record{Accepted: true, Evidence: work.Evidence{Planner: &work.RunEvidence{PlanReady: true}}}, "Review", "Review and publish plan"},
	} {
		if gardenNext(tc.r, true) != tc.short || gardenNext(tc.r, false) != tc.long {
			t.Fatal("wrong next action")
		}
	}
}

func TestGardenAttentionMemoryPaletteAndKeys(t *testing.T) {
	m, fb := attentionEnv(t, 120, 35)
	if newKeyMap(nil).GardenAttention.Help().Key != "a" {
		t.Fatal("missing default key")
	}
	if err := validateKeyBinding("garden_attention", "g", nil); err == nil {
		t.Fatal("group key bindable")
	}
	if err := validateKeyBinding("garden_attention", "w", nil); err != nil {
		t.Fatal(err)
	}
	m.keys = newKeyMap(map[string]string{"garden_attention": "w"})
	m = pressAll(t, m, "a")
	if m.garden.board.layout != layoutBoard {
		t.Fatal("old key remains active")
	}
	gen := m.garden.board.saveGen
	m = pressAll(t, m, "w", "g", " ")
	if m.garden.board.saveGen != gen+1 || m.garden.board.rememberedView != "attention" || m.gardenRememberView() != nil {
		t.Fatal("attention view memory rewrites an unchanged layout")
	}
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.GardenView(m.repoDir) != "attention" {
		t.Fatal("attention not persisted")
	}
	next := sized(t, New(m.repoDir, &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(fb)), 120, 35)
	next, _ = drainAll(t, next, next.Init())
	if next.garden.board.layout != layoutAttention || next.garden.board.attn.grouped {
		t.Fatal("view memory restored more than layout")
	}
	tgt, ok := paletteTargetForLabel("Garden: Needs you")
	if !ok {
		t.Fatal("missing palette action")
	}
	next = pressAll(t, next, "enter")
	next, cmd := next.goToPalette(tgt)
	next, _ = drainAll(t, next, cmd)
	if next.garden.board.layout != layoutAttention || next.garden.board.opened != 0 {
		t.Fatal("palette did not restore attention parent")
	}
	next = pressAll(t, next, "tab")
	if next.gardenScreen() != scrDrafts {
		t.Fatal("Tab did not reach drafts")
	}
	next = pressAll(t, next, "tab")
	if next.garden.board.layout != layoutAttention || next.gardenScreen() != scrBoard {
		t.Fatal("Tab lost work layout")
	}
}

func TestGardenTableColumnBudgetAndSignals(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		c := gardenTableCols(width, 5)
		if c.title < 28 || c.next < 8 {
			t.Fatalf("%d: %+v", width, c)
		}
		used, count := 2, 0
		for _, w := range []int{c.issue, c.title, c.state, c.signal, c.next, c.typ, c.prio} {
			if w > 0 {
				used += w
				count++
			}
		}
		if used+(count-1)*tableGap > width {
			t.Fatal("column budget exceeds width", c)
		}
		for col := work.Column(0); col < work.ColumnCount; col++ {
			if c.state < ansi.StringWidth(boardState(work.Record{Column: col})) {
				t.Fatal("state budget", col, c)
			}
		}
		if width == 80 && (c.signal != 0 || c.next != 8 || c.typ != 0 || c.prio != 0) {
			t.Fatal("compact columns", c)
		}
		if width == 120 && (c.signal < 18 || c.next <= 8) {
			t.Fatal("medium columns", c)
		}
		if width == 160 && (c.typ != 8 || c.prio != 2) {
			t.Fatal("wide columns", c)
		}
	}
	r := work.Record{Column: work.PlanReview, Accepted: true}
	r.Item.Verification.SupersededApprovals = []garden.Approval{{}}
	if got := gardenTableSignal(r, true); !strings.HasPrefix(got, "! prior approval") || !strings.Contains(got, "Plan needs approval") {
		t.Fatal("warning lost", got)
	}
	if got := gardenNext(work.Record{Column: work.Running}, true); got != "Review" {
		t.Fatal(got)
	}
	r = work.Record{Column: work.Ready, Accepted: true}
	r.Item.Classification.Stage = garden.StageReady
	if gardenNext(r, true) != "Start" || gardenNext(r, false) != "Start implementation" {
		t.Fatal("start action")
	}
}

func TestGardenAttentionEmptyAndUnknown(t *testing.T) {
	m, _ := attentionEnv(t, 120, 35)
	m = pressAll(t, m, "a")
	b := &m.garden.board
	b.query.Text = "Task 2 "
	b.reselect()
	m.gardenBoardVisible()
	mustContain(t, "no matching requests", view(m), "(nothing matching this filter needs you)")
	b = &m.garden.board
	b.query.Text = ""
	b.snapshot.Items = b.snapshot.Items[1:2] // only working implementation
	b.reselect()
	m.gardenBoardVisible()
	mustContain(t, "no requests", view(m), "(nothing needs you)")
	b = &m.garden.board
	b.loaded = false
	mustContain(t, "unknown", view(m), "(unknown until a successful read)")
}

func TestGardenAttentionRefreshPreservesSearchAndHeaderFocus(t *testing.T) {
	m, _ := attentionEnv(t, 120, 35)
	m = pressAll(t, m, "a", "g")
	m = tpress(t, m, "home")
	phase := m.garden.board.attn.lines[m.garden.board.attn.line].phase
	id := m.garden.board.selection.ID
	m = pressAll(t, m, "R")
	v := m.garden.board.attn
	if !v.lines[v.line].header || v.lines[v.line].phase != phase || m.garden.board.selection.ID != id {
		t.Fatal("refresh moved header focus")
	}
	m = pressAll(t, m, "/", "T")
	focus := m.shellFocus
	m, cmd := m.gardenLoadWork()
	m, _ = drainAll(t, m, cmd)
	if !m.garden.board.searching || m.shellFocus != focus || m.garden.board.search.Value() != "T" {
		t.Fatal("refresh stole search focus")
	}
	m = pressAll(t, m, "esc")
	if m.garden.board.searching {
		t.Fatal("search did not close")
	}
}

func TestGardenTableFeedbackSurvivesSummary(t *testing.T) {
	for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
		for _, width := range []int{80, 120, 160} {
			for _, search := range []string{"", "foo"} {
				t.Run(fmt.Sprint(layout, width, search), func(t *testing.T) {
					m, _ := attentionEnv(t, width, 35)
					b := &m.garden.board
					b.setLayout(layout)
					b.query.Text = search
					b.reselect()
					b.collection.Excluded = 2
					b.notice = ""
					out := m.gsTable(width, m.gardenBoardRows())
					feedback := ansi.Strip(out[1])
					mustContain(t, "excluded feedback", feedback, "2 draft, unverified or invalid items excluded")
					if search != "" {
						mustContain(t, "applied search feedback", feedback, "Search: foo")
					}
					if n := strings.Count(ansi.Strip(strings.Join(out, "\n")), "Group: "); n != 1 {
						t.Fatalf("Group label appears %d times", n)
					}
					b.notice = "Retained last good work."
					mustContain(t, "notice override", ansi.Strip(m.gsTable(width, m.gardenBoardRows())[1]), b.notice)
				})
			}
		}
	}
}

func TestGardenAttentionRestoresSharedSelection(t *testing.T) {
	for _, from := range []gardenLayout{layoutBoard, layoutTable} {
		for _, exit := range []string{"a", "v", "Garden: Board", "Garden: Table"} {
			for _, moved := range []bool{false, true} {
				t.Run(fmt.Sprint(from, exit, moved), func(t *testing.T) {
					m, fb := attentionEnv(t, 120, 35)
					b := &m.garden.board
					b.setLayout(from)
					b.selection.Focus(b.collection, 1) // A live idle planner is not a request.
					b.attn.anchor = len(b.attn.records) - 1
					reads := fb.Count("Work")
					m = pressAll(t, m, "a")
					b = &m.garden.board
					if b.selection.ID != b.attn.records[0].Item.Issue.ID {
						t.Fatal("entry reused a stale attention anchor")
					}
					mustContain(t, "entry notice", b.notice, "Selected #101 does not need you; showing #103.")
					want := garden.IssueID(1)
					if moved {
						m = pressAll(t, m, "down")
						want = m.garden.board.selection.ID
						if want != 4 {
							t.Fatal("cursor did not choose another need")
						}
					}
					layout := from
					if strings.HasPrefix(exit, "Garden:") {
						tgt, ok := paletteTargetForLabel(exit)
						if !ok {
							t.Fatal("missing palette action")
						}
						next, cmd := m.goToPalette(tgt)
						m, _ = drainAll(t, next, cmd)
						layout = layoutBoard
						if exit == "Garden: Table" {
							layout = layoutTable
						}
					} else {
						m = pressAll(t, m, exit)
						if exit == "v" {
							layout = layoutTable
						}
					}
					b = &m.garden.board
					if b.layout != layout || b.selection.ID != want {
						t.Fatalf("return layout/identity = %v/#%d, want %v/#%d", b.layout, b.selection.ID, layout, want)
					}
					if layout == layoutTable && b.table.lines[b.table.line].rec.Item.Issue.ID != want {
						t.Fatal("table cursor did not follow restored identity")
					}
					if fb.Count("Work") != reads || fb.Writes() != 0 {
						t.Fatal("switching layouts called the backend")
					}
				})
			}
		}
	}
}

func TestGardenAttentionDoesNotRestoreRemovedIdentity(t *testing.T) {
	m, fb := attentionEnv(t, 120, 35)
	m = pressAll(t, m, "v", "a")
	selected := m.garden.board.selection.ID
	fb.WorkView.Snapshot.Items = fb.WorkView.Snapshot.Items[1:]
	m = pressAll(t, m, "R", "a")
	if m.garden.board.selection.ID != selected {
		t.Fatal("return restored an issue that left the collection")
	}
}

func TestGardenTableCollapsedFooterAndHeaderDetail(t *testing.T) {
	for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			m, _ := attentionEnv(t, 160, 48)
			b := &m.garden.board
			b.setLayout(layout)
			b.selection.Focus(b.collection, 3)
			b.tableFocus()
			m = pressAll(t, m, "g", " ")
			b = &m.garden.board
			v := b.tableView()
			l := v.lines[v.line]
			if !l.header || !v.collapsed[l.phase] {
				t.Fatal("fixture did not collapse a group")
			}
			visible := len(v.records) - l.group
			out := m.gsTable(160, m.gardenBoardRows())
			mustContain(t, "visible item count", ansi.Strip(out[len(out)-1]), fmt.Sprintf("Rows 1–%d of %d (%d hidden)", visible, visible, l.group))
			if layout == layoutAttention {
				mustContain(t, "needs count includes hidden requests", ansi.Strip(out[len(out)-1]), fmt.Sprintf("%d of %d need you", len(v.records), len(b.collection.Records)))
			}
			strip := ansi.Strip(strings.Join(out[len(out)-4:len(out)-1], "\n"))
			mustContain(t, "collapsed header detail", strip, fmt.Sprintf("▸ %s  %d (collapsed)", l.phase, l.group), "Enter expand")
			mustNotContain(t, "header has no item action", strip, "next:", "Enter open")
			m = pressAll(t, m, "enter")
			for m.garden.board.tableView().line > m.garden.board.tableView().headerIndex(l.phase) {
				m = pressAll(t, m, "up")
			}
			b = &m.garden.board
			v = b.tableView()
			if !v.lines[v.line].header {
				t.Fatal("expanded group header is not focused")
			}
			out = m.gsTable(160, m.gardenBoardRows())
			strip = ansi.Strip(strings.Join(out[len(out)-4:len(out)-1], "\n"))
			mustContain(t, "expanded header detail", strip, fmt.Sprintf("▾ %s  %d", l.phase, l.group), "Enter collapse")
			m = pressAll(t, m, "down")
			b = &m.garden.board
			r, _, _ := b.collection.Find(b.selection.ID)
			out = m.gsTable(160, m.gardenBoardRows())
			mustContain(t, "item opens before taking action", ansi.Strip(out[len(out)-2]), "Enter open · next: "+gardenNext(r, false))
		})
	}
}

func TestGardenTableExpandPreservesUnrelatedNotice(t *testing.T) {
	for _, notice := range []string{"", "Refresh failed; retained last good board."} {
		m, _ := attentionEnv(t, 120, 35)
		m = pressAll(t, m, "v", "g", " ")
		if notice != "" {
			m.garden.board.notice = notice
		}
		m = pressAll(t, m, "enter")
		if m.garden.board.notice != notice {
			t.Fatalf("expand notice = %q, want %q", m.garden.board.notice, notice)
		}
	}
}

func TestGardenSavedOverridesCannotShadowGroupKeys(t *testing.T) {
	for _, source := range []string{"repo", "personal"} {
		for _, action := range []string{"garden_view", "garden_attention", "garden_stages"} {
			for _, candidate := range []string{"g", "space", " ", "o"} {
				t.Run(fmt.Sprint(source, action, candidate), func(t *testing.T) {
					e := newSettingsEnv(t)
					if source == "repo" {
						body := fmt.Sprintf("keys:\n  %s: %q\n", action, candidate)
						if err := os.WriteFile(filepath.Join(e.repo, ".bonsai.yaml"), []byte(body), 0o600); err != nil {
							t.Fatal(err)
						}
					} else {
						e.writeState(t, fmt.Sprintf(`{"prefs":{"keys":{%q:%q}}}`, action, candidate))
					}
					loaded := e.model(t, 120, 35)
					if err := validateKeyBinding(action, candidate, nil); err == nil {
						t.Fatal("Settings accepted a reserved Garden key")
					}
					bindings := map[string][]string{
						"garden_view":      loaded.keys.GardenView.Keys(),
						"garden_attention": loaded.keys.GardenAttention.Keys(),
						"garden_stages":    loaded.keys.GardenStages.Keys(),
					}
					if !slices.Equal(bindings[action], defaultBindings[action].keys) {
						t.Fatalf("saved reserved override applied: %v", bindings[action])
					}
					for _, layout := range []gardenLayout{layoutTable, layoutAttention} {
						m, _ := attentionEnv(t, 120, 35)
						m.keys = loaded.keys
						m.garden.board.setLayout(layout)
						m = pressAll(t, m, "g", " ")
						v := m.garden.board.tableView()
						if m.garden.board.layout != layout || !v.grouped || !v.lines[v.line].header || !v.collapsed[v.lines[v.line].phase] {
							t.Fatal("saved override swallowed grouping/collapse")
						}
					}
				})
			}
		}
	}
}
