package ui

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/overlay"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

var overlaySizes = [][2]int{{80, 24}, {120, 35}}

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheel(x, y int, down bool) tea.MouseMsg {
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

// overlayEnv is the Garden on the given layout with the shared board fixture.
func overlayEnv(t *testing.T, layout string, w, h int) (Model, *gardenfake.Backend) {
	t.Helper()
	fb := boardFake(t)
	m := gardenEnv(t, fb, config.Prefs{}, w, h)
	if layout == "table" {
		m = pressAll(t, m, "v")
	}
	return m, fb
}

// cellOf finds where an Issue number is drawn in the body of the rendered shell, the way a person would aim.
func cellOf(t *testing.T, m Model, number int64) (x, y int) {
	t.Helper()
	re := regexp.MustCompile(fmt.Sprintf(`#%d\b`, number))
	for row, line := range strings.Split(view(m), "\n") {
		if row < shellHeaderRows+settingsSubmenuRows || row >= m.height-shellFooterRows {
			continue
		}
		if loc := re.FindStringIndex(line); loc != nil {
			return ansi.StringWidth(line[:loc[0]]), row
		}
	}
	t.Fatalf("#%d is not drawn:\n%s", number, view(m))
	return 0, 0
}

// visibleOther is the first drawn card or row that is not the selected one.
func visibleOther(t *testing.T, m Model) work.Record {
	t.Helper()
	out := view(m)
	for _, r := range m.garden.board.collection.Records {
		if r.Item.Issue.ID != m.garden.board.selection.ID && regexp.MustCompile(fmt.Sprintf(`#%d\b`, r.Item.Issue.Number)).MatchString(out) {
			return r
		}
	}
	t.Fatalf("no other card is drawn:\n%s", out)
	return work.Record{}
}

// panelRows is the overlay panel's text with the origin-specific close label masked.
func panelRows(m Model) string {
	x, y, w, h := overlay.Center(m.width, m.height)
	lines := strings.Split(view(m), "\n")
	var out []string
	for _, l := range lines[y : y+h] {
		out = append(out, ansi.Truncate(ansi.TruncateLeft(l, x, ""), w, ""))
	}
	return strings.NewReplacer("Back to Board", "Back to ORIGIN", "Back to Table", "Back to ORIGIN", " Board ", " ORIGIN ", " Table ", " ORIGIN ").Replace(strings.Join(out, "\n"))
}

func TestIssueOverlaySameIssueFromBoardAndTable(t *testing.T) {
	for _, sz := range overlaySizes {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			board, _ := overlayEnv(t, "board", sz[0], sz[1])
			table, _ := overlayEnv(t, "table", sz[0], sz[1])
			id := board.garden.board.selection.ID
			table.garden.board.selection.Focus(table.garden.board.collection, id)
			table.garden.board.tableFocus()
			board, table = pressAll(t, board, "enter"), pressAll(t, table, "enter")
			if board.garden.board.opened != id || table.garden.board.opened != id {
				t.Fatalf("opened %d / %d, want %d", board.garden.board.opened, table.garden.board.opened, id)
			}
			if a, b := panelRows(board), panelRows(table); a != b {
				t.Fatalf("panels differ by origin:\n%s\n---\n%s", a, b)
			}
			mustContain(t, "board origin", view(board), "esc ")
			assertBounded(t, "board", board.View(), sz[0], sz[1])
			assertBounded(t, "table", table.View(), sz[0], sz[1])
		})
	}
}

func TestIssueOverlayMouseAndKeyboardOpenTheSameIssue(t *testing.T) {
	for _, layout := range []string{"board", "table"} {
		for _, sz := range overlaySizes {
			t.Run(fmt.Sprintf("%s/%v", layout, sz), func(t *testing.T) {
				m, fb := overlayEnv(t, layout, sz[0], sz[1])
				// Another card than the selected one, so the click has to move the selection too.
				target := visibleOther(t, m)
				id, number := target.Item.Issue.ID, target.Item.Issue.Number

				kb := m
				kb.garden.board.selection.Focus(kb.garden.board.collection, id)
				kb.garden.board.tableFocus()
				kb = pressAll(t, kb, "enter")

				x, y := cellOf(t, m, number)
				mouse, cmd := send(t, m, click(x, y))
				if cmd != nil {
					t.Fatal("opening started work")
				}
				if mouse.garden.board.opened != id || mouse.garden.board.selection.ID != id || mouse.shellFocus != shellBody {
					t.Fatalf("click opened %d selected %d, want %d", mouse.garden.board.opened, mouse.garden.board.selection.ID, id)
				}
				if layout == "board" && !slices.Equal(kb.garden.board.selection.Scroll[:], mouse.garden.board.selection.Scroll[:]) {
					t.Fatal("click and Enter disagree on scroll")
				}
				if panelRows(kb) != panelRows(mouse) {
					t.Fatal("click and Enter drew different panels")
				}
				if fb.Writes() != 0 {
					t.Fatal("opening mutated the backend")
				}
				// Empty space between cards opens nothing.
				if blank, _ := send(t, m, click(0, y)); blank.garden.board.opened != 0 {
					t.Fatal("the margin opened an issue")
				}
			})
		}
	}
}

func TestIssueOverlayTrapsKeysAndClicks(t *testing.T) {
	for _, sz := range overlaySizes {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m, _ := overlayEnv(t, "board", sz[0], sz[1])
			m = pressAll(t, m, "down", "enter")
			opened, sel, ws := m.garden.board.opened, m.garden.board.selection, m.workspace
			for _, k := range []tea.KeyMsg{runeKey("q"), runeKey("?"), {Type: tea.KeyCtrlP}, runeKey("v"), runeKey("/"), runeKey("t"), runeKey("x"), runeKey("o"),
				{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true}} {
				var cmd tea.Cmd
				m, cmd = send(t, m, k)
				if cmd != nil {
					t.Fatalf("%q reached a global: %T", k.String(), cmd())
				}
				if m.modal != nil || m.workspace != ws || m.garden.board.opened != opened || !reflect.DeepEqual(m.garden.board.selection, sel) || m.garden.board.layout != layoutBoard || m.garden.board.searching {
					t.Fatalf("%q leaked out of the overlay", k.String())
				}
			}
			// Down and up scroll the body; they never move the dimmed board's selection.
			m = pressAll(t, m, "down", "up", "tab", "shift+tab")
			if !reflect.DeepEqual(m.garden.board.selection, sel) {
				t.Fatal("navigation moved the board selection")
			}
			x, y, w, h := overlay.Center(sz[0], sz[1])
			for _, p := range [][2]int{{0, 0}, {sz[0] - 1, sz[1] - 1}, {x - 1, y + 2}, {x + w, y + 2}, {x + 2, y + h}} {
				if next, cmd := send(t, m, click(p[0], p[1])); cmd != nil || next.garden.board.opened != opened || !reflect.DeepEqual(next.garden.board.selection, sel) || next.workspace != ws {
					t.Fatalf("click outside the panel at %v leaked", p)
				}
			}
			// Clicks inside the body are inert too.
			if next, _ := send(t, m, click(x+3, y+6)); next.garden.board.opened != opened {
				t.Fatal("body click changed the overlay")
			}
		})
	}
}

func TestIssueOverlayTabCyclesEnabledActionsAndEnterMatchesShortcut(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	_ = fb
	m = plannerPress(t, m, "enter")
	if !m.gardenOverlayOpen() {
		t.Fatalf("overlay not open:\n%s", view(m))
	}
	var order []string
	for range 6 {
		m = pressAll(t, m, "tab")
		order = append(order, m.garden.board.overlayFocus)
	}
	if want := []string{"close", "start", "run-settings", "attempts", "close", "start"}; !slices.Equal(order, want) {
		t.Fatalf("tab order %v, want %v", order, want)
	}
	if m = pressAll(t, m, "shift+tab"); m.garden.board.overlayFocus != "close" {
		t.Fatalf("shift+tab wrapped to %q", m.garden.board.overlayFocus)
	}
	for _, tc := range []struct {
		shortcut string
		tabs     int
		check    func(Model) bool
	}{
		{"s", 2, func(m Model) bool { return m.garden.launch.stage == launchStageConfirm }},
		{"e", 3, func(m Model) bool { return m.garden.launch.stage == launchStageChoose }},
		{"i", 4, func(m Model) bool { return m.garden.launch.stage == launchStageHub }},
	} {
		base, _, _ := fakeLaunchUIEnv(t, 120, 35)
		base = plannerPress(t, base, "enter")
		viaKey := plannerPress(t, base, tc.shortcut)
		keys := slices.Repeat([]string{"tab"}, tc.tabs)
		viaTab := plannerPress(t, base, append(keys, "enter")...)
		if !tc.check(viaKey) || !tc.check(viaTab) {
			t.Fatalf("%s: shortcut reached %v, tab+enter reached %v", tc.shortcut, tc.check(viaKey), tc.check(viaTab))
		}
		if view(viaKey) != view(viaTab) {
			t.Fatalf("%s: Tab+Enter and the shortcut drew different screens", tc.shortcut)
		}
		// A click on the pinned action is the third route to the same screen.
		_, _, w, h := overlay.Center(base.width, base.height)
		_, hits := base.issueOverlayRows(w, h)
		x0, y0, _, _ := overlay.Center(base.width, base.height)
		var viaClick Model
		for _, hit := range hits {
			if hit.action == map[string]string{"s": "start", "e": "run-settings", "i": "attempts"}[tc.shortcut] {
				var cmd tea.Cmd
				viaClick, cmd = send(t, base, click(x0+hit.x0, y0+hit.y))
				viaClick = plannerRun(t, viaClick, cmd)
			}
		}
		if view(viaClick) != view(viaKey) {
			t.Fatalf("%s: click drew a different screen", tc.shortcut)
		}
	}
}

func TestIssueOverlayDisabledActionKeepsItsReason(t *testing.T) {
	m, _, _ := fakeLaunchUIEnv(t, 120, 35)
	m.garden.board.selection.Focus(m.garden.board.collection, 2)
	m = plannerPress(t, m, "enter")
	// #102 reads as in progress, which a Ready-only start must refuse with its reason.
	m.garden.board.collection.Columns[work.Approved][0].Item.Classification.Stage = garden.StageInProgress
	var start issueControl
	for _, c := range m.issueControls() {
		if c.id == "start" {
			start = c
		}
	}
	if m.garden.board.opened != 2 || start.enabled || start.reason != launchNotReadyText {
		t.Fatalf("opened %d start = %+v", m.garden.board.opened, start)
	}
	for range 6 {
		m = pressAll(t, m, "tab")
		if m.garden.board.overlayFocus == "start" {
			t.Fatal("Tab focused a disabled action")
		}
	}
	mustContain(t, "reason", flat(view(m)), "s unavailable: Only a Ready Issue")
	m = plannerPress(t, m, "s")
	if m.garden.launch.stage != launchStageNone {
		t.Fatal("the disabled start ran")
	}
}

func TestIssueOverlayNoLaunchBackendHidesStart(t *testing.T) {
	m, _ := overlayEnv(t, "board", 80, 24)
	m = pressAll(t, m, "enter")
	for _, c := range m.issueControls() {
		if c.id == "start" || c.id == "attempts" {
			t.Fatalf("%s offered without a launch backend", c.id)
		}
	}
	mustContain(t, "reason", flat(view(m)), "no local launch")
}

func TestIssueOverlayReturnsToExactOriginAfterNavigationAndResize(t *testing.T) {
	for _, layout := range []string{"board", "table"} {
		t.Run(layout, func(t *testing.T) {
			fb := tableFake(t, 36)
			m := gardenEnv(t, fb, config.Prefs{}, 80, 24)
			if layout == "table" {
				m = pressAll(t, m, "v")
			}
			m = pressAll(t, m, slices.Repeat([]string{"down"}, 12)...)
			// Resizing re-fits the window by itself, so the control gets the same resizes without the overlay.
			control := sized(t, sized(t, m, 120, 35), 80, 24)
			m = pressAll(t, m, "enter", "right", "down", "down", "tab", "shift+tab", "end", "home")
			m = sized(t, m, 120, 35)
			m = sized(t, m, 80, 24)
			m = pressAll(t, m, "esc")
			b, c := m.garden.board, control.garden.board
			if b.opened != 0 || !reflect.DeepEqual(b.selection, c.selection) || b.table.scroll != c.table.scroll || b.table.anchor != c.table.anchor || b.layout != c.layout {
				t.Fatalf("returned to %+v, want %+v", b.selection, c.selection)
			}
			if m.View() != control.View() {
				t.Fatal("the board drew differently after the overlay")
			}
			assertBounded(t, "after close", m.View(), 80, 24)
		})
	}
}

func TestIssueOverlayScrollsAndKeepsChromeAcrossResize(t *testing.T) {
	fb := boardFake(t)
	for i := range fb.WorkView.Snapshot.Items {
		fb.WorkView.Snapshot.Items[i].Issue.Body = "FIRST\n" + strings.Repeat("Read-only summary line.\n", 60) + "LAST"
	}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "end")
	if m.garden.board.docScroll == 0 {
		t.Fatal("End did not scroll")
	}
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {80, 24}} {
		m = sized(t, m, sz[0], sz[1])
		out := view(m)
		mustContain(t, "pinned chrome", out, "Issue #101", "esc ", "Overview")
		if !strings.Contains(out, "LAST") || strings.Contains(out, "FIRST") {
			t.Fatalf("scroll position lost at %dx%d", sz[0], sz[1])
		}
		bodyW, rows := m.gardenOverlayBody()
		bodyLines := len(m.gardenDocument(bodyW))
		if m.garden.board.docScroll > max(0, bodyLines-rows) {
			t.Fatalf("scroll %d past the end (%d lines, %d rows)", m.garden.board.docScroll, bodyLines, rows)
		}
		assertBounded(t, "resized", m.View(), sz[0], sz[1])
	}
	// Page keys and the wheel move by the body, never past either end.
	m = pressAll(t, m, "home")
	x, y, _, _ := overlay.Center(m.width, m.height)
	m, _ = send(t, m, wheel(x+4, y+6, true))
	if m.garden.board.docScroll != overlayWheelLines {
		t.Fatalf("wheel scrolled %d", m.garden.board.docScroll)
	}
	m, _ = send(t, m, wheel(0, 0, true))
	if m.garden.board.docScroll != overlayWheelLines {
		t.Fatal("wheel outside the panel scrolled the body")
	}
	m, _ = send(t, m, wheel(x+4, y+6, false))
	m, _ = send(t, m, wheel(x+4, y+6, false))
	if m.garden.board.docScroll != 0 {
		t.Fatalf("wheel up went to %d", m.garden.board.docScroll)
	}
}

func TestIssueOverlayTabsByKeyAndClick(t *testing.T) {
	for _, sz := range overlaySizes {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m, _ := overlayEnv(t, "board", sz[0], sz[1])
			m = pressAll(t, m, "enter")
			x, y, w, h := overlay.Center(sz[0], sz[1])
			_, hits := m.issueOverlayRows(w, h)
			tabs := 0
			for _, hit := range hits {
				if hit.tab < 0 {
					continue
				}
				tabs++
				byKey := pressAll(t, m, slices.Repeat([]string{"right"}, hit.tab)...)
				byClick, _ := send(t, m, click(x+hit.x0, y+hit.y))
				if byClick.garden.board.section != hit.tab || byKey.garden.board.section != hit.tab || view(byKey) != view(byClick) {
					t.Fatalf("tab %d: key %d click %d", hit.tab, byKey.garden.board.section, byClick.garden.board.section)
				}
			}
			if tabs != len(gardenDocumentSections) {
				t.Fatalf("%d tabs hit-testable, want %d", tabs, len(gardenDocumentSections))
			}
			for _, name := range []string{"Overview", "Specification", "Plan", "Activity"} {
				mustContain(t, "tabs", view(m), name)
			}
		})
	}
}

func TestIssueOverlayCloseByEscBackAndClick(t *testing.T) {
	m, _ := overlayEnv(t, "table", 80, 24)
	base := pressAll(t, m, "down")
	x, y, w, h := overlay.Center(80, 24)
	opened := pressAll(t, base, "enter")
	_, hits := opened.issueOverlayRows(w, h)
	var back overlayHit
	for _, hit := range hits {
		if hit.action == "close" {
			back = hit
		}
	}
	byClick, _ := send(t, opened, click(x+back.x0, y+back.y))
	byEsc := pressAll(t, opened, "esc")
	byTab := pressAll(t, opened, "tab", "enter")
	for name, got := range map[string]Model{"click": byClick, "esc": byEsc, "tab+enter": byTab} {
		if got.garden.board.opened != 0 || got.View() != base.View() {
			t.Fatalf("%s did not restore the board exactly", name)
		}
	}
}

func TestIssueOverlayItemRemovedByRefreshShowsNoticeNotAnotherTicket(t *testing.T) {
	for _, layout := range []string{"board", "table"} {
		t.Run(layout, func(t *testing.T) {
			m, fb := overlayEnv(t, layout, 80, 24)
			m = pressAll(t, m, "down", "enter", "end")
			gone := m.garden.board.opened
			fb.WorkView.Snapshot.Items = slices.DeleteFunc(fb.WorkView.Snapshot.Items, func(it gardensync.Item) bool { return it.Issue.ID == gone })
			m = pressAll(t, m, "R")
			b := m.garden.board
			if b.opened != 0 || b.overlayFocus != "" || b.docScroll != 0 || !strings.Contains(b.notice, "Opened issue left this view; returned to") {
				t.Fatalf("opened=%d focus=%q notice=%q", b.opened, b.overlayFocus, b.notice)
			}
			out := view(m)
			mustContain(t, "notice", out, "Opened issue left this view")
			mustNotContain(t, "panel", out, "Issue #")
			if m.gardenOverlayOpen() {
				t.Fatal("another ticket took the overlay")
			}
			assertBounded(t, "after removal", m.View(), 80, 24)
		})
	}
}

func TestIssueOverlayFilteredOutAndForeignRepositoryCloseIt(t *testing.T) {
	m, _ := overlayEnv(t, "board", 80, 24)
	m = pressAll(t, m, "enter")
	m.garden.board.openedRepo = m.garden.status.Target.RepoID + 1
	m = pressAll(t, m, "R")
	if m.garden.board.opened != 0 || !strings.Contains(m.garden.board.notice, "another repository") {
		t.Fatalf("foreign overlay stayed: %+v", m.garden.board.notice)
	}
}

func TestIssueOverlayStaleAsyncRepliesNeverReachAnotherTicket(t *testing.T) {
	t.Run("planner reply for a closed issue", func(t *testing.T) {
		m, _ := plannerEnv(t, 120, 35)
		m, cmd := send(t, m, keyFor("enter"))
		m, cmd = send(t, m, keyFor("p"))
		if cmd == nil || m.garden.board.opened != 1 {
			t.Fatal("p did not request the planner preview")
		}
		late := runCmds(cmd)
		m = pressAll(t, m, "esc", "down", "enter")
		if m.garden.board.opened != 2 {
			t.Fatalf("opened %d", m.garden.board.opened)
		}
		for _, msg := range late {
			m, _ = send(t, m, msg)
		}
		if m.garden.planner.stage != plannerNone || m.garden.board.opened != 2 || !m.gardenOverlayOpen() {
			t.Fatalf("a late reply for #1 took over #2: stage %d", m.garden.planner.stage)
		}
	})
	t.Run("older work reply while open", func(t *testing.T) {
		m, _ := overlayEnv(t, "board", 80, 24)
		m = pressAll(t, m, "enter")
		before := m.garden.board.collection
		m, _ = send(t, m, gardenWorkMsg{seq: m.garden.seq + 7})
		if m.garden.board.opened == 0 || len(m.garden.board.collection.Records) != len(before.Records) {
			t.Fatal("an unrequested work reply changed the open board")
		}
	})
	t.Run("refresh in flight keeps the overlay", func(t *testing.T) {
		m, _ := overlayEnv(t, "board", 80, 24)
		m = pressAll(t, m, "enter")
		m, cmd := send(t, m, runeKey("R"))
		if cmd == nil || !m.gardenOverlayOpen() {
			t.Fatal("refresh closed the overlay or did not run")
		}
	})
}

func TestIssueOverlayTerminalOwnsKeysAndCtrlBracketStillEscapes(t *testing.T) {
	for _, sz := range overlaySizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		m = plannerPress(t, requestPlan(t, openPlanner(t, m)), "ctrl+s")
		if !m.plannerTerminalMode() || m.gardenOverlayOpen() {
			t.Fatalf("typing=%v overlay=%v", m.plannerTerminalMode(), m.gardenOverlayOpen())
		}
		// Typing mode: q, esc, tab and the mouse belong to the agent, not to the overlay or the shell.
		for _, msg := range []tea.Msg{keyFor("q"), keyFor("esc"), keyFor("tab"), click(2, 12)} {
			var cmd tea.Cmd
			m, cmd = send(t, m, msg)
			if cmd != nil {
				for _, out := range runCmds(cmd) {
					if _, quit := out.(tea.QuitMsg); quit {
						t.Fatalf("%v quit through the terminal", msg)
					}
				}
			}
			if !m.plannerTerminalMode() || m.garden.board.opened != 1 || m.workspace != wsGarden {
				t.Fatalf("%v escaped the terminal", msg)
			}
		}
		var cmd tea.Cmd
		m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
		m = plannerRun(t, m, cmd)
		if m.plannerTerminalMode() || m.gardenOverlayOpen() {
			t.Fatal("Ctrl+] must return control to Bonsai without opening the overlay over the session")
		}
		// Leaving the planner screens brings the overlay back; the session is never cancelled by it.
		for range 4 {
			if m.gardenOverlayOpen() {
				break
			}
			m = plannerPress(t, m, "esc")
		}
		if !m.gardenOverlayOpen() || rig.garden.Count("CancelPlanner") != 0 {
			t.Fatalf("overlay=%v cancels=%d\n%s", m.gardenOverlayOpen(), rig.garden.Count("CancelPlanner"), view(m))
		}
		m = plannerPress(t, m, "esc")
		if m.garden.board.opened != 0 || rig.garden.Count("CancelPlanner") != 0 {
			t.Fatal("closing the overlay touched the planner")
		}
	}
}

func TestIssueOverlayNeverStartsWorkWhileReading(t *testing.T) {
	m, rig, fb := fakeLaunchUIEnv(t, 80, 24)
	writes := rig.garden.Writes()
	m = plannerPress(t, m, "enter", "right", "right", "right", "right", "left", "down", "end", "home", "tab", "shift+tab", "esc")
	if rig.garden.Writes() != writes || len(fb.starts) != 0 || len(rig.agents.Sessions.Starts()) != 0 {
		t.Fatal("reading a tab started or wrote something")
	}
}

func TestIssueOverlayFitsEverySizeAndTheme(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 10}, {80, 9}, {20, 8}, {1, 1}, {0, 0}, {0, 3}}
	for _, preset := range theme.Presets() {
		for _, ascii := range []bool{false, true} {
			glyphs := "unicode"
			if ascii {
				glyphs = "ascii"
			}
			fb := boardFake(t)
			for i := range fb.WorkView.Snapshot.Items {
				fb.WorkView.Snapshot.Items[i].Issue.Title = "ASCII title"
				fb.WorkView.Snapshot.Items[i].Issue.Body = "ASCII body"
				for j := range fb.WorkView.Snapshot.Items[i].Verification.Revisions {
					fb.WorkView.Snapshot.Items[i].Verification.Revisions[j].Content = "ASCII content"
				}
			}
			m := gardenEnv(t, fb, config.Prefs{Theme: preset, AgentGlyphs: glyphs}, 120, 35)
			m = pressAll(t, m, "enter")
			for _, sz := range sizes {
				t.Run(fmt.Sprintf("%s/%s/%v", preset, glyphs, sz), func(t *testing.T) {
					m = sized(t, m, sz[0], sz[1])
					frame := m.View()
					if sz[1] > 0 {
						assertOverlayFrame(t, frame, sz[0], sz[1])
					}
					if ascii {
						for _, r := range ansi.Strip(frame) {
							if r > 127 {
								t.Fatalf("non-ASCII %q in ascii mode", r)
							}
						}
					}
					if sz[0] >= 64 && sz[1] >= 24 && !strings.Contains(ansi.Strip(frame), "esc ") {
						t.Fatal("close action lost")
					}
				})
			}
		}
	}
}

func assertOverlayFrame(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("frame height %d, want %d", len(lines), h)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > w {
			t.Fatalf("frame width %d, want at most %d: %q", lipgloss.Width(line), w, ansi.Strip(line))
		}
	}
}

func TestIssueOverlayDimsParentAndPaintsOpaquePanel(t *testing.T) {
	for _, sz := range overlaySizes {
		m, _ := overlayEnv(t, "board", sz[0], sz[1])
		board := m.View()
		m = pressAll(t, m, "enter")
		palette := theme.Current
		view := m.View()
		if theme.Current != palette {
			t.Fatal("overlay mutated the shared palette")
		}
		x, y, w, h := overlay.Center(sz[0], sz[1])
		screen := vt.NewEmulator(sz[0]+1, sz[1]+1)
		_, _ = screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
		fg := screen.CellAt(0, 0).Style.Fg
		if fg == nil {
			t.Fatal("parent has no explicit dim foreground")
		}
		r, g, b, _ := fg.RGBA()
		dr, dg, db, _ := theme.ANSIColor(theme.Current.Dim).RGBA()
		if r != dr || g != dg || b != db {
			t.Fatal("parent does not use the dim palette")
		}
		wr, wg, wb, _ := theme.ANSIColor(theme.Current.Surface).RGBA()
		for row := y; row < y+h; row++ {
			for col := x; col < x+w; col++ {
				cell := screen.CellAt(col, row)
				if cell == nil || cell.Content == "" {
					continue // the second half of a wide rune carries no style of its own
				}
				bg := cell.Style.Bg
				if bg == nil {
					t.Fatalf("transparent panel cell at %d,%d", col, row)
				}
				edge := col == x || col == x+w-1 || row == y || row == y+h-1
				if r, g, b, _ := bg.RGBA(); edge && (r != wr || g != wg || b != wb) {
					t.Fatalf("border cell %d,%d is not on the panel surface", col, row)
				}
			}
		}
		closed := pressAll(t, m, "esc")
		if closed.View() != board {
			t.Fatal("closing did not restore the exact parent")
		}
	}
}

// overlayDemo runs the real Bubble Tea program and logs each frame as plain text, so a pty session can be
// reviewed without replaying escape sequences.
type overlayDemo struct {
	model Model
	log   *os.File
}

func (d overlayDemo) Init() tea.Cmd { return d.model.Init() }
func (d overlayDemo) View() string  { return d.model.View() }
func (d overlayDemo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := d.model.Update(msg)
	d.model = next.(Model)
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg, tea.WindowSizeMsg:
		fmt.Fprintf(d.log, "=== %T %v (opened=%d layout=%s)\n%s\n", msg, msg, d.model.garden.board.opened, d.model.garden.board.layoutName(), ansi.Strip(d.model.View()))
	}
	return d, cmd
}

// TestGardenIssueOverlayTerminalDemo is the pty fixture: the same Issue opened from Board and from Table in the
// real program with the fake Garden. Set BONSAI_OVERLAY_DEMO_LOG to a file to receive the frames.
func TestGardenIssueOverlayTerminalDemo(t *testing.T) {
	path := os.Getenv("BONSAI_OVERLAY_DEMO_LOG")
	if path == "" {
		t.Skip("interactive terminal fixture; set BONSAI_OVERLAY_DEMO_LOG")
	}
	log, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	m := gardenEnv(t, boardFake(t), config.Prefs{}, 80, 24)
	if _, err := tea.NewProgram(overlayDemo{model: m, log: log}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		t.Fatal(err)
	}
}
