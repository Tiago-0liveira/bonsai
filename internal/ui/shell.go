package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
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

// key is the lowercase name persisted as a startup or remembered workspace.
func (w workspace) key() string { return strings.ToLower(workspaceInfo[w].name) }

// workspaceByKey resolves a persisted name; unknown or empty names report false.
func workspaceByKey(k string) (workspace, bool) {
	for i := workspace(0); i < workspaceCount; i++ {
		if i.key() == k {
			return i, true
		}
	}
	return 0, false
}

// startupWorkspace picks where the shell opens: the explicit startup choice,
// else the one remembered for this repository, else Garden. Stored names that
// no longer exist fall through to the next rule.
func (m Model) startupWorkspace() workspace {
	if ws, ok := workspaceByKey(m.work.StartWorkspace); ok {
		return ws
	}
	if !m.work.NoWorkspaceMemory {
		if ws, ok := workspaceByKey(m.state.LastWorkspace(m.repoDir)); ok {
			return ws
		}
	}
	return wsGarden
}

// workspaceSavedMsg reports a finished last-workspace write.
type workspaceSavedMsg struct {
	gen uint64
	err error
}

// selectWorkspace switches the tab, hands the keyboard back to the shell's own
// navigation, and persists the choice through a command when remembering is on.
func (m Model) selectWorkspace(ws workspace) (Model, tea.Cmd) {
	if ws == m.workspace {
		return m, nil
	}
	if m.workspace == wsGarden {
		// A read that lands after the user left must not reopen a step for them.
		m.garden.abandon()
	}
	m.workspace = ws
	m.shellFocus = shellNav
	m.set.editing, m.set.capturing = false, false
	var cmds []tea.Cmd
	if ws == wsAccounts && m.acct.present && !m.acct.started {
		m.acct.started = true
		cmds = append(cmds, m.acct.model.Init())
	}
	if ws == wsGarden {
		// The one network read of the Garden workspace: entering it, never a key.
		var load tea.Cmd
		m, load = m.gardenLoadStatus()
		cmds = append(cmds, load)
	}
	if !m.work.NoWorkspaceMemory {
		m.wsGen++
		gen, st, dir, name := m.wsGen, m.state, m.repoDir, ws.key()
		cmds = append(cmds, func() tea.Msg {
			return workspaceSavedMsg{gen: gen, err: st.PersistLastWorkspace(gen, dir, name)}
		})
	}
	switch len(cmds) {
	case 0:
		return m, nil
	case 1:
		return m, cmds[0]
	}
	return m, tea.Batch(cmds...)
}

// shellFocus says who owns keyboard input in the redesigned shell. In
// shellNav the shell's tab keys apply. In shellBody the workspace body gets
// each key first, but the quit, palette and help bindings still work because
// they are not text input. In shellInput (a text field, later key capture) the
// body owns every key except ctrl+c, so the shell can never steal keystrokes.
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
	// A text field, dropdown or confirmation inside Accounts takes every key,
	// so typing a name can never quit or jump workspaces.
	if m.workspace == wsAccounts && m.shellFocus == shellBody && m.acct.present && m.acct.model.Capturing() {
		return shellInput
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
		m.gardenBoardVisible()
		m.gardenClampDocument()
		if m.modal != nil {
			m.modal.SetSize(msg.Width, msg.Height)
		}
		return m, nil
	case modals.CancelMsg:
		m.modal = nil
		m.set.pending = nil
		return m, nil
	case modals.SubmitMsg:
		kind := msg.Kind
		m.modal = nil
		switch kind {
		case modals.KindPalette:
			if t, ok := paletteTargetForLabel(msg.Value); ok {
				return m.goToPalette(t)
			}
		case modals.KindConfigConfirm:
			return m.confirmRepoWrite()
		}
		return m, nil
	case settingsSavedMsg:
		return m.onSettingsSaved(msg), nil
	case workspaceSavedMsg:
		if msg.err != nil && !errors.Is(msg.err, config.ErrSuperseded) && msg.gen == m.wsGen {
			m.status = "Could not remember workspace: " + msg.err.Error()
		}
		return m, nil
	case configSavedMsg:
		return m.onRepoConfigSaved(msg), nil
	case tea.KeyMsg:
		if m.modal != nil {
			nm, cmd := m.modal.Update(msg)
			m.modal = &nm
			return m, cmd
		}
		return m.shellKey(msg)
	}
	if handled, nm, cmd := m.gardenMsg(msg); handled {
		return nm, cmd
	}
	_, nm, cmd := m.accountsMsg(msg)
	return nm, cmd
}

func (m Model) shellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c quits, except that an open modal owns it first (it cancels the
	// modal; a second ctrl+c then quits). Every other key yields to a body or
	// input that owns the keyboard, including the letters bound to quit, help
	// and the palette.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	focus := m.currentShellFocus()
	if focus == shellDialog {
		return m, nil
	}
	if focus == shellInput {
		return m.bodyKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Palette):
		return m.openShellPalette(), nil
	case key.Matches(msg, m.keys.Help):
		return m.openShellHelp(), nil
	case key.Matches(msg, m.keys.Prefs):
		return m.selectWorkspace(wsSettings)
	}
	if focus == shellBody {
		return m.bodyKey(msg)
	}
	next := m.workspace
	switch {
	case m.workspace == wsGarden && (msg.String() == "enter" || msg.String() == "down"):
		return m.gardenEnterBody()
	case m.workspace == wsSettings && (msg.String() == "enter" || msg.String() == "down"):
		m.shellFocus = shellBody
		m.set.level = levelCategory
		return m, nil
	case m.workspace == wsAccounts && (msg.String() == "enter" || msg.String() == "down"):
		m.shellFocus = shellBody
		return m, nil
	case key.Matches(msg, m.keys.AgentAccounts):
		// The Agents tab's profiles shortcut lands on the same workspace.
		return m.openAccounts(accounts.TabOverview)
	case key.Matches(msg, m.keys.Tab), msg.String() == "right", msg.String() == "l":
		next = (m.workspace + 1) % workspaceCount
	case key.Matches(msg, m.keys.ShiftTab), msg.String() == "left", msg.String() == "h":
		next = (m.workspace + workspaceCount - 1) % workspaceCount
	default:
		next = altDigitWorkspace(msg, next)
	}
	return m.selectWorkspace(next)
}

// altDigitWorkspace maps alt+digit to a workspace; bare digits belong to agent
// sessions, so they never select.
func altDigitWorkspace(msg tea.KeyMsg, fallback workspace) workspace {
	for i := workspace(0); i < workspaceCount; i++ {
		if msg.String() == fmt.Sprintf("alt+%d", i+1) {
			return i
		}
	}
	return fallback
}

// bodyKey routes a key to the focused workspace body. Settings, Accounts and
// Garden have one.
func (m Model) bodyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspace != wsSettings && m.workspace != wsAccounts && m.workspace != wsGarden {
		return m, nil
	}
	if m.shellFocus == shellBody && m.currentShellFocus() == shellBody {
		if ws := altDigitWorkspace(msg, m.workspace); ws != m.workspace {
			return m.selectWorkspace(ws)
		}
	}
	switch m.workspace {
	case wsAccounts:
		return m.accountsKey(msg)
	case wsGarden:
		return m.gardenKey(msg)
	}
	nm, cmd := m.settingsKey(msg)
	// Moving onto the Garden category is when Settings first needs its status.
	wm, load := nm.(Model).gardenWarm()
	return wm, tea.Batch(cmd, load)
}

const (
	paletteGoPrefix       = "Go to "
	paletteSettingsPrefix = "Settings: "
	paletteAccountsPrefix = "Accounts: "
	paletteEditKeys       = paletteSettingsPrefix + "Edit key bindings"
)

// paletteTarget is where a palette entry leads: a workspace and, for Settings
// entries, one of its categories.
type paletteTarget struct {
	ws          workspace
	category    int
	hasCategory bool
	// section opens an Accounts section instead of the workspace as it was left.
	section      accounts.Tab
	hasSection   bool
	gardenAction string
}

// paletteTargets lists every entry in palette order with its target.
func paletteTargets() (labels []string, targets map[string]paletteTarget) {
	targets = map[string]paletteTarget{}
	add := func(label string, t paletteTarget) {
		labels = append(labels, label)
		targets[label] = t
	}
	for i := workspace(0); i < workspaceCount; i++ {
		add(paletteGoPrefix+i.name(), paletteTarget{ws: i})
	}
	keys := 0
	for i, c := range settingCategories {
		add(paletteSettingsPrefix+c.name, paletteTarget{ws: wsSettings, category: i, hasCategory: true})
		if c.id == "keys" {
			keys = i
		}
	}
	add(paletteEditKeys, paletteTarget{ws: wsSettings, category: keys, hasCategory: true})
	add(paletteAccountsPrefix+"Usage", paletteTarget{ws: wsAccounts, section: accounts.TabUsage, hasSection: true})
	add(paletteAccountsPrefix+"Defaults", paletteTarget{ws: wsAccounts, section: accounts.TabDefaults, hasSection: true})
	add("Garden: Board", paletteTarget{ws: wsGarden, gardenAction: "board"})
	add("Garden: Table", paletteTarget{ws: wsGarden, gardenAction: "table"})
	add("Garden: Search approved work", paletteTarget{ws: wsGarden, gardenAction: "search"})
	add("Garden: Refresh", paletteTarget{ws: wsGarden, gardenAction: "refresh"})
	return labels, targets
}

func paletteTargetForLabel(label string) (paletteTarget, bool) {
	_, targets := paletteTargets()
	t, ok := targets[label]
	return t, ok
}

// goToPalette selects a palette target. Going through selectWorkspace keeps the
// last-workspace memory; a Settings category entry lands on its category row.
func (m Model) goToPalette(t paletteTarget) (Model, tea.Cmd) {
	if t.hasSection {
		return m.openAccounts(t.section)
	}
	m, cmd := m.selectWorkspace(t.ws)
	if t.gardenAction != "" {
		m.shellFocus = shellBody
		if m.garden.hasStatus && m.garden.status.Caps.Read {
			m.garden.sub = subBoard
			if b := &m.garden.board; b.opened != 0 && (t.gardenAction == "board" || t.gardenAction == "table") {
				b.opened, b.docScroll = 0, 0 // the detail belongs to the old layout's parent
			}
			switch t.gardenAction {
			case "board":
				m.garden.board.setLayout(layoutBoard)
			case "table":
				m.garden.board.setLayout(layoutTable)
			}
			if t.gardenAction == "board" || t.gardenAction == "table" {
				m.gardenBoardVisible()
				remember := m.gardenRememberView()
				var read tea.Cmd
				if !m.garden.board.loaded {
					m, read = m.gardenLoadWork()
				}
				return m, tea.Batch(cmd, remember, read)
			}
			if t.gardenAction == "search" {
				var read tea.Cmd
				if !m.garden.board.loaded {
					m, read = m.gardenLoadWork()
				}
				nm, extra := m.gardenStartSearch()
				return nm, tea.Batch(cmd, read, extra)
			}
			if t.gardenAction == "refresh" || !m.garden.board.loaded {
				return m.gardenLoadWork()
			}
		} else {
			m.garden.notice = "Connect a readable Garden before opening approved work."
			if t.gardenAction == "refresh" {
				return m.gardenLoadStatus()
			}
		}
		return m, cmd
	}
	if t.hasCategory {
		m.shellFocus = shellBody
		m.set = settingsState{cat: t.category, level: levelCategory, saveErr: m.set.saveErr}
		m, load := m.gardenWarm()
		return m, tea.Batch(cmd, load)
	}
	return m, cmd
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
	items, _ := paletteTargets()
	modal := modals.NewFuzzy(modals.KindPalette, "Go to", filterItems(items), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// shellHints are the navigation hints in the order the footer drops them
// when the width runs out, most important first. Keys are the effective ones,
// so scoped overrides show up.
func (m Model) shellHints() []shellHint {
	if m.currentShellFocus() == shellInput && m.workspace == wsAccounts {
		return m.accountsHints()
	}
	if m.currentShellFocus() == shellInput && m.workspace == wsGarden {
		if m.gardenScreen() == scrDrafts {
			return m.gardenDraftsHints()
		}
		return []shellHint{{"enter", "apply search"}, {"esc", "cancel"}}
	}
	if m.currentShellFocus() == shellInput {
		if m.set.capturing {
			return []shellHint{{"new key", "bind"}, {"esc", "cancel"}, {"backspace", "reset"}}
		}
		return []shellHint{{"enter", "save"}, {"esc", "cancel"}}
	}
	h := []shellHint{}
	if m.currentShellFocus() == shellNav {
		h = append(h, shellHint{"←/→", "workspace"})
		switch m.workspace {
		case wsSettings:
			h = append(h, shellHint{"enter/↓", "open settings"})
		case wsAccounts:
			h = append(h, shellHint{"enter/↓", "open accounts"})
		}
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsSettings {
		h = append(h, m.settingsHints()...)
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsAccounts {
		h = append(h, m.accountsHints()...)
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsGarden {
		h = append(h, m.gardenHints()...)
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
	extra := []shellHint{{"h/l", "workspace"}, {m.keys.AgentAccounts.Help().Key, "open Accounts"}}
	for _, h := range append(m.shellHints(), extra...) {
		items = append(items, m.chrome(fmt.Sprintf("%-10s %s", h.key, h.desc)))
	}
	modal := modals.NewFuzzy(modals.KindKeymap, "Keybindings (redesign preview)", filterItems(items), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}
