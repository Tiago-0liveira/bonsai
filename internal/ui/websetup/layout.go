package websetup

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Below this width the option screens stack the explanation under the list.
const twoPaneMinWidth = 100

type hint struct{ key, desc string }

// hints renders the footer the same way the bonsai TUI does: bold key, dim
// description, joined by " · ".
func (m Model) hints(hs []hint) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, m.st.key.Render(h.key)+" "+m.st.dim.Render(h.desc))
	}
	return strings.Join(parts, m.st.dim.Render(" · "))
}

// frame lays out one screen: a title bar with the step counter (or status) on
// the right, a rule, the body, and the key hints on the last line. The result
// is exactly the window height and never wider than the window.
func (m Model) frame(title, right, body string, hs []hint) string {
	w, h := m.size()
	inner := w - 2
	head := " " + m.st.title.Render(title)
	if right != "" {
		gap := w - lipgloss.Width(head) - lipgloss.Width(right) - 1
		if gap < 1 {
			gap = 1
		}
		head += strings.Repeat(" ", gap) + right
	}
	lines := []string{head, " " + m.st.dim.Render(strings.Repeat("─", inner))}

	bodyLines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	room := h - len(lines) - 1
	if m.flash != "" {
		room--
	}
	if room < 1 {
		room = 1
	}
	if len(bodyLines) > room {
		bodyLines = append(bodyLines[:room-1], m.st.dim.Render("  …"))
	}
	lines = append(lines, bodyLines...)
	for len(lines) < h-1-boolInt(m.flash != "") {
		lines = append(lines, "")
	}
	if m.flash != "" {
		lines = append(lines, " "+m.st.warn.Render(m.flash))
	}
	lines = append(lines, " "+m.hints(hs))
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, w, "…")
	}
	return strings.Join(lines, "\n")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// size is the window size, defaulting to 80×24 before the first resize.
func (m Model) size() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// panes puts the option list and its explanation side by side on wide
// terminals, and stacks them (explanation below a rule) on narrow ones.
// right renders the explanation for a given text width.
func (m Model) panes(left string, right func(width int) string) string {
	w, _ := m.size()
	if w >= twoPaneMinWidth {
		leftBox := m.st.boxFocus.Render(left)
		rightWidth := w - lipgloss.Width(leftBox) - 4
		rightBox := m.st.box.Width(rightWidth).Render(right(rightWidth - 2))
		return lipgloss.JoinHorizontal(lipgloss.Top, " ", leftBox, " ", rightBox)
	}
	return left + "\n " + m.st.dim.Render(strings.Repeat("─", w-2)) + "\n" + indent(right(w-4), 2)
}

// hang renders label and a value wrapped to width, continuation lines
// indented under the value.
func (m Model) hang(label string, labelWidth int, value string, width int) string {
	lines := strings.Split(m.wrap(value, width-labelWidth), "\n")
	pad := strings.Repeat(" ", labelWidth)
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if i == 0 {
			lines[i] = padRight(label, labelWidth) + line
		} else {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// wrap word-wraps text to width columns.
func (m Model) wrap(text string, width int) string {
	if width < 10 {
		width = 10
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}

func indent(text string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}

// padRight pads s with spaces to width columns (ANSI-aware).
func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
