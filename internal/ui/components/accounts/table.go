package accounts

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	tableGutter     = 2  // selection marker and its space
	tableGap        = 2  // between columns
	tableMinAccount = 10 // narrowest ACCOUNT column that is still worth showing
	tableMaxAccount = 28 // names beyond this stop widening the column
	tableMinProv    = 8  // width of the PROVIDER heading
	// tableWantAccount keeps short names from collapsing the column, so the
	// other columns line up the same way across lists.
	tableWantAccount = 16
)

// Column is a data column that follows the fixed ACCOUNT and PROVIDER columns
// of a Table. The Accounts view uses PLAN and CONNECTION; the launcher uses
// the five-hour capacity and its reset countdown.
type Column struct {
	Title string
	// Width is the cell count the column asks for; longer cells are elided.
	Width int
	// Cell renders the (already styled) cell text for an account.
	Cell func(agentsapi.Account) string
	// Optional columns are the first to go when the table is narrow.
	Optional bool
	// Under columns move below the account name, on a second line of the row,
	// when dropping the optional ones is not enough.
	Under bool
	// Narrow lists ever tighter forms of the column. They are tried, in order,
	// before an optional column is dropped, so a column can shrink its meter
	// and then lose it while keeping the number.
	Narrow []Variant
	// UnderCell replaces Cell on the second line of a two-line row, where the
	// cell can afford a label ("5h left 72%").
	UnderCell func(agentsapi.Account) string
}

// Variant is a narrower form of a Column.
type Variant struct {
	// Title replaces the column heading when set.
	Title string
	Width int
	Cell  func(agentsapi.Account) string
}

// at is the column in its stage-th narrow form (0 is the full form).
func (c Column) at(stage int) Column {
	if stage == 0 || len(c.Narrow) == 0 {
		return c
	}
	v := c.Narrow[min(stage, len(c.Narrow))-1]
	if v.Title != "" {
		c.Title = v.Title
	}
	c.Width, c.Cell = v.Width, v.Cell
	return c
}

// Table is the scrollable account table shared by the Accounts view and the
// launcher. It keeps the selected row visible, shows a range indicator when
// rows are hidden and adapts to narrow widths and short heights, so callers
// only choose the columns.
type Table struct {
	// Accounts in display order (Browser.Ordered).
	Accounts []agentsapi.Account
	Selected agents.AccountID
	Columns  []Column
	ASCII    bool
	// Focused marks the selection with the accent; an unfocused table keeps the
	// marker but quiets it, so only one area looks active.
	Focused bool
	// UnderSep joins the cells of a two-line row's second line; two spaces
	// when empty.
	UnderSep string
}

// tableLayout is the column plan chosen for a width.
type tableLayout struct {
	account, provider int
	cols              []Column // columns drawn in the row line
	under             []Column // columns drawn below the account name
}

func (l tableLayout) rowHeight() int {
	if len(l.under) > 0 {
		return 2
	}
	return 1
}

// layout picks the widest column set that leaves ACCOUNT readable: all
// columns, then without the optional ones, then with the rest folded under the
// name. ok is false when even that does not fit.
func (t Table) layout(w int) (tableLayout, bool) {
	provW := tableMinProv
	nameW := 0
	for _, a := range t.Accounts {
		provW = max(provW, ansi.StringWidth(providerName(a.Provider)))
		nameW = max(nameW, ansi.StringWidth(a.Name))
	}
	want := min(max(nameW+2, tableWantAccount), tableMaxAccount)

	stages := 1
	for _, c := range t.Columns {
		stages = max(stages, len(c.Narrow)+1)
	}
	// forms is the column set in its stage-th narrow form.
	forms := func(stage int, keep func(Column) bool) []Column {
		var out []Column
		for _, c := range t.Columns {
			if keep(c) {
				out = append(out, c.at(stage))
			}
		}
		return out
	}
	everyone := func(Column) bool { return true }
	notOptional := func(c Column) bool { return !c.Optional }
	try := func(cols, under []Column) (tableLayout, bool) {
		used := tableGutter + provW + tableGap
		for _, c := range cols {
			used += c.Width + tableGap
		}
		acct := w - used
		if acct < tableMinAccount {
			return tableLayout{}, false
		}
		return tableLayout{account: min(acct, want), provider: provW, cols: cols, under: under}, true
	}
	for s := range stages {
		if l, ok := try(forms(s, everyone), nil); ok {
			return l, true
		}
	}
	for s := range stages {
		if l, ok := try(forms(s, notOptional), nil); ok {
			return l, true
		}
	}
	var inline, under []Column
	for _, c := range forms(0, notOptional) {
		if c.Under {
			under = append(under, c)
		} else {
			inline = append(inline, c)
		}
	}
	return try(inline, under)
}

// Fits reports whether the table can draw at width w; callers fall back to a
// single-account selector when it cannot.
func (t Table) Fits(w int) bool {
	_, ok := t.layout(w)
	return ok
}

// Height is the line count that shows every account at width w, including the
// heading.
func (t Table) Height(w int) int {
	l, ok := t.layout(w)
	if !ok {
		return 1 + len(t.Accounts)
	}
	return 1 + max(len(t.Accounts), 1)*l.rowHeight()
}

// RowHeight is the lines one account takes at width w: 2 once the columns have
// moved under the name.
func (t Table) RowHeight(w int) int {
	l, _ := t.layout(w)
	return max(l.rowHeight(), 1)
}

// View draws at most h lines of exactly w cells: a heading, then the rows
// around the selection, then a range indicator when rows are hidden. Very
// short heights show only the selected row. It returns nil when the table does
// not fit the width.
func (t Table) View(w, h int) []string {
	l, ok := t.layout(w)
	if !ok || h <= 0 {
		return nil
	}
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	if len(t.Accounts) == 0 {
		lines := []string{t.heading(l, w), dim.Render("No accounts yet.")}
		return padLines(lines[:min(h, len(lines))], w)
	}

	rh := l.rowHeight()
	n := len(t.Accounts)
	sel := max(t.selectedIndex(), 0)

	var lines []string
	switch {
	case 1+n*rh <= h:
		lines = append(lines, t.heading(l, w))
		for _, a := range t.Accounts {
			lines = append(lines, t.row(a, l, w)...)
		}
	case h-1 >= rh:
		// Heading plus a window of rows; the indicator takes a line when at
		// least one row still fits beside it.
		visible := (h - 2) / rh
		indicator := visible >= 1
		if !indicator {
			visible = 1
		}
		start := min(max(sel-(visible-1)/2, 0), n-visible)
		lines = append(lines, t.heading(l, w))
		for _, a := range t.Accounts[start : start+visible] {
			lines = append(lines, t.row(a, l, w)...)
		}
		if indicator {
			lines = append(lines, dim.Render(strings.Repeat(" ", tableGutter)+t.rangeText(start+1, start+visible, n)))
		}
	default:
		lines = t.row(t.Accounts[sel], l, w)
	}
	return padLines(lines[:min(len(lines), h)], w)
}

func (t Table) rangeText(from, to, total int) string {
	return fmt.Sprintf("%d-%d of %d", from, to, total)
}

func (t Table) selectedIndex() int {
	for i, a := range t.Accounts {
		if a.ID == t.Selected {
			return i
		}
	}
	return -1
}

func padLines(lines []string, w int) []string {
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return lines
}

func (t Table) heading(l tableLayout, w int) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	row := strings.Repeat(" ", tableGutter) + cell("ACCOUNT", l.account) + strings.Repeat(" ", tableGap) + cell("PROVIDER", l.provider)
	for _, c := range l.cols {
		row += strings.Repeat(" ", tableGap) + cell(c.Title, c.Width)
	}
	return dim.Render(fit(row, w))
}

// cell cuts or pads text to exactly w cells, eliding with an ellipsis.
func cell(text string, w int) string {
	if ansi.StringWidth(text) > w {
		text = ansi.Truncate(text, w, "…")
	}
	return text + strings.Repeat(" ", max(w-ansi.StringWidth(text), 0))
}

func (t Table) row(a agentsapi.Account, l tableLayout, w int) []string {
	th := theme.Current
	selected := a.ID == t.Selected
	marker := " "
	if selected {
		mark := "▸"
		if t.ASCII {
			mark = ">"
		}
		style := lipgloss.NewStyle().Bold(true)
		if t.Focused {
			style = style.Foreground(th.Accent)
		} else {
			style = style.Foreground(th.Dim)
		}
		marker = style.Render(mark)
	}
	name := lipgloss.NewStyle().Foreground(th.Text)
	if selected {
		name = name.Bold(true)
	}
	row := marker + " " +
		name.Render(cell(a.Name, l.account)) + strings.Repeat(" ", tableGap) +
		theme.ProviderStyle(a.Provider).Render(cell(providerName(a.Provider), l.provider))
	for _, c := range l.cols {
		row += strings.Repeat(" ", tableGap) + cellStyled(c.Cell(a), c.Width)
	}
	lines := []string{row}
	if len(l.under) > 0 {
		var parts []string
		for _, c := range l.under {
			if c.UnderCell != nil {
				parts = append(parts, c.UnderCell(a))
			} else {
				parts = append(parts, c.Cell(a))
			}
		}
		sep := t.UnderSep
		if sep == "" {
			sep = "  "
		}
		lines = append(lines, strings.Repeat(" ", tableGutter)+ansi.Truncate(strings.Join(parts, sep), max(w-tableGutter, 1), "…"))
	}
	return lines
}

// cellStyled is cell for text that already carries styling; truncation keeps
// the escape sequences balanced.
func cellStyled(text string, w int) string {
	if ansi.StringWidth(text) > w {
		return ansi.Truncate(text, w, "…") + "\x1b[0m"
	}
	return text + strings.Repeat(" ", w-ansi.StringWidth(text))
}
