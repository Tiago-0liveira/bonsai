package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	tableInspectorMin = 34
	// Optional columns join only while the title keeps this much room, so the
	// task identity is never traded for metadata.
	tableMinTitle = 44
	tableGap      = 2
)

// tableCols are the widths of the visible columns; 0 hides an optional one.
// Marker, ISSUE, TITLE and STATE are always present and STATE is never narrower
// than the longest state name.
type tableCols struct{ issue, title, typ, state, local, prio, status int }

// tableLocalMinTitle is the title room the LOCAL column may leave: it is the answer to "is my planner still
// running", so it outranks the optional columns, but never the title.
const tableLocalMinTitle = 28

// gardenTableCols lays out the columns. localLong and localShort are the widest long and short local badges of
// the rows (0 when no row has one); the short form is tried when the long one does not fit.
func gardenTableCols(width, issueWidth int, inspector bool, localLong, localShort int) tableCols {
	c := tableCols{issue: issueWidth}
	for s := work.Column(0); s < work.ColumnCount; s++ {
		c.state = max(c.state, len(s.String()))
	}
	// lead + marker, then a gap before every column after ISSUE
	fixed := 2 + c.issue + tableGap*2 + c.state
	c.title = max(12, width-fixed)
	for _, w := range []int{localLong, localShort} {
		if w > 0 && c.title-w-tableGap >= tableLocalMinTitle {
			c.local = w
			c.title -= w + tableGap
			break
		}
	}
	try := func(w int, set *int) {
		if c.title-w-tableGap >= tableMinTitle {
			*set = w
			c.title -= w + tableGap
		}
	}
	// The inspector already names the type, so there the priority is tried first.
	if inspector {
		try(2, &c.prio)
		try(8, &c.typ)
	} else {
		try(8, &c.typ)
		try(2, &c.prio)
	}
	try(16, &c.status)
	return c
}

// cell clips to w by display width and pads, so wide runes never overflow.
func tableCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func (m Model) gardenTableLine(width int, bg lipgloss.Color, c tableCols, marker string, mfg lipgloss.Color, texts [6]string, fgs [6]lipgloss.Color, bold bool) string {
	return m.gardenTableLine7(width, bg, c, marker, mfg,
		[7]string{texts[0], texts[1], texts[2], texts[3], "", texts[4], texts[5]},
		[7]lipgloss.Color{fgs[0], fgs[1], fgs[2], fgs[3], fgs[3], fgs[4], fgs[5]}, bold)
}

// gardenTableLine7 is the Table's own row, with the LOCAL cell between STATE and P.
func (m Model) gardenTableLine7(width int, bg lipgloss.Color, c tableCols, marker string, mfg lipgloss.Color, texts [7]string, fgs [7]lipgloss.Color, bold bool) string {
	var sb strings.Builder
	sb.WriteString(seg(mfg, bg, true).Render(" " + marker))
	widths := [7]int{c.issue, c.title, c.typ, c.state, c.local, c.prio, c.status}
	for i, w := range widths {
		if w == 0 {
			continue
		}
		if i > 0 {
			sb.WriteString(seg("", bg, false).Render(strings.Repeat(" ", tableGap)))
		}
		sb.WriteString(seg(fgs[i], bg, bold || i == 0).Render(tableCell(texts[i], w)))
	}
	return shellRow(width, bg, sb.String(), "")
}

func (m Model) gsTable(width, rows int) []string {
	p := theme.Current
	b := m.garden.board
	records := b.rows
	inspector := width >= 120
	tableW, inspW := width, 0
	if inspector {
		inspW = max(tableInspectorMin, width/3)
		tableW = width - inspW - 3
	}
	issueW := 5
	for _, r := range records {
		issueW = max(issueW, len(fmt.Sprint(r.Item.Issue.Number))+1)
	}
	localLong, localShort := 0, 0
	for _, r := range records {
		if lb, ok := m.localBadgeFor(r.Item.Issue.ID); ok {
			localLong = max(localLong, ansi.StringWidth(lb.long))
			localShort = max(localShort, ansi.StringWidth(lb.short), len("LOCAL"))
		}
	}
	cols := gardenTableCols(tableW, issueW, inspector, localLong, localShort)
	capacity := gardenTableCapacity(rows)
	row := b.rowIndex()
	scroll := tableScrollFor(b.tableScroll, row, capacity, len(records))

	title := "APPROVED WORK / ALL STATES"
	if width < 100 {
		title = "APPROVED WORK" // the count and filter chip need the room
	}
	out := m.gardenWorkHead(width, title, b.sort.String(), "")
	body := max(0, rows-3)
	region := make([]string, 0, body)
	head := [7]string{"ISSUE", "TITLE", "TYPE", "STATE", "LOCAL", "P", "STATUS"}
	muted := [7]lipgloss.Color{p.Muted, p.Muted, p.Muted, p.Muted, p.Muted, p.Muted, p.Muted}
	region = append(region, m.gardenTableLine7(tableW, p.Raised, cols, " ", p.Muted, head, muted, true))
	empty := ""
	switch {
	case !b.loaded || b.snapshot.SuccessfulAt.IsZero():
		empty = "(unknown until a successful read)"
	case len(records) == 0 && b.query.Active():
		empty = "(no approved work matches this filter)"
	case len(records) == 0:
		empty = "(no approved work yet)"
	}
	if empty != "" {
		region = append(region, boardLine(tableW, p.Background, p.Muted, false, " "+empty))
	}
	for n := scroll; n < len(records) && n < scroll+capacity; n++ {
		r := records[n]
		selected := r.Item.Issue.ID == b.selection.ID
		bg, fg, marker := p.Surface, p.Text, " "
		if selected {
			bg, fg, marker = p.Raised, p.Success, "▌"
			if m.agentGlyphsASCII() {
				marker = ">"
			}
		}
		status, statusFg := gardenApprovalIndicator(r)
		texts := [7]string{fmt.Sprintf("#%d", r.Item.Issue.Number), boardTitle(r.Item.Issue.Title), string(r.Item.Classification.Type), r.Column.String(), "", r.Priority, status}
		fgs := [7]lipgloss.Color{fg, fg, p.Muted, boardPhaseColor(r.Column.Phase()), p.Muted, p.Muted, statusFg}
		if lb, ok := m.localBadgeFor(r.Item.Issue.ID); ok {
			texts[4], fgs[4] = lb.long, lb.fg
			if ansi.StringWidth(lb.long) > cols.local {
				texts[4] = lb.short
			}
		}
		region = append(region, m.gardenTableLine7(tableW, bg, cols, marker, p.Success, texts, fgs, selected))
	}
	for len(region) < body {
		region = append(region, blankRow(tableW, p.Background))
	}
	region = region[:body]
	if inspector {
		side := m.gardenInspector(inspW, len(region), records, row)
		sep := seg(p.Border, p.Background, false).Render(" " + m.chrome("│") + " ")
		for i := range region {
			region[i] += sep + side[i]
		}
	}
	out = append(out, region...)
	footer := "No rows"
	if len(records) > 0 {
		footer = fmt.Sprintf("Rows %d–%d of %d", scroll+1, min(scroll+capacity, len(records)), len(records))
	}
	out = append(out, gText(width, p.Muted, false, footer+" · sort: "+b.sort.String()))
	return m.settingsASCII(out[:rows])
}

// gardenInspector describes the selected row: identity, state and the next
// human gate. It reads the same record the table highlights.
func (m Model) gardenInspector(w, n int, records []work.Record, row int) []string {
	p := theme.Current
	var lines []string
	line := func(bg, fg lipgloss.Color, bold bool, s string) {
		lines = append(lines, boardLine(w, bg, fg, bold, " "+s))
	}
	blank := func() { line(p.Background, p.Muted, false, "") }
	if row < 0 {
		line(p.Background, p.Muted, false, "No item selected")
	} else {
		r := records[row]
		line(p.Background, p.Text, true, fmt.Sprintf("#%d / %s", r.Item.Issue.Number, r.Item.Classification.Type))
		blank()
		title := strings.Split(ansi.Wrap(boardTitle(r.Item.Issue.Title), max(1, w-2), ""), "\n")
		for i := 0; i < min(3, len(title)); i++ {
			text := title[i]
			if i == 2 && len(title) > 3 {
				text = ansi.Truncate(text, max(1, w-3), "") + "…"
			}
			line(p.Background, p.Text, false, text)
		}
		blank()
		line(p.Raised, boardPhaseColor(r.Column.Phase()), true, " "+r.Column.String())
		status, fg := gardenApprovalIndicator(r)
		line(p.Background, fg, false, status)
		if lb, ok := m.localBadgeFor(r.Item.Issue.ID); ok {
			line(p.Background, lb.fg, false, "local: "+lb.long)
		}
		blank()
		line(p.Background, p.Muted, true, "NEXT HUMAN GATE")
		for _, l := range strings.Split(ansi.Wrap(work.NextGate(r.Column), max(1, w-2), ""), "\n") {
			line(p.Background, p.Text, false, l)
		}
		blank()
		line(p.Raised, p.Success, true, " Enter open item")
	}
	for len(lines) < n {
		blank()
	}
	return lines[:n]
}
