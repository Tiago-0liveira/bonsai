// Package modals provides a single reusable centered overlay that operates in
// one of several modes: free text input, static selection, live fuzzy search, or
// yes/no confirmation. It stays decoupled from the core layer: dynamic filtering
// is supplied by the parent as a callback.
package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Kind identifies which feature opened the modal so the parent can route the
// submitted value.
type Kind string

const (
	KindNone        Kind = ""
	KindCopyFile    Kind = "copy_file"
	KindScripts     Kind = "scripts"
	KindRunCommand  Kind = "run_command" // free-text ad-hoc command from the scripts modal
	KindAliases     Kind = "aliases"
	KindPalette     Kind = "palette" // command palette (fuzzy over every action)
	KindYank        Kind = "yank"    // pick what to copy to the clipboard
	KindBranches    Kind = "branches"
	KindCommit      Kind = "commit"
	KindPrune       Kind = "prune"
	KindNewAlias    Kind = "new_alias"     // enter new alias name
	KindNewAliasCmd Kind = "new_alias_cmd" // enter new alias command
	KindAgentTask     Kind = "agent_task"      // enter AI agent task prompt
	KindAgentAutoTask Kind = "agent_auto_task" // enter AI agent task prompt with auto branch & worktree

	// PR actions.
	KindMergeStrategy Kind = "merge_strategy" // pick merge/squash/rebase
	KindReviewApprove Kind = "review_approve" // optional body for approve
	KindReviewChanges Kind = "review_changes" // body for request-changes
	KindReviewComment Kind = "review_comment" // body for review comment
	KindPRTitle       Kind = "pr_title"       // new PR title
	KindPRBody        Kind = "pr_body"        // new PR body
	KindUpdateBase    Kind = "update_base"    // pick rebase/merge for update-from-base

	// Generic confirmation for destructive ops.
	KindConfirm   Kind = "confirm"
	KindBulkPrune Kind = "bulk_prune"

	// Reference overlays.
	KindKeymap Kind = "keymap" // searchable list of every keybinding
	KindLegend Kind = "legend" // status-glyph legend

	// Worktree creation flow.
	KindCreateSource   Kind = "create_source"   // pick new/existing/PR
	KindCreateNew      Kind = "create_new"      // enter new branch name
	KindCreateExisting Kind = "create_existing" // pick existing branch
	KindCreatePR       Kind = "create_pr"       // pick pull request

	// Process viewer.
	KindProcesses Kind = "processes"
	// Quit keep/kill multi-select.
	KindQuit Kind = "quit"
	// Restart-policy picker (no/on-failure/always).
	KindSetPolicy Kind = "set_policy"
	// Multi-select of processes to show together in the merged log.
	KindMultiView Kind = "multi_view"
	// Free-text tag for a process's multi-view label.
	KindRenameProc Kind = "rename_proc"

	// Config editors (.bonsai.yaml).
	KindConfigChoice  Kind = "config_choice"  // sub-picker: map entry or list item
	KindConfigValue   Kind = "config_value"   // final value for a config setting
	KindConfigConfirm Kind = "config_confirm" // confirm removal of a config entry

	KindPrefEditor Kind = "pref_editor" // personal "open editor" override (state.json)

	// Scrollable read-only diff of a single file.
	KindDiffFile Kind = "diff_file"
)

// mode is the interaction style of the modal.
type mode int

const (
	modeInput mode = iota
	modeSelect
	modeFuzzy
	modeConfirm
	modePrune
	modeScroll
	modeMultiSelect
)

// SubmitMsg is emitted when the user confirms a choice.
type SubmitMsg struct {
	Kind  Kind
	Value string
}

// CancelMsg is emitted when the user dismisses the modal.
type CancelMsg struct{ Kind Kind }

// MultiSubmitMsg is emitted when a multi-select modal is confirmed. Selected
// holds the indices (into the items passed to NewMultiSelect) that are checked.
type MultiSubmitMsg struct {
	Kind     Kind
	Selected []int
}

// FilterFunc maps a query to an ordered candidate list.
type FilterFunc func(query string) []string

// Section is a named group of items in a sectioned fuzzy modal. Items are
// already filtered to the current query by the parent-supplied SectionFilterFunc.
type Section struct {
	Name  string
	Items []string
}

// SectionFilterFunc maps a query to an ordered list of sections, each holding
// only the items that match the query.
type SectionFilterFunc func(query string) []Section

// visRow is one rendered line in a sectioned modal: either a section header or
// an item within a section. It is recomputed from the sections + collapse state.
type visRow struct {
	header  bool
	section int
	item    int
}

// Model is the overlay state.
type Model struct {
	kind   Kind
	mode   mode
	title  string
	body   string // optional read-only text shown above an input (e.g. commit preview)
	input  textinput.Model
	items  []string
	cursor int
	filter FilterFunc
	width  int
	height int

	// Sectioned fuzzy fields.
	sectioned     bool
	sections      []Section
	sectionFilter SectionFilterFunc
	expanded      map[string]bool // section name -> manually expanded (default collapsed)

	// Prune-mode fields.
	canMerge   bool
	mergeOn    bool
	forceOn    bool
	mergeLabel string
	tailSteps  []string

	// Scroll-mode fields.
	vp      viewport.Model
	content string

	// Multi-select fields.
	selected []bool
}

// NewInput builds a free-text modal (e.g. commit message).
func NewInput(kind Kind, title, placeholder string) Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	return Model{kind: kind, mode: modeInput, title: title, input: ti}
}

// NewSelect builds a static selection modal.
func NewSelect(kind Kind, title string, items []string) Model {
	return Model{kind: kind, mode: modeSelect, title: title, items: items}
}

// NewFuzzy builds a live fuzzy-search modal. filter is called on every keystroke
// and its result becomes the visible list; initial seeds the first render.
func NewFuzzy(kind Kind, title string, filter FilterFunc, initial []string) Model {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Focus()
	return Model{kind: kind, mode: modeFuzzy, title: title, input: ti, items: initial, filter: filter}
}

// NewSectionedFuzzy builds a live fuzzy-search modal whose results are grouped
// into collapsible sections. It opens collapsed (headers only); typing a query
// auto-expands every section with a match and hides the rest. filter is called on
// every keystroke; initial seeds the first render.
func NewSectionedFuzzy(kind Kind, title string, filter SectionFilterFunc, initial []Section) Model {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Focus()
	return Model{
		kind:          kind,
		mode:          modeFuzzy,
		title:         title,
		input:         ti,
		sectioned:     true,
		sections:      initial,
		sectionFilter: filter,
		expanded:      map[string]bool{},
	}
}

// NewMultiSelect builds a checkbox list modal. preselected sets the initial
// checked state per item (a shorter/nil slice defaults the rest to unchecked).
// Space toggles the row under the cursor; enter emits MultiSubmitMsg.
func NewMultiSelect(kind Kind, title string, items []string, preselected []bool) Model {
	sel := make([]bool, len(items))
	copy(sel, preselected)
	return Model{kind: kind, mode: modeMultiSelect, title: title, items: items, selected: sel}
}

// NewScroll builds a read-only scrollable modal (e.g. a single file's diff).
func NewScroll(kind Kind, title, content string) Model {
	return Model{kind: kind, mode: modeScroll, title: title, content: content}
}

// NewConfirm builds a yes/no modal.
func NewConfirm(kind Kind, title string) Model {
	return Model{kind: kind, mode: modeConfirm, title: title, items: []string{"Yes", "No"}, cursor: 1}
}

// NewPrune builds the prune-confirmation modal. It shows the ordered pipeline as
// an arrow flow. When canMerge is true a leading "merge" step can be toggled on
// (default off); mergeLabel names it and tailSteps are the always-run steps.
func NewPrune(kind Kind, title, mergeLabel string, tailSteps []string, canMerge bool) Model {
	return Model{
		kind:       kind,
		mode:       modePrune,
		title:      title,
		canMerge:   canMerge,
		mergeLabel: mergeLabel,
		tailSteps:  tailSteps,
	}
}

// MergeEnabled reports whether the merge step is toggled on.
func (m Model) MergeEnabled() bool { return m.mergeOn }

// SetMergeDefault sets the initial state of the merge toggle (the prune-merge
// preference). It has no effect when the modal offers no merge step.
func (m *Model) SetMergeDefault(on bool) { m.mergeOn = on && m.canMerge }

// ForceEnabled reports whether the force (discard uncommitted changes) option
// is toggled on.
func (m Model) ForceEnabled() bool { return m.forceOn }

// Kind returns the modal kind.
func (m Model) Kind() Kind { return m.kind }

// SetSize records the available screen size for centering. In scroll mode it also
// sizes the inner viewport to most of the screen and (re)wraps the content.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	if m.mode == modeScroll {
		vw := w * 3 / 4
		if vw < 20 {
			vw = w - 8
		}
		vh := h * 3 / 4
		if vh < 5 {
			vh = h - 8
		}
		if vw < 1 {
			vw = 1
		}
		if vh < 1 {
			vh = 1
		}
		m.vp = viewport.New(vw, vh)
		m.setScrollContent()
	}
}

// SetScrollContent replaces the scroll modal's body (e.g. once an async diff
// load completes).
func (m *Model) SetScrollContent(s string) {
	m.content = s
	m.setScrollContent()
}

func (m *Model) setScrollContent() {
	s := m.content
	if m.vp.Width > 0 {
		s = lipgloss.NewStyle().Width(m.vp.Width).Render(s)
	}
	m.vp.SetContent(s)
}

// SetBody sets read-only text rendered above the input field (input mode only).
func (m *Model) SetBody(s string) { m.body = s }

// SetInitial prefills the input field and moves the cursor to the end.
func (m *Model) SetInitial(v string) {
	m.input.SetValue(v)
	m.input.CursorEnd()
}

func (m Model) submit(value string) tea.Cmd {
	k := m.kind
	return func() tea.Msg { return SubmitMsg{Kind: k, Value: value} }
}

func (m Model) cancel() tea.Cmd {
	k := m.kind
	return func() tea.Msg { return CancelMsg{Kind: k} }
}

// Update handles key input and returns the (possibly emitting) modal.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// Scroll mode: esc/q close, everything else drives the viewport.
	if m.mode == modeScroll {
		switch key.String() {
		case "esc", "ctrl+c", "q":
			return m, m.cancel()
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	switch key.String() {
	case "esc", "ctrl+c":
		return m, m.cancel()

	case "up", "ctrl+k":
		if m.mode != modeInput {
			m.moveCursor(-1)
		}
		return m, nil

	case "down", "ctrl+j":
		if m.mode != modeInput {
			m.moveCursor(1)
		}
		return m, nil

	case "left", "right":
		// In a sectioned modal, arrows collapse/expand the header under the
		// cursor; on an item they fall through to the text field.
		if m.sectioned {
			rows := m.visibleRows()
			if m.cursor >= 0 && m.cursor < len(rows) && rows[m.cursor].header {
				m.expanded[m.sections[rows[m.cursor].section].Name] = key.String() == "right"
				return m, nil
			}
		}

	case " ", "m":
		// Toggle merge only in prune mode; toggle the checkbox in multi-select
		// mode; in text modes these are literal keys and fall through to the input.
		if m.mode == modePrune {
			if m.canMerge {
				m.mergeOn = !m.mergeOn
			}
			return m, nil
		}
		if m.mode == modeMultiSelect {
			if m.cursor >= 0 && m.cursor < len(m.selected) {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
			return m, nil
		}

	case "f":
		// Toggle force only in prune mode.
		if m.mode == modePrune {
			m.forceOn = !m.forceOn
			return m, nil
		}

	case "enter":
		if m.sectioned {
			return m.onSectionedEnter()
		}
		if m.mode == modeMultiSelect {
			return m, m.onMultiSubmit()
		}
		return m, m.onEnter()
	}

	// Text-entry modes consume the remaining keys.
	if m.mode == modeInput || m.mode == modeFuzzy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.mode == modeFuzzy {
			if m.sectioned && m.sectionFilter != nil {
				m.sections = m.sectionFilter(m.input.Value())
				m.cursor = m.firstItemRow()
			} else if m.filter != nil {
				m.items = m.filter(m.input.Value())
				m.cursor = 0
			}
		}
		return m, cmd
	}
	return m, nil
}

// visibleRows flattens the sections into the currently visible lines: every
// section header, plus the items of expanded sections. A non-empty query
// force-expands sections that have matches and hides sections that have none.
func (m Model) visibleRows() []visRow {
	hasQuery := strings.TrimSpace(m.input.Value()) != ""
	var rows []visRow
	for si, s := range m.sections {
		if hasQuery && len(s.Items) == 0 {
			continue
		}
		rows = append(rows, visRow{header: true, section: si})
		if hasQuery || m.expanded[s.Name] {
			for ii := range s.Items {
				rows = append(rows, visRow{section: si, item: ii})
			}
		}
	}
	return rows
}

// firstItemRow returns the index of the first item row, or 0 (the first header)
// when nothing is expanded.
func (m Model) firstItemRow() int {
	for i, r := range m.visibleRows() {
		if !r.header {
			return i
		}
	}
	return 0
}

// onSectionedEnter toggles the header under the cursor or submits the item.
func (m Model) onSectionedEnter() (Model, tea.Cmd) {
	rows := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return m, nil
	}
	r := rows[m.cursor]
	if r.header {
		name := m.sections[r.section].Name
		m.expanded[name] = !m.expanded[name]
		return m, nil
	}
	return m, m.submit(m.sections[r.section].Items[r.item])
}

// onMultiSubmit emits the checked indices.
func (m Model) onMultiSubmit() tea.Cmd {
	var sel []int
	for i, on := range m.selected {
		if on {
			sel = append(sel, i)
		}
	}
	k := m.kind
	return func() tea.Msg { return MultiSubmitMsg{Kind: k, Selected: sel} }
}

func (m *Model) moveCursor(delta int) {
	n := len(m.items)
	if m.sectioned {
		n = len(m.visibleRows())
	}
	if n == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
}

func (m Model) onEnter() tea.Cmd {
	switch m.mode {
	case modeInput:
		return m.submit(m.input.Value())
	case modeConfirm:
		if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor] == "Yes" {
			return m.submit("yes")
		}
		return m.cancel()
	case modePrune:
		switch {
		case m.mergeOn && m.forceOn:
			return m.submit("merge,force")
		case m.mergeOn:
			return m.submit("merge")
		case m.forceOn:
			return m.submit("force")
		default:
			return m.submit("confirm")
		}
	default: // select, fuzzy
		if m.cursor >= 0 && m.cursor < len(m.items) {
			return m.submit(m.items[m.cursor])
		}
		return m.cancel()
	}
}

var (
	boxStyle      lipgloss.Style
	titleStyle    lipgloss.Style
	cursorStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	dimStyle      lipgloss.Style
	arrowStyle    lipgloss.Style
	onStyle       lipgloss.Style
	offStyle      lipgloss.Style
	dangerStyle   lipgloss.Style
)

func init() { SetTheme(theme.Current) }

// SetTheme rebuilds modal styles from a palette.
func SetTheme(p theme.Palette) {
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Accent).Padding(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	cursorStyle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(p.Accent)
	dimStyle = lipgloss.NewStyle().Foreground(p.Dim)
	arrowStyle = lipgloss.NewStyle().Foreground(p.Accent)
	onStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	offStyle = lipgloss.NewStyle().Foreground(p.Dim)
	dangerStyle = lipgloss.NewStyle().Foreground(p.Danger)
}

// View renders the centered overlay.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	switch m.mode {
	case modeScroll:
		b.WriteString(m.vp.View())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("↑/↓ scroll · esc close"))
	case modePrune:
		b.WriteString(m.renderPrune())
	case modeMultiSelect:
		b.WriteString(m.renderMultiSelect())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("↑/↓ move · space toggle · enter confirm · esc cancel"))
	case modeInput:
		if m.body != "" {
			b.WriteString(m.body) // may carry its own ANSI color (git status)
			b.WriteString("\n\n")
		}
		b.WriteString(m.input.View())
		b.WriteString("\n")
	case modeFuzzy:
		b.WriteString(m.input.View())
		b.WriteString("\n")
		if m.sectioned {
			b.WriteString(m.renderSections())
			b.WriteString("\n")
			b.WriteString(dimStyle.Render("↑/↓ move · →/← expand/collapse · enter select · esc cancel"))
		} else {
			b.WriteString(m.renderList())
			b.WriteString("\n")
			b.WriteString(dimStyle.Render("↑/↓ move · enter select · esc cancel"))
		}
	default:
		if m.body != "" {
			b.WriteString(m.body)
			b.WriteString("\n\n")
		}
		b.WriteString(m.renderList())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("↑/↓ move · enter select · esc cancel"))
	}

	box := boxStyle.Render(b.String())
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderPrune draws the merge/force toggles and the ordered pipeline as an
// arrow flow.
func (m Model) renderPrune() string {
	var b strings.Builder

	if m.canMerge {
		box, label := "[ ]", offStyle.Render(m.mergeLabel)
		if m.mergeOn {
			box, label = onStyle.Render("[x]"), onStyle.Render(m.mergeLabel)
		}
		fmt.Fprintf(&b, "%s %s\n\n", box, label)
	}

	forceBox := "[ ]"
	if m.forceOn {
		forceBox = "[x]"
	}
	fmt.Fprintf(&b, "%s %s\n\n", forceBox, dangerStyle.Render("force (discard uncommitted changes)"))

	arrow := arrowStyle.Render(" → ")
	steps := make([]string, 0, len(m.tailSteps)+1)
	if m.mergeOn && m.canMerge {
		steps = append(steps, onStyle.Render(m.mergeLabel))
	}
	steps = append(steps, m.tailSteps...)
	b.WriteString(strings.Join(steps, arrow))
	b.WriteString("\n\n")

	hint := "f toggle force · enter confirm · esc cancel"
	if m.canMerge {
		hint = "space toggle merge · " + hint
	}
	b.WriteString(dangerStyle.Render("This is destructive.") + " " + dimStyle.Render(hint))
	return b.String()
}

// renderSections draws the collapsible section list with a scroll window around
// the cursor, mirroring renderList's windowing.
func (m Model) renderSections() string {
	rows := m.visibleRows()
	if len(rows) == 0 {
		return dimStyle.Render("(no matches)")
	}
	const maxRows = 14
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(rows) {
		end = len(rows)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		r := rows[i]
		sel := i == m.cursor
		if r.header {
			s := m.sections[r.section]
			arrow := "▸"
			if strings.TrimSpace(m.input.Value()) != "" || m.expanded[s.Name] {
				arrow = "▾"
			}
			line := fmt.Sprintf("%s %s (%d)", arrow, s.Name, len(s.Items))
			if sel {
				b.WriteString(cursorStyle.Render("› " + line))
			} else {
				b.WriteString("  " + titleStyle.Render(line))
			}
		} else {
			item := m.sections[r.section].Items[r.item]
			if sel {
				b.WriteString(cursorStyle.Render("  › "))
				b.WriteString(selectedStyle.Render(item))
			} else {
				b.WriteString("    " + item)
			}
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderMultiSelect draws the checkbox list with the cursor.
func (m Model) renderMultiSelect() string {
	if len(m.items) == 0 {
		return dimStyle.Render("(none)")
	}
	var b strings.Builder
	for i, item := range m.items {
		box := "[ ]"
		if i < len(m.selected) && m.selected[i] {
			box = onStyle.Render("[x]")
		}
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("› ") + box + " " + selectedStyle.Render(item))
		} else {
			b.WriteString("  " + box + " " + item)
		}
		if i < len(m.items)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderList() string {
	if len(m.items) == 0 {
		return dimStyle.Render("(no matches)")
	}
	const maxRows = 12
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.items) {
		end = len(m.items)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("› "))
			b.WriteString(selectedStyle.Render(m.items[i]))
		} else {
			b.WriteString("  ")
			b.WriteString(m.items[i])
		}
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
