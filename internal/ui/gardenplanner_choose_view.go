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

const (
	// plannerWideMin is the pane width from which the planner's account choice gets two columns.
	plannerWideMin = 100
	plannerGap     = 3
)

// plannerWide reports whether the account choice uses the two-column layout; a narrower pane keeps the
// shared single-column dialog.
func (d launchDialog) plannerWide() bool {
	return d.planner && d.w >= plannerWideMin && d.Listed() && d.ListErr() == ""
}

// composeChoose lays out exactly h lines of the planner's account choice: the issue, the read-only contract as a
// chip, the contract panel on the left and the account, options and actions on the right. Blank rows and panel
// padding go first in a short pane; the buttons never do.
func (d launchDialog) composeChoose(a agentsapi.Account, iw, h int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	rule := dim.Render(strings.Repeat(d.rule(), iw))
	bottom := []string{rule, dim.Render(d.footerFit(iw))}

	heading := "Request AI plan"
	if d.issue != "" {
		heading = strings.ToUpper(d.issue)
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render(ansi.Truncate(heading, max(iw-26, 8), "…"))
	chip := d.contractChip(a, iw)
	head := []string{leftRight(title, dim.Render("read-only / no worktree"), iw), chip}
	if h-4-len(bottom) >= 14 {
		head = []string{head[0], "", chip, ""}
	}

	body := max(h-len(head)-len(bottom), 0)
	leftW := 36
	if iw >= 140 {
		leftW = 40
	}
	rw := iw - leftW - plannerGap
	left := d.chooseLeft(a, leftW, body)
	right := d.chooseRight(a, rw, body)
	gap := strings.Repeat(" ", plannerGap)

	lines := append([]string(nil), head...)
	for i := 0; i < body; i++ {
		lines = append(lines, left[i]+gap+fit(right[i], rw))
	}
	lines = append(lines, bottom...)
	if len(lines) > h && h > 0 {
		lines = append(lines[:h-1], lines[len(lines)-1])
	}
	return lines
}

// contractChip is the permission the chosen account would run with, as a chip, or why it cannot plan.
func (d launchDialog) contractChip(a agentsapi.Account, iw int) string {
	th := theme.Current
	ok, bad := "✓", "✗"
	if d.ascii {
		ok, bad = "[ok]", "[x]"
	}
	text, fg := ok+" Read-only / no worktree", th.Success
	if !a.ReadOnly.Supported {
		text, fg = bad+" "+plannerUnavailableText(a), th.Danger
	}
	return seg(fg, th.Raised, true).Render(" " + ansi.Truncate(text, max(iw-2, 1), "…") + " ")
}

// chooseLeft is the tinted contract panel: what enforces read-only and what the session may still do, taken from
// the provider's own verdict.
func (d launchDialog) chooseLeft(a agentsapi.Account, w, h int) []string {
	th := theme.Current
	row := func(fg lipgloss.Color, bold bool, s string) string {
		return shellRow(w, th.Surface, seg(fg, th.Surface, bold).Render("  "+s), "")
	}
	wrap := func(fg lipgloss.Color, text string) (out []string) {
		for _, l := range strings.Split(ansi.Wrap(text, max(w-4, 1), ""), "\n") {
			out = append(out, row(fg, false, l))
		}
		return out
	}
	sections := [][]string{{row(th.Muted, true, "READ-ONLY / LOCAL MACHINE")}}
	var what []string
	if a.ReadOnly.Supported {
		what = wrap(th.Text, a.ReadOnly.Mechanism)
		for _, allow := range a.ReadOnly.Allows {
			what = append(what, wrap(th.Muted, "· "+allow)...)
		}
	} else {
		what = wrap(th.Danger, plannerUnavailableText(a))
	}
	sections = append(sections, what, wrap(th.Muted, "No worktree is created."))

	size := 0
	for _, s := range sections {
		size += len(s)
	}
	// Spacing shrinks from the generous layout to none before any text is cut.
	pad, sep := 1, 2
	for (pad+sep*(len(sections)-1)+size > h) && (pad > 0 || sep > 0) {
		if pad > 0 {
			pad = 0
		} else {
			sep--
		}
	}
	blank := row(th.Text, false, "")
	out := make([]string, 0, h)
	for range pad {
		out = append(out, blank)
	}
	for i, s := range sections {
		if i > 0 {
			for range sep {
				out = append(out, blank)
			}
		}
		out = append(out, s...)
	}
	for len(out) < h {
		out = append(out, blank)
	}
	return out[:h]
}

// chooseRight is the account table, the options, the save box, the capacity of the chosen account and the actions
// pinned to the last row, in exactly h lines.
func (d launchDialog) chooseRight(a agentsapi.Account, w, h int) []string {
	th := theme.Current
	danger := lipgloss.NewStyle().Foreground(th.Danger)

	tbl := d.table()
	useTable := tbl.Fits(w)
	tableNeed, rowH := 1, 1
	if useTable {
		tableNeed, rowH = tbl.Height(w), tbl.RowHeight(w)
	}
	formLines, from, to := d.formLines(w)
	tail := []string{d.saveRow()}
	if d.err != "" {
		errLines := strings.Split(ansi.Wrap(d.err, max(w-2, 1), ""), "\n")
		for _, l := range errLines[:min(len(errLines), 2)] {
			tail = append(tail, danger.Render("  "+l))
		}
	}
	capacity := d.capacityLines(a, w)
	avail := h - 1 - len(tail) // the actions row and the save box

	spare := avail - tableNeed - len(formLines)
	take := func(n int) bool {
		if spare < n {
			return false
		}
		spare -= n
		return true
	}
	gapForm, gapTail := take(1), take(1)
	showCap := len(capacity) > 0 && take(len(capacity))
	gapCap := showCap && take(1)
	// Fields breathe like the design's rows while nothing is being edited; an open dropdown needs its lines packed.
	if len(formLines) > 1 && !d.form.Editing() && take(len(formLines)-1) {
		spaced := make([]string, 0, 2*len(formLines)-1)
		for i, l := range formLines {
			if i > 0 {
				spaced = append(spaced, "")
			}
			spaced = append(spaced, l)
		}
		formLines, from, to = spaced, 2*from, max(2*to-1, 0)
	}
	if gapForm {
		avail--
	}
	if gapTail {
		avail--
	}
	if showCap {
		avail -= len(capacity)
		if gapCap {
			avail--
		}
	}

	// Table rows yield before the options do, but the table keeps a few rows (or the selected one when very short).
	tableH, formH := tableNeed, len(formLines)
	if tableNeed+formH > avail {
		tableH = max(avail-formH, 0)
		if floor := min(tableNeed, 1+3*rowH+1); tableH < floor {
			tableH = min(max(avail-min(formH, 3), rowH), floor)
		}
		tableH = min(tableH, max(avail, 0))
		formH = max(avail-tableH, 0)
	}
	var tableLines []string
	if useTable {
		tableLines = tbl.View(w, tableH)
	} else {
		tableLines = []string{d.sidebar().Selector(d.focus == launchFocusAccount)}
	}
	window, _ := scrollWindow(formLines, from, to, formH)

	var out []string
	blank := func(on bool) {
		if on {
			out = append(out, "")
		}
	}
	out = append(out, tableLines...)
	blank(gapForm)
	out = append(out, window...)
	blank(gapTail)
	out = append(out, tail...)
	if showCap {
		blank(gapCap)
		out = append(out, capacity...)
	}
	out = out[:min(len(out), max(h-1, 0))]
	for len(out) < h-1 {
		out = append(out, "")
	}
	return append(out, d.chooseButtons(a, w))
}

// capacityLines is the five-hour capacity of the chosen account, or nothing when no value is known.
func (d launchDialog) capacityLines(a agentsapi.Account, w int) []string {
	th := theme.Current
	meter, used, ok := accounts.UsedMeter(d.Usage(), a, d.now(), min(20, max(w-16, 1)), d.ascii)
	if !ok {
		return nil
	}
	return []string{
		"  " + meter + "  " + fmt.Sprintf("%d%% used", used),
		lipgloss.NewStyle().Foreground(th.Dim).Render("  5h capacity / selected account"),
	}
}

// chooseButtons is Cancel and the primary action at the bottom right of the column.
func (d launchDialog) chooseButtons(a agentsapi.Account, w int) string {
	th := theme.Current
	focused := d.focus == launchFocusButtons
	label, disabled := d.primaryLabel(a)
	primary := gButton(label, focused && d.button == 1)
	if disabled {
		primary = seg(th.Dim, th.Raised, false).Render(" " + label + " ")
	}
	row := gButton("Cancel", focused && d.button == 0) + "   " + primary
	return leftRight("", row, w)
}
