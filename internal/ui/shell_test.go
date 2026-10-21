package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func shellModel(t *testing.T, prefs config.Prefs, keys map[string]string) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	cfg := &config.Config{Keys: keys}
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs = prefs
	return New(t.TempDir(), cfg, st, WithLayout(LayoutRedesign))
}

func sized(t *testing.T, m Model, w, h int) Model {
	t.Helper()
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if cmd != nil {
		t.Fatalf("resize returned a command")
	}
	return nm.(Model)
}

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestParseLayout(t *testing.T) {
	for in, want := range map[string]Layout{"legacy": LayoutLegacy, "redesign": LayoutRedesign} {
		if got, err := ParseLayout(in); err != nil || got != want {
			t.Errorf("ParseLayout(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "Redesign", "new"} {
		if _, err := ParseLayout(in); err == nil {
			t.Errorf("ParseLayout(%q) accepted", in)
		}
	}
}

func TestShellInitStartsNothing(t *testing.T) {
	if cmd := shellModel(t, config.Prefs{}, nil).Init(); cmd != nil {
		t.Error("redesign Init must not start domain work")
	}
	t.Setenv("HOME", t.TempDir())
	legacy := New(t.TempDir(), &config.Config{}, &config.State{})
	if legacy.Init() == nil {
		t.Error("legacy Init lost its startup commands")
	}
	explicit := New(t.TempDir(), &config.Config{}, &config.State{}, WithLayout(LayoutLegacy))
	if explicit.Init() == nil {
		t.Error("explicit legacy layout lost its startup commands")
	}
}

func TestShellGeometry(t *testing.T) {
	tests := []struct {
		name          string
		w, h, sub, dt int
		body          int
		ok            bool
	}{
		{"80x24", 80, 24, 0, 0, 18, true},
		{"120x35", 120, 35, 0, 0, 29, true},
		{"160x48", 160, 48, 0, 0, 42, true},
		{"submenu and detail rows are reserved first", 80, 24, 1, 3, 14, true},
		{"min size", shellMinWidth, shellMinHeight, 0, 0, shellMinHeight - 6, true},
		{"too narrow", shellMinWidth - 1, 24, 0, 0, 18, false},
		{"too short", 80, shellMinHeight - 1, 0, 0, shellMinHeight - 7, false},
		{"reserved rows leave no body", 80, 12, 3, 3, 0, false},
	}
	for _, tc := range tests {
		r, ok := shellGeometry(tc.w, tc.h, tc.sub, tc.dt)
		if ok != tc.ok || r.body != tc.body {
			t.Errorf("%s: body=%d ok=%v, want body=%d ok=%v", tc.name, r.body, ok, tc.body, tc.ok)
		}
		if ok && r.header+r.submenu+r.body+r.detail+r.footer != tc.h {
			t.Errorf("%s: regions do not add up to the height: %+v", tc.name, r)
		}
	}
}

func assertBounded(t *testing.T, label, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > h {
		t.Errorf("%s: %d lines in %d rows", label, len(lines), h)
	}
	for i, l := range lines {
		if got := lipgloss.Width(l); got > w {
			t.Errorf("%s: line %d is %d wide in %d cols: %q", label, i, got, w, ansi.Strip(l))
		}
	}
}

func TestShellViewStaysInsideViewport(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 10}, {63, 24}, {80, 9}, {40, 5}, {20, 3}, {1, 1}, {5, 1}}
	for _, ascii := range []bool{false, true} {
		for _, preset := range theme.Presets() {
			prefs := config.Prefs{Theme: preset}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			base := shellModel(t, prefs, nil)
			for _, sz := range sizes {
				m := sized(t, base, sz[0], sz[1])
				label := fmt.Sprintf("%s ascii=%v %dx%d", preset, ascii, sz[0], sz[1])
				view := m.View()
				assertBounded(t, label, view, sz[0], sz[1])
				if sz[0] >= 20 && sz[1] >= 3 {
					// An open dialog must be bounded too, and below the minimum
					// the resize notice replaces it.
					dm := m.openShellPalette()
					assertBounded(t, label+" palette", dm.View(), sz[0], sz[1])
					if sz[0] < shellMinWidth || sz[1] < shellMinHeight {
						if dm.View() != view {
							t.Errorf("%s: dialog shown below the minimum size", label)
						}
					}
				}
				if sz[0] >= shellMinWidth && sz[1] >= shellMinHeight {
					if got := len(strings.Split(view, "\n")); got != sz[1] {
						t.Errorf("%s: %d lines, want exactly %d", label, got, sz[1])
					}
				}
			}
		}
	}
}

func TestShellTabsAllVisibleInOrder(t *testing.T) {
	labels := []string{"Garden", "Playground", "Worktrees", "Accounts", "Settings"}
	for _, ascii := range []bool{false, true} {
		for _, preset := range theme.Presets() {
			prefs := config.Prefs{Theme: preset}
			if ascii {
				prefs.AgentGlyphs = "ascii"
			}
			for _, w := range []int{shellMinWidth, 70, 80, 120} {
				m := sized(t, shellModel(t, prefs, nil), w, 24)
				tabs := strings.Split(ansi.Strip(m.View()), "\n")[1]
				at := -1
				for _, l := range labels {
					i := strings.Index(tabs, l)
					if i < 0 || i < at {
						t.Fatalf("%s ascii=%v w=%d: %q missing or out of order in %q", preset, ascii, w, l, tabs)
					}
					at = i
				}
				if ascii {
					for _, r := range tabs {
						if r > 127 {
							t.Errorf("%s w=%d: non-ASCII %q in ascii tabs %q", preset, w, string(r), tabs)
							break
						}
					}
				}
			}
		}
	}
}

func TestShellBodyIsEmpty(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	for ws := workspace(0); ws < workspaceCount; ws++ {
		if ws == wsSettings || ws == wsAccounts || ws == wsGarden || ws == wsPlayground {
			continue // the workspaces with a body so far
		}
		m.workspace = ws
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		for i, l := range lines[shellHeaderRows : len(lines)-shellFooterRows] {
			if strings.TrimSpace(l) != "" {
				t.Errorf("%s body line %d not empty: %q", ws.name(), i, l)
			}
		}
	}
}

func TestShellResizeNotice(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 40, 8)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "needs 64x10") {
		t.Errorf("notice missing requirement: %q", view)
	}
	// Growing the terminal brings the chrome back.
	m = sized(t, m, 80, 24)
	if !strings.Contains(ansi.Strip(m.View()), "Playground") {
		t.Error("shell did not return after resize")
	}
}

func TestShellKeySelection(t *testing.T) {
	tests := []struct {
		name  string
		start workspace
		keys  []tea.Msg
		want  workspace
	}{
		{"right", wsGarden, []tea.Msg{tea.KeyMsg{Type: tea.KeyRight}}, wsPlayground},
		{"l", wsGarden, []tea.Msg{runeKey("l")}, wsPlayground},
		{"left wraps", wsGarden, []tea.Msg{tea.KeyMsg{Type: tea.KeyLeft}}, wsSettings},
		{"h", wsAccounts, []tea.Msg{runeKey("h")}, wsWorktrees},
		{"tab", wsGarden, []tea.Msg{tea.KeyMsg{Type: tea.KeyTab}}, wsPlayground},
		{"shift+tab", wsGarden, []tea.Msg{tea.KeyMsg{Type: tea.KeyShiftTab}}, wsSettings},
		{"right wraps", wsSettings, []tea.Msg{tea.KeyMsg{Type: tea.KeyRight}}, wsGarden},
		{"alt+4", wsGarden, []tea.Msg{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4"), Alt: true}}, wsAccounts},
		{"bare digit is ignored", wsGarden, []tea.Msg{runeKey("4")}, wsGarden},
		{"g stays free", wsGarden, []tea.Msg{runeKey("g")}, wsGarden},
	}
	for _, tc := range tests {
		m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
		m.workspace = tc.start
		for _, k := range tc.keys {
			var cmd tea.Cmd
			m, cmd = send(t, m, k)
			// Only a real change is remembered; the command is the persist.
			if changed := m.workspace != tc.start; (cmd != nil) != changed {
				t.Errorf("%s: command=%v, workspace changed=%v", tc.name, cmd != nil, changed)
			}
		}
		if m.workspace != tc.want {
			t.Errorf("%s: workspace = %s, want %s", tc.name, m.workspace.name(), tc.want.name())
		}
	}
}

func TestShellNavigationYieldsToInputFocus(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	for _, f := range []shellFocus{shellBody, shellInput} {
		m.shellFocus = f
		m2, _ := send(t, m, tea.KeyMsg{Type: tea.KeyRight})
		if m2.workspace != m.workspace {
			t.Errorf("focus %s still switched workspaces", f)
		}
		if !strings.Contains(ansi.Strip(m2.View()), "FOCUS: Garden / "+f.String()) {
			t.Errorf("footer does not show focus %s", f)
		}
	}
}

func TestShellBodyFocusKeepsGlobalBindings(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	m.shellFocus = shellBody
	if _, cmd := send(t, m, runeKey("q")); cmd == nil || cmd() != tea.Quit() {
		t.Error("q must still quit while the body has focus")
	}
	if m2, _ := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK}); m2.modal == nil {
		t.Error("palette must open while the body has focus")
	}
	if m2, _ := send(t, m, runeKey("?")); m2.modal == nil {
		t.Error("help must open while the body has focus")
	}
}

func TestShellPaletteSelectsWorkspaceOnly(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if cmd != nil || m.modal == nil || m.modal.Kind() != modals.KindPalette {
		t.Fatalf("ctrl+k did not open the palette (cmd=%v)", cmd)
	}
	view := ansi.Strip(m.View())
	for ws := workspace(0); ws < workspaceCount; ws++ {
		if !strings.Contains(view, "Go to "+ws.name()) {
			t.Errorf("palette missing Go to %s:\n%s", ws.name(), view)
		}
	}
	for _, domain := range []string{"worktree", "Prune", "Pull", "Quit", "alias"} {
		if strings.Contains(view, domain) {
			t.Errorf("palette lists domain command %q", domain)
		}
	}
	for _, ws := range []workspace{wsAccounts, wsGarden, wsSettings} {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
		m, cmd = send(t, m, modals.SubmitMsg{Kind: modals.KindPalette, Value: "Go to " + ws.name()})
		if cmd != nil {
			if _, ok := cmd().(workspaceSavedMsg); !ok {
				t.Errorf("palette selection of %s returned domain work", ws.name())
			}
		}
		if m.workspace != ws || m.modal != nil {
			t.Errorf("after Go to %s: workspace=%s modal open=%v", ws.name(), m.workspace.name(), m.modal != nil)
		}
	}
}

func TestShellPaletteTypingAndEnterEndToEnd(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	for _, r := range "sett" {
		m, _ = send(t, m, runeKey(string(r)))
	}
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in the palette produced no submit")
	}
	m, cmd = send(t, m, cmd())
	if m.workspace != wsSettings {
		t.Errorf("workspace=%s, want Settings", m.workspace.name())
	}
	if cmd != nil {
		if _, ok := cmd().(workspaceSavedMsg); !ok {
			t.Errorf("palette selection returned domain work")
		}
	}
}

func TestShellDialogTakesKeyPrecedence(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.currentShellFocus() != shellDialog {
		t.Fatal("open palette should own focus")
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRight}, {Type: tea.KeyDown}, {Type: tea.KeyTab}, runeKey("l"), runeKey("q")} {
		m, _ = send(t, m, k)
		if m.workspace != wsGarden {
			t.Fatalf("key %q reached the tabs while the palette was open", k.String())
		}
		if m.modal == nil {
			t.Fatalf("key %q closed the palette", k.String())
		}
	}
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc produced no cancel")
	}
	m, _ = send(t, m, cmd())
	if m.modal != nil {
		t.Error("esc did not close the palette")
	}
}

func TestShellHelpAndQuit(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	h, cmd := send(t, m, runeKey("?"))
	if cmd != nil || h.modal == nil || h.modal.Kind() != modals.KindKeymap {
		t.Fatalf("? did not open help (cmd=%v)", cmd)
	}
	if view := ansi.Strip(h.View()); !strings.Contains(view, "workspace") || !strings.Contains(view, "alt+1-5") {
		t.Errorf("help does not describe the shell keys:\n%s", view)
	}
	h, _ = send(t, h, modals.CancelMsg{})
	if h.modal != nil {
		t.Error("help did not close")
	}
	for _, k := range []tea.KeyMsg{runeKey("q"), {Type: tea.KeyCtrlC}} {
		_, cmd := send(t, m, k)
		if cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s did not quit", k.String())
		}
	}
}

func TestShellHonorsKeyOverrides(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, map[string]string{"palette": "ctrl+o", "quit": "x", "focus_next": "n"}), 120, 35)
	view := ansi.Strip(m.View())
	for _, want := range []string{"ctrl+o commands", "x quit", "n/shift+tab next/prev"} {
		if !strings.Contains(view, want) {
			t.Errorf("footer does not show the effective binding %q:\n%s", want, view)
		}
	}
	if m2, _ := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK}); m2.modal != nil {
		t.Error("the replaced default palette key still opens it")
	}
	if m2, _ := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlO}); m2.modal == nil {
		t.Error("the override key did not open the palette")
	}
	if m2, _ := send(t, m, runeKey("n")); m2.workspace != wsPlayground {
		t.Error("overridden focus_next did not select the next workspace")
	}
	if _, cmd := send(t, m, runeKey("x")); cmd == nil {
		t.Error("overridden quit key did nothing")
	}
}

func TestShellFooterHintsFitNarrow(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 64, 24)
	footer := ansi.Strip(m.View())
	for _, want := range []string{"FOCUS: Garden / Navigation", "←/→ workspace", "ctrl+k commands"} {
		if !strings.Contains(footer, want) {
			t.Errorf("narrow footer lost %q:\n%s", want, footer)
		}
	}
}

func TestShellIgnoresDomainMessages(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	for _, msg := range []tea.Msg{worktreesMsg{}, prTickMsg{}, updateAvailableMsg{}} {
		m2, cmd := send(t, m, msg)
		if cmd != nil || m2.workspace != m.workspace {
			t.Errorf("%T triggered work (cmd=%v)", msg, cmd)
		}
	}
}

func TestShellInputFocusKeepsLetterKeys(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{}, nil), 80, 24)
	m.shellFocus = shellInput
	for _, k := range []tea.KeyMsg{runeKey("q"), runeKey("?"), {Type: tea.KeyCtrlK}} {
		m2, cmd := send(t, m, k)
		if cmd != nil || m2.modal != nil {
			t.Errorf("%s acted while an input owned the keyboard", k.String())
		}
	}
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c must still quit")
	}
}

func TestShellHelpIsASCIIInASCIIMode(t *testing.T) {
	m := sized(t, shellModel(t, config.Prefs{AgentGlyphs: "ascii"}, nil), 80, 24)
	m, _ = send(t, m, runeKey("?"))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "</>") {
		t.Errorf("help lost the workspace hint:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "workspace") || strings.Contains(line, "commands") {
			for _, r := range line {
				if r > 127 && !strings.ContainsRune("│╭╮╰╯─›", r) {
					t.Errorf("non-ASCII %q in help item %q", string(r), line)
				}
			}
		}
	}
}
