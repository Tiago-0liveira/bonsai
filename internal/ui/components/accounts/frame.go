// Package accounts renders profile, launch and usage views inside the Agents tab.
package accounts

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	wideLeft   = 26
	narrowLeft = 24
	wideBreak  = 76
	// Title, spacer and footer; all other rows belong to the content.
	chrome = 3
)

// LeftWidth leaves room for both columns and their spacing.
func LeftWidth(w int) int {
	left := narrowLeft
	if w >= wideBreak {
		left = wideLeft
	}
	return max(min(left, (w-4)/2), 0)
}

// ContentWidth leaves one cell of padding on either side.
func ContentWidth(w int) int { return max(w-2, 0) }

// RightWidth is the detail column width of a two-column frame.
func RightWidth(w int) int { return max(w-LeftWidth(w)-4, 0) }

// BodyRows is the available height after the title and footer.
func BodyRows(h int) int { return max(h-chrome, 0) }

// Frame fills the pane with two columns, without an enclosing border.
func Frame(title string, left, right []string, footer string, w, h int) string {
	body := make([]string, BodyRows(h))
	for i := range body {
		body[i] = fit(at(left, i), LeftWidth(w)) + "  " + fit(at(right, i), RightWidth(w))
	}
	return FrameSingle(title, body, footer, w, h)
}

// FrameSingle fills the pane with a title, content and a pinned footer.
func FrameSingle(title string, body []string, footer string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent)
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	rows := []string{titleStyle.Render(strings.ToUpper(title)), ""}
	for i := range BodyRows(h) {
		rows = append(rows, at(body, i))
	}
	rows = append(rows, dim.Render(footer))
	if len(rows) > h {
		rows = append(rows[:h-1], rows[len(rows)-1])
	}
	for i := range rows {
		rows[i] = fit(" "+fit(rows[i], ContentWidth(w)), w)
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
// style cannot bleed into the next column.
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

// Compact switches to a single column when two columns would crowd the fields.
func Compact(w int) bool { return w < 76 }
