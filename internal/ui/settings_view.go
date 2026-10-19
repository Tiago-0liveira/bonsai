package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// settingsSubmenuRows are the breadcrumb, category row and rule.
	settingsSubmenuRows = 3
	// settingsWideMin is the width from which the section list is a side panel;
	// below it the sections collapse into one row above the fields.
	settingsWideMin = 100
	settingsPanelW  = 28
	settingsGap     = 2
)

// settingsGlyphs extends the shared ASCII map with the glyphs only Settings
// draws. Every replacement is one cell, like the original, so widths hold.
var settingsGlyphs = strings.NewReplacer("›", ">", "▸", ">", "▌", "|", "⚙", "=", "—", "-")

// settingsASCII maps a finished line when the ASCII fallback is on.
func (m Model) settingsASCII(lines []string) []string {
	if !m.agentGlyphsASCII() {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = m.chrome(settingsGlyphs.Replace(l))
	}
	return out
}

func blankRow(width int, bg lipgloss.Color) string { return shellRow(width, bg, "", "") }

// settingsSubmenu renders the breadcrumb, the category row and a thin rule.
func (m Model) settingsSubmenu(width int) []string {
	p := theme.Current
	cat, sec := m.curCategory(), m.curSection()
	sep := seg(p.Muted, p.Surface, false).Render(" › ")
	crumb := seg(p.Muted, p.Surface, false).Render(" Settings") + sep +
		seg(p.Muted, p.Surface, false).Render(cat.name) + sep +
		seg(p.Text, p.Surface, true).Render(sec.name)

	var b strings.Builder
	b.WriteString(seg("", p.Surface, false).Render(" "))
	for i, c := range settingCategories {
		label := " " + c.name + " "
		switch {
		case i == m.set.cat && m.set.level == levelCategory:
			b.WriteString(seg(p.Background, p.Accent, true).Render(label))
		case i == m.set.cat:
			// Underline keeps the parent visible when Raised matches the base.
			b.WriteString(seg(p.Accent, p.Raised, true).Underline(true).Render(label))
		default:
			b.WriteString(seg(p.Muted, p.Surface, false).Render(label))
		}
		b.WriteString(seg("", p.Surface, false).Render(" "))
	}
	rule := seg(p.Border, p.Background, false).Render(strings.Repeat("─", width))
	return m.settingsASCII([]string{
		shellRow(width, p.Surface, crumb, ""),
		shellRow(width, p.Surface, b.String(), ""),
		rule,
	})
}

// settingsBody renders exactly rows lines of exactly width cells.
func (m Model) settingsBody(width, rows int) []string {
	p := theme.Current
	cat := m.curCategory()
	title := seg(p.Text, p.Background, true).Render(" ⚙ " + strings.ToUpper(cat.name))
	chip := seg(p.Muted, p.Raised, false).Render(" Scope: " + m.categoryScope(cat) + " ")
	lines := []string{shellRow(width, p.Background, title, chip+seg("", p.Background, false).Render(" "))}

	rest := rows - 1
	if rest >= 8 {
		lines = append(lines, blankRow(width, p.Background))
		rest--
	}
	if rest > 0 {
		lines = append(lines, m.settingsContent(width, rest)...)
	}
	for len(lines) < rows {
		lines = append(lines, blankRow(width, p.Background))
	}
	return m.settingsASCII(lines[:rows])
}

func (m Model) settingsContent(width, rows int) []string {
	p := theme.Current
	cat, sec := m.curCategory(), m.curSection()
	if width >= settingsWideMin {
		rightW := width - settingsPanelW - settingsGap
		left := m.sectionPanel(settingsPanelW, rows, cat)
		right := m.fieldPane(rightW, rows, sec, true)
		out := make([]string, rows)
		gap := seg("", p.Background, false).Render(strings.Repeat(" ", settingsGap))
		for i := range out {
			out[i] = left[i] + gap + right[i]
		}
		return out
	}
	out := []string{m.sectionStrip(width, cat)}
	rest := rows - 1
	if rest >= 6 {
		out = append(out, blankRow(width, p.Background))
		rest--
	}
	if rest > 0 {
		out = append(out, m.fieldPane(width, rest, sec, false)...)
	}
	return out
}

// window picks the first item to draw so the cursor stays visible, centered
// when the list is longer than the space.
func window(n, visible, cursor int) int {
	if n <= visible || visible <= 0 {
		return 0
	}
	return max(0, min(cursor-visible/2, n-visible))
}

// pitchFor is rows per item: a blank spacer between items when comfortable
// density and the height allow it, otherwise packed.
func (m Model) pitchFor(n, rows int) int {
	if m.work.Density != "compact" && n*2-1 <= rows {
		return 2
	}
	return 1
}

// sectionPanel is the left list of sections (wide layout).
func (m Model) sectionPanel(width, rows int, cat settingCategory) []string {
	p := theme.Current
	panel := func(left string) string { return shellRow(width, p.Surface, left, "") }
	out := []string{panel(seg(p.Muted, p.Surface, true).Render(" IN " + strings.ToUpper(cat.name)))}
	if rows > 3 {
		out = append(out, panel(""))
	}
	listRows := rows - len(out)
	n := len(cat.sections)
	pitch := m.pitchFor(n, listRows)
	visible := (listRows + pitch - 1) / pitch
	start := window(n, visible, m.set.sec)
	for i := start; i < n && i < start+visible; i++ {
		name := cat.sections[i].name
		if i == m.set.sec {
			bar := p.Muted
			if m.set.level == levelSection {
				bar = p.Accent
			}
			out = append(out, shellRow(width, p.Raised, seg(bar, p.Raised, true).Render("▌")+seg(p.Text, p.Raised, true).Render(" "+name), ""))
		} else {
			out = append(out, panel(seg(p.Muted, p.Surface, false).Render("  "+name)))
		}
		if pitch == 2 && len(out) < rows {
			out = append(out, panel(""))
		}
	}
	for len(out) < rows {
		out = append(out, panel(""))
	}
	return out[:rows]
}

// sectionStrip is the one-row section list of the narrow layout; it scrolls
// from the left so the selected section stays visible.
func (m Model) sectionStrip(width int, cat settingCategory) string {
	p := theme.Current
	segs := make([]string, len(cat.sections))
	widths := make([]int, len(cat.sections))
	for i, s := range cat.sections {
		label := " " + s.name + " "
		if i == m.set.sec {
			fg := p.Muted
			if m.set.level == levelSection {
				fg = p.Accent
			}
			segs[i] = seg(fg, p.Raised, true).Underline(true).Render(label)
		} else {
			segs[i] = seg(p.Muted, p.Background, false).Render(label)
		}
		widths[i] = lipgloss.Width(label) + 1
	}
	first, total := 0, 0
	for i := 0; i <= m.set.sec; i++ {
		total += widths[i]
	}
	for first < m.set.sec && total+1 > width {
		total -= widths[first]
		first++
	}
	var b strings.Builder
	b.WriteString(seg("", p.Background, false).Render(" "))
	for i := first; i < len(segs); i++ {
		b.WriteString(segs[i])
		b.WriteString(seg("", p.Background, false).Render(" "))
	}
	return shellRow(width, p.Background, b.String(), "")
}

// fieldPane renders the selected section's fields plus detail lines.
func (m Model) fieldPane(width, rows int, sec settingSection, wide bool) []string {
	p := theme.Current
	blank := blankRow(width, p.Background)
	var out []string
	if wide {
		out = append(out, shellRow(width, p.Background, seg(p.Muted, p.Background, true).Render(" "+strings.ToUpper(sec.name)), ""))
		if rows > 3 {
			out = append(out, blank)
		}
	}
	fields := m.visibleFields(sec)
	avail := rows - len(out)
	var extra []string
	if sec.extra != nil {
		extra = sec.extra(m, width-2)
	}

	// Drop the lower-priority bands (extras, then detail lines) until the list
	// fits; the list itself scrolls when even that is not enough.
	type budget struct{ extra, detail int }
	opts := []budget{{0, 3}, {0, 2}, {0, 1}, {0, 0}}
	if len(extra) > 0 {
		opts = append([]budget{{len(extra) + 1, 3}}, opts...)
	}
	pick := opts[len(opts)-1]
search:
	for _, minList := range []int{len(fields), min(len(fields), 3), 1} {
		for _, o := range opts {
			if avail-o.extra-o.detail >= minList {
				pick = o
				break search
			}
		}
	}
	listRows := max(avail-pick.extra-pick.detail, 0)
	pitch := m.pitchFor(len(fields), listRows)
	visible := (listRows + pitch - 1) / pitch
	cursor := 0
	if m.set.level == levelField {
		cursor = max(min(m.set.field, len(fields)-1), 0)
	}
	start := window(len(fields), visible, cursor)
	var list []string
	for i := start; i < len(fields) && i < start+visible; i++ {
		if pitch == 2 && i > start {
			list = append(list, blank)
		}
		list = append(list, m.fieldRow(width, fields[i], i == cursor && m.set.level == levelField, wide))
	}
	out = append(out, list[:min(len(list), listRows)]...)
	if pick.extra > 0 {
		out = append(out, blank)
		for _, l := range extra {
			out = append(out, shellRow(width, p.Background, l, ""))
		}
	}
	for len(out) < rows-pick.detail {
		out = append(out, blank)
	}
	out = append(out, m.fieldDetail(width, pick.detail, fields, cursor)...)
	for len(out) < rows {
		out = append(out, blank)
	}
	return out[:rows]
}

// fieldRow draws one field: marker, label, value chip, and origin (wide).
func (m Model) fieldRow(width int, f settingField, selected, wide bool) string {
	p := theme.Current
	bg := p.Background
	// Wide enough for the longest label ("Confirm destructive actions").
	labelW := 28
	if width < 78 {
		labelW = 22
	}
	marker, labelStyle := "  ", seg(p.Text, bg, false)
	switch {
	case f.kind == kindUnavailable:
		labelStyle = seg(p.Dim, bg, false)
		if selected {
			marker = seg(p.Accent, bg, true).Render("▸") + " "
		}
	case selected:
		marker = seg(p.Accent, bg, true).Render("▸") + " "
		labelStyle = seg(p.Accent, bg, true)
	}
	if marker == "  " {
		marker = seg("", bg, false).Render(marker)
	}
	label := ansi.Truncate(f.label, labelW-1, "…")
	left := seg("", bg, false).Render(" ") + marker + labelStyle.Render(padRight(label, labelW))

	room := width - lipgloss.Width(left) - 1
	if room < 4 {
		return shellRow(width, bg, left, "")
	}
	if f.kind == kindUnavailable {
		return shellRow(width, bg, left+seg(p.Dim, bg, false).Italic(true).Render(ansi.Truncate(f.note, room, "…")), "")
	}
	text := ""
	switch {
	case f.display != nil:
		text = f.display(m)
	case f.kind == kindChoice || f.kind == kindToggle:
		for _, c := range f.choices {
			if c.value == f.get(m) {
				text = c.label
			}
		}
	default:
		text = f.get(m)
	}

	var value string
	switch {
	case m.set.capturing && selected:
		value = seg(p.Background, p.Accent, true).Render("[press a key]")
	case m.set.editing && selected:
		in := m.set.input
		in.Width = min(max(room-4, 4), 40)
		in.TextStyle = seg(p.Text, p.Raised, false)
		value = seg(p.Accent, p.Raised, true).Render("[") + in.View() + seg(p.Accent, p.Raised, true).Render("]")
	case f.kind == kindReadonly:
		value = seg(p.Muted, bg, false).Render(ansi.Truncate(text, room, "…"))
	default:
		suffix := "]"
		if f.kind == kindChoice || f.kind == kindToggle {
			suffix = " v]"
		}
		text = ansi.Truncate(text, max(room-1-len(suffix), 1), "…")
		style := seg(p.Text, p.Raised, false)
		if selected {
			style = seg(p.Background, p.Accent, true)
		}
		value = style.Render("[" + text + suffix)
	}
	row := left + value
	if wide && f.origin != nil && !m.set.editing {
		if o := room - lipgloss.Width(value) - 2; o > 8 {
			row += seg(p.Muted, bg, false).Render("  " + ansi.Truncate(f.origin(m), o, "…"))
		}
	}
	return shellRow(width, bg, row, "")
}

// fieldDetail is the help line and the source/error line of the selected
// field, shown only while the cursor is on the fields.
func (m Model) fieldDetail(width, n int, fields []settingField, cursor int) []string {
	p := theme.Current
	blank := blankRow(width, p.Background)
	out := make([]string, n)
	for i := range out {
		out[i] = blank
	}
	if n == 0 || len(fields) == 0 || m.set.level != levelField {
		return out
	}
	f := fields[cursor]
	help := f.help
	if f.kind == kindUnavailable {
		help = f.note
	}
	helpLine := seg(p.Muted, p.Background, false).Render(ansi.Truncate("  "+help, width, "…"))
	if m.set.capturing {
		prompt := "  Press a new key for " + defaultBindings[f.action].desc + " · esc cancel · backspace reset"
		helpLine = seg(p.Accent, p.Background, true).Render(ansi.Truncate(prompt, width, "…"))
	}
	var info string
	switch {
	case m.set.editErr != "":
		info = seg(p.Danger, p.Background, true).Render(ansi.Truncate("  "+m.set.editErr, width, "…"))
	case m.set.saveErr != "":
		info = seg(p.Danger, p.Background, true).Render(ansi.Truncate("  "+m.set.saveErr, width, "…"))
	case f.origin != nil:
		src := "  Source: " + f.origin(m)
		if f.scope == scopeRepo && f.repoKey != "" {
			src += " · writes .bonsai.yaml after you confirm"
		}
		info = seg(p.Muted, p.Background, false).Render(ansi.Truncate(src, width, "…"))
	}
	var lines []string
	switch {
	case n == 1 && m.set.editErr != "":
		lines = []string{info} // a rejection outranks the prompt when space is tight
	case n >= 3:
		lines = []string{"", helpLine, info}
	case n == 2:
		lines = []string{helpLine, info}
	default:
		lines = []string{helpLine}
	}
	for i, l := range lines {
		if i < n {
			out[i] = shellRow(width, p.Background, l, "")
		}
	}
	return out
}

// swatchStrip previews the live neutral and semantic colors.
func swatchStrip(_ Model, width int) []string {
	p := theme.Current
	cells := []struct {
		label  string
		bg, fg lipgloss.Color
	}{
		{"Base", p.Background, p.Text}, {"Panel", p.Surface, p.Text}, {"Raised", p.Raised, p.Text},
		{"Text", p.Text, p.Background}, {"Accent", p.Accent, p.Background}, {"Warning", p.Warning, p.Background},
	}
	cw := max(min(width/len(cells), 12), 4)
	var b strings.Builder
	b.WriteString(seg("", p.Background, false).Render(" "))
	for _, c := range cells {
		label := ansi.Truncate(c.label, cw-1, "")
		pad := cw - 1 - lipgloss.Width(label)
		b.WriteString(seg(c.fg, c.bg, false).Render(" " + label + strings.Repeat(" ", pad)))
	}
	return []string{
		seg(p.Muted, p.Background, true).Render(" SURFACE + SEMANTIC COLORS"),
		b.String(),
	}
}
