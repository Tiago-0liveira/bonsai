package ui

import (
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
)

type tableView struct {
	grouped              bool
	collapsed            [work.PhaseCount + 1]bool
	line, scroll, anchor int
	collapseNotice       string
	// Cached slices are replaced, never mutated across Bubble Tea model copies.
	records []work.Record
	lines   []tableLine
}

type tableLine struct {
	header bool
	group  int
	phase  work.Phase
	rec    work.Record
}

func gardenTableStrip(rows int) int {
	if rows >= 18 {
		return 3
	}
	return 0
}
func gardenTableCapacity(rows int) int { return max(1, rows-4-gardenTableStrip(rows)) }

// tableScrollFor keeps the anchor when the focused line is already visible.
func tableScrollFor(scroll, row, capacity, n int) int {
	capacity = max(1, capacity)
	scroll = max(0, min(scroll, n-capacity))
	row = max(0, min(row, n-1))
	if row < scroll {
		return row
	}
	if row >= scroll+capacity {
		return row - capacity + 1
	}
	return scroll
}

func (b *gardenBoardState) tableView() *tableView {
	if b.layout == layoutAttention {
		return &b.attn
	}
	return &b.table
}
func (v *tableView) rebuild(rows []work.Record) {
	v.records = rows
	lines := make([]tableLine, 0, len(rows)+int(work.PhaseCount)+1)
	if !v.grouped {
		for _, r := range rows {
			lines = append(lines, tableLine{rec: r, phase: r.Phase})
		}
	} else {
		for _, g := range work.GroupByPhase(rows) {
			lines = append(lines, tableLine{header: true, group: len(g.Records), phase: g.Phase})
			if !v.collapsed[g.Phase] {
				for _, r := range g.Records {
					lines = append(lines, tableLine{rec: r, phase: g.Phase})
				}
			}
		}
	}
	v.lines = lines
}
func (b *gardenBoardState) rebuildTables() {
	b.table.rebuild(b.rows)
	b.attn.rebuild(work.NeedsYou(b.rows))
}
func (v *tableView) headerIndex(phase work.Phase) int {
	for i, l := range v.lines {
		if l.header && l.phase == phase {
			return i
		}
	}
	return -1
}
func (b *gardenBoardState) rowIndex() int {
	for i, r := range b.tableView().records {
		if r.Item.Issue.ID == b.selection.ID {
			return i
		}
	}
	return -1
}
func (b *gardenBoardState) tableFocus() string {
	v := b.tableView()
	if i := b.rowIndex(); i >= 0 {
		v.anchor = i
		b.selection.Focus(b.collection, b.selection.ID)
		for j, l := range v.lines {
			if !l.header && l.rec.Item.Issue.ID == b.selection.ID {
				v.line = j
				return ""
			}
		}
		r := v.records[i]
		phase := r.Phase
		if !r.Placed {
			phase = work.PhaseCount
		}
		v.line = max(0, v.headerIndex(phase))
		return ""
	}
	left, was := b.selection.ID != 0, v.anchor
	v.anchor = max(0, min(was, len(v.records)-1))
	if len(v.records) == 0 {
		v.line = 0
		if b.layout == layoutAttention {
			return ""
		} // Keep the Board's identity when nothing needs attention.
		b.selection.ID = 0
		if left {
			return "Selected issue left this view; no matching work remains."
		}
		return ""
	}
	r := v.records[v.anchor]
	b.selection.Focus(b.collection, r.Item.Issue.ID)
	b.tableFocus()
	if !left {
		return ""
	}
	if b.layout == layoutAttention {
		return fmt.Sprintf("Selected issue does not need you; selected #%d.", r.Item.Issue.Number)
	}
	if was >= len(v.records) {
		return "Selected issue left this view; selected the previous row."
	}
	return "Selected issue left this view; selected the row now in its place."
}

func (b *gardenBoardState) toggleGroup() {
	v := b.tableView()
	if !v.grouped || len(v.lines) == 0 {
		return
	}
	phase := v.lines[v.line].phase
	v.collapsed[phase] = !v.collapsed[phase]
	v.rebuild(v.records)
	b.tableFocus()
	if v.collapsed[phase] {
		v.line = max(0, v.headerIndex(phase))
		if i := b.rowIndex(); i >= 0 {
			r := v.records[i]
			if r.Phase == phase && r.Placed || !r.Placed && phase == work.PhaseCount {
				v.collapseNotice = fmt.Sprintf("%s collapsed; selected #%d is hidden inside.", phase, r.Item.Issue.Number)
				b.notice = v.collapseNotice
			}
		}
	} else {
		// Expanding another group leaves its header focused.
		if i := b.rowIndex(); i < 0 || (v.records[i].Placed && v.records[i].Phase != phase) || (!v.records[i].Placed && phase != work.PhaseCount) {
			v.line = max(0, v.headerIndex(phase))
		}
		if b.notice == v.collapseNotice {
			b.notice = ""
		}
		v.collapseNotice = ""
	}
}

func (m *Model) gardenTableKey(k string) {
	b := &m.garden.board
	v := b.tableView()
	switch k {
	case "o":
		b.sort = b.sort.Next()
		b.rows = b.collection.Rows(b.sort)
		b.rebuildTables()
		b.tableFocus()
		return
	case "g":
		v.grouped = !v.grouped
		v.rebuild(v.records)
		b.tableFocus()
		return
	case " ", "space":
		b.toggleGroup()
		return
	}
	if len(v.lines) == 0 {
		return
	}
	i, page := v.line, gardenTableCapacity(m.gardenBoardRows())
	switch k {
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup":
		i -= page
	case "pgdown":
		i += page
	case "home":
		i = 0
	case "end":
		i = len(v.lines) - 1
	default:
		return
	}
	v.line = max(0, min(i, len(v.lines)-1))
	l := v.lines[v.line]
	if !l.header {
		b.selection.Focus(b.collection, l.rec.Item.Issue.ID)
		v.anchor = b.rowIndex()
	}
}
