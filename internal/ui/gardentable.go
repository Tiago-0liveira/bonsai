package ui

// gardenTableCapacity is how many rows fit under the Table's header row. The
// shared work header takes two lines, the column header one and the footer one.
func gardenTableCapacity(rows int) int { return max(1, rows-4) }

// tableScrollFor keeps the focused row inside [scroll, scroll+capacity) without
// moving the anchor when the row is already visible, so Esc from detail
// restores exactly what was on screen.
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

// gardenTableKey moves the shared selection by row; sorting only reorders the
// view, so no key here can reach GitHub.
func (m *Model) gardenTableKey(k string) {
	b := &m.garden.board
	if k == "o" {
		b.sort = b.sort.Next()
		b.rows = b.collection.Rows(b.sort)
		b.tableFocus()
		return
	}
	rows := b.rows
	if len(rows) == 0 {
		return
	}
	i, page := max(0, b.rowIndex()), gardenTableCapacity(m.gardenBoardRows())
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
		i = len(rows) - 1
	default:
		return
	}
	i = max(0, min(i, len(rows)-1))
	b.tableRow = i
	b.selection.Focus(b.collection, rows[i].Item.Issue.ID)
}
