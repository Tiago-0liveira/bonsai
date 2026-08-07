package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const leftPaneRatio = 35 // percent of width for the worktree list

var (
	focusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("205"))
	blurredBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	paneTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 1)
)

// dims returns the pane geometry for the current terminal size:
// left/right outer widths and the shared inner (inside-border) height.
func (m Model) dims() (leftW, rightW, innerH int) {
	leftW = m.width * leftPaneRatio / 100
	if leftW < 12 {
		leftW = 12
	}
	rightW = m.width - leftW
	barH := lipgloss.Height(m.statusBar())
	innerH = m.height - barH - 2 // 2 = top+bottom border rows
	if innerH < 1 {
		innerH = 1
	}
	return leftW, rightW, innerH
}

// layout recomputes child component sizes from the current terminal size.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	leftW, rightW, innerH := m.dims()
	m.list.SetSize(leftW-2, innerH)
	m.term.SetSize(rightW-2, innerH-1) // -1 for the pane title line
}

// View renders two bordered panes filling the screen above a status/help bar,
// with any active modal drawn centered on top.
func (m Model) View() string {
	if !m.ready {
		return "Loading bonsai…"
	}
	if m.modal != nil {
		return m.modal.View()
	}

	leftW, rightW, innerH := m.dims()

	left := m.borderFor(m.focus == focusList).
		Width(leftW - 2).Height(innerH).
		Render(clampHeight(m.list.View(), innerH))

	rightBody := paneTitle.Render(m.term.Title()) + "\n" + m.term.View()
	right := m.borderFor(m.focus == focusTerminal).
		Width(rightW - 2).Height(innerH).
		Render(clampHeight(rightBody, innerH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())
}

func (m Model) borderFor(focused bool) lipgloss.Style {
	if focused {
		return focusedBorder
	}
	return blurredBorder
}

// statusBar renders the keybinding help (compact, or full when toggled with ?).
// In compact mode a status/error is appended, truncated to the leftover width so
// the keymap stays visible. Every line is clamped to the terminal width so the
// bar can never overflow and break the layout.
func (m Model) statusBar() string {
	help := m.help.View(m.keys)

	if m.help.ShowAll {
		return truncateToWidth(help, m.width)
	}

	line := help
	msg := m.status
	if m.err != nil {
		msg = "error: " + m.err.Error()
	}
	if msg != "" {
		if avail := m.width - lipgloss.Width(help) - 3; avail >= 8 {
			line = help + "  " + statusStyle.Render(ansi.Truncate(msg, avail, "…"))
		}
	}
	return truncateToWidth(line, m.width)
}

// clampHeight drops any lines of s beyond the first h.
func clampHeight(s string, h int) string {
	if h < 1 {
		h = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// truncateToWidth clamps each line of s to width with an ellipsis.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}
