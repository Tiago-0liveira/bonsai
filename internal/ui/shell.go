package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

// Layout selects which TUI the root model runs. The redesign is an opt-in
// preview until its workspaces replace the legacy screens.
type Layout string

const (
	LayoutLegacy   Layout = "legacy"
	LayoutRedesign Layout = "redesign"
)

// ParseLayout validates a --tui-layout value.
func ParseLayout(s string) (Layout, error) {
	switch Layout(s) {
	case LayoutLegacy, LayoutRedesign:
		return Layout(s), nil
	}
	return "", fmt.Errorf("invalid --tui-layout %q: want %q or %q", s, LayoutLegacy, LayoutRedesign)
}

// WithLayout selects the TUI layout. The zero value keeps the legacy TUI.
func WithLayout(l Layout) Option {
	return func(m *Model) { m.layoutKind = l }
}

// workspace is one top-level destination of the redesigned shell.
type workspace int

const (
	wsGarden workspace = iota
	wsPlayground
	wsWorktrees
	wsAccounts
	wsSettings
	workspaceCount
)

// workspaceInfo carries the label and the unicode/ASCII icons of a workspace.
// ASCII icons are one cell like the unicode ones so tab widths do not change.
var workspaceInfo = [workspaceCount]struct{ name, icon, ascii string }{
	wsGarden:     {"Garden", "♧", "*"},
	wsPlayground: {"Playground", "▣", "#"},
	wsWorktrees:  {"Worktrees", "⌘", "%"},
	wsAccounts:   {"Accounts", "◎", "@"},
	wsSettings:   {"Settings", "⚙", "="},
}

func (w workspace) name() string { return workspaceInfo[w].name }

// shellFocus says who owns keyboard input in the redesigned shell. Workspaces
// that hold a terminal or a text field (later plans) take shellBody or
// shellInput; while they do, the shell's own navigation keys stand down so it
// can never steal keystrokes from them. Nothing in the empty bodies sets
// those two yet.
type shellFocus int

const (
	shellNav shellFocus = iota
	shellBody
	shellDialog
	shellInput
)

func (f shellFocus) String() string {
	switch f {
	case shellBody:
		return "Body"
	case shellDialog:
		return "Dialog"
	case shellInput:
		return "Input"
	default:
		return "Navigation"
	}
}

// currentShellFocus resolves the focus, letting an open overlay win.
func (m Model) currentShellFocus() shellFocus {
	if m.modal != nil {
		return shellDialog
	}
	return m.shellFocus
}

// Outer-region sizes of the shell. Submenu and detail rows are hooks for later
// workspaces and stay zero until one needs them.
const (
	shellHeaderRows = 3
	shellFooterRows = 3
	shellMinWidth   = 64
	shellMinHeight  = 10
	// shellCompactBelow is the width under which tabs drop their gaps.
	shellCompactBelow = 72
)

// shellRegions is the single geometry calculation: exact rows per outer region,
// reserved before the body is sized.
type shellRegions struct {
	width, header, submenu, body, detail, footer int
}

// shellGeometry reserves the fixed regions and gives the body the rest. ok is
// false when the viewport is too small for the chrome.
func shellGeometry(width, height, submenu, detail int) (shellRegions, bool) {
	r := shellRegions{width: width, header: shellHeaderRows, submenu: submenu, detail: detail, footer: shellFooterRows}
	r.body = height - r.header - r.submenu - r.detail - r.footer
	return r, width >= shellMinWidth && height >= shellMinHeight && r.body >= 1
}

// shellUpdate is the redesigned shell's Update: it owns only sizing, the
// palette/help dialogs and workspace selection, and starts no domain work.
func (m Model) shellUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		if m.modal != nil {
			m.modal.SetSize(msg.Width, msg.Height)
		}
		return m, nil
	case modals.CancelMsg:
		m.modal = nil
		return m, nil
	case modals.SubmitMsg:
		kind := msg.Kind
		m.modal = nil
		if kind == modals.KindPalette {
			if ws, ok := workspaceForPaletteLabel(msg.Value); ok {
				m.workspace = ws
			}
		}
		return m, nil
	case tea.KeyMsg:
		if m.modal != nil {
			nm, cmd := m.modal.Update(msg)
			m.modal = &nm
			return m, cmd
		}
		return m.shellKey(msg)
	}
	return m, nil
}

func (m Model) shellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits; every other key yields to a body or input that owns
	// the keyboard, including the letters bound to quit, help and the palette.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.currentShellFocus() != shellNav {
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Palette):
		return m.openShellPalette(), nil
	case key.Matches(msg, m.keys.Help):
		return m.openShellHelp(), nil
	}
	switch {
	case key.Matches(msg, m.keys.Tab), msg.String() == "right", msg.String() == "l":
		m.workspace = (m.workspace + 1) % workspaceCount
	case key.Matches(msg, m.keys.ShiftTab), msg.String() == "left", msg.String() == "h":
		m.workspace = (m.workspace + workspaceCount - 1) % workspaceCount
	default:
		// alt+digit selects directly; bare digits belong to agent sessions.
		for i := workspace(0); i < workspaceCount; i++ {
			if msg.String() == fmt.Sprintf("alt+%d", i+1) {
				m.workspace = i
			}
		}
	}
	return m, nil
}

const paletteGoPrefix = "Go to "

func workspaceForPaletteLabel(label string) (workspace, bool) {
	for i := workspace(0); i < workspaceCount; i++ {
		if label == paletteGoPrefix+i.name() {
			return i, true
		}
	}
	return 0, false
}

// filterItems is a case-insensitive substring filter over a fixed list.
func filterItems(items []string) modals.FilterFunc {
	return func(q string) []string {
		q = strings.ToLower(strings.TrimSpace(q))
		var out []string
		for _, it := range items {
			if q == "" || strings.Contains(strings.ToLower(it), q) {
				out = append(out, it)
			}
		}
		return out
	}
}

// openShellPalette lists the workspaces: selecting one only moves the tab.
func (m Model) openShellPalette() Model {
	items := make([]string, workspaceCount)
	for i := range items {
		items[i] = paletteGoPrefix + workspace(i).name()
	}
	modal := modals.NewFuzzy(modals.KindPalette, "Go to workspace", filterItems(items), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// shellHints are the navigation hints in the order the footer drops them
// when the width runs out, most important first. Keys are the effective ones,
// so scoped overrides show up.
func (m Model) shellHints() []shellHint {
	h := []shellHint{}
	if m.currentShellFocus() == shellNav {
		h = append(h, shellHint{"←/→", "workspace"})
	}
	h = append(h,
		shellHint{m.keys.Palette.Help().Key, "commands"},
		shellHint{m.keys.Help.Help().Key, "keys"},
		shellHint{m.keys.Quit.Help().Key, "quit"},
	)
	if m.currentShellFocus() == shellNav {
		h = append(h,
			shellHint{m.keys.Tab.Help().Key + "/" + m.keys.ShiftTab.Help().Key, "next/prev"},
			shellHint{"alt+1-5", "jump"},
		)
	}
	return h
}

type shellHint struct{ key, desc string }

// openShellHelp is the key reference for the shell: the keys that work here,
// not the legacy worktree bindings.
func (m Model) openShellHelp() Model {
	var items []string
	for _, h := range append(m.shellHints(), shellHint{"h/l", "workspace"}) {
		items = append(items, m.chrome(fmt.Sprintf("%-10s %s", h.key, h.desc)))
	}
	modal := modals.NewFuzzy(modals.KindKeymap, "Keybindings (redesign preview)", filterItems(items), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}
