package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// pickerView is the open sessions picker as plain text, at the pane size.
func (e *agentsEnv) pickerView() string {
	return ansi.Strip(e.m.agentOverlayView(e.m.agentPaneSize()))
}

// openPicker makes the pane w x h cells, adds the sessions and opens the picker.
func (e *agentsEnv) openPicker(t *testing.T, w, h int) {
	t.Helper()
	e.relist(t)
	e.sizeTo(t, w, h)
	e.openTab(t)
	e.key(t, "a")
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions == nil {
		t.Fatal("picker not open")
	}
}

// checkPickerBounds fails when the picker overflows its pane or loses the
// search row, the selected row or the footer.
func checkPickerBounds(t *testing.T, e *agentsEnv, wantSelected string) string {
	t.Helper()
	w, h := e.m.agentPaneSize()
	view := e.pickerView()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d: %d rows, want %d:\n%s", w, h, len(lines), h, view)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > w {
			t.Errorf("%dx%d: row wider than the pane: %q", w, h, l)
		}
	}
	for _, want := range []string{"/ ", "Esc ", wantSelected} {
		if !strings.Contains(view, want) {
			t.Errorf("%dx%d: %q must stay reachable:\n%s", w, h, want, view)
		}
	}
	if !strings.Contains(view, "▌") && wantSelected != "" {
		t.Errorf("%dx%d: no selection marker:\n%s", w, h, view)
	}
	return view
}

func (e *agentsEnv) addSessions(n int) {
	for i := range n {
		e.fb.Sessions.Add(e.agent(fmt.Sprintf("s%02d", i), "claude", "exited", e.wtA, fmt.Sprintf("Session number %02d", i), i))
	}
}

func TestSessionsPickerSplitView(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(
			e.agent("r1", "codex", "running", e.wtA, "Redesign agents", 4),
			e.agent("r2", "claude", "running", e.wtA, "Review changes", 3),
			e.agent("x1", "claude", "exited", e.wtA, "Fix terminal resize", 2),
			e.agent("f1", "codex", "failed", e.wtA, "Update keybindings", 1),
		)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "Redesign agents")
		for _, want := range []string{
			"Sessions", "feat-a", "All sessions in this worktree", "4 sessions", "/ Search sessions…",
			"SESSION", "STATUS", "│", "CODEX", "● Running", "Claude · work", "Account", "Worktree",
			"[ Enter Open session ]", "↑↓ Select", "/ Search", "n New session", "Esc Back",
		} {
			if !strings.Contains(view, want) {
				t.Errorf("%v: missing %q:\n%s", size, want, view)
			}
		}
		if n := strings.Count(view, "Esc Back"); n != 1 {
			t.Errorf("%v: one footer expected, got %d", size, n)
		}
		// The inspector follows the selection without a confirmation step.
		e.send(t, tea.KeyMsg{Type: tea.KeyDown})
		e.send(t, tea.KeyMsg{Type: tea.KeyDown})
		view = e.pickerView()
		if !strings.Contains(view, "CLAUDE") || !strings.Contains(view, "✓ Exited") {
			t.Errorf("%v: inspector did not follow selection:\n%s", size, view)
		}
		// The overlay owns its footer: the status bar adds none.
		if got := e.m.contextHelp(); got != "" {
			t.Errorf("status bar help = %q, want empty", got)
		}
	}
}

func TestSessionsPickerInspectorOnlySafeFields(t *testing.T) {
	e := newAgentsEnv(t)
	s := e.agent("abcdef1234567890", "claude", "failed", e.wtA, "Broken", 1)
	code := 2
	s.ExitCode = &code
	s.Model = "sonnet"
	s.Error = "boom\x1b[31m red"
	e.fb.Sessions.Add(s)
	e.openPicker(t, 140, 40)
	view := e.pickerView()
	for _, want := range []string{"Model", "sonnet", "Exit code", "2", "Error", "boom", "…34567890"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b") || strings.Contains(view, "1 sessions") || !strings.Contains(view, "1 session") {
		t.Errorf("sanitized text and singular count expected:\n%s", view)
	}
}

func TestSessionsPickerDuplicateNames(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(
			e.agent("aaaa-b6364576", "codex", "running", e.wtA, "Same name", 2),
			e.agent("bbbb-1234abcd", "codex", "running", e.wtA, "Same name", 1),
			e.agent("c", "claude", "running", e.wtA, "Unique", 0),
		)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "Same name")
		for _, want := range []string{"Same name · …b6364576", "Same name · …1234abcd"} {
			if !strings.Contains(view, want) {
				t.Errorf("%v: missing %q:\n%s", size, want, view)
			}
		}
		if strings.Contains(view, "Unique ·") {
			t.Errorf("%v: unique names get no suffix:\n%s", size, view)
		}
	}
}

func TestSessionsPickerLongNames(t *testing.T) {
	long := strings.Repeat("very long session name ", 6)
	for _, size := range [][2]int{{80, 24}, {140, 40}, {50, 20}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(
			e.agent("aaaa-b6364576", "codex", "running", e.wtA, long, 2),
			e.agent("bbbb-1234abcd", "codex", "running", e.wtA, long, 1),
		)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "…")
		if !strings.Contains(view, "…b6364576") || !strings.Contains(view, "…1234abcd") {
			t.Errorf("%v: the ID suffix must survive truncation:\n%s", size, view)
		}
	}
}

func TestSessionsPickerEmptyAndNoMatches(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		e := newAgentsEnv(t)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "")
		for _, want := range []string{"0 sessions", "No sessions in this worktree yet", "n New session"} {
			if !strings.Contains(view, want) {
				t.Errorf("%v: empty picker lacks %q:\n%s", size, want, view)
			}
		}
		e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
		if e.m.agents.overlay == nil {
			t.Error("enter on an empty list must do nothing")
		}

		e = newAgentsEnv(t)
		e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Alpha", 1))
		e.openPicker(t, size[0], size[1])
		e.key(t, "/")
		e.key(t, "zzz")
		view = checkPickerBounds(t, e, "")
		for _, want := range []string{`No sessions match "zzz".`, "0 of 1 sessions", "Enter Open  Esc Clear search"} {
			if !strings.Contains(view, want) {
				t.Errorf("%v: no-match view lacks %q:\n%s", size, want, view)
			}
		}
		if strings.Contains(view, "[ Enter Open session ]") {
			t.Errorf("%v: no inspector without a selection:\n%s", size, view)
		}
		e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
		if e.m.agents.overlay == nil {
			t.Error("enter without a match must do nothing")
		}
	}
}

func TestSessionsPickerScrollsManySessions(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		e := newAgentsEnv(t)
		e.addSessions(30)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "Session number 29") // newest first
		if !strings.Contains(view, "of 30") {
			t.Errorf("%v: scroll range missing:\n%s", size, view)
		}
		for range 29 {
			e.send(t, tea.KeyMsg{Type: tea.KeyDown})
		}
		view = checkPickerBounds(t, e, "Session number 00")
		if strings.Contains(view, "Session number 29") {
			t.Errorf("%v: window must have scrolled:\n%s", size, view)
		}
		e.send(t, tea.KeyMsg{Type: tea.KeyDown}) // stays on the last row
		if got := e.m.agents.overlay.sessions.cursor; got != 29 {
			t.Errorf("cursor = %d, want 29", got)
		}
		for range 29 {
			e.send(t, tea.KeyMsg{Type: tea.KeyUp})
		}
		checkPickerBounds(t, e, "Session number 29")
	}
}

func TestSessionsPickerNarrowStacksDetailsBelow(t *testing.T) {
	for _, size := range [][2]int{{60, 24}, {50, 20}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(
			e.agent("r1", "codex", "running", e.wtA, "Redesign agents", 2),
			e.agent("x1", "claude", "exited", e.wtA, "Fix terminal", 1),
		)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "Redesign agents")
		if strings.Contains(view, " │ ") {
			t.Errorf("%v: narrow layout must not split columns:\n%s", size, view)
		}
		list := strings.Index(view, "Redesign agents")
		det := strings.Index(view, "Account")
		if det < 0 || det < list {
			t.Errorf("%v: details belong below the list:\n%s", size, view)
		}
		if !strings.Contains(view, "[ Enter Open session ]") {
			t.Errorf("%v: open action missing:\n%s", size, view)
		}
	}
}

func TestSessionsPickerShortTerminalKeepsSearchRowAndFooter(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {80, 9}, {60, 8}, {50, 7}} {
		e := newAgentsEnv(t)
		e.addSessions(6)
		e.openPicker(t, size[0], size[1])
		for range 3 {
			e.send(t, tea.KeyMsg{Type: tea.KeyDown})
		}
		checkPickerBounds(t, e, "Session number 02")
	}
}

func TestSessionsPickerKeys(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("r1", "claude", "running", e.wtA, "Alpha", 2),
		e.agent("r2", "codex", "running", e.wtA, "Beta", 1),
	)
	e.openPicker(t, 100, 30)
	e.key(t, "/")
	e.key(t, "n") // text while searching, not "new session"
	if e.m.agents.overlay == nil || e.m.agents.overlay.launch != nil {
		t.Fatal("n while searching must be typed")
	}
	if !strings.Contains(e.pickerView(), "/ n") {
		t.Errorf("typed text missing:\n%s", e.pickerView())
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyEsc})
	e.send(t, tea.KeyMsg{Type: tea.KeyEsc})
	e.run(t, e.lastCmd)
	if e.m.agents.overlay != nil {
		t.Error("esc on the idle list closes the picker")
	}

	e.key(t, "a")
	e.send(t, tea.KeyMsg{Type: tea.KeyDown})
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	e.run(t, e.lastCmd)
	if s, _, _ := e.m.selectedSession(cleanPath(e.wtA)); s.ID != "r2" || e.m.agents.overlay != nil {
		t.Errorf("enter opens the highlighted session: %s overlay=%v", s.ID, e.m.agents.overlay)
	}
}

func TestSessionsPickerFollowsListChanges(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("r1", "claude", "running", e.wtA, "Alpha", 2),
		e.agent("r2", "codex", "running", e.wtA, "Beta", 1),
	)
	e.openPicker(t, 100, 30)
	e.send(t, tea.KeyMsg{Type: tea.KeyDown})
	e.fb.Sessions.Add(e.agent("r3", "codex", "running", e.wtA, "Gamma", 5))
	e.relist(t)
	p := e.m.agents.overlay.sessions
	if got, _ := p.selected(); got.ID != "r2" || len(p.sessions) != 3 {
		t.Errorf("selection must survive a refresh: %s of %d", got.ID, len(p.sessions))
	}
}

func TestSessionsPickerASCII(t *testing.T) {
	e := newAgentsEnv(t)
	e.m.state.Prefs.AgentGlyphs = "ascii"
	e.fb.Sessions.Add(e.agent("r1", "claude", "running", e.wtA, "Alpha", 2))
	e.openPicker(t, 100, 30)
	view := e.m.agentOverlayView(e.m.agentPaneSize())
	if bad := nonASCII(view); len(bad) > 0 {
		t.Errorf("non-ASCII %q:\n%s", string(bad), ansi.Strip(view))
	}
	if !strings.Contains(ansi.Strip(view), "Up/Down Select") {
		t.Errorf("ascii footer:\n%s", ansi.Strip(view))
	}
}

func TestSessionsPickUsesPickersWorktree(t *testing.T) {
	e := accountsEnv(t)
	e.openPicker(t, 100, 24)
	path := e.m.agents.overlay.sessions.path
	if path == "" {
		t.Fatal("picker lost its worktree path")
	}
	// The cursor moved to another worktree while the picker was open.
	e.m.agents.selected[cleanPath(path)] = "stale"
	nm, _ := e.m.onAgentsPick(path, "picked")
	if got := nm.(Model).agents.selected[cleanPath(path)]; got != "picked" {
		t.Errorf("selected = %q, want picked", got)
	}
	if _, cmd := e.m.onAgentsPick("", "x"); cmd != nil {
		t.Error("pick without a worktree must be a no-op")
	}
}
