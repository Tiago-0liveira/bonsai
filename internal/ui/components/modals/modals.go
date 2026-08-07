// Package modals provides a single reusable centered overlay that operates in
// one of several modes: free text input, static selection, live fuzzy search, or
// yes/no confirmation. It stays decoupled from the core layer: dynamic filtering
// is supplied by the parent as a callback.
package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	KindBranches    Kind = "branches"
	KindCommit      Kind = "commit"
	KindPrune       Kind = "prune"
	KindNewAlias    Kind = "new_alias"     // enter new alias name
	KindNewAliasCmd Kind = "new_alias_cmd" // enter new alias command

	// Worktree creation flow.
	KindCreateSource   Kind = "create_source"   // pick new/existing/PR
	KindCreateNew      Kind = "create_new"      // enter new branch name
	KindCreateExisting Kind = "create_existing" // pick existing branch
	KindCreatePR       Kind = "create_pr"       // pick pull request

	// Process viewer.
	KindProcesses Kind = "processes"
)

// mode is the interaction style of the modal.
type mode int

const (
	modeInput mode = iota
	modeSelect
	modeFuzzy
	modeConfirm
	modePrune
)

// SubmitMsg is emitted when the user confirms a choice.
type SubmitMsg struct {
	Kind  Kind
	Value string
}

// CancelMsg is emitted when the user dismisses the modal.
type CancelMsg struct{ Kind Kind }

// FilterFunc maps a query to an ordered candidate list.
type FilterFunc func(query string) []string

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

	// Prune-mode fields.
	canMerge   bool
	mergeOn    bool
	mergeLabel string
	tailSteps  []string
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

// Kind returns the modal kind.
func (m Model) Kind() Kind { return m.kind }

// SetSize records the available screen size for centering.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetBody sets read-only text rendered above the input field (input mode only).
func (m *Model) SetBody(s string) { m.body = s }

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

	case " ", "m":
		// Toggle merge only in prune mode; in text modes these are literal keys
		// and must fall through to the input field.
		if m.mode == modePrune {
			if m.canMerge {
				m.mergeOn = !m.mergeOn
			}
			return m, nil
		}

	case "enter":
		return m, m.onEnter()
	}

	// Text-entry modes consume the remaining keys.
	if m.mode == modeInput || m.mode == modeFuzzy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.mode == modeFuzzy && m.filter != nil {
			m.items = m.filter(m.input.Value())
			m.cursor = 0
		}
		return m, cmd
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
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
		if m.mergeOn {
			return m.submit("merge")
		}
		return m.submit("confirm")
	default: // select, fuzzy
		if m.cursor >= 0 && m.cursor < len(m.items) {
			return m.submit(m.items[m.cursor])
		}
		return m.cancel()
	}
}

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("205")).
			Padding(1, 2)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	arrowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	onStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	offStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dangerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// View renders the centered overlay.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	switch m.mode {
	case modePrune:
		b.WriteString(m.renderPrune())
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
		b.WriteString(m.renderList())
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("↑/↓ move · enter select · esc cancel"))
	default:
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

// renderPrune draws the merge toggle and the ordered pipeline as an arrow flow.
func (m Model) renderPrune() string {
	var b strings.Builder

	if m.canMerge {
		box, label := "[ ]", offStyle.Render(m.mergeLabel)
		if m.mergeOn {
			box, label = onStyle.Render("[x]"), onStyle.Render(m.mergeLabel)
		}
		fmt.Fprintf(&b, "%s %s\n\n", box, label)
	}

	arrow := arrowStyle.Render(" → ")
	steps := make([]string, 0, len(m.tailSteps)+1)
	if m.mergeOn && m.canMerge {
		steps = append(steps, onStyle.Render(m.mergeLabel))
	}
	steps = append(steps, m.tailSteps...)
	b.WriteString(strings.Join(steps, arrow))
	b.WriteString("\n\n")

	hint := "enter confirm · esc cancel"
	if m.canMerge {
		hint = "space toggle merge · " + hint
	}
	b.WriteString(dangerStyle.Render("This is destructive.") + " " + dimStyle.Render(hint))
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
