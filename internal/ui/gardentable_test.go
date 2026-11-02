package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

// tableFake holds n accepted items spread over states and priorities, in an
// order unrelated to issue numbers, so State, Number and Priority sorts differ.
func tableFake(t *testing.T, n int) *gardenfake.Backend {
	t.Helper()
	fb := gardenFake(gardenapi.StateConnected)
	stages := []garden.Stage{garden.StageReady, garden.StageInProgress, garden.StageReview, garden.StageDone, garden.StageBlocked}
	prios := []string{"priority:p3", "priority:p0", "priority:p2", "priority:p1"}
	for i := 1; i <= n; i++ {
		it := boardItem(t, i, stages[i%len(stages)], i%7 == 0)
		it.Issue.Number = int64(i*37%1009 + 100)
		it.Issue.Labels[3] = prios[i%len(prios)]
		fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, it)
	}
	return fb
}

func tpress(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		typ := map[string]tea.KeyType{"pgdown": tea.KeyPgDown, "pgup": tea.KeyPgUp, "home": tea.KeyHome, "end": tea.KeyEnd}
		if kt, ok := typ[k]; ok {
			var cmd tea.Cmd
			m, cmd = send(t, m, tea.KeyMsg{Type: kt})
			m, _ = drainAll(t, m, cmd)
			continue
		}
		m = pressAll(t, m, k)
	}
	return m
}
func tableEnv(t *testing.T, fb *gardenfake.Backend, prefs config.Prefs, w, h int) Model {
	t.Helper()
	return tpress(t, pressAll(t, gardenEnv(t, fb, prefs, w, h), "v"), "home")
}
func rowIDs(rows []work.Record) []garden.IssueID {
	var ids []garden.IssueID
	for _, r := range rows {
		ids = append(ids, r.Item.Issue.ID)
	}
	return ids
}
func boardIDs(c work.Collection) []garden.IssueID {
	var ids []garden.IssueID
	for _, col := range c.Columns {
		ids = append(ids, rowIDs(col)...)
	}
	return ids
}

func TestGardenTableAndBoardShowIdenticalEligibleSetsForEveryFilter(t *testing.T) {
	fb := tableFake(t, 40)
	draft := boardItem(t, 90, garden.StageReady, false)
	draft.Issue.Labels[2] = garden.FormatStageLabel(garden.StageDraft)
	unverified := boardItem(t, 91, garden.StageReady, false)
	unverified.Verification = garden.Verification{}
	fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, draft, unverified)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	check := func(label string) {
		t.Helper()
		b := m.garden.board
		got, want := rowIDs(b.collection.Rows(b.sort)), boardIDs(b.collection)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) || slices.Contains(got, 90) || slices.Contains(got, 91) {
			t.Fatalf("%s: table %v board %v", label, got, want)
		}
	}
	if m.garden.board.collection.Excluded != 2 {
		t.Fatalf("drafts/unverified admitted: excluded %d", m.garden.board.collection.Excluded)
	}
	for _, layoutKey := range []string{"", "v"} {
		m = pressAll(t, m, layoutKey)
		check("all")
		m = pressAll(t, m, "t")
		check("type")
		m = pressAll(t, m, "p", "p")
		check("priority")
		m = pressAll(t, m, "x", "/", "t", "a", "s", "k", "enter")
		check("text")
		m = pressAll(t, m, "x")
	}
}

func TestGardenTableToggleKeepsIdentityAndIssuesNoRead(t *testing.T) {
	fb := tableFake(t, 40)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	reads := fb.Count("Work")
	m, cmd := send(t, m, runeKey("v"))
	m, seen := drainAll(t, m, cmd)
	for _, msg := range seen {
		if _, ok := msg.(gardenWorkMsg); ok {
			t.Fatal("toggle issued a work read")
		}
	}
	if m.garden.board.layout != layoutTable {
		t.Fatal("v did not open Table")
	}
	m = tpress(t, m, "down", "down", "pgdown", "o")
	id := m.garden.board.selection.ID
	rows := m.garden.board.collection.Rows(m.garden.board.sort)
	if id == 0 || rows[m.garden.board.table.anchor].Item.Issue.ID != id {
		t.Fatalf("table cursor %d does not own %d", m.garden.board.table.anchor, id)
	}
	r, _, _ := m.garden.board.collection.Find(id)
	m, cmd = send(t, m, runeKey("v"))
	m, seen = drainAll(t, m, cmd)
	for _, msg := range seen {
		if _, ok := msg.(gardenWorkMsg); ok {
			t.Fatal("toggle back issued a work read")
		}
	}
	s := m.garden.board.selection
	if m.garden.board.layout != layoutBoard || s.ID != id || s.Column != work.Column(r.Phase) {
		t.Fatalf("board landed on %+v, want %d in %v", s, id, r.Column)
	}
	if fb.Count("Work") != reads || fb.Writes() != 0 {
		t.Fatal("layout toggle touched the backend")
	}
	mustContain(t, "board card", view(m), fmt.Sprintf("#%d", r.Item.Issue.Number), "Board")
	m = pressAll(t, m, "enter", "esc")
	if m.garden.board.layout != layoutBoard || m.garden.board.opened != 0 || !reflect.DeepEqual(m.garden.board.selection, s) {
		t.Fatal("Board round trip lost its card")
	}
}

func TestGardenTableEnterOpensSharedDetailAndEscRestoresRowAndScroll(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {160, 48}} {
		fb := tableFake(t, 36)
		m := tableEnv(t, fb, config.Prefs{}, sz[0], sz[1])
		for row := 0; row < 36; row++ {
			b := m.garden.board
			rows := b.collection.Rows(b.sort)
			if b.selection.ID != rows[row].Item.Issue.ID {
				t.Fatalf("row %d selected %d", row, b.selection.ID)
			}
			id, scroll, tr := b.selection.ID, b.table.scroll, b.table.anchor
			m = pressAll(t, m, "enter")
			if m.garden.board.opened != id {
				t.Fatalf("row %d opened %d", row, m.garden.board.opened)
			}
			mustContain(t, "detail", view(m), fmt.Sprintf("Issue #%d", rows[row].Item.Issue.Number), "esc Back to Table", "Issue / Overview")
			m = pressAll(t, m, "esc")
			if m.garden.board.opened != 0 || m.garden.board.layout != layoutTable || m.garden.board.selection.ID != id || m.garden.board.table.scroll != scroll || m.garden.board.table.anchor != tr {
				t.Fatalf("row %d: Esc did not restore row/scroll", row)
			}
			m = pressAll(t, m, "down")
		}
		if fb.Writes() != 0 {
			t.Fatal("opening rows mutated backend")
		}
	}
}

func TestGardenTableLargeListKeepsFocusVisibleAndFitsEverySize(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
			t.Run(fmt.Sprintf("%dx%d/%t", sz[0], sz[1], ascii), func(t *testing.T) {
				fb := tableFake(t, 300)
				for i := range fb.WorkView.Snapshot.Items {
					if i%3 == 0 {
						fb.WorkView.Snapshot.Items[i].Issue.Title = strings.Repeat("界é", 40)
					}
				}
				glyphs := "unicode"
				if ascii {
					glyphs = "ascii"
				}
				m := tableEnv(t, fb, config.Prefs{AgentGlyphs: glyphs}, sz[0], sz[1])
				capacity := gardenTableCapacity(m.gardenBoardRows())
				verify := func(step string) {
					t.Helper()
					b := m.garden.board
					if b.table.anchor < b.table.scroll || b.table.anchor >= b.table.scroll+capacity {
						t.Fatalf("%s: row %d outside [%d,%d)", step, b.table.anchor, b.table.scroll, b.table.scroll+capacity)
					}
					out := m.View()
					assertBounded(t, step, out, sz[0], sz[1])
					if len(strings.Split(out, "\n")) != sz[1] {
						t.Fatalf("%s: row reservation", step)
					}
					body := m.gsTable(sz[0], m.gardenBoardRows())
					if len(body) != m.gardenBoardRows() {
						t.Fatalf("%s: %d body rows", step, len(body))
					}
					for i, line := range body {
						if w := lipgloss.Width(line); w != sz[0] {
							t.Fatalf("%s: line %d is %d wide", step, i, w)
						}
					}
					plain := ansi.Strip(strings.Join(body, "\n"))
					if ascii {
						for _, r := range strings.Map(func(r rune) rune {
							if r == '界' || r == 'é' || unicode.Is(unicode.Mn, r) {
								return -1
							}
							return r
						}, plain) {
							if r > 127 {
								t.Fatalf("%s: non-ASCII chrome %q", step, r)
							}
						}
					}
					rows := b.collection.Rows(b.sort)
					marker := "▌"
					if ascii {
						marker = ">"
					}
					selectedLine := false
					for _, line := range strings.Split(plain, "\n") {
						if !strings.HasPrefix(line, marker) && !strings.HasPrefix(line, "▏") && !strings.HasPrefix(line, "|") {
							continue
						}
						for _, r := range rows[b.table.scroll:min(len(rows), b.table.scroll+capacity)] {
							if strings.Contains(line, fmt.Sprintf("#%d ", r.Item.Issue.Number)) {
								if !strings.Contains(line, r.Column.String()) {
									t.Fatalf("%s: STATE %q clipped in %q", step, r.Column, line)
								}
								if r.Item.Issue.ID == b.selection.ID && strings.Contains(line, marker) {
									selectedLine = true
								}
							}
						}
					}
					if !selectedLine {
						t.Fatalf("%s: selected row lost its marker", step)
					}
				}
				verify("start")
				for i := 0; i < capacity+5; i++ {
					m = tpress(t, m, "down")
					verify(fmt.Sprintf("down %d", i))
				}
				for _, k := range []string{"pgdown", "pgdown", "end", "pgup", "up", "home", "end", "left", "right", "h", "l"} {
					m = tpress(t, m, k)
					verify(k)
				}
				if m.garden.board.table.anchor != 299 {
					t.Fatalf("end left row %d", m.garden.board.table.anchor)
				}
			})
		}
	}
}

func TestGardenTableColumnsFollowWidth(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  string
	}{{80, "ISSUE TITLE STATE NEXT"}, {120, "ISSUE TITLE STATE SIGNAL NEXT"}, {160, "ISSUE TITLE STATE SIGNAL NEXT TYPE P"}} {
		fb := tableFake(t, 20)
		m := tableEnv(t, fb, config.Prefs{}, tc.width, 35)
		var header string
		for _, line := range strings.Split(view(m), "\n") {
			if strings.Contains(line, "ISSUE") {
				header = line
			}
		}
		if i := strings.Index(header, "│"); i >= 0 {
			header = header[:i]
		}
		if got := strings.Join(strings.Fields(header), " "); got != tc.want {
			t.Errorf("%d: columns %q want %q", tc.width, got, tc.want)
		}
		r, _, _ := m.garden.board.collection.Find(m.garden.board.selection.ID)
		mustContain(t, "detail strip", view(m), "Enter open · next: "+gardenNext(r, false))
	}
}

func TestGardenTableEmptyOfflineAndUnknown(t *testing.T) {
	fb := tableFake(t, 12)
	m := tableEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "/", "z", "z", "z", "enter")
	out := view(m)
	mustContain(t, "empty filter", out, "(no approved work matches this filter)", "No rows", "filtered / 12")
	if m.garden.board.selection.ID != 0 {
		t.Fatal("empty filter kept a selection")
	}
	if m = pressAll(t, m, "enter", "down", "end"); m.garden.board.opened != 0 {
		t.Fatal("Enter opened nothing-selected")
	}
	m = pressAll(t, m, "x")
	if m.garden.board.selection.ID == 0 {
		t.Fatal("clear did not restore selection")
	}

	last := m.garden.board.selection.ID
	fb.WorkErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub is offline."}
	m = pressAll(t, m, "R")
	out = view(m)
	mustContain(t, "offline", out, "offline / stale", "GitHub is offline.", "#")
	mustNotContain(t, "offline", out, "(no approved work")
	if m.garden.board.selection.ID != last || len(m.garden.board.collection.Records) != 12 {
		t.Fatal("failed refresh replaced last good rows")
	}

	unknown := gardenFake(gardenapi.StateConnected)
	unknown.WorkView.Snapshot = gardensync.Snapshot{Source: gardensync.SourceStale, Problems: []gardensync.Problem{{Code: "offline"}}}
	m = tableEnv(t, unknown, config.Prefs{}, 80, 24)
	mustContain(t, "unknown", view(m), "No successful read yet", "(unknown until a successful read)", "offline / stale")
	mustNotContain(t, "unknown", view(m), "(no approved work")
}

func TestGardenTableSelectionDeletionIsDeterministic(t *testing.T) {
	remove := func(fb *gardenfake.Backend, id garden.IssueID) {
		fb.WorkView.Snapshot.Items = slices.DeleteFunc(fb.WorkView.Snapshot.Items, func(it gardensync.Item) bool { return it.Issue.ID == id })
	}
	t.Run("middle picks the row now at the index", func(t *testing.T) {
		fb := tableFake(t, 20)
		m := tableEnv(t, fb, config.Prefs{}, 120, 35)
		m = tpress(t, m, "down", "down", "down")
		b := m.garden.board
		rows := b.collection.Rows(b.sort)
		remove(fb, b.selection.ID)
		want := rows[4].Item.Issue.ID
		m = pressAll(t, m, "R")
		if m.garden.board.selection.ID != want || m.garden.board.table.anchor != 3 || m.garden.board.notice != "Selected issue left this view; selected the row now in its place." {
			t.Fatalf("got %d row %d notice %q want %d", m.garden.board.selection.ID, m.garden.board.table.anchor, m.garden.board.notice, want)
		}
		mustContain(t, "notice", view(m), "Selected issue left this view; selected the row now in its place.")
	})
	t.Run("last row picks the previous", func(t *testing.T) {
		fb := tableFake(t, 20)
		m := tpress(t, tableEnv(t, fb, config.Prefs{}, 120, 35), "end")
		b := m.garden.board
		rows := b.collection.Rows(b.sort)
		remove(fb, b.selection.ID)
		m = pressAll(t, m, "R")
		if m.garden.board.selection.ID != rows[len(rows)-2].Item.Issue.ID || m.garden.board.table.anchor != len(rows)-2 {
			t.Fatal("last-row deletion did not pick previous row")
		}
		if m.garden.board.notice != "Selected issue left this view; selected the previous row." {
			t.Fatalf("notice %q", m.garden.board.notice)
		}
	})
	t.Run("everything deleted selects nothing", func(t *testing.T) {
		fb := tableFake(t, 3)
		m := tableEnv(t, fb, config.Prefs{}, 80, 24)
		fb.WorkView.Snapshot.Items = nil
		m = pressAll(t, m, "R")
		if m.garden.board.selection.ID != 0 || m.garden.board.notice != "Selected issue left this view; no matching work remains." {
			t.Fatalf("%+v %q", m.garden.board.selection, m.garden.board.notice)
		}
	})
	t.Run("opened issue deleted returns to Table", func(t *testing.T) {
		fb := tableFake(t, 6)
		m := pressAll(t, tableEnv(t, fb, config.Prefs{}, 80, 24), "enter")
		remove(fb, m.garden.board.opened)
		m = pressAll(t, m, "R")
		if m.garden.board.opened != 0 || m.garden.board.layout != layoutTable || !strings.Contains(m.garden.board.notice, "returned to Table") {
			t.Fatalf("notice %q", m.garden.board.notice)
		}
	})
}

func TestGardenTableSortCycleIsStableAndReadOnly(t *testing.T) {
	fb := tableFake(t, 30)
	m := tableEnv(t, fb, config.Prefs{}, 120, 35)
	m = tpress(t, m, "down", "down")
	id := m.garden.board.selection.ID
	var orders [][]garden.IssueID
	for _, want := range []work.Sort{work.SortNumber, work.SortPriority, work.SortState} {
		m = pressAll(t, m, "o")
		b := m.garden.board
		if b.sort != want || b.selection.ID != id || b.collection.Rows(b.sort)[b.table.anchor].Item.Issue.ID != id {
			t.Fatalf("sort %v lost the selection", want)
		}
		ids := rowIDs(b.collection.Rows(b.sort))
		if !slices.Equal(ids, rowIDs(b.collection.Rows(b.sort))) {
			t.Fatal("unstable order")
		}
		orders = append(orders, ids)
		mustContain(t, "sort label", view(m), "Sort: "+want.String(), "sort: "+want.String())
	}
	if slices.Equal(orders[0], orders[1]) || slices.Equal(orders[1], orders[2]) {
		t.Fatal("sorts did not differ")
	}
	if fb.Writes() != 0 {
		t.Fatal("sort mutated backend")
	}
	// o is a Table key only.
	m = pressAll(t, m, "v", "o")
	if m.garden.board.sort != work.SortState {
		t.Fatal("o sorted on the Board")
	}
}

func TestGardenViewToggleIsScopedAndRebindable(t *testing.T) {
	if newKeyMap(nil).ViewProcs.Help().Key != "v" || newKeyMap(nil).GardenView.Help().Key != "v" || len(keyCollisions(nil)) != 0 {
		t.Fatal("defaults changed")
	}
	if err := validateKeyBinding("garden_view", "L", nil); err != nil {
		t.Fatalf("Garden scope must not see Processes keys: %v", err)
	}
	if err := validateKeyBinding("processes", "v", map[string]string{"garden_view": "v"}); err != nil {
		t.Fatalf("Worktree scope must not see Garden keys: %v", err)
	}
	if validateKeyBinding("garden_view", "q", nil) == nil {
		t.Fatal("global quit key bindable in Garden")
	}
	m := gardenEnv(t, tableFake(t, 8), config.Prefs{}, 120, 35)
	m.keys = newKeyMap(map[string]string{"garden_view": "w"})
	mustContain(t, "board hint", flat(footerText(m)), "w table")
	m = pressAll(t, m, "v")
	if m.garden.board.layout != layoutBoard {
		t.Fatal("old key still toggles")
	}
	m = pressAll(t, m, "w")
	if m.garden.board.layout != layoutTable {
		t.Fatal("override does not toggle")
	}
	mustContain(t, "table hint", flat(footerText(m)), "w board", "o sort", "↑/↓ select", "enter open")
	mustNotContain(t, "table hint", flat(footerText(m)), "v board")
}

func footerText(m Model) string { return ansi.Strip(m.View()) }

func TestGardenTableChromeCrumbsFocusAndPalette(t *testing.T) {
	m := tableEnv(t, tableFake(t, 8), config.Prefs{}, 120, 35)
	out := view(m)
	mustContain(t, "table chrome", out, "Garden › Table", "Approved table", "APPROVED WORK  8", "Table", "▤")
	b := m.garden.board
	r, _, _ := b.collection.Find(b.selection.ID)
	if got := m.gardenBoardIdentity(); got != fmt.Sprintf("#%d  %s / %s", r.Item.Issue.Number, boardTitle(r.Item.Issue.Title), r.Column) {
		t.Fatalf("identity %q", got)
	}
	m = pressAll(t, m, "enter")
	mustContain(t, "detail crumbs", view(m), fmt.Sprintf("Garden › Table › #%d", r.Item.Issue.Number), "Issue / Overview")
	m = pressAll(t, m, "esc", "v")
	mustContain(t, "board", view(m), "Garden › Board", "Approved ")
	for _, label := range []string{"Garden: Board", "Garden: Table"} {
		tgt, ok := paletteTargetForLabel(label)
		if !ok {
			t.Fatalf("no palette entry %q", label)
		}
		var cmd tea.Cmd
		m, cmd = m.goToPalette(tgt)
		m, _ = drainAll(t, m, cmd)
		want := layoutBoard
		if label == "Garden: Table" {
			want = layoutTable
		}
		if m.garden.board.layout != want || m.gardenScreen() != scrBoard {
			t.Fatalf("%s left layout %v", label, m.garden.board.layout)
		}
	}
}

func TestGardenTableViewRememberedPerRepository(t *testing.T) {
	fb := gardenFake(gardenapi.StateReadOnly)
	m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
	m = pressAll(t, m, "enter", "v")
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.GardenView(m.repoDir) != "table" || st.GardenView(t.TempDir()) != "" {
		t.Fatal("table view not remembered per repository")
	}
	m2 := sized(t, New(m.repoDir, &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(gardenFake(gardenapi.StateReadOnly))), 80, 24)
	m2, _ = drainAll(t, m2, m2.Init())
	if m2.gardenScreen() != scrBoard || m2.garden.board.layout != layoutTable {
		t.Fatal("remembered Table did not reopen")
	}
	m = pressAll(t, m, "v")
	st, _ = config.LoadState()
	if st.GardenView(m.repoDir) != "board" {
		t.Fatal("Board not remembered after toggling back")
	}
}

func TestGardenViewKeyCannotShadowGardenKeys(t *testing.T) {
	for k := range gardenReservedKeys {
		if err := validateKeyBinding("garden_view", k, nil); err == nil || !strings.Contains(err.Error(), "Garden key") {
			t.Errorf("%q accepted for garden_view: %v", k, err)
		}
	}
	if err := validateKeyBinding("garden_view", "w", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGardenPaletteSwitchClosesOpenDetail(t *testing.T) {
	m := tableEnv(t, tableFake(t, 6), config.Prefs{}, 80, 24)
	for _, label := range []string{"Garden: Board", "Garden: Table"} {
		m = pressAll(t, m, "enter")
		if m.garden.board.opened == 0 {
			t.Fatal("detail did not open")
		}
		tgt, _ := paletteTargetForLabel(label)
		var cmd tea.Cmd
		m, cmd = m.goToPalette(tgt)
		m, _ = drainAll(t, m, cmd)
		if m.garden.board.opened != 0 || m.garden.board.docScroll != 0 {
			t.Fatalf("%s left the detail open", label)
		}
		mustNotContain(t, label, view(m), "Issue document")
		if label == "Garden: Board" {
			m = pressAll(t, m, "v")
		}
	}
}

func TestTableScrollForKeepsAnchorWhenVisible(t *testing.T) {
	for _, tc := range []struct{ scroll, row, capacity, n, want int }{
		{5, 7, 10, 100, 5}, {5, 4, 10, 100, 4}, {5, 15, 10, 100, 6}, {0, 99, 10, 100, 90}, {50, 0, 10, 8, 0}, {3, 2, 10, 0, 0},
	} {
		if got := tableScrollFor(tc.scroll, tc.row, tc.capacity, tc.n); got != tc.want {
			t.Errorf("%+v: %d", tc, got)
		}
	}
}
