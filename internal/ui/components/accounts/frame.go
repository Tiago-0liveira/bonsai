// Package accounts renders profile, launch and usage views inside the Agents tab.
package accounts

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Title, spacer and footer; all other rows belong to the content.
const chrome = 3

// ContentWidth leaves one cell of padding on either side.
func ContentWidth(w int) int { return max(w-2, 0) }

// bodyRows is the available height after the title and footer.
func bodyRows(h int) int { return max(h-chrome, 0) }

// FrameSingle fills the pane with a title, content and a pinned footer.
func FrameSingle(title string, body []string, footer string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent)
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	rows := []string{titleStyle.Render(strings.ToUpper(title)), ""}
	for i := range bodyRows(h) {
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
