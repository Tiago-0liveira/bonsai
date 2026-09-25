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

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Item is a worktree row, optionally decorated with ahead/behind metrics and a
// connected pull-request number.
type Item struct {
	WT         git.Worktree
	Metrics    git.Metrics
	HasMetrics bool
	// PR is the number of a PR whose head is this branch (0 = none).
	PR int
	// PRState is the connected PR's state (gh.StateOpen / StateMerged /
	// StateClosed). Empty only when the PR is not in the fetched window.
	PRState string
	// PRDraft marks the connected PR as a draft.
	PRDraft bool
	// PRReview is the connected PR's review decision (gh.ReviewApproved /
	// ReviewChangesRequested, "" otherwise).
	PRReview string
	// Status holds the worktree's working-tree change counts for the badge.
	Status git.StatusSummary
	// Checks is the CI rollup for the connected PR: "pass"/"fail"/"pending"/"".
	Checks string
	// Running is the number of processes currently running in this worktree.
	Running int
	// AgentProfile is the selected AGYM profile for an active agent run.
	AgentProfile string
	// AgentStatus is the current status of the active agent run.
	AgentStatus string
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

// dirtyBadge renders working-tree change counts: a colored "●N" for tracked
// changes (staged + modified) and a dim "?M" for untracked files. Clean
// worktrees render "".
func (i Item) dirtyBadge() string {
	var parts []string
	if n := i.Status.Staged + i.Status.Modified; n > 0 {
		parts = append(parts, dirtyStyle.Render(fmt.Sprintf("●%d", n)))
	}
	if i.Status.Untracked > 0 {
		parts = append(parts, descStyle.Render(fmt.Sprintf("?%d", i.Status.Untracked)))
	}
	return strings.Join(parts, " ")
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
	titleStyle         lipgloss.Style
	prBadgeStyle       lipgloss.Style
	draftBadgeStyle    lipgloss.Style
	mergedBadgeStyle   lipgloss.Style
	approvedBadgeStyle lipgloss.Style
	closedBadgeStyle   lipgloss.Style
	changesBadgeStyle  lipgloss.Style
	normalTitle        lipgloss.Style
	selectedTitle      lipgloss.Style
	descStyle          lipgloss.Style
	accentStyle        lipgloss.Style
	dirtyStyle         lipgloss.Style
	runningStyle       lipgloss.Style
	checkPass          lipgloss.Style
	checkFail          lipgloss.Style
	checkPending       lipgloss.Style
)

// titleIcon decorates the pane header, sitting in the app's top-left corner.
const titleIcon = "🪴"

func init() { SetTheme(theme.Current) }

// SetTheme rebuilds the list styles from a palette.
func SetTheme(p theme.Palette) {
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	prBadgeStyle = lipgloss.NewStyle().Foreground(p.PRBadge).Bold(true)
	draftBadgeStyle = lipgloss.NewStyle().Foreground(p.Dim)
	mergedBadgeStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	approvedBadgeStyle = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	closedBadgeStyle = lipgloss.NewStyle().Foreground(p.Danger)
	changesBadgeStyle = lipgloss.NewStyle().Foreground(p.Danger)
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

// PRStatusModes are the display modes for the row PR status indicator, in
// cycle order.
var PRStatusModes = []string{"full", "compact", "off"}

// prStatusMode is the active display mode; "full" is the default.
var prStatusMode = PRStatusModes[0]

// SetPRStatusMode selects how row PR status is shown. Unknown modes fall
// back to "full".
func SetPRStatusMode(mode string) {
	switch mode {
	case "compact", "off":
		prStatusMode = mode
	default:
		prStatusMode = "full"
	}
}

// prStatus maps the connected PR's state, draft flag and review decision to
// its indicator glyph, label and style. Half circles mark not-ready states
// (draft, unapproved, changes requested); full circles mark settled ones
// (approved, merged, closed).
func (i Item) prStatus() (glyph, label string, st lipgloss.Style) {
	switch i.PRState {
	case gh.StateMerged:
		return "●", "merged", mergedBadgeStyle
	case gh.StateClosed:
		return "●", "closed", closedBadgeStyle
	}
	if i.PRDraft {
		return "◐", "draft", draftBadgeStyle
	}
	switch i.PRReview {
	case gh.ReviewApproved:
		return "●", "approved", approvedBadgeStyle
	case gh.ReviewChangesRequested:
		return "◐", "changes", changesBadgeStyle
	}
	return "◐", "open", prBadgeStyle
}

// prBadge renders the connected PR badge: a colored "#N" plus, depending on
// the display mode, a state glyph ("compact") or glyph and label ("full").
// With "off" only the number shows. Branches named "pr-N" without a known
// state always render the plain badge.
func (i Item) prBadge() string {
	n, ok := i.prNumber()
	if !ok {
		return ""
	}
	if i.PRState == "" || prStatusMode == "off" {
		return prBadgeStyle.Render(fmt.Sprintf("#%d ", n))
	}
	glyph, label, st := i.prStatus()
	switch prStatusMode {
	case "compact":
		return prBadgeStyle.Render(fmt.Sprintf("#%d", n)) + " " + st.Render(glyph) + " "
	default: // full
		return prBadgeStyle.Render(fmt.Sprintf("#%d", n)) + " " + st.Render(glyph+" "+label) + " "
	}
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
	title += it.prBadge()
	title += nameStyle.Render(it.Title())
	if badge := it.dirtyBadge(); badge != "" {
		title += " " + badge
	}
	if it.Running > 0 {
		title += " " + runningStyle.Render(fmt.Sprintf("▶%d", it.Running))
	}
	if it.AgentStatus != "" {
		agentLabel := "agent: "
		if it.AgentProfile != "" && it.AgentProfile != "auto" {
			agentLabel += it.AgentProfile + " · "
		}
		agentLabel += it.AgentStatus
		title += " " + accentStyle.Render(agentLabel)
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
	l.Title = titleIcon + " Worktrees"
	l.Styles.Title = titleStyle
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
func (m *Model) SetTitle(s string) { m.list.Title = titleIcon + " " + s }

// Focus / Blur toggle the focused style flag.
func (m *Model) Focus() { m.focused = true }
func (m *Model) Blur()  { m.focused = false }

// Selected returns the highlighted item and whether one exists.
func (m Model) Selected() (Item, bool) {
	it, ok := m.list.SelectedItem().(Item)
	return it, ok
}

// SelectByPath moves the highlight to the worktree with the given path, if
// present. Returns whether a match was found.
func (m *Model) SelectByPath(path string) bool {
	for i, li := range m.list.Items() {
		if it, ok := li.(Item); ok && it.WT.Path == path {
			m.list.Select(i)
			return true
		}
	}
	return false
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

// View renders the list, re-applying the title style in case the theme changed
// after the list was constructed.
func (m Model) View() string {
	l := m.list
	l.Styles.Title = titleStyle
	return l.View()
}

// Focused reports focus state (used by the parent for border color).
func (m Model) Focused() bool { return m.focused }
