package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const leftPaneRatio = 35 // percent of width for the worktree list

var (
	focusedBorder lipgloss.Style
	blurredBorder lipgloss.Style
	statusStyle   lipgloss.Style
	paneTitle     lipgloss.Style
	activeTab     lipgloss.Style
	inactiveTab   lipgloss.Style
	procAccent    lipgloss.Style
	procDim       lipgloss.Style
	procRunning   lipgloss.Style
	procFailed    lipgloss.Style
	procDone      lipgloss.Style
	// PR-pane role styles.
	prStateOpen   lipgloss.Style
	prStateClosed lipgloss.Style
	prStateMerged lipgloss.Style
	prWarn        lipgloss.Style
	prSection     lipgloss.Style
	prLabel       lipgloss.Style
	prMeta        lipgloss.Style
	diffAdd       lipgloss.Style
	diffDel       lipgloss.Style
	diffCursorSt  lipgloss.Style
	diffPathSt    lipgloss.Style
)

func init() { applyTheme() }

// applyTheme rebuilds the package's styles from theme.Current. Called at startup
// and again after config resolves the palette.
func applyTheme() {
	p := theme.Current
	focusedBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.BorderFocus)
	blurredBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border)
	statusStyle = lipgloss.NewStyle().Foreground(p.Warning)
	paneTitle = lipgloss.NewStyle().Bold(true).Foreground(p.Accent).Padding(0, 1)
	activeTab = lipgloss.NewStyle().Bold(true).Foreground(p.Accent)
	inactiveTab = lipgloss.NewStyle().Foreground(p.Dim)
	procAccent = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	procDim = lipgloss.NewStyle().Foreground(p.Dim)
	procRunning = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	procFailed = lipgloss.NewStyle().Foreground(p.Danger).Bold(true)
	procDone = lipgloss.NewStyle().Foreground(p.Dim)
	prStateOpen = lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	prStateClosed = lipgloss.NewStyle().Foreground(p.Danger).Bold(true)
	prStateMerged = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	prWarn = lipgloss.NewStyle().Foreground(p.Warning).Bold(true)
	prSection = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	prLabel = lipgloss.NewStyle().Foreground(p.PRBadge)
	prMeta = lipgloss.NewStyle().Foreground(p.Dim)
	diffAdd = lipgloss.NewStyle().Foreground(p.Success)
	diffDel = lipgloss.NewStyle().Foreground(p.Danger)
	diffCursorSt = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	diffPathSt = lipgloss.NewStyle().Foreground(p.Text)
}

const (
	procHint      = "↑/↓ select · k kill · r restart · n new · x remove · v log"
	procEmptyHint = "no processes yet — press n to run a script or command"
)

// renderProcFooter builds the Processes-tab footer pinned to the bottom of the
// right pane: a divider, the colored process list with the selection cursor, and
// the keybind hint. The selected process's output scrolls above it (see View).
func (m Model) renderProcFooter() string {
	wt, ok := m.selectedWorktree()
	if !ok {
		return procDim.Render("(no worktree selected)")
	}
	procs := m.procs.List(wt.Path)
	if len(procs) == 0 {
		return procDim.Render(procEmptyHint)
	}
	sel, selOK := m.activeProcess(wt.Path)
	var b strings.Builder
	b.WriteString(rule(m.termInnerWidth()) + "\n")
	for _, p := range procs {
		cursor := "  "
		if selOK && p.ID == sel.ID {
			cursor = procAccent.Render("› ")
		}
		b.WriteString(cursor + ansi.Truncate(procLine(p), m.termInnerWidth(), "…") + "\n")
	}
	b.WriteString(procDim.Render(procHint))
	return b.String()
}

// procLine renders one process row with a color-coded status, plus any local
// URL the process has printed (e.g. a dev server's address).
func procLine(p *coreexec.Process) string {
	status := p.Status()
	var st string
	switch status {
	case "running":
		st = procRunning.Render(status)
	case "failed":
		st = procFailed.Render(status)
	default:
		st = procDone.Render(status)
	}
	line := fmt.Sprintf("#%d %s (%s)", p.ID, p.Label, st)
	if u := p.LastURL(); u != "" {
		line += "  " + procDim.Render(u)
	}
	return line
}

const prPaneHint = "enter desc · t commits · a approve · c changes · m merge · x close · O reopen · y ready"

// renderPRDetail builds the PR tab body GitHub-style: a header block, a
// collapsible description, collapsible commits, CI checks, and the review/comment
// timeline. Sections use color; the description and commits fold via prExpandDesc
// / prExpandCommits.
func (m Model) renderPRDetail(d gh.PRDetail, checks []gh.Check) string {
	w := m.termInnerWidth()
	var b strings.Builder

	// ── Title line: #num + title ────────────────────────────────────────────
	b.WriteString(procAccent.Render("#"+itoa(d.Number)) + "  " + prSection.Render(d.Title) + "\n")

	// ── State / author line ─────────────────────────────────────────────────
	badge := stateBadge(d.State)
	if d.IsDraft && d.State == "OPEN" {
		badge += " " + prMeta.Render("· draft")
	}
	author := ""
	if d.Author != "" {
		author = prMeta.Render("  @" + d.Author)
	}
	opened := ""
	if t := shortTime(d.CreatedAt); t != "" {
		opened = prMeta.Render("  opened " + t)
	}
	b.WriteString(badge + author + opened + "\n")

	// ── Branch + diffstat + mergeable ───────────────────────────────────────
	stat := diffAdd.Render(fmt.Sprintf("+%d", d.Additions)) + " " +
		diffDel.Render(fmt.Sprintf("−%d", d.Deletions)) + prMeta.Render(fmt.Sprintf(" · %d files", d.ChangedFiles))
	b.WriteString(prMeta.Render(d.Base+" ← ") + d.Head + "   " + stat + "   " + mergeableBadge(d.Mergeable) + "\n")

	// ── Labels / reviewers ──────────────────────────────────────────────────
	if len(d.Labels) > 0 {
		var ls []string
		for _, l := range d.Labels {
			ls = append(ls, prLabel.Render("["+l+"]"))
		}
		b.WriteString(strings.Join(ls, " ") + "\n")
	}
	if len(d.Reviewers) > 0 {
		b.WriteString(prMeta.Render("reviewers pending: ") + "@" + strings.Join(d.Reviewers, " @") + "\n")
	}
	if d.URL != "" {
		b.WriteString(prMeta.Render(d.URL) + "\n")
	}

	b.WriteString(rule(w) + "\n")

	// ── Description (collapsible) ───────────────────────────────────────────
	body := strings.TrimSpace(d.Body)
	switch {
	case body == "":
		b.WriteString(prSection.Render("▾ Description") + prMeta.Render("  (empty)") + "\n")
	case m.prExpandDesc:
		b.WriteString(prSection.Render("▾ Description") + prMeta.Render("  (enter to collapse)") + "\n")
		b.WriteString(body + "\n")
	default:
		b.WriteString(prSection.Render("▸ Description") + prMeta.Render("  (enter to expand)") + "\n")
		b.WriteString(prMeta.Render("  "+firstLine(body)) + "\n")
	}
	b.WriteString(rule(w) + "\n")

	// ── Commits (collapsible) ───────────────────────────────────────────────
	tri := "▸"
	if m.prExpandCommits {
		tri = "▾"
	}
	b.WriteString(prSection.Render(tri+" Commits") + prMeta.Render(fmt.Sprintf("  (%d · t)", len(d.Commits))) + "\n")
	if m.prExpandCommits {
		for _, c := range d.Commits {
			line := "  " + procAccent.Render(c.Short()) + "  " + c.Headline
			if c.Author != "" {
				line += prMeta.Render("  @" + c.Author)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString(rule(w) + "\n")

	// ── Checks ──────────────────────────────────────────────────────────────
	if len(checks) > 0 {
		b.WriteString(prSection.Render("Checks") + "\n")
		for _, c := range checks {
			b.WriteString("  " + checkMark(c.Bucket) + " " + c.Name + "\n")
		}
		b.WriteString(rule(w) + "\n")
	}

	// ── Activity timeline ───────────────────────────────────────────────────
	b.WriteString(prSection.Render("Activity") + "\n")
	if len(d.Reviews) == 0 && len(d.Comments) == 0 {
		b.WriteString(prMeta.Render("  (no activity yet)") + "\n")
	}
	for _, rv := range d.Reviews {
		var verb string
		switch rv.State {
		case "APPROVED":
			verb = prStateOpen.Render("✓ approved")
		case "CHANGES_REQUESTED":
			verb = prStateClosed.Render("✗ requested changes")
		default:
			verb = prMeta.Render("commented")
		}
		fmt.Fprintf(&b, " @%s %s %s\n", rv.Author, verb, prMeta.Render(shortTime(rv.SubmittedAt)))
		if s := strings.TrimSpace(rv.Body); s != "" {
			b.WriteString(indent(s) + "\n")
		}
	}
	for _, c := range d.Comments {
		fmt.Fprintf(&b, " @%s %s %s\n", c.Author, prMeta.Render("commented"), prMeta.Render(shortTime(c.CreatedAt)))
		if s := strings.TrimSpace(c.Body); s != "" {
			b.WriteString(indent(s) + "\n")
		}
	}

	b.WriteString("\n" + prMeta.Render(prPaneHint) + "\n")
	return b.String()
}

const diffPaneHint = "↑/↓ select · enter open diff · esc close"

// diffHeaderLines is the number of content lines rendered above the first file
// row (summary + rule), used to map the cursor index to a viewport line.
const diffHeaderLines = 2

// renderDiff builds the changed-file list for the Diff tab: a summary line and
// one row per file with +/− counts. The selected row is marked with a cursor;
// pressing enter opens that file's diff in a scrollable modal.
func (m Model) renderDiff() string {
	if len(m.diffFiles) == 0 {
		return prMeta.Render("(no changes vs " + config.BaseBranch(m.diffBase) + ")")
	}
	var b strings.Builder

	// Summary.
	totA, totD := 0, 0
	for _, f := range m.diffFiles {
		if f.Add > 0 {
			totA += f.Add
		}
		if f.Del > 0 {
			totD += f.Del
		}
	}
	b.WriteString(fmt.Sprintf("%s %s %s %s\n",
		prSection.Render(fmt.Sprintf("%d files changed", len(m.diffFiles))),
		prMeta.Render("·"),
		diffAdd.Render(fmt.Sprintf("+%d", totA)),
		diffDel.Render(fmt.Sprintf("−%d", totD))))
	b.WriteString(rule(m.termInnerWidth()) + "\n")

	for i, f := range m.diffFiles {
		cursor := "  "
		path := diffPathSt.Render(f.Path)
		if i == m.diffCursor {
			cursor = diffCursorSt.Render("› ")
			path = diffCursorSt.Render(f.Path)
		}
		counts := ""
		if f.Add < 0 { // binary
			counts = prMeta.Render("  (binary)")
		} else {
			counts = "  " + diffAdd.Render(fmt.Sprintf("+%d", f.Add)) + " " + diffDel.Render(fmt.Sprintf("−%d", f.Del))
		}
		b.WriteString(cursor + path + counts + "\n")
	}

	b.WriteString("\n" + prMeta.Render(diffPaneHint) + "\n")
	return b.String()
}

// inspectPaneHint is the footer hint shown at the bottom of the Inspector tab.
const inspectPaneHint = "worktree detail · R refresh"

// renderInspect builds the Inspector tab body: working-tree status, last
// commit, diff-vs-base summary, and disk/stash stats for the selected worktree.
func (m Model) renderInspect() string {
	wt, ok := m.selectedWorktree()
	if !ok {
		return prMeta.Render("(no worktree selected)")
	}
	d := m.inspect
	w := m.termInnerWidth()
	var b strings.Builder

	// ── Header: branch + path ──────────────────────────────────────────────
	b.WriteString(prSection.Render(wt.Branch) + prMeta.Render("  "+wt.Path) + "\n")
	b.WriteString(rule(w) + "\n")

	// ── Working tree ───────────────────────────────────────────────────────
	b.WriteString(prSection.Render("Working tree") + "\n")
	if !d.status.Dirty() {
		b.WriteString("  " + procRunning.Render("✓ clean") + "\n")
	} else {
		var parts []string
		if d.status.Staged > 0 {
			parts = append(parts, fmt.Sprintf("%d staged", d.status.Staged))
		}
		if d.status.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", d.status.Modified))
		}
		if d.status.Untracked > 0 {
			parts = append(parts, fmt.Sprintf("%d untracked", d.status.Untracked))
		}
		b.WriteString("  " + prWarn.Render("● "+strings.Join(parts, " · ")) + "\n")
	}
	b.WriteString(rule(w) + "\n")

	// ── Last commit ────────────────────────────────────────────────────────
	b.WriteString(prSection.Render("Last commit") + "\n")
	if d.commitOK {
		b.WriteString("  " + diffPathSt.Render(d.commit.Subject) + "\n")
		meta := d.commit.Author
		if !d.commit.When.IsZero() {
			meta += " · " + ago(d.commit.When)
		}
		b.WriteString("  " + prMeta.Render(meta) + "\n")
	} else {
		b.WriteString("  " + prMeta.Render("(no commits yet)") + "\n")
	}
	b.WriteString(rule(w) + "\n")

	// ── Diff vs base ───────────────────────────────────────────────────────
	if d.base != "" {
		b.WriteString(prSection.Render("Diff vs "+config.BaseBranch(d.base)) + "\n")
		if len(d.files) == 0 {
			b.WriteString("  " + prMeta.Render("(no changes)") + "\n")
		} else {
			totA, totD := 0, 0
			for _, f := range d.files {
				if f.Add > 0 {
					totA += f.Add
				}
				if f.Del > 0 {
					totD += f.Del
				}
			}
			b.WriteString(fmt.Sprintf("  %s %s %s %s\n",
				prMeta.Render(fmt.Sprintf("%d files", len(d.files))),
				prMeta.Render("·"),
				diffAdd.Render(fmt.Sprintf("+%d", totA)),
				diffDel.Render(fmt.Sprintf("−%d", totD))))
		}
		b.WriteString(rule(w) + "\n")
	}

	// ── Disk & stash ───────────────────────────────────────────────────────
	var stats []string
	if d.diskOK {
		stats = append(stats, fs.HumanSize(d.diskKB)+" on disk")
	}
	if d.stashes > 0 {
		stats = append(stats, fmt.Sprintf("%d stash(es)", d.stashes))
	}
	if len(stats) > 0 {
		b.WriteString(prSection.Render("Stats") + "\n")
		b.WriteString("  " + prMeta.Render(strings.Join(stats, " · ")) + "\n")
	}

	b.WriteString("\n" + prMeta.Render(inspectPaneHint) + "\n")
	return b.String()
}

// ago renders a coarse relative time ("3m", "2h", "5d ago") for the inspector.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

const checksPaneHint = "branch workflow runs · R refresh"

// renderChecks builds the Checks tab body: the selected branch's recent GitHub
// Actions runs, newest first, with pass/fail/pending glyphs.
func (m Model) renderChecks() string {
	wt, ok := m.selectedWorktree()
	if !ok {
		return prMeta.Render("(no worktree selected)")
	}
	w := m.termInnerWidth()
	var b strings.Builder

	b.WriteString(prSection.Render("Runs · "+wt.Branch) + "\n")
	if n, isPR := m.prForBranch(wt.Branch); isPR {
		b.WriteString(prMeta.Render("connected: ") + prLabel.Render("PR #"+itoa(n)) + "\n")
	}
	b.WriteString(rule(w) + "\n")

	if m.ciErr != "" {
		b.WriteString(prMeta.Render("(gh unavailable: "+m.ciErr+")") + "\n")
	} else if len(m.ciRuns) == 0 {
		b.WriteString(prMeta.Render("no workflow runs for branch "+wt.Branch) + "\n")
	} else {
		for _, r := range m.ciRuns {
			line := "  " + checkMark(r.Bucket()) + " " + diffPathSt.Render(r.Name)
			meta := r.Workflow
			if r.Event != "" {
				meta += " · " + r.Event
			}
			if when := runWhen(r.CreatedAt); when != "" {
				meta += " · " + when
			}
			line += "  " + prMeta.Render(meta)
			b.WriteString(ansi.Truncate(line, w, "…") + "\n")
		}
	}

	b.WriteString("\n" + prMeta.Render(checksPaneHint) + "\n")
	return b.String()
}

// runWhen renders a run's createdAt as a relative time, falling back to the
// date when the timestamp doesn't parse.
func runWhen(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return ago(t)
	}
	return shortTime(ts)
}

// stateBadge renders a colored PR state badge.
func stateBadge(state string) string {
	switch state {
	case "OPEN":
		return prStateOpen.Render("● OPEN")
	case "MERGED":
		return prStateMerged.Render("● MERGED")
	case "CLOSED":
		return prStateClosed.Render("● CLOSED")
	default:
		return prMeta.Render(state)
	}
}

// mergeableBadge renders a colored mergeability indicator.
func mergeableBadge(m string) string {
	switch m {
	case "MERGEABLE":
		return prStateOpen.Render("✓ mergeable")
	case "CONFLICTING":
		return prStateClosed.Render("✗ conflicting")
	default:
		return prMeta.Render("mergeable: unknown")
	}
}

// termInnerWidth returns a safe render width for the right pane body.
func (m Model) termInnerWidth() int {
	_, rightW, _ := m.dims()
	w := rightW - 4
	if w < 10 {
		w = 10
	}
	if w > 100 {
		w = 100
	}
	return w
}

// rule renders a horizontal divider of the given width.
func rule(w int) string {
	if w < 4 {
		w = 4
	}
	return prMeta.Render(strings.Repeat("─", w))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// checkMark renders a colored glyph for a check bucket.
func checkMark(bucket string) string {
	switch bucket {
	case "pass":
		return procRunning.Render("✓")
	case "fail", "cancel":
		return procFailed.Render("✗")
	case "pending":
		return prWarn.Render("◐")
	default:
		return procDim.Render("·")
	}
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "     " + l
	}
	return strings.Join(lines, "\n")
}

// shortTime trims an ISO-8601 timestamp to its date (YYYY-MM-DD).
func shortTime(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// dims returns the pane geometry for the current terminal size:
// left/right outer widths and the shared inner (inside-border) height.
func (m Model) dims() (leftW, rightW, innerH int) {
	leftW = m.width * leftPaneRatio / 100
	if leftW < 12 {
		leftW = 12
	}
	rightW = m.width - leftW
	barH := lipgloss.Height(m.statusBar())
	innerH = m.height - barH - 2 // 2 = top+bottom border rows
	if innerH < 1 {
		innerH = 1
	}
	return leftW, rightW, innerH
}

// layout recomputes child component sizes from the current terminal size.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	leftW, rightW, innerH := m.dims()
	m.list.SetSize(leftW-2, innerH)
	m.term.SetSize(rightW-2, innerH-1) // -1 for the pane title line
}

// View renders two bordered panes filling the screen above a status/help bar,
// with any active modal drawn centered on top.
func (m Model) View() string {
	if !m.ready {
		return "Loading bonsai…"
	}
	if m.prefs != nil {
		return m.prefs.View()
	}
	if m.modal != nil {
		return m.modal.View()
	}

	leftW, rightW, innerH := m.dims()

	left := m.borderFor(m.focus == focusList).
		Width(leftW - 2).Height(innerH).
		Render(clampHeight(m.list.View(), innerH))

	var rightBody string
	if m.rightTab == tabProcs {
		// Output scrolls in the term viewport; the process list + hint sit in a
		// footer pinned to the bottom. Shrink a copy of the term to leave room.
		footer := m.renderProcFooter()
		footerH := lipgloss.Height(footer)
		term := m.term
		outH := innerH - 1 - footerH // -1 for the tab strip line
		outH = max(outH, 1)
		term.SetSize(rightW-2, outH)
		rightBody = m.tabStrip() + "\n" + term.View() + "\n" + footer
	} else {
		rightBody = m.tabStrip() + "\n" + m.term.View()
	}
	right := m.borderFor(m.focus == focusTerminal).
		Width(rightW - 2).Height(innerH).
		Render(clampHeight(rightBody, innerH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())
}

// tabLabel is the header text for a tab, including its jump key hint.
func tabLabel(t rightTab) string {
	switch t {
	case tabProcs:
		return " Processes (v) "
	case tabDiff:
		return " Diff (d) "
	case tabPR:
		return " PR (P) "
	case tabInspect:
		return " Inspect (i) "
	case tabChecks:
		return " Checks (b) "
	default:
		return " Git Log "
	}
}

// visibleTabs returns the right-pane tabs available for the current selection:
// Log, Processes and Inspect always; Diff for non-main worktrees; Checks when
// the selection is on a branch; PR when the selection has a connected PR.
func (m Model) visibleTabs() []rightTab {
	tabs := []rightTab{tabLog, tabProcs, tabInspect}
	if wt, ok := m.selectedWorktree(); ok {
		if !wt.IsMain {
			tabs = append(tabs, tabDiff)
		}
		if wt.Branch != "" && wt.Branch != "(detached)" {
			tabs = append(tabs, tabChecks)
		}
		if _, isPR := m.prForBranch(wt.Branch); isPR {
			tabs = append(tabs, tabPR)
		}
	}
	return tabs
}

// tabStrip renders the right-pane tab headers with the active tab highlighted.
func (m Model) tabStrip() string {
	var parts []string
	for _, t := range m.visibleTabs() {
		if t == m.rightTab {
			parts = append(parts, activeTab.Render(tabLabel(t)))
		} else {
			parts = append(parts, inactiveTab.Render(tabLabel(t)))
		}
	}
	return strings.Join(parts, inactiveTab.Render("│")) + inactiveTab.Render("  shift+tab ⇄")
}

func (m Model) borderFor(focused bool) lipgloss.Style {
	if focused {
		return focusedBorder
	}
	return blurredBorder
}

// statusBar renders a context-aware key hint line plus any status/error
// message, truncated to the terminal width so the bar never overflows.
func (m Model) statusBar() string {
	help := m.contextHelp()

	msg := m.status
	if m.err != nil {
		msg = "error: " + m.err.Error()
	}
	line := help
	if msg != "" {
		if avail := m.width - lipgloss.Width(help) - 3; avail >= 8 {
			line = help + "  " + statusStyle.Render(ansi.Truncate(msg, avail, "…"))
		}
	}
	return truncateToWidth(line, m.width)
}

// contextHelp renders the keys that matter right now: worktree actions while
// the list is focused, tab cycling while the right pane is, plus the always-on
// palette/preferences/keys/quit.
func (m Model) contextHelp() string {
	bindings := []key.Binding{m.keys.Tab}
	if m.focus == focusList {
		bindings = append(bindings, m.keys.Enter, m.keys.Create, m.keys.Filter)
	} else {
		bindings = append(bindings, m.keys.ShiftTab)
	}
	bindings = append(bindings, m.keys.Palette, m.keys.Prefs, m.keys.Help, m.keys.Quit)

	keySt := lipgloss.NewStyle().Foreground(theme.Current.Text)
	descSt := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, keySt.Render(h.Key)+" "+descSt.Render(h.Desc))
	}
	return strings.Join(parts, descSt.Render(" · "))
}

// clampHeight drops any lines of s beyond the first h.
func clampHeight(s string, h int) string {
	if h < 1 {
		h = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// truncateToWidth clamps each line of s to width with an ellipsis.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}
