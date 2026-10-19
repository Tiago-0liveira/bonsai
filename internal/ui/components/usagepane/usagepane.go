// Package usagepane renders meter strips below the worktree list. It is pure
// rendering; the model owns fetching, visibility, and the available space.
package usagepane

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	percentCells      = 4 // "100%"
	minMeterCells     = 4
	primaryMeterCells = 12 // grow the primary meter before adding a secondary one
	minNameCells      = 6
	maxNameCells      = 12
	borderCells       = 2
	padCells          = 2 // one space inside each border
	railCells         = 2 // rail and following space
)

// Account is one account's row data.
type Account struct {
	Provider           agents.ProviderID
	Name               string
	Primary, Secondary *agents.UsageLimit
	Stale              bool // last refresh failed or is overdue
	Err                string
	Loading            bool // no data yet
}

type mode int

const (
	modeFull    mode = iota // three content lines per account
	modeCompact             // one content line per account
)

// Height is the outer height View draws for maxRows (borders included), or 0
// when even one line does not fit.
func Height(accounts []Account, maxRows int) int {
	if maxRows < borderCells+1 {
		return 0
	}
	_, rows, _ := plan(accounts, maxRows-borderCells)
	return rows + borderCells
}

// View draws the box into at most maxRows rows (borders included). headerAge
// is the oldest account's age, shown in the top border when it fits.
func View(accounts []Account, headerAge string, width, maxRows int, now time.Time, ascii bool) string {
	if maxRows < borderCells+1 || width < 12 {
		return ""
	}
	cw := width - borderCells - padCells
	md, rows, shown := plan(accounts, maxRows-borderCells)
	visible := accounts[:shown]
	layout := compactLayout(visible, cw-railCells, ascii)
	labelW := windowLabelWidth(visible, cw-railCells)
	meterW := expandedMeterWidth(visible, cw-railCells, labelW, now, ascii)
	var lines []string
	if len(accounts) == 0 {
		lines = []string{dim("No agent accounts")}
	}
	for _, a := range visible {
		if md == modeCompact {
			lines = append(lines, rail(a.Provider, ascii)+compactLine(a, layout, ascii))
		} else {
			for _, line := range blockLines(a, cw-railCells, labelW, meterW, now, ascii) {
				lines = append(lines, rail(a.Provider, ascii)+line)
			}
		}
	}
	if more := len(accounts) - shown; more > 0 && len(lines) < rows {
		lines = append(lines, dim(fmt.Sprintf("+%d more", more)))
	}

	b := borders(ascii)
	edge := lipgloss.NewStyle().Foreground(theme.Current.Border)
	out := []string{topBorder(width, headerAge, ascii, b, edge)}
	for _, l := range lines {
		out = append(out, edge.Render(b.v)+" "+fit(l, cw)+" "+edge.Render(b.v))
	}
	out = append(out, edge.Render(b.bl+strings.Repeat(b.h, width-borderCells)+b.br))
	return strings.Join(out, "\n")
}

// plan keeps automatic sizing and account overflow: all expanded blocks,
// then one row per account, then the existing +N more row.
func plan(accounts []Account, avail int) (md mode, rows, shown int) {
	if len(accounts) == 0 {
		return modeCompact, 1, 0
	}
	if n := 3 * len(accounts); n <= avail {
		return modeFull, n, len(accounts)
	}
	if len(accounts) <= avail {
		return modeCompact, len(accounts), len(accounts)
	}
	if avail == 1 {
		return modeCompact, 1, 1
	}
	return modeCompact, avail, avail - 1
}

func blockLines(a Account, cw, labelW, meterW int, now time.Time, ascii bool) []string {
	lines := []string{headerLine(a, cw, ascii), "", ""}
	if state := accountState(a, ascii); state != "" {
		lines[1] = dim(truncate(state, cw, ascii))
		return lines
	}
	i := 1
	for _, w := range windows(a) {
		if w.limit != nil {
			lines[i] = limitLine(w.limit, w.label(false), cw, labelW, meterW, now, ascii)
			i++
		}
	}
	return lines
}

func headerLine(a Account, cw int, ascii bool) string {
	provider := providerName(a.Provider)
	sep := " · "
	if ascii {
		sep = " / "
	}
	head := dim(provider + sep)
	if lipgloss.Width(head)+minNameCells > cw {
		head = providerGlyph(a.Provider, ascii) + " "
	}
	mark := staleMark(a)
	nameW := max(cw-lipgloss.Width(head)-lipgloss.Width(mark), 0)
	return head + text(pad(truncate(inline(a.Name), nameW, ascii), nameW)) + mark
}

type window struct {
	limit    *agents.UsageLimit
	fallback string
}

func windows(a Account) []window {
	return []window{{a.Primary, "Pri"}, {a.Secondary, "Sec"}}
}

func (w window) label(compact bool) string {
	label := inline(usagefmt.WindowName(w.limit, w.fallback))
	if compact && label == "Week" {
		return "W"
	}
	return label
}

func windowLabelWidth(accounts []Account, cw int) int {
	width := 0
	for _, a := range accounts {
		for _, w := range windows(a) {
			if w.limit != nil {
				width = max(width, lipgloss.Width(w.label(false)))
			}
		}
	}
	return min(width, max(cw-percentCells-1, 1))
}

// limitLine keeps label and value ahead of the meter and reset caption. The
// common label column makes both headline windows line up across accounts.
func limitLine(l *agents.UsageLimit, label string, cw, labelW, meterW int, now time.Time, ascii bool) string {
	label = dim(pad(truncate(label, labelW, ascii), labelW))
	frac, ok := agents.RemainingFraction(l)
	if !ok {
		return label + " " + dim("Unavailable")
	}
	pct := capacity(frac)
	parts := []string{label}
	if meterW > 0 {
		parts = append(parts, bar(frac, meterW, ascii))
	}
	parts = append(parts, pct)
	line := strings.Join(parts, " ")
	if l.ResetsAt != nil {
		reset := resetCaption(l, now, ascii)
		if lipgloss.Width(line)+2+lipgloss.Width(reset) <= cw {
			line += "  " + dim(reset)
		}
	}
	return line
}

// Expanded windows have their own rows, so both meters can fill the width.
// Reserve a shared reset column only when a useful meter still fits.
func expandedMeterWidth(accounts []Account, cw, labelW int, now time.Time, ascii bool) int {
	room := max(cw-labelW-percentCells-2, 0)
	resetW := 0
	for _, a := range accounts {
		for _, w := range windows(a) {
			if _, ok := agents.RemainingFraction(w.limit); ok && w.limit.ResetsAt != nil {
				resetW = max(resetW, 2+lipgloss.Width(resetCaption(w.limit, now, ascii)))
			}
		}
	}
	if room-resetW >= minMeterCells {
		return room - resetW
	}
	return room
}

func resetCaption(l *agents.UsageLimit, now time.Time, ascii bool) string {
	mark := "↻ "
	if ascii {
		mark = "~ "
	}
	return mark + usagefmt.ResetsIn(now, l.ResetsAt, true)
}

type compactColumns struct {
	name   int
	meters [2]int
	labels [2]int
}

// compactLayout shares identity columns and gives the remaining width to
// meters. Numeric values survive when a narrow sidebar cannot fit meters.
func compactLayout(accounts []Account, cw int, ascii bool) compactColumns {
	// Provider glyph and space, then the name separator. Status marks stay
	// inside the name column so they cannot displace a window's percentage.
	const identityCells = 3
	layout := compactColumns{}
	var numeric [2]bool
	nameW := minNameCells
	stateW := 0
	for _, a := range accounts {
		nameW = max(nameW, lipgloss.Width(inline(a.Name))+lipgloss.Width(staleMark(a)))
		stateW = max(stateW, lipgloss.Width(accountState(a, ascii)))
		for i, w := range windows(a) {
			if _, ok := agents.RemainingFraction(w.limit); ok {
				numeric[i] = true
			}
			if w.limit != nil {
				layout.labels[i] = max(layout.labels[i], lipgloss.Width(w.label(true)))
			}
		}
	}
	metricWidth := func() int {
		n := 0
		for _, label := range layout.labels {
			if label == 0 {
				continue
			}
			if n > 0 {
				n++
			}
			n += label + 1 + percentCells
		}
		return n
	}
	// Unusually long provider labels must also yield to the numeric columns.
	for cw-identityCells-metricWidth() < 1 && max(layout.labels[0], layout.labels[1]) > 1 {
		if layout.labels[0] >= layout.labels[1] {
			layout.labels[0]--
		} else {
			layout.labels[1]--
		}
	}
	nameRoom := cw - identityCells - max(metricWidth(), min(stateW, max(cw-identityCells-1, 0)))
	layout.name = min(nameW, maxNameCells, max(nameRoom, 1))
	if (numeric[0] || numeric[1]) && nameRoom >= minNameCells+minMeterCells+1 {
		layout.name = min(layout.name, nameRoom-minMeterCells-1)
	}
	// State messages share the metric area but do not consume meter space on
	// accounts that have values. Give the primary window the available width
	// until it is substantial, then add and grow the secondary meter.
	room := cw - identityCells - layout.name - metricWidth()
	switch {
	case numeric[0] && numeric[1] && room >= primaryMeterCells+minMeterCells+2:
		layout.meters[0] = max(primaryMeterCells, (room-1)/2)
		layout.meters[1] = room - 2 - layout.meters[0]
	case numeric[0]:
		layout.meters[0] = max(room-1, 0)
	case numeric[1]:
		layout.meters[1] = max(room-1, 0)
	}
	return layout
}

// compactLine always uses the existing primary then secondary window, never
// substitutes the most-used window, and never includes reset details.
func compactLine(a Account, layout compactColumns, ascii bool) string {
	mark := staleMark(a)
	nameW := max(layout.name-lipgloss.Width(mark), 0)
	head := providerGlyph(a.Provider, ascii) + " " + text(pad(truncate(inline(a.Name), nameW, ascii), nameW)) + mark + " "
	if state := accountState(a, ascii); state != "" {
		return head + dim(state)
	}
	var parts []string
	for i, w := range windows(a) {
		if w.limit == nil {
			continue
		}
		label := dim(pad(truncate(w.label(true), layout.labels[i], ascii), layout.labels[i]))
		frac, ok := agents.RemainingFraction(w.limit)
		if !ok {
			parts = append(parts, label+" "+dim("N/A"))
			continue
		}
		value := capacity(frac)
		if layout.meters[i] > 0 {
			value = bar(frac, layout.meters[i], ascii) + " " + value
		}
		parts = append(parts, label+" "+value)
	}
	return head + strings.Join(parts, " ")
}

func accountState(a Account, ascii bool) string {
	switch {
	case a.Loading:
		if ascii {
			return "Loading..."
		}
		return "Loading…"
	case a.Primary != nil || a.Secondary != nil:
		return ""
	case a.Err != "":
		return "Unavailable"
	default:
		return "No shared windows"
	}
}

func staleMark(a Account) string {
	if a.Stale || a.Err != "" {
		return lipgloss.NewStyle().Foreground(theme.Current.Warning).Render("!")
	}
	return ""
}

func topBorder(width int, headerAge string, ascii bool, b border, edge lipgloss.Style) string {
	title := " Usage · left "
	if ascii {
		title = " Usage / left "
	}
	if lipgloss.Width(title) > width-borderCells {
		title = "Usage left"
	}
	title = truncate(title, width-borderCells, ascii)
	right := ""
	if headerAge != "" {
		right = " " + inline(headerAge) + " "
	}
	fill := width - borderCells - lipgloss.Width(title) - lipgloss.Width(right)
	if fill < 1 {
		right = ""
		fill = max(width-borderCells-lipgloss.Width(title), 0)
	}
	return edge.Render(b.tl) + text(title) + edge.Render(strings.Repeat(b.h, fill)) + dim(right) + edge.Render(b.tr)
}

type border struct{ tl, tr, bl, br, h, v string }

func borders(ascii bool) border {
	if ascii {
		return border{"+", "+", "+", "+", "-", "|"}
	}
	return border{"╭", "╮", "╰", "╯", "─", "│"}
}

func providerName(p agents.ProviderID) string {
	s := inline(string(p))
	if s == "" {
		return "Agent"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func providerGlyph(p agents.ProviderID, ascii bool) string {
	return theme.ProviderStyle(p).Render(theme.ProviderGlyph(p, ascii))
}

func rail(provider agents.ProviderID, ascii bool) string {
	if ascii {
		return theme.ProviderStyle(provider).Render("| ")
	}
	return theme.ProviderStyle(provider).Render("▌ ")
}

func bar(frac float64, width int, ascii bool) string {
	b := usagefmt.Bar(frac, width)
	filled, empty := "▰", "▱"
	if ascii {
		filled, empty = "#", "."
	}
	// Color each run independently so unused capacity stays muted even when
	// the remaining capacity is low or a fetch is stale.
	n := strings.Count(b, "█")
	return rankStyle(usagefmt.RankOf(frac)).Render(strings.Repeat(filled, n)) +
		dim(strings.Repeat(empty, width-n))
}

func capacity(frac float64) string {
	return rankStyle(usagefmt.RankOf(frac)).Render(fmt.Sprintf("%*d%%", percentCells-1, usagefmt.Percent(frac)))
}

func rankStyle(r usagefmt.Rank) lipgloss.Style {
	th := theme.Current
	switch r {
	case usagefmt.RankGood, usagefmt.RankFair:
		return lipgloss.NewStyle().Foreground(th.Success)
	case usagefmt.RankLow:
		return lipgloss.NewStyle().Foreground(th.Warning)
	default:
		return lipgloss.NewStyle().Foreground(th.Danger)
	}
}

func text(s string) string { return lipgloss.NewStyle().Foreground(theme.Current.Text).Render(s) }
func dim(s string) string  { return lipgloss.NewStyle().Foreground(theme.Current.Dim).Render(s) }

func inline(s string) string { return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s) }

func truncate(s string, w int, ascii bool) string {
	tail := "…"
	if ascii {
		tail = "."
	}
	return ansi.Truncate(s, w, tail)
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// fit pads or cuts a styled line to exactly w cells.
func fit(s string, w int) string {
	return pad(ansi.Truncate(s, w, ""), w)
}
