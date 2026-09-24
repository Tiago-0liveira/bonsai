package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/terminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

// procModel is a model on the Processes tab with three processes in the
// selected worktree, enough for the footer to take several rows.
func procModel() Model {
	m := renderModel()
	m.rightTab = tabProcs
	m.term = terminal.New()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}}})
	m.activeProc = map[string]int{}
	m.procMultiSel = map[string][]int{}
	m.procSearch = map[string]string{}
	m.procs = &procView{recs: []*procstore.Record{
		{ID: 1, Label: "dev", Worktree: "/w/feat", Status: procstore.StatusRunning},
		{ID: 2, Label: "api", Worktree: "/w/feat", Status: procstore.StatusFailed},
		{ID: 3, Label: "test", Worktree: "/w/feat", Status: procstore.StatusDone},
	}}
	return m
}

// The output viewport has to be sized to the area it is drawn into. When it
// thinks it is taller, it clamps its own scroll offset and the tail of the log
// becomes unreachable.
func TestTermHeightLeavesRoomForProcFooter(t *testing.T) {
	m := procModel()
	_, _, innerH := m.dims()

	footerH := len(strings.Split(m.renderProcFooter(), "\n"))
	if footerH < 3 {
		t.Fatalf("expected a multi-row footer, got %d rows", footerH)
	}
	if got, want := m.termHeight(innerH), innerH-1-footerH; got != want {
		t.Fatalf("termHeight on Processes tab = %d, want %d (pane %d - tab strip - footer %d)",
			got, want, innerH, footerH)
	}

	m.rightTab = tabLog
	if got, want := m.termHeight(innerH), innerH-1; got != want {
		t.Fatalf("termHeight on Git Log tab = %d, want %d", got, want)
	}
}

func TestLayoutSizesTermForActiveTab(t *testing.T) {
	m := procModel()
	_, _, innerH := m.dims()
	m.layout()
	procH := m.term.Height()

	m.rightTab = tabLog
	m.layout()
	if m.term.Height() <= procH {
		t.Fatalf("leaving the Processes tab should give the footer's rows back: %d -> %d",
			procH, m.term.Height())
	}
	if m.term.Height() != innerH-1 {
		t.Fatalf("Git Log viewport height = %d, want %d", m.term.Height(), innerH-1)
	}
}

func TestRenderProcLogColorsMarkers(t *testing.T) {
	log := "starting\n" +
		procstore.Marker{Kind: procstore.MarkerExit, Code: 1, Text: "exited 1 · failed · ran 2s"}.Encode() +
		"after\n"
	out := renderProcLog(log, 40)

	if strings.Contains(out, procstore.MarkerSentinel) {
		t.Fatalf("raw marker leaked into the pane: %q", out)
	}
	for _, want := range []string{"starting", "exited 1 · failed · ran 2s", "after"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderProcLog dropped %q:\n%s", want, out)
		}
	}
	// Outcome drives the color: a clean exit and a failure must not look alike.
	// (Rendering here is color-free — the test binary has no TTY — so compare
	// the styles the renderer picks.)
	ok := markerStyle(procstore.Marker{Kind: procstore.MarkerExit, Code: 0}).GetForeground()
	fail := markerStyle(procstore.Marker{Kind: procstore.MarkerExit, Code: 1}).GetForeground()
	stop := markerStyle(procstore.Marker{Kind: procstore.MarkerStopped}).GetForeground()
	start := markerStyle(procstore.Marker{Kind: procstore.MarkerStart}).GetForeground()
	if ok == fail || ok == stop || fail == stop || start == fail {
		t.Errorf("marker colors collide: ok=%v fail=%v stop=%v start=%v", ok, fail, stop, start)
	}
}

func TestProcSearchKeepsRunDelimiters(t *testing.T) {
	mk := procstore.Marker{Kind: procstore.MarkerStart, Text: "started · dev · 10:00:00"}
	log := mk.Encode() + "hit here\nmiss\n"

	got := applyProcSearch(log, "hit")
	if !strings.Contains(got, "hit here") {
		t.Fatalf("filter dropped the match: %q", got)
	}
	if strings.Contains(got, "miss") {
		t.Fatalf("filter kept a non-match: %q", got)
	}
	if !strings.Contains(got, procstore.MarkerSentinel) {
		t.Fatalf("filter dropped the run delimiter: %q", got)
	}
}

// The wheel must scroll the log the pointer is over, not move the process
// selection (terminals translate the wheel into arrow keys when mouse tracking
// is off, which is how it used to change the selection).
func TestMouseWheelScrollsLogNotSelection(t *testing.T) {
	m := procModel()
	m.activeProc["/w/feat"] = 2
	m.layout()
	m.term.SetContent(strings.Repeat("a line of output\n", 200))
	m.term.GotoTop()

	leftW, _, _ := m.dims()
	wheel := tea.MouseMsg{
		X: leftW + 5, Y: 3,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	}
	nm, _ := m.onMouse(wheel)
	got := nm.(Model)

	if got.term.ScrollPercent() <= 0 {
		t.Errorf("wheel over the log did not scroll it (%.2f)", got.term.ScrollPercent())
	}
	if got.activeProc["/w/feat"] != 2 {
		t.Errorf("wheel changed the selected process: %d", got.activeProc["/w/feat"])
	}
}

func TestMouseWheelOverListLeavesLogAlone(t *testing.T) {
	m := procModel()
	m.layout()
	m.term.SetContent(strings.Repeat("a line of output\n", 200))
	m.term.GotoTop()

	wheel := tea.MouseMsg{X: 1, Y: 3, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	nm, _ := m.onMouse(wheel)
	if got := nm.(Model); got.term.ScrollPercent() > 0 {
		t.Errorf("wheel over the worktree list scrolled the log (%.2f)", got.term.ScrollPercent())
	}
}

func TestMouseOffIgnoresWheel(t *testing.T) {
	m := procModel()
	m.mouseOff = true
	m.layout()
	m.term.SetContent(strings.Repeat("a line of output\n", 200))
	m.term.GotoTop()

	leftW, _, _ := m.dims()
	wheel := tea.MouseMsg{X: leftW + 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	nm, _ := m.onMouse(wheel)
	if got := nm.(Model); got.term.ScrollPercent() > 0 {
		t.Errorf("wheel scrolled with the mouse turned off (%.2f)", got.term.ScrollPercent())
	}
}

// Nothing drawn in the right pane may be wider than the pane, or it spills over
// the border into the worktree list beside it.
func TestProcOutputStaysInsidePane(t *testing.T) {
	const paneWidth = 40
	nasty := procstore.SanitizeOutput(
		"build  10%\rbuild 100% " + strings.Repeat("x", 300) + "\n" + // rewritten line, then no word breaks
			"\x1b[2Kerased line\n" + // erase-line: would wipe the pane to the left
			strings.Repeat("word ", 60) + "\n") // plain long line

	term := terminal.New()
	term.SetSize(paneWidth, 12)
	term.SetContent(renderProcLog(nasty, paneWidth))

	view := term.View()
	if strings.Contains(view, "\r") {
		t.Errorf("carriage return survived into the pane: %q", view)
	}
	if strings.Contains(view, "\x1b[2K") {
		t.Errorf("erase-line sequence survived into the pane: %q", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > paneWidth {
			t.Errorf("line %d is %d cells wide, pane is %d: %q", i, w, paneWidth, line)
		}
	}
}
