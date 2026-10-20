package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// maxLaunchGaps is how many blank separator lines a roomy pane may use.
const maxLaunchGaps = 5

// table is the account table with the five-hour capacity and its countdown, so
// accounts can be compared before one is chosen.
func (d launchDialog) table() accounts.Table {
	return accounts.Table{
		Accounts: d.Ordered(), Selected: d.Selected(),
		Columns:  accounts.LaunchColumns(d.Usage(), d.now, d.ascii),
		ASCII:    d.ascii,
		Focused:  d.focus == launchFocusAccount,
		UnderSep: " · ",
	}
}

func (d launchDialog) rule() string {
	if d.ascii {
		return "-"
	}
	return "─"
}

// View fills the pane with the launcher. The pane draws the one outer
// boundary; the launcher only supplies lines.
func (d launchDialog) View() string {
	w, h := accounts.FrameSize(d.w, d.h)
	if w == 0 || h == 0 {
		return ""
	}
	iw := accounts.ContentWidth(w)
	lines := d.compose(iw, h)
	for i := range lines {
		lines[i] = fit(" "+fit(lines[i], iw), w)
	}
	return strings.Join(lines, "\n")
}

// compose lays out exactly h lines: title, account table, divider, scrollable
// options, save box, actions and one footer. The options, actions and footer
// are reserved first, so the table gives up rows before any of them is cut.
func (d launchDialog) compose(iw, h int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	danger := lipgloss.NewStyle().Foreground(th.Danger)
	rule := dim.Render(strings.Repeat(d.rule(), iw))

	heading, scope := "New session", d.target.branch
	if d.planner {
		heading, scope = "Request AI plan", "read-only / no worktree"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render(heading)
	top := []string{leftRight(title, dim.Render(scope), iw), rule}
	if cur, ok := d.Current(); d.planner && ok {
		top = append(top, d.plannerContract(cur, iw))
	}
	bottom := []string{rule, dim.Render(d.footerFit(iw))}
	free := max(h-len(top)-len(bottom), 0)

	var body []string
	a, ok := d.Current()
	switch {
	case !d.Listed():
		body = []string{dim.Render("Loading accounts…")}
	case d.ListErr() != "":
		body = []string{danger.Render("Couldn't load accounts"), dim.Render(d.ListErr())}
	case !ok:
		body = []string{dim.Render("No accounts yet."), dim.Render("Esc, then m to add one.")}
	default:
		body = d.options(a, iw, free)
	}

	lines := append(top, body...)
	keep := max(h-len(bottom), 0)
	for len(lines) < keep {
		lines = append(lines, "")
	}
	lines = append(lines[:min(len(lines), keep)], bottom...)
	if len(lines) > h && h > 0 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return lines
}

// plannerContract is the permission the chosen account would run with, or why it cannot plan. Choosing the
// account and pressing the primary action is what accepts it.
func (d launchDialog) plannerContract(a agentsapi.Account, iw int) string {
	th := theme.Current
	if !a.ReadOnly.Supported {
		return ansi.Truncate(lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+plannerUnavailableText(a)), iw, "…")
	}
	text := "✓ Read-only: " + a.ReadOnly.Mechanism
	if len(a.ReadOnly.Allows) > 0 {
		text += " · " + strings.Join(a.ReadOnly.Allows, " · ")
	}
	return ansi.Truncate(lipgloss.NewStyle().Foreground(th.Success).Render(text), iw, "…")
}

// options is the body for a selected account: table, divider, options, save
// box and actions in at most free lines.
func (d launchDialog) options(a agentsapi.Account, iw, free int) []string {
	th := theme.Current
	danger := lipgloss.NewStyle().Foreground(th.Danger)

	tbl := d.table()
	useTable := tbl.Fits(iw)
	tableNeed, rowH := 1, 1
	if useTable {
		tableNeed, rowH = tbl.Height(iw), tbl.RowHeight(iw)
	}

	formLines, from, to := d.formLines(iw)
	tail := []string{d.saveRow()}
	if d.err != "" {
		errLines := strings.Split(ansi.Wrap(d.err, max(iw-2, 1), ""), "\n")
		for _, l := range errLines[:min(len(errLines), 2)] {
			tail = append(tail, danger.Render("  "+l))
		}
	}
	buttons := d.buttonsRow(a, iw)

	avail := free - 1 - len(tail) - 1 // divider, save and errors, actions
	spare := avail - tableNeed - len(formLines)
	gaps := [maxLaunchGaps]bool{}
	for i := range gaps {
		gaps[i] = i < spare
	}
	// Blank lines are the first thing to go in a short pane; the order is the
	// order the design gives them up in reverse.
	var beforeDiv, afterRule, afterDiv, beforeSave, beforeBtn = gaps[0], gaps[1], gaps[2], gaps[3], gaps[4]
	for _, g := range gaps {
		if g {
			avail--
		}
	}

	// Rows of the table yield before the options do, but the table keeps a few
	// rows (or just the selected one when the pane is very short).
	tableH, formH := tableNeed, len(formLines)
	if tableNeed+formH > avail {
		tableH = max(avail-formH, 0)
		if floor := min(tableNeed, 1+3*rowH+1); tableH < floor {
			tableH = min(max(avail-min(formH, 3), rowH), floor)
		}
		// The save box and actions are already reserved; a table taller than
		// what is left would push them out of the pane.
		tableH = min(tableH, max(avail, 0))
		formH = max(avail-tableH, 0)
	}

	var tableLines []string
	if useTable {
		tableLines = tbl.View(iw, tableH)
	} else {
		tableLines = []string{d.sidebar().Selector(d.focus == launchFocusAccount)}
	}
	window, start := scrollWindow(formLines, from, to, formH)

	var out []string
	add := func(on bool, l ...string) {
		if on {
			out = append(out, "")
		}
		out = append(out, l...)
	}
	add(afterRule)
	out = append(out, tableLines...)
	add(beforeDiv, d.divider(a, iw, start, len(window), len(formLines)))
	add(afterDiv)
	out = append(out, window...)
	add(beforeSave, tail[0])
	out = append(out, tail[1:]...)
	add(beforeBtn, buttons)
	return out
}

// formLines is the Name row followed by the provider's launch fields, and the
// line range the focused control occupies so scrolling keeps it visible.
func (d launchDialog) formLines(iw int) (lines []string, from, to int) {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	danger := lipgloss.NewStyle().Foreground(th.Danger)

	labelW := 4
	if d.formLoaded {
		labelW = d.form.CompactLabelWidth(iw)
	}
	lines = []string{d.nameRow(iw, labelW)}
	if d.focus == launchFocusName {
		from, to = 0, 1
	}
	switch {
	case d.formErr != "":
		lines = append(lines, danger.Render("Couldn't load options"), dim.Render(d.formErr))
	case !d.formLoaded:
		lines = append(lines, dim.Render("Loading options…"))
	default:
		fl, ff, ft := d.form.CompactLines(iw)
		lines = append(lines, fl...)
		if d.focus == launchFocusForm {
			from, to = ff+1, ft+1
		}
	}
	return lines, from, to
}

// nameRow is the Name field in the same label/value shape as the provider
// fields, so the column of values lines up.
func (d launchDialog) nameRow(iw, labelW int) string {
	th := theme.Current
	focused := d.focus == launchFocusName
	marker, label := "  ", lipgloss.NewStyle().Foreground(th.Dim)
	br := lipgloss.NewStyle()
	if focused {
		accent := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
		marker, label, br = accent.Render("> "), accent, accent
	}
	head := marker + label.Render(fit("Name", labelW)) + "  "
	valueW := iw - ansi.StringWidth(head) - 4
	if valueW < 3 {
		return head + ansi.Truncate(d.name.Value(), max(iw-ansi.StringWidth(head), 1), "…")
	}
	d.name.Prompt = ""
	d.name.Width = valueW - 1
	return head + br.Render("[ ") + fit(d.name.View(), valueW) + br.Render(" ]")
}

// scrollWindow cuts lines to h, keeping [from, to) visible. It returns the
// first line shown.
func scrollWindow(lines []string, from, to, h int) ([]string, int) {
	if len(lines) <= h {
		return lines, 0
	}
	h = max(h, 0)
	start := 0
	if to > h {
		start = to - h
	}
	start = min(min(start, from), len(lines)-h)
	return lines[start : start+h], max(start, 0)
}

// divider names the account the options belong to, so it stays clear while
// editing, and says which lines are showing when the options scroll.
func (d launchDialog) divider(a agentsapi.Account, w, start, shown, total int) string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	r := d.rule()
	name := ansi.Truncate(a.Name, max(w-ansi.StringWidth(providerName(a.Provider))-32, 4), "…")
	label := dim.Render(" Session options · ") + theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, d.ascii)+" "+providerName(a.Provider)) +
		dim.Render(" / ") + lipgloss.NewStyle().Foreground(th.Text).Render(name) + " "
	right := ""
	if shown < total {
		right = dim.Render(fmt.Sprintf(" %d-%d of %d ", start+1, start+shown, total))
	}
	fill := max(w-2-ansi.StringWidth(label)-ansi.StringWidth(right), 0)
	return dim.Render(strings.Repeat(r, 2)) + label + dim.Render(strings.Repeat(r, fill)) + right
}

func (d launchDialog) saveRow() string {
	box := "□"
	switch {
	case d.ascii && d.save:
		box = "[x]"
	case d.ascii:
		box = "[ ]"
	case d.save:
		box = "■"
	}
	row := box + " Save as account defaults"
	if d.focus == launchFocusSave {
		return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent).Render("> " + row)
	}
	return "  " + row
}

// buttonsRow puts Cancel and the primary action at the bottom right. A
// sign-in account swaps Start for the sign-in action, and a start in flight
// shows in place.
func (d launchDialog) buttonsRow(a agentsapi.Account, iw int) string {
	focused := d.focus == launchFocusButtons
	start := "Start session"
	disabled := false
	if d.planner {
		start = "Accept read-only and request plan"
	}
	switch {
	case d.starting:
		start, disabled = "Starting…", true
	case d.planner && !a.ReadOnly.Supported:
		start, disabled = "Cannot plan", true
	case accounts.NeedsSignIn(a.Info):
		start = "Sign in first"
	}
	btn := func(label string, on, off bool) string {
		st := lipgloss.NewStyle()
		switch {
		case on:
			st = st.Reverse(true).Bold(true)
		case off:
			st = st.Faint(true)
		}
		return st.Render(label)
	}
	row := btn("Cancel", focused && d.button == 0, false) + "     " + btn("[ "+start+" ]", focused && d.button == 1, disabled)
	return leftRight("", row, iw)
}

// footer is the one hint line, for the area that has focus.
func (d launchDialog) footer() string {
	var s string
	switch {
	case d.starting:
		s = "Starting…  Esc Cancel"
	case d.focus == launchFocusForm && d.form.Editing():
		s = "↑↓ Choice  Enter Select  Ctrl+S Start  Esc Close"
	case d.focus == launchFocusAccount:
		s = "↑↓ Account  Enter Options  Ctrl+S Start  Esc Back"
	case d.focus == launchFocusSave:
		s = "Space Toggle  Tab Next field  Ctrl+S Start  Esc Back"
	case d.focus == launchFocusButtons:
		s = "←→ Action  Enter Select  Ctrl+S Start  Esc Back"
	default:
		s = "Tab Next field  Shift+Tab Previous  Ctrl+S Start  Esc Back"
	}
	if d.planner {
		s = strings.ReplaceAll(s, "Start", "Request")
	}
	if d.ascii {
		s = strings.NewReplacer("↑↓", "j/k", "←→", "h/l").Replace(s)
	}
	return s
}

// footerFit shortens words, not keys, until the hints fit w, so a narrow pane
// never cuts one mid-word.
func (d launchDialog) footerFit(w int) string {
	s := d.footer()
	for _, r := range [][2]string{
		{"Tab Next field", "Tab Next"}, {"Shift+Tab Previous", "S-Tab Prev"}, {"Enter Options", "Enter"},
		{"Ctrl+S", "^S"}, {"  ", " "},
	} {
		if ansi.StringWidth(s) <= w {
			break
		}
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	return ansi.Truncate(s, w, "…")
}
