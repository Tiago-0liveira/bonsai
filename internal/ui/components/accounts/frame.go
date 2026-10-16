// Package accounts holds the pieces of the agent accounts modal and the quick
// usage overlay. The frame and sidebar live apart from the modal model so the
// new-agent dialog draws the same chrome and lists accounts the same way.
package accounts

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// wideLeft is the sidebar width that has room for the usage column.
	wideLeft = 26
	// narrowLeft is the sidebar width on screens too small for wideLeft.
	narrowLeft = 24
	// wideBreak is the frame width from which the sidebar uses wideLeft.
	wideBreak = 76
	// chrome is the rows the frame spends on border, spacer, separator and
	// footer; the body gets the rest.
	chrome = 5
)

// LeftWidth is the sidebar content width for a frame of width w. It shrinks
// on tiny frames so the right column always keeps some cells.
func LeftWidth(w int) int {
	left := narrowLeft
	if w >= wideBreak {
		left = wideLeft
	}
	// border(2) + divider(1) + padding(4) are spent outside both columns.
	return max(min(left, (w-7)/2), 1)
}

// BodyRows is how many rows a frame of height h leaves for column content.
func BodyRows(h int) int { return max(h-chrome, 0) }

// Frame draws the two-column modal chrome in exactly h lines of w cells. The
// left column is LeftWidth(w) wide and the right column takes the rest; both
// get one cell of padding. left and right may carry ANSI and are cut to fit.
func Frame(title string, left, right []string, footer string, w, h int) string {
	leftW := LeftWidth(w)
	rightW := max(w-leftW-7, 0)
	bs := borderStyle()
	bar := bs.Render("│")

	rows := make([]string, 0, h)
	rows = append(rows, topBorder(title, w))
	rows = append(rows, bar+strings.Repeat(" ", max(w-2, 0))+bar)
	for i := range BodyRows(h) {
		rows = append(rows, bar+" "+fit(at(left, i), leftW)+" "+bs.Render("│")+" "+fit(at(right, i), rightW)+" "+bar)
	}
	rows = append(rows, bs.Render("├"+strings.Repeat("─", leftW+2)+"┴"+strings.Repeat("─", max(w-leftW-5, 0))+"┤"))
	rows = append(rows, footerRow(footer, w, bar))
	rows = append(rows, bs.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
	return finish(rows, h)
}

// FrameSingle is Frame with one column, for overlays such as quick usage.
func FrameSingle(title string, body []string, footer string, w, h int) string {
	bs := borderStyle()
	bar := bs.Render("│")
	inner := max(w-4, 0)

	rows := make([]string, 0, h)
	rows = append(rows, topBorder(title, w))
	rows = append(rows, bar+strings.Repeat(" ", max(w-2, 0))+bar)
	for i := range BodyRows(h) {
		rows = append(rows, bar+" "+fit(at(body, i), inner)+" "+bar)
	}
	rows = append(rows, bs.Render("├"+strings.Repeat("─", max(w-2, 0))+"┤"))
	rows = append(rows, footerRow(footer, w, bar))
	rows = append(rows, bs.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
	return finish(rows, h)
}

// borderStyle matches the existing modals' accent border; the palette is read
// per render so a theme change needs no rebuild.
func borderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Accent)
}

func topBorder(title string, w int) string {
	bs := borderStyle()
	if w < 6 {
		return bs.Render("╭" + strings.Repeat("─", max(w-2, 0)) + "╮")
	}
	t := ansi.Truncate(strings.ToUpper(title), w-6, "")
	// "╭─ " + title + " " + fill + "╮"
	fill := w - 5 - ansi.StringWidth(t)
	return bs.Render("╭─ ") +
		lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent).Render(t) +
		bs.Render(" "+strings.Repeat("─", fill)+"╮")
}

func footerRow(footer string, w int, bar string) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	text := ansi.Truncate(footer, max(w-4, 0), "")
	pad := max(w-4-ansi.StringWidth(text), 0)
	return bar + " " + dim.Render(text) + strings.Repeat(" ", pad) + " " + bar
}

// finish keeps the exact-height contract when h is too small for the chrome:
// the top and bottom borders survive and the middle is dropped.
func finish(rows []string, h int) string {
	if h < len(rows) {
		keep := make([]string, 0, h)
		if h > 0 {
			keep = append(keep, rows[:h-1]...)
			keep = append(keep, rows[len(rows)-1])
		}
		rows = keep
	}
	return strings.Join(rows, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// fit cuts or pads s to exactly n cells. A cut line is reset so an unclosed
// style cannot bleed into the border drawn after it.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	width := ansi.StringWidth(s)
	if width > n {
		return ansi.Truncate(s, n, "") + "\x1b[0m"
	}
	return s + strings.Repeat(" ", n-width)
}
