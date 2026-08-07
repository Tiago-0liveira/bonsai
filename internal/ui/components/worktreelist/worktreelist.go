// Package worktreelist is the left-pane list of git worktrees.
package worktreelist

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Item is a worktree row, optionally decorated with ahead/behind metrics and a
// connected pull-request number.
type Item struct {
	WT         git.Worktree
	Metrics    git.Metrics
	HasMetrics bool
	// PR is the number of an open PR whose head is this branch (0 = none).
	PR int
	// Dirty is true when the worktree has uncommitted changes.
	Dirty bool
	// Checks is the CI rollup for the connected PR: "pass"/"fail"/"pending"/"".
	Checks string
	// Running is the number of processes currently running in this worktree.
	Running int
}

// Title is the branch name (or "(main)").
func (i Item) Title() string {
	name := i.WT.Branch
	if name == "" {
		name = "(detached)"
	}
	if i.WT.IsMain {
		name += " (main)"
	}
	return name
}

// Description shows the path plus ahead/behind arrows.
func (i Item) Description() string {
	if i.HasMetrics && (i.Metrics.Ahead > 0 || i.Metrics.Behind > 0) {
		return fmt.Sprintf("%s  ↑%d ↓%d", i.WT.Path, i.Metrics.Ahead, i.Metrics.Behind)
	}
	return i.WT.Path
}

// FilterValue drives list filtering.
func (i Item) FilterValue() string { return i.WT.Branch + " " + i.WT.Path }

// checkDot renders a colored CI rollup indicator, or "" when unknown.
func checkDot(rollup string) string {
	switch rollup {
	case "pass":
		return checkPass.Render("●")
	case "fail":
		return checkFail.Render("●")
	case "pending":
		return checkPending.Render("◐")
	}
	return ""
}

// prNumber returns the connected PR number: an explicitly-set PR (discovered via
// gh) takes precedence, otherwise a "pr-<N>" branch name is parsed.
func (i Item) prNumber() (int, bool) {
	if i.PR > 0 {
		return i.PR, true
	}
	if strings.HasPrefix(i.WT.Branch, "pr-") {
		if n, err := strconv.Atoi(strings.TrimPrefix(i.WT.Branch, "pr-")); err == nil {
			return n, true
		}
	}
	return 0, false
}

var (
	prBadgeStyle  lipgloss.Style
	normalTitle   lipgloss.Style
	selectedTitle lipgloss.Style
	descStyle     lipgloss.Style
	accentStyle   lipgloss.Style
	dirtyStyle    lipgloss.Style
	runningStyle  lipgloss.Style
	checkPass     lipgloss.Style
	checkFail     lipgloss.Style
	checkPending  lipgloss.Style
)

func init() { SetTheme(theme.Current) }

// SetTheme rebuilds the list styles from a palette.
func SetTheme(p theme.Palette) {
	prBadgeStyle = lipgloss.NewStyle().Foreground(p.PRBadge).Bold(true)
	normalTitle = lipgloss.NewStyle().Foreground(p.Text)
	selectedTitle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	descStyle = lipgloss.NewStyle().Foreground(p.Dim)
	accentStyle = lipgloss.NewStyle().Foreground(p.Accent)
	dirtyStyle = lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
	runningStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	checkPass = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	checkFail = lipgloss.NewStyle().Foreground(p.Danger).Bold(true)
	checkPending = lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
}

// itemDelegate renders a two-line row, coloring a leading "#N" PR badge
// independently so it survives selection highlighting.
type itemDelegate struct{}

func (itemDelegate) Height() int                         { return 2 }
func (itemDelegate) Spacing() int                        { return 1 }
func (itemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(Item)
	if !ok {
		return
	}
	width := m.Width()
	selected := index == m.Index()

	pointer := "  "
	nameStyle := normalTitle
	if selected {
		pointer = accentStyle.Render("› ")
		nameStyle = selectedTitle
	}

	title := pointer
	if dot := checkDot(it.Checks); dot != "" {
		title += dot + " "
	}
	if n, isPR := it.prNumber(); isPR {
		title += prBadgeStyle.Render(fmt.Sprintf("#%d ", n))
	}
	title += nameStyle.Render(it.Title())
	if it.Dirty {
		title += " " + dirtyStyle.Render("●")
	}
	if it.Running > 0 {
		title += " " + runningStyle.Render(fmt.Sprintf("▶%d", it.Running))
	}

	desc := "  " + descStyle.Render(it.Description())

	if width > 0 {
		title = ansi.Truncate(title, width, "…")
		desc = ansi.Truncate(desc, width, "…")
	}
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// Model wraps a bubbles list.
type Model struct {
	list    list.Model
	focused bool
}

// New builds an empty worktree list.
func New() Model {
	l := list.New(nil, itemDelegate{}, 0, 0)
	l.Title = "Worktrees"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return Model{list: l}
}

// SetItems replaces the list contents.
func (m *Model) SetItems(items []Item) {
	li := make([]list.Item, len(items))
	for i, it := range items {
		li[i] = it
	}
	m.list.SetItems(li)
}

// SetSize resizes the list.
func (m *Model) SetSize(w, h int) { m.list.SetSize(w, h) }

// SetTitle sets the list header (e.g. to show the active sort mode).
func (m *Model) SetTitle(s string) { m.list.Title = s }

// Focus / Blur toggle the focused style flag.
func (m *Model) Focus() { m.focused = true }
func (m *Model) Blur()  { m.focused = false }

// Selected returns the highlighted item and whether one exists.
func (m Model) Selected() (Item, bool) {
	it, ok := m.list.SelectedItem().(Item)
	return it, ok
}

// SettingFilter reports whether the filter text input is currently active, in
// which case the list should own every keystroke.
func (m Model) SettingFilter() bool { return m.list.SettingFilter() }

// Update forwards messages to the underlying list.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the list.
func (m Model) View() string { return m.list.View() }

// Focused reports focus state (used by the parent for border color).
func (m Model) Focused() bool { return m.focused }
