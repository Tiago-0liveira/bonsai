// Package prefs is the in-app preferences overlay: theme preset picker with
// live apply, a full keybinding editor, and behavioral defaults. It owns no
// persistence or styling side effects — every change is emitted as a SaveMsg
// snapshot for the parent to apply and store.
package prefs

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// SaveMsg carries the full preferences snapshot after any change.
type SaveMsg struct {
	Theme      string
	Sort       string
	PruneMerge bool
	// PRStatus is the worktree-list PR status display mode (full/compact/off).
	PRStatus string
	// Keys holds personal keybinding overrides (action -> key).
	Keys map[string]string
}

// CloseMsg is emitted when the user dismisses the preferences.
type CloseMsg struct{}

// JumpMsg asks the parent to open another surface (e.g. the aliases modal).
type JumpMsg struct{ Target string }

// Action is one rebindable action, supplied by the parent so this package
// stays decoupled from the key registry.
type Action struct {
	Name    string // config key, e.g. "new_worktree"
	Desc    string // help text, e.g. "new worktree"
	Default string // default key
	Section string // group name, e.g. "Git"
}

// SortModes are the worktree list orderings, in cycle order.
var SortModes = []string{"name", "ahead", "behind", "pr", "activity", "dirty"}

// PRStatusModes are the worktree-list PR status display modes, in cycle order.
var PRStatusModes = []string{"full", "compact", "off"}

// reservedKeys can never be bound: they drive navigation and quit semantics
// inside bonsai (and its modals).
var reservedKeys = map[string]bool{
	"esc": true, "ctrl+c": true, "enter": true, " ": true,
	"tab": true, "shift+tab": true,
	"up": true, "down": true, "left": true, "right": true,
}

type rowKind int

const (
	rowHeader rowKind = iota
	rowPreset
	rowSort
	rowPruneMerge
	rowPRStatus
	rowAliases
	rowKey
)

type row struct {
	kind   rowKind
	header string
	preset string
	action Action
}

// Model is the preferences overlay state.
type Model struct {
	presets    []string
	theme      string
	sort       string
	pruneMerge bool
	prStatus   string
	actions    []Action
	defaults   map[string]string // action -> default key
	overrides  map[string]string // action -> personal key (working copy)
	rows       []row
	collapsed  map[int]bool // header row index -> collapsed
	cursor     int
	capture    string // action awaiting a new key, "" when idle
	msg        string // transient status line
	width      int
	height     int
}

// New builds the overlay. actions must be ordered by section; a section header
// is inserted wherever the section changes. startOnKeys places the cursor on
// the first keybinding row (used when arriving from the keymap modal).
func New(themePreset, sort string, pruneMerge bool, prStatus string, presets []string, actions []Action, startOnKeys bool) Model {
	if sort == "" {
		sort = SortModes[0]
	}
	if prStatus == "" {
		prStatus = PRStatusModes[0]
	}
	m := Model{
		presets:    presets,
		theme:      themePreset,
		sort:       sort,
		pruneMerge: pruneMerge,
		prStatus:   prStatus,
		actions:    actions,
		defaults:   map[string]string{},
		overrides:  map[string]string{},
		collapsed:  map[int]bool{},
	}
	m.rows = append(m.rows, row{kind: rowHeader, header: "Appearance"})
	for _, p := range presets {
		m.rows = append(m.rows, row{kind: rowPreset, preset: p})
	}
	m.rows = append(m.rows,
		row{kind: rowHeader, header: "Defaults"},
		row{kind: rowSort},
		row{kind: rowPruneMerge},
		row{kind: rowPRStatus},
		row{kind: rowAliases},
		row{kind: rowHeader, header: "Keybindings"},
	)
	section := ""
	for _, a := range actions {
		m.defaults[a.Name] = a.Default
		if a.Section != section {
			section = a.Section
			m.rows = append(m.rows, row{kind: rowHeader, header: "  " + section})
		}
		m.rows = append(m.rows, row{kind: rowKey, action: a})
	}
	if startOnKeys {
		for i, r := range m.rows {
			if r.kind == rowKey {
				m.cursor = i
				break
			}
		}
	} else {
		// Fold the long Keybindings tree by default: collapse its top header and
		// every per-section sub-header so the overlay opens compact.
		for i, r := range m.rows {
			if r.kind == rowHeader && (r.header == "Keybindings" || m.headerLevel(i) == 1) {
				m.collapsed[i] = true
			}
		}
		m.cursor = 1 // first preset
	}
	return m
}

// headerLevel is 0 for top-level headers and 1 for indented sub-headers
// (the per-section keybinding groups, prefixed with "  ").
func (m Model) headerLevel(i int) int {
	if strings.HasPrefix(m.rows[i].header, "  ") {
		return 1
	}
	return 0
}

// visibleRows returns the indices of rows currently shown, honoring collapsed
// headers. A collapsed header hides every row (including sub-headers) until the
// next header at its level or shallower.
func (m Model) visibleRows() []int {
	type hdr struct {
		level     int
		collapsed bool
	}
	var stack []hdr
	hidden := func() bool {
		for _, h := range stack {
			if h.collapsed {
				return true
			}
		}
		return false
	}
	out := make([]int, 0, len(m.rows))
	for i := range m.rows {
		if m.rows[i].kind == rowHeader {
			lvl := m.headerLevel(i)
			for len(stack) > 0 && stack[len(stack)-1].level >= lvl {
				stack = stack[:len(stack)-1]
			}
			if !hidden() {
				out = append(out, i)
			}
			stack = append(stack, hdr{level: lvl, collapsed: m.collapsed[i]})
		} else if !hidden() {
			out = append(out, i)
		}
	}
	return out
}

// SetOverrides loads the personal key overrides in effect.
func (m *Model) SetOverrides(ov map[string]string) {
	for k, v := range ov {
		if v != "" {
			m.overrides[k] = v
		}
	}
}

// SetSize records the screen size for centering and window sizing.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m Model) effective(action string) string {
	if k, ok := m.overrides[action]; ok && k != "" {
		return k
	}
	return m.defaults[action]
}

// conflict reports the action that already holds candidate, or "".
func (m Model) conflict(action, candidate string) string {
	for _, a := range m.actions {
		if a.Name == action {
			continue
		}
		if m.effective(a.Name) == candidate {
			return a.Desc
		}
	}
	return ""
}

func (m Model) descOf(action string) string {
	for _, a := range m.actions {
		if a.Name == action {
			return a.Desc
		}
	}
	return action
}

func (m Model) save() tea.Cmd {
	keys := make(map[string]string, len(m.overrides))
	for k, v := range m.overrides {
		keys[k] = v
	}
	snap := SaveMsg{Theme: m.theme, Sort: m.sort, PruneMerge: m.pruneMerge, PRStatus: m.prStatus, Keys: keys}
	return func() tea.Msg { return snap }
}

// Update handles key input while the overlay is open.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.capture != "" {
		return m.captureKey(key)
	}
	switch key.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return CloseMsg{} }
	case "up", "ctrl+k":
		m.move(-1)
	case "down", "ctrl+j":
		m.move(1)
	case "left":
		return m.activate(-1)
	case "right":
		return m.activate(1)
	case "enter", " ":
		return m.activate(0)
	}
	return m, nil
}

// captureKey handles input while rebinding an action.
func (m Model) captureKey(key tea.KeyMsg) (Model, tea.Cmd) {
	action := m.capture
	desc := m.descOf(action)
	switch key.String() {
	case "esc", "ctrl+c":
		m.capture, m.msg = "", ""
		return m, nil
	case "backspace", "delete":
		m.capture = ""
		if _, ok := m.overrides[action]; ok {
			delete(m.overrides, action)
			m.msg = desc + " reset to default (" + m.defaults[action] + ")"
			return m, m.save()
		}
		m.msg = desc + " is already at its default (" + m.defaults[action] + ")"
		return m, nil
	}

	candidate := key.String()
	// Pressing the action's own default restores it, even for keys that are
	// normally reserved or shared by default (rebase vs restart_proc both use r).
	if candidate == m.defaults[action] {
		delete(m.overrides, action)
		m.capture = ""
		m.msg = desc + " → " + candidate + " (default)"
		return m, m.save()
	}
	if reservedKeys[candidate] {
		m.msg = candidate + " is reserved — pick another key"
		return m, nil
	}
	if other := m.conflict(action, candidate); other != "" {
		m.msg = candidate + " is already bound to “" + other + "”"
		return m, nil
	}

	m.overrides[action] = candidate
	m.capture = ""
	m.msg = desc + " → " + candidate
	return m, m.save()
}

func (m *Model) move(delta int) {
	vis := m.visibleRows()
	if len(vis) == 0 {
		return
	}
	pos := 0
	for idx, i := range vis {
		if i == m.cursor {
			pos = idx
			break
		}
	}
	pos = (pos + delta + len(vis)) % len(vis)
	m.cursor = vis[pos]
	m.msg = ""
}

// activate performs the action of the row under the cursor. dir gives a cycle
// direction for rows that support left/right (-1 / +1).
func (m Model) activate(dir int) (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	r := m.rows[m.cursor]
	switch r.kind {
	case rowHeader:
		// enter (dir 0) toggles; ← collapses, → expands.
		if dir == 0 {
			m.collapsed[m.cursor] = !m.collapsed[m.cursor]
		} else {
			m.collapsed[m.cursor] = dir < 0
		}
		m.msg = ""
		return m, nil

	case rowPreset:
		m.theme = r.preset
		m.msg = "theme: " + r.preset
		return m, m.save()

	case rowSort:
		d := dir
		if d == 0 {
			d = 1
		}
		idx := 0
		for i, s := range SortModes {
			if s == m.sort {
				idx = i
			}
		}
		m.sort = SortModes[(idx+d+len(SortModes))%len(SortModes)]
		m.msg = "default sort: " + m.sort
		return m, m.save()

	case rowPruneMerge:
		m.pruneMerge = !m.pruneMerge
		m.msg = ""
		return m, m.save()

	case rowPRStatus:
		d := dir
		if d == 0 {
			d = 1
		}
		idx := 0
		for i, s := range PRStatusModes {
			if s == m.prStatus {
				idx = i
			}
		}
		m.prStatus = PRStatusModes[(idx+d+len(PRStatusModes))%len(PRStatusModes)]
		m.msg = "PR status: " + m.prStatus
		return m, m.save()

	case rowAliases:
		return m, func() tea.Msg { return JumpMsg{Target: "aliases"} }

	case rowKey:
		m.capture = r.action.Name
		m.msg = ""
		return m, nil
	}
	return m, nil
}

var (
	boxStyle    lipgloss.Style
	titleStyle  lipgloss.Style
	headerStyle lipgloss.Style
	cursorStyle lipgloss.Style
	activeStyle lipgloss.Style
	dimStyle    lipgloss.Style
	onStyle     lipgloss.Style
	accentStyle lipgloss.Style
)

func init() { SetTheme(theme.Current) }

// SetTheme rebuilds overlay styles from a palette.
func SetTheme(p theme.Palette) {
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Accent).Padding(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	cursorStyle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	activeStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	dimStyle = lipgloss.NewStyle().Foreground(p.Dim)
	onStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(p.Accent)
}

const labelCol = 34 // left column width for key rows

// View renders the centered overlay.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Preferences") + "\n\n")
	b.WriteString(m.renderRows())
	b.WriteString("\n")

	if m.capture != "" {
		b.WriteString(cursorStyle.Render(fmt.Sprintf("press a new key for “%s” · esc cancel · ⌫ reset", m.descOf(m.capture))))
	} else if m.msg != "" {
		b.WriteString(dimStyle.Render(m.msg))
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ move · ←/→ adjust · enter select/fold · esc close"))

	box := boxStyle.Render(b.String())
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// windowLines is how many rows the list shows, sized from the screen.
func (m Model) windowLines() int {
	n := m.height*3/4 - 7
	if n < 8 {
		n = 8
	}
	if n > 30 {
		n = 30
	}
	if n > len(m.rows) {
		n = len(m.rows)
	}
	return n
}

func (m Model) renderRows() string {
	vis := m.visibleRows()
	n := m.windowLines()
	if n > len(vis) {
		n = len(vis)
	}
	cpos := 0
	for idx, i := range vis {
		if i == m.cursor {
			cpos = idx
			break
		}
	}
	start := 0
	if cpos >= n {
		start = cpos - n + 1
	}
	if start+n > len(vis) {
		start = len(vis) - n
	}
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for k := start; k < start+n && k < len(vis); k++ {
		b.WriteString(m.renderRow(vis[k]))
		if k < start+n-1 && k < len(vis)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderRow(i int) string {
	r := m.rows[i]
	sel := i == m.cursor
	cursor := "  "
	if sel {
		cursor = cursorStyle.Render("› ")
	}

	switch r.kind {
	case rowHeader:
		arrow := "▾ "
		if m.collapsed[i] {
			arrow = "▸ "
		}
		indent := ""
		if m.headerLevel(i) == 1 {
			indent = "  "
		}
		name := strings.TrimLeft(r.header, " ")
		if sel {
			return cursorStyle.Render("› ") + indent + cursorStyle.Render(arrow+name)
		}
		return "  " + indent + headerStyle.Render(arrow+name)

	case rowPreset:
		mark := dimStyle.Render("○")
		name := r.preset
		if r.preset == m.theme {
			mark = activeStyle.Render("●")
			name = activeStyle.Render(r.preset + "  (active)")
		} else if sel {
			name = cursorStyle.Render(r.preset)
		}
		return cursor + mark + " " + name

	case rowSort:
		label := "default sort order"
		if sel {
			label = cursorStyle.Render(label)
		}
		return cursor + pad(label, labelCol) + accentStyle.Render("◂ "+m.sort+" ▸")

	case rowPruneMerge:
		label := "merge PR by default when pruning"
		if sel {
			label = cursorStyle.Render(label)
		}
		val := dimStyle.Render("off")
		if m.pruneMerge {
			val = onStyle.Render("on")
		}
		return cursor + pad(label, labelCol) + val

	case rowPRStatus:
		label := "PR status in worktree list"
		if sel {
			label = cursorStyle.Render(label)
		}
		return cursor + pad(label, labelCol) + accentStyle.Render("◂ "+m.prStatus+" ▸")

	case rowAliases:
		label := "manage aliases…"
		if sel {
			label = cursorStyle.Render(label)
		}
		return cursor + label + dimStyle.Render("  (opens the aliases menu)")

	case rowKey:
		label := r.action.Desc
		if sel {
			label = cursorStyle.Render(label)
		}
		k := m.effective(r.action.Name)
		_, custom := m.overrides[r.action.Name]
		var keyCol string
		if custom {
			keyCol = accentStyle.Render(pad(k, 10)) + dimStyle.Render("custom · ⌫ resets")
		} else {
			keyCol = pad(k, 10) + dimStyle.Render("default")
		}
		return cursor + pad(label, labelCol) + keyCol
	}
	return ""
}

// pad pads s with spaces to width w (ANSI-aware via lipgloss.Width).
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
