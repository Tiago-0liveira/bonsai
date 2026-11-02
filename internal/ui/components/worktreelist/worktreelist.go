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

	gh "github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Item is a worktree row, optionally decorated with ahead/behind metrics and a
// connected pull-request number.
type Item struct {
	WT          git.Worktree
	Metrics     git.Metrics
	HasMetrics  bool
	DefaultBase string
	BaseMerged  bool
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

// checkDot renders a CI rollup with a distinct shape, or "" when unknown.
func checkDot(rollup string) string { return checkGlyph(rollup, false) }
func checkGlyph(rollup string, ascii bool) string {
	pass, fail, pending := "✓", "✗", "◌"
	if ascii {
		pass, fail, pending = "+", "x", "~"
	}
	switch rollup {
	case "pass":
		return checkPass.Render(pass)
	case "fail":
		return checkFail.Render(fail)
	case "pending":
		return checkPending.Render(pending)
	}
	return ""
}

// dirtyBadge renders working-tree change counts: a colored "±N" for tracked
// changes (staged + modified) and a dim "?M" for untracked files. Clean
// worktrees render "".
func (i Item) dirtyBadge() string {
	var parts []string
	if n := i.Status.Staged + i.Status.Modified; n > 0 {
		parts = append(parts, dirtyStyle.Render(fmt.Sprintf("%s%d", TrackedGlyph(false), n)))
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
	mergedBranchStyle  lipgloss.Style
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
	mergedBranchStyle = lipgloss.NewStyle().Foreground(p.PhaseReview)
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
var PRStatusModes = []string{"full", "compact", "color", "off"}

// prStatusMode is the active display mode; "full" is the default.
var prStatusMode = PRStatusModes[0]

// SetPRStatusMode selects how row PR status is shown. Unknown modes fall
// back to "full".
func SetPRStatusMode(mode string) {
	switch mode {
	case "compact", "color", "off":
		prStatusMode = mode
	default:
		prStatusMode = "full"
	}
}

// prStatus keeps PR state readable without relying on colour.
func (i Item) prStatus() (label string, st lipgloss.Style) {
	switch i.PRState {
	case gh.StateMerged:
		return "merged", mergedBadgeStyle
	case gh.StateClosed:
		return "closed", closedBadgeStyle
	}
	if i.PRDraft {
		return "draft", draftBadgeStyle
	}
	switch i.PRReview {
	case gh.ReviewApproved:
		return "approved", approvedBadgeStyle
	case gh.ReviewChangesRequested:
		return "changes", changesBadgeStyle
	}
	return "open", prBadgeStyle
}

// prBadge shows a PR number and state word. Off and color keep only the number.
func (i Item) prBadge() string {
	n, ok := i.prNumber()
	if !ok {
		return ""
	}
	if i.PRState == "" || prStatusMode == "off" || prStatusMode == "color" {
		return prBadgeStyle.Render(fmt.Sprintf("#%d", n))
	}
	label, st := i.prStatus()
	return prBadgeStyle.Render(fmt.Sprintf("#%d", n)) + " " + st.Render(label)
}

// prBranchColor follows PR state rather than review decisions, so an open PR
// stays green even when changes are requested.
func (i Item) prBranchColor() lipgloss.TerminalColor {
	switch i.PRState {
	case gh.StateMerged:
		return mergedBranchStyle.GetForeground()
	case gh.StateClosed:
		return closedBadgeStyle.GetForeground()
	}
	if i.PRDraft {
		return draftBadgeStyle.GetForeground()
	}
	return approvedBadgeStyle.GetForeground()
}

// itemDelegate renders a two-line row, coloring a leading "#N" PR badge
// independently so it survives selection highlighting.
type itemDelegate struct {
	embedded bool
	home     string
	ascii    bool
	// badges returns the pre-styled agent glyphs for a worktree path; nil
	// means agents are off and rows render exactly as before.
	badges func(path string) string
}

func (itemDelegate) Height() int                         { return 2 }
func (itemDelegate) Spacing() int                        { return 1 }
func (itemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(Item)
	if !ok {
		return
	}
	width := m.Width()
	selected := index == m.Index()

	pointer := "  "
	nameStyle := normalTitle
	if selected {
		cursor := "› "
		if d.ascii {
			cursor = "> "
		}
		pointer = accentStyle.Render(cursor)
		nameStyle = selectedTitle
	}

	if _, hasPR := it.prNumber(); prStatusMode == "color" && hasPR {
		nameStyle = nameStyle.Foreground(it.prBranchColor())
	}

	prBlock := func(row Item) string {
		if _, ok := row.prNumber(); !ok {
			return ""
		}
		block := row.prBadge()
		if checks := checkGlyph(row.Checks, d.ascii); checks != "" {
			block += " " + checks
		}
		return block
	}
	inlinePrefix := ""
	if block := prBlock(it); prStatusMode != "off" && block != "" {
		inlinePrefix = block + " "
	}
	prefix := ""
	switch prStatusMode {
	case "off":
	case "full":
		// State words vary in width ("open" vs "merged"), so a shared column
		// would push every branch, PR or not, toward the middle of the row.
		prefix = inlinePrefix
	default:
		prWidth := 0
		for _, li := range m.VisibleItems() {
			if row, ok := li.(Item); ok {
				prWidth = max(prWidth, ansi.StringWidth(prBlock(row)))
			}
		}
		if prWidth > 0 {
			block := prBlock(it)
			prefix = block + strings.Repeat(" ", prWidth-ansi.StringWidth(block)+1)
		}
	}

	columns := func(row Item) []string {
		dirty := row.dirtyBadge()
		if d.ascii {
			dirty = strings.ReplaceAll(dirty, TrackedGlyph(false), TrackedGlyph(true))
		}
		if row.Running > 0 {
			glyph := RunningGlyph(d.ascii)
			dirty += " " + runningStyle.Render(fmt.Sprintf("%s%d", glyph, row.Running))
		}
		agents := ""
		if d.badges != nil {
			agents = d.badges(row.WT.Path)
		}
		metrics := ""
		if row.HasMetrics && (row.Metrics.Ahead > 0 || row.Metrics.Behind > 0) {
			up, down := AheadBehindGlyphs(d.ascii)
			metrics = fmt.Sprintf("%s%d %s%d", up, row.Metrics.Ahead, down, row.Metrics.Behind)
		}
		pr, checks := "", ""
		if prStatusMode == "off" {
			pr, checks = strings.TrimSpace(row.prBadge()), checkGlyph(row.Checks, d.ascii)
		}
		return []string{pr, checks, strings.TrimSpace(dirty), agents, metrics}
	}
	cells := columns(it)
	widths := make([]int, len(cells))
	for _, li := range m.VisibleItems() {
		if row, ok := li.(Item); ok {
			for j, c := range columns(row) {
				widths[j] = max(widths[j], ansi.StringWidth(c))
			}
		}
	}
	tailW := 0
	for _, w := range widths {
		if w > 0 {
			tailW += w + 1
		}
	}
	if (prStatusMode == "compact" || prStatusMode == "color") &&
		ansi.StringWidth(pointer+prefix+it.Title())+tailW > width {
		// Spend alignment padding before shortening the branch or its PR block.
		prefix = inlinePrefix
	}
	nameW := max(width-2-ansi.StringWidth(prefix)-tailW, 1)
	tail := ""
	for j, c := range cells {
		if widths[j] > 0 {
			tail += " " + c + strings.Repeat(" ", max(widths[j]-ansi.StringWidth(c), 0))
		}
	}
	title := nameStyle.Render(ansi.Truncate(it.Title(), nameW, "…"))
	title += strings.Repeat(" ", max(nameW-ansi.StringWidth(title), 0)) + tail
	// Reserve the complete PR block before truncating the branch and tail.
	remaining := max(width-ansi.StringWidth(pointer+prefix), 0)
	if width > 0 && ansi.StringWidth(title) > remaining {
		// At extreme widths preserve the complete opaque agent badge if it fits.
		agents := cells[3]
		if agents != "" && ansi.StringWidth(agents) < remaining {
			title = ansi.Truncate(title, max(remaining-ansi.StringWidth(agents)-1, 0), "…") + " " + agents
		} else {
			title = ansi.Truncate(title, remaining, "…")
		}
	}
	title = pointer + prefix + title
	if width > 0 && ansi.StringWidth(title) > width {
		// Only an oversized pointer and PR block can exhaust the branch budget.
		title = ansi.Truncate(title, width, "")
	}
	path := it.WT.Path
	if d.home != "" && (path == d.home || strings.HasPrefix(path, d.home+"/")) {
		path = "~" + strings.TrimPrefix(path, d.home)
	}
	label := it.baseLabel(m.Items(), d.ascii, max(width-4, 0))
	labelW := ansi.StringWidth(label)
	pathW := max(width-2, 0)
	if labelW > 0 {
		pathW = max(pathW-labelW-1, 0)
	}
	if labelW > 0 && ansi.StringWidth(path) > pathW {
		path = ansi.TruncateLeft(path, ansi.StringWidth(path)-pathW+1, "…")
		path = ansi.Truncate(path, pathW, "")
	}
	desc := "  " + descStyle.Render(path)
	if labelW > 0 {
		desc += strings.Repeat(" ", max(width-ansi.StringWidth(desc)-labelW, 0)) + label
	}
	if width > 0 {
		ellipsis := ""
		if labelW == 0 {
			ellipsis = "…"
		}
		desc = ansi.Truncate(desc, width, ellipsis)
	}

	if d.embedded {
		bg := theme.Current.Surface
		if selected {
			bg = theme.Current.Raised
		}
		row := func(s string) string { return lipgloss.NewStyle().Background(bg).Width(width).Render(s) }
		title, desc = row(title), row(desc)
	}
	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// Model wraps a bubbles list.
type Model struct {
	list     list.Model
	focused  bool
	embedded bool
	badges   func(string) string
	home     string
	ascii    bool
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
	selected := ""
	if it, ok := m.Selected(); ok {
		selected = it.WT.Path
	}
	if cmd := m.list.SetItems(li); cmd != nil {
		// The installed list filter is pure; applying it here keeps refreshed
		// metrics from blanking an active filter until another keystroke.
		m.list, _ = m.list.Update(cmd())
	}
	if selected != "" {
		m.SelectByPath(selected)
	}
}

// SetAgentBadges installs the per-path agent glyph source shown on each row's
// first line; nil removes it. The component stays data-agnostic: the parent
// owns the sessions and the styling.
func (m *Model) SetAgentBadges(fn func(path string) string) {
	m.badges = fn
	m.list.SetDelegate(itemDelegate{badges: fn, embedded: m.embedded, home: m.home, ascii: m.ascii})
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
	for i, li := range m.list.VisibleItems() {
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
	if !m.embedded {
		count := len(l.VisibleItems())
		position := 0
		if count > 0 {
			position = l.Index() + 1
		}
		l.Title = strings.Replace(l.Title, "Worktrees", fmt.Sprintf("Worktrees %d/%d", position, count), 1)
	}
	l.Title = ansi.Truncate(l.Title, max(l.Width()-4, 0), "…")
	lines := strings.Split(l.View(), "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, l.Width(), "")
	}
	return strings.Join(lines, "\n")
}

// Focused reports focus state (used by the parent for border color).
func (m Model) Focused() bool { return m.focused }

// SetEmbedded uses the shell's heading and shaded rows instead of a second app title.
func (m *Model) SetEmbedded(on bool) {
	m.embedded = on
	m.list.SetDelegate(itemDelegate{badges: m.badges, embedded: on, home: m.home, ascii: m.ascii})
	m.list.SetShowTitle(!on)
}

// SetPathHome injects the home directory resolved by the parent's load command.
func (m *Model) SetPathHome(home string) {
	m.home = strings.TrimRight(home, "/")
	m.SetAgentBadges(m.badges)
}
func (m *Model) SetASCII(ascii bool) { m.ascii = ascii; m.SetAgentBadges(m.badges) }

// SelectCreatedPath reveals a newly created checkout even during filtering.
func (m *Model) SelectCreatedPath(path string) bool {
	if m.SelectByPath(path) {
		return true
	}
	m.list.ResetFilter()
	return m.SelectByPath(path)
}

func TrackedGlyph(ascii bool) string {
	if ascii {
		return "+/-"
	}
	return "±"
}
func RunningGlyph(ascii bool) string {
	if ascii {
		return ">"
	}
	return "▶"
}

func AheadBehindGlyphs(ascii bool) (string, string) {
	if ascii {
		return "^", "v"
	}
	return "↑", "↓"
}

// BaseGlyphs follows the list's ASCII preference for terminals without glyphs.
func BaseGlyphs(ascii bool) (string, string, string) {
	if ascii {
		return "^", "~", "!"
	}
	return "⤴", "⟳", "⚠"
}

func (i Item) baseLabel(items []list.Item, ascii bool, budget int) string {
	name := i.Metrics.Base
	source := i.Metrics.BaseSource
	if !i.HasMetrics || name == "" || (name == i.DefaultBase && (source == "default" || source == "guess")) {
		return ""
	}
	// Only drop a category prefix when the resulting name remains unambiguous.
	if _, short, ok := strings.Cut(name, "/"); ok {
		unique := true
		for _, li := range items {
			if row, ok := li.(Item); ok && row.WT.Branch != name {
				other := row.WT.Branch
				if _, tail, hasPrefix := strings.Cut(other, "/"); hasPrefix {
					other = tail
				}
				if other == short {
					unique = false
					break
				}
			}
		}
		if unique {
			name = short
		}
	}
	arrow, moved, merged := BaseGlyphs(ascii)
	hint := ""
	if i.BaseMerged {
		hint = checkFail.Render(merged) + " "
	} else if i.Metrics.Behind > 0 {
		hint = checkPending.Render(moved) + " "
	}
	prefix := hint + descStyle.Render(arrow+" ")
	// The base takes space from the path first, retaining at least three name cells.
	name = ansi.Truncate(name, max(budget-ansi.StringWidth(prefix), 3), "…")
	return prefix + descStyle.Render(name)
}
