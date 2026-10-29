package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const tableGap = 2

type tableCols struct{ issue, title, state, signal, next, typ, prio int }

// Optional columns leave at least 28 cells for the title. STATE includes the
// exact card label, including its glyph and local planner qualifier.
func gardenTableCols(width, issueWidth int) tableCols {
	c := tableCols{issue: issueWidth}
	for s := work.Column(0); s < work.ColumnCount; s++ {
		c.state = max(c.state, ansi.StringWidth(boardState(work.Record{Column: s})))
	}
	c.title = max(12, width-2-c.issue-tableGap*2-c.state)
	add := func(w int, dst *int) {
		if c.title-w-tableGap >= 28 {
			*dst = w
			c.title -= w + tableGap
		}
	}
	add(8, &c.next)
	if c.next == 0 || c.title-18-tableGap < 28 {
		return c
	}
	add(min(30, c.title-28-tableGap), &c.signal)
	if c.next > 0 {
		extra := min(22-c.next, c.title-28)
		c.next += extra
		c.title -= extra
	}
	add(2, &c.prio)
	add(8, &c.typ)
	return c
}

func tableCell(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func gardenNext(r work.Record, short bool) string {
	need, _ := work.Attention(r)
	var a, b string
	switch need {
	case work.NeedInput:
		a, b = "Answer", "Answer the agent"
	case work.NeedPlanApproval:
		a, b = "Review", "Review plan"
	case work.NeedPlanPublish:
		a, b = "Review", "Review and publish plan"
	case work.NeedStart:
		a, b = "Start", "Start implementation"
	case work.NeedHumanReview:
		a, b = "Review", "Review changes"
	case work.NeedBlocker:
		a, b = "Resolve", "Resolve blocker"
	default:
		b = work.NextGate(r.Column)
		a, _, _ = strings.Cut(b, " ")
	}
	if short {
		return a
	}
	return b
}

func gardenTableSignal(r work.Record, attention, localKnown bool) string {
	text := boardActivity(r, false, localKnown)
	if attention {
		_, text = work.Attention(r)
	}
	if warning, _ := gardenApprovalIndicator(r); strings.HasPrefix(warning, "!") {
		text = warning + " · " + text
	}
	return boardTitle(text)
}

func (m Model) tableCells(width int, bg lipgloss.Color, c tableCols, marker string, rail lipgloss.Color, texts [7]string, bold bool) string {
	p := theme.Current
	var sb strings.Builder
	sb.WriteString(seg(rail, bg, true).Render(marker + " "))
	widths := [7]int{c.issue, c.title, c.state, c.signal, c.next, c.typ, c.prio}
	for i, w := range widths {
		if w == 0 {
			continue
		}
		if i > 0 {
			sb.WriteString(seg("", bg, false).Render(strings.Repeat(" ", tableGap)))
		}
		fg := p.Muted
		if i < 2 {
			fg = p.Text
		}
		if i == 2 {
			fg = rail
		}
		sb.WriteString(seg(fg, bg, bold).Render(tableCell(texts[i], w)))
	}
	return shellRow(width, bg, sb.String(), "")
}

func (m Model) gsTable(width, rows int) []string {
	p := theme.Current
	b := m.garden.board
	v := b.tableView()
	attention := b.layout == layoutAttention
	issueW := 5
	for _, r := range v.records {
		issueW = max(issueW, len(fmt.Sprint(r.Item.Issue.Number))+1)
	}
	cols := gardenTableCols(width, issueW)
	capacity := gardenTableCapacity(rows)
	scroll := tableScrollFor(v.scroll, v.line, capacity, len(v.lines))
	group := "None"
	if v.grouped {
		group = "Phase"
	}
	title := "APPROVED WORK"
	if attention {
		title = "NEEDS YOU"
	}
	sort := b.sort.String()
	if width < 100 {
		sort = ""
	}
	s := b.collection.Summarize()
	summary := fmt.Sprintf("● %d active  ◇ %d need you  ! %d blocked", s.Active, s.NeedsYou, s.Blocked)
	if attention {
		summary = "Decisions, questions and blockers"
	}
	out := m.gardenWorkHead(width, title, sort, summary)
	out = append(out, m.tableCells(width, p.Raised, cols, " ", p.Muted, [7]string{"ISSUE", "TITLE", "STATE", "SIGNAL", "NEXT", "TYPE", "P"}, true))
	empty := ""
	switch {
	case !b.loaded || b.snapshot.SuccessfulAt.IsZero():
		empty = "(unknown until a successful read)"
	case len(v.records) == 0 && attention && b.query.Active():
		empty = "(nothing matching this filter needs you)"
	case len(v.records) == 0 && attention:
		empty = "(nothing needs you)"
	case len(v.records) == 0 && b.query.Active():
		empty = "(no approved work matches this filter)"
	case len(v.records) == 0:
		empty = "(no approved work yet)"
	}
	bodyStart := len(out)
	if empty != "" {
		out = append(out, boardLine(width, p.Background, p.Muted, false, " "+empty))
	}
	before, shown, visible := 0, 0, 0
	for i, l := range v.lines {
		if !l.header {
			visible++
		}
		if i < scroll && !l.header {
			before++
		}
		if i < scroll || i >= scroll+capacity {
			continue
		}
		bg := p.Surface
		if i == v.line {
			bg = p.Raised
		}
		if l.header {
			out = append(out, boardLine(width, bg, boardPhaseColor(l.phase), true, m.gardenGroupHeader(l, v.collapsed[l.phase])))
			continue
		}
		shown++
		r := l.rec
		selected := r.Item.Issue.ID == b.selection.ID
		marker := "▏"
		if selected {
			marker = "▌"
		}
		if m.agentGlyphsASCII() {
			marker = "|"
			if selected {
				marker = ">"
			}
		}
		rail := boardPhaseColor(r.Phase)
		if r.Column == work.Blocked {
			rail = p.Danger
		}
		texts := [7]string{fmt.Sprintf("#%d", r.Item.Issue.Number), boardTitle(r.Item.Issue.Title), boardState(r), gardenTableSignal(r, attention, m.garden.local.loaded && !m.garden.local.stale), gardenNext(r, cols.next <= 8), string(r.Item.Classification.Type), r.Priority}
		out = append(out, m.tableCells(width, bg, cols, marker, rail, texts, selected))
	}
	for len(out) < bodyStart+capacity {
		out = append(out, blankRow(width, p.Background))
	}
	out = out[:bodyStart+capacity]
	if gardenTableStrip(rows) > 0 {
		if len(v.lines) > 0 && v.lines[v.line].header {
			l := v.lines[v.line]
			action := "collapse"
			if v.collapsed[l.phase] {
				action = "expand"
			}
			out = append(out,
				boardLine(width, p.Background, boardPhaseColor(l.phase), true, m.gardenGroupHeader(l, v.collapsed[l.phase])),
				blankRow(width, p.Background),
				boardLine(width, p.Raised, p.Warning, true, "Enter "+action))
		} else if r, _, ok := b.collection.Find(b.selection.ID); ok && b.rowIndex() >= 0 {
			out = append(out,
				boardLine(width, p.Background, p.Warning, true, fmt.Sprintf("#%d  %s", r.Item.Issue.Number, gardenTableSignal(r, attention, m.garden.local.loaded && !m.garden.local.stale))),
				boardLine(width, p.Background, p.Text, false, boardTitle(r.Item.Issue.Title)),
				boardLine(width, p.Raised, p.Warning, true, "Enter open · next: "+gardenNext(r, false)))
		} else {
			for range 3 {
				out = append(out, blankRow(width, p.Background))
			}
		}
	}
	footer := "No rows"
	if len(v.records) > 0 {
		first, last := 0, 0
		if shown > 0 {
			first, last = before+1, before+shown
		}
		footer = fmt.Sprintf("Rows %d–%d of %d", first, last, visible)
	}
	if hidden := len(v.records) - visible; hidden > 0 {
		footer += fmt.Sprintf(" (%d hidden)", hidden)
	}
	if attention {
		footer += fmt.Sprintf(" · %d of %d need you", len(v.records), len(b.collection.Records))
	}
	footer += " · sort: " + b.sort.String() + " · Group: " + group
	out = append(out, gText(width, p.Muted, false, footer))
	return m.settingsASCII(out[:rows])
}

func (m Model) gardenGroupHeader(l tableLine, collapsed bool) string {
	glyph, suffix := "▾", ""
	if m.agentGlyphsASCII() {
		glyph = "v"
	}
	if collapsed {
		glyph, suffix = "▸", " (collapsed)"
		if m.agentGlyphsASCII() {
			glyph = ">"
		}
	}
	return fmt.Sprintf("%s %s  %d%s", glyph, l.phase, l.group, suffix)
}
