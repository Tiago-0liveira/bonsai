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
	percentCells = 4
	maxNameCells = 12
	borderCells  = 2
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

// Summary selects the account with the least headroom across either window.
// Unknown data stays unknown rather than implying spare capacity.
func Summary(accounts []Account) ([]Account, []int) {
	var rows []Account
	var counts []int
	ranks := []float64{}
	indices := map[agents.ProviderID]int{}
	for _, a := range accounts {
		rank := 2.0
		for _, w := range windows(a) {
			if f, ok := agents.RemainingFraction(w.limit); ok {
				rank = min(rank, f)
			}
		}
		if i, ok := indices[a.Provider]; ok {
			counts[i]++
			if rank < ranks[i] {
				rows[i], ranks[i] = a, rank
			}
		} else {
			indices[a.Provider] = len(rows)
			rows = append(rows, a)
			counts = append(counts, 1)
			ranks = append(ranks, rank)
		}
	}
	return rows, counts
}

func SummaryHeight(accounts []Account) int { rows, _ := Summary(accounts); return len(rows) + 2 }

func ViewSummary(accounts []Account, age string, width, maxRows int, ascii bool) string {
	return ViewSummaryAt(accounts, age, width, maxRows, time.Now(), ascii)
}

// ViewSummaryAt uses the supplied clock for the selected accounts' reset times.
func ViewSummaryAt(accounts []Account, age string, width, maxRows int, now time.Time, ascii bool) string {
	if width < 12 || maxRows < 3 {
		return ""
	}
	rows, counts := Summary(accounts)
	b := borders(ascii)
	edge := lipgloss.NewStyle().Foreground(theme.Current.Border)
	out := []string{topBorder(width, age, ascii, b, edge)}
	labels := make([]string, len(rows))
	labelWidth := 0
	// The stale marker gets its own cell after the label, as in expanded rows,
	// so a marked provider does not push its columns right.
	anyStale := false
	for i, a := range rows {
		labels[i] = providerGlyph(a.Provider, ascii) + " " + providerName(a.Provider)
		anyStale = anyStale || staleMark(a) != ""
		if counts[i] > 1 {
			labels[i] += fmt.Sprintf(" +%d", counts[i]-1)
		}
		labelWidth = max(labelWidth, ansi.StringWidth(labels[i]))
	}
	resetWidth := 0
	for _, a := range rows {
		if a.Primary != nil && a.Primary.ResetsAt != nil {
			resetWidth = max(resetWidth, len(compactReset(now, *a.Primary.ResetsAt)))
		}
	}
	// Reserve space for both capacity windows before shortening labels.
	slot := 0
	if anyStale {
		slot = 1
	}
	labelWidth = min(labelWidth, max(1, width-4-17-slot))
	for i, a := range rows {
		if i >= maxRows-2 {
			break
		}
		line := fit(labels[i], labelWidth)
		if anyStale {
			if mark := staleMark(a); mark != "" {
				line += mark
			} else {
				line += " "
			}
		}
		if state := accountState(a, ascii); state != "" {
			line += " " + state
		} else {
			for j, w := range windows(a) {
				if w.limit != nil {
					line += " " + w.label(true) + " "
					if f, ok := agents.RemainingFraction(w.limit); ok {
						line += capacity(f)
					} else {
						line += "N/A"
					}
					if j == 0 && resetWidth > 0 && labelWidth+17+1+resetWidth <= width-4 {
						reset := ""
						if w.limit.ResetsAt != nil {
							reset = compactReset(now, *w.limit.ResetsAt)
						}
						line += " " + dim(strings.Repeat(" ", resetWidth-len(reset))+reset)
					}
				}
			}
		}
		out = append(out, edge.Render(b.v)+" "+fit(line, width-4)+" "+edge.Render(b.v))
	}
	out = append(out, bottomBorder(rows[:min(len(rows), maxRows-2)], width, b, edge))
	return strings.Join(out, "\n")
}

// Pages counts compact account pages, including a short final page.
func Pages(accounts, rows int) int {
	if rows < 3 || accounts == 0 {
		return 1
	}
	return (accounts + rows - 3) / (rows - 2)
}

// ViewPage keeps a stable outer height while rotating, so the worktree list
// never jumps when the last page contains fewer accounts.
func ViewPage(accounts []Account, status string, width, rows int, now time.Time, ascii bool, page int) string {
	if width < 12 || rows < 3 {
		return ""
	}
	rows = min(rows, max(3, len(accounts)+2))
	pages := Pages(len(accounts), rows)
	page = max(0, page) % pages
	start := min(page*(rows-2), len(accounts))
	end := min(start+rows-2, len(accounts))
	b := borders(ascii)
	edge := lipgloss.NewStyle().Foreground(theme.Current.Border)
	// Page position has priority over the descriptive title in narrow panes.
	title := " Usage "
	if width >= 24 {
		title = " Usage · left "
		if ascii {
			title = " Usage / left "
		}
	}
	room := width - 2 - lipgloss.Width(title)
	status = truncate(status, max(0, room), ascii)
	top := edge.Render(b.tl) + text(title) + edge.Render(strings.Repeat(b.h, max(0, room-lipgloss.Width(status)))) + dim(status) + edge.Render(b.tr)
	out := []string{top}
	plan := planColumns(accounts, width-4, now)
	for _, a := range accounts[start:end] {
		out = append(out, edge.Render(b.v)+" "+fit(pageLine(a, plan, now, ascii), width-4)+" "+edge.Render(b.v))
	}
	for len(out) < rows-1 {
		out = append(out, edge.Render(b.v)+" "+strings.Repeat(" ", width-4)+" "+edge.Render(b.v))
	}
	out = append(out, bottomBorder(accounts[start:end], width, b, edge))
	return strings.Join(out, "\n")
}

func bottomBorder(accounts []Account, width int, b border, edge lipgloss.Style) string {
	hint := ""
	for _, a := range accounts {
		if a.Stale || a.Err != "" {
			hint = " ! stale "
			break
		}
	}
	if len(hint) > width-4 {
		hint = ""
	}
	return edge.Render(b.bl) + dim(hint) + edge.Render(strings.Repeat(b.h, width-2-len(hint))+b.br)
}

type columnPlan struct {
	name, reset, meter                                 int
	primaryReset, weekReset, primaryBar, weekBar, week bool
}

// Plan against every account so carousel pages retain the same columns.
func planColumns(accounts []Account, width int, now time.Time) columnPlan {
	p := columnPlan{primaryReset: true, weekReset: true, primaryBar: true, weekBar: true, week: true}
	for _, a := range accounts {
		p.name = max(p.name, ansi.StringWidth(inline(a.Name)))
		for _, w := range windows(a) {
			if w.limit != nil && w.limit.ResetsAt != nil {
				p.reset = max(p.reset, len(compactReset(now, *w.limit.ResetsAt)))
			}
		}
	}
	p.name = min(maxNameCells, max(1, p.name))
	// Preserve the primary percentage even in the smallest supported pane.
	p.name = min(p.name, max(1, width-11))
	size := func() int {
		n := 2 + p.name + 1 + 1 + 2 + 1 + percentCells
		if p.primaryReset && p.reset > 0 {
			n += 1 + p.reset
		}
		if p.primaryBar {
			n += 5
		}
		if p.week {
			n += 1 + 1 + 1 + percentCells
		}
		if p.weekReset && p.reset > 0 {
			n += 1 + p.reset
		}
		if p.weekBar {
			n += 5
		}
		return n
	}
	for _, drop := range []*bool{&p.weekBar, &p.primaryBar, &p.weekReset, &p.primaryReset, &p.week} {
		if size() <= width {
			break
		}
		*drop = false
	}
	bars := 0
	if p.primaryBar {
		bars++
	}
	if p.weekBar {
		bars++
	}
	if bars > 0 {
		p.meter = 4 + (width-size())/bars
	}
	return p
}

func pageLine(a Account, p columnPlan, now time.Time, ascii bool) string {
	marker := staleMark(a)
	if marker == "" {
		marker = " "
	}
	head := providerGlyph(a.Provider, ascii) + " " + text(pad(truncate(inline(a.Name), p.name, ascii), p.name)) + marker
	if state := accountState(a, ascii); state != "" {
		return head + " " + dim(state)
	}
	line := head
	for i, w := range windows(a) {
		if i == 1 && !p.week {
			break
		}
		label := "5h"
		if i == 1 {
			label = "W"
		}
		value := dim(" N/A")
		frac, known := agents.RemainingFraction(w.limit)
		if known {
			value = capacity(frac)
		}
		line += " " + dim(label) + " " + value
		if (i == 0 && p.primaryReset || i == 1 && p.weekReset) && p.reset > 0 {
			reset := ""
			if w.limit != nil && w.limit.ResetsAt != nil {
				reset = compactReset(now, *w.limit.ResetsAt)
			}
			line += " " + dim(strings.Repeat(" ", p.reset-len(reset))+reset)
		}
		if i == 0 && p.primaryBar || i == 1 && p.weekBar {
			meter := strings.Repeat(" ", p.meter)
			if known {
				meter = bar(frac, p.meter, ascii)
			}
			line += " " + meter
		}
	}
	return line
}

func compactReset(now, reset time.Time) string {
	minutes := int(reset.Sub(now) / time.Minute)
	if minutes <= 0 {
		return "now"
	}
	if minutes >= 1440 {
		return fmt.Sprintf("%dd%dh", minutes/1440, minutes%1440/60)
	}
	if minutes >= 60 {
		return fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}
