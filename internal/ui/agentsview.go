package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	agentsWideMin   = 100 // below: one-row header, shorter strip hints
	agentsNarrowMin = 60  // below: a single summary row, no header
)

func providerName(p agents.ProviderID) string {
	if p == "" {
		return "Agent"
	}
	s := string(p)
	return strings.ToUpper(s[:1]) + s[1:]
}

func sessionName(s agentsapi.Session) string {
	if s.Name != "" {
		return s.Name
	}
	return providerName(s.Provider)
}

func (m Model) agentGlyph(p agents.ProviderID) string {
	return theme.ProviderStyle(p).Render(theme.ProviderGlyph(p, m.agentGlyphsASCII()))
}

func dimText(s string) string { return lipgloss.NewStyle().Foreground(theme.Current.Dim).Render(s) }

// stateDot colors the lifecycle state: transitions warn, running is green,
// failures red, finished dim.
func stateDot(state string) string {
	c := theme.Current.Dim
	switch state {
	case "starting", "stopping":
		c = theme.Current.Warning
	case "running":
		c = theme.Current.Success
	case "failed":
		c = theme.Current.Danger
	}
	label := state
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return lipgloss.NewStyle().Foreground(c).Render("● " + label)
}

// fit truncates to width and pads with spaces to exactly width.
func fit(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	if w := ansi.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// leftRight keeps right at the row's end while there is room for a truncated
// left label and a separating cell.
func leftRight(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap >= 1 {
		return left + strings.Repeat(" ", gap) + right
	}
	rightWidth := ansi.StringWidth(right)
	if width < rightWidth+2 {
		return fit(left, width)
	}
	left = ansi.Truncate(left, width-rightWidth-1, "…")
	gap = width - ansi.StringWidth(left) - rightWidth
	return left + strings.Repeat(" ", gap) + right
}

// renderAgentsPane draws the Agents tab body in exactly h rows of at most w
// cells: strip, header, rule, then the terminal area with every remaining row.
func (m Model) renderAgentsPane(w, h int) string {
	rows := m.agentsRows(w, h)
	for len(rows) < h {
		rows = append(rows, "")
	}
	return strings.Join(rows[:h], "\n")
}

func (m Model) agentsRows(w, h int) []string {
	if !m.agents.active() {
		return []string{fit(unsupportedNotice, w)}
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return []string{fit(dimText("(no worktree selected)"), w)}
	}
	path := cleanPath(wt.Path)
	strip := m.stripSessions(path)
	sel, selIdx, hasSel := m.selectedSession(path)
	rule := dimText(strings.Repeat("─", w))

	var fixed []string
	switch {
	case !hasSel:
		fixed = []string{fit(m.emptyStrip(w), w), rule}
	case w < agentsNarrowMin:
		row := m.narrowRow(sel, selIdx, len(strip))
		if stale := m.agentsStaleIndicator(); stale != "" {
			row = leftRight(row, dimText(stale), w)
		}
		fixed = []string{fit(row, w)}
	case w < agentsWideMin:
		fixed = []string{
			m.sessionStrip(strip, selIdx, w, "", "a: All"),
			fit(m.compactHeader(sel, w), w),
			rule,
		}
	default:
		fixed = []string{
			m.sessionStrip(strip, selIdx, w, "Sessions: ", "a: All  n: New"),
			fit(m.fullHeaderTitle(sel), w),
			fit(m.usageRow(sel, w), w),
			rule,
		}
	}
	if h-len(fixed) < minTerminalRows {
		return []string{fit("Pane too small", w)}
	}
	body := h - len(fixed)
	var area []string
	if hasSel {
		area = placeholderArea(sel, w, body)
	} else {
		area = emptyArea(wt.Branch, w, body)
	}
	return append(fixed, area...)
}

func (m Model) emptyStrip(w int) string {
	hints := "a: All  n: New"
	if w < agentsWideMin {
		hints = "a: All"
	}
	hints = m.agentsStripStatus(hints)
	return leftRight(dimText("No running agents"), dimText(hints), w)
}

func (m Model) agentsStaleIndicator() string {
	if m.agents.err == "" {
		return ""
	}
	if m.agentGlyphsASCII() {
		return "! stale"
	}
	return "⚠ stale"
}

func (m Model) agentsStripStatus(hints string) string {
	if stale := m.agentsStaleIndicator(); stale != "" {
		return stale + "  " + hints
	}
	return hints
}

// narrowRow is the single summary row for panes under 60 cells.
func (m Model) narrowRow(s agentsapi.Session, idx, n int) string {
	name := ansi.Truncate(sessionName(s), stripNameCells, "…")
	return "Agents (g) │ " + m.agentGlyph(s.Provider) + " " + name + " [" + itoa(idx+1) + "/" + itoa(n) + "] │ a: Switch"
}

// sessionStrip lays out numbered labels left to right. When they overflow, it
// shows a window that always contains the selected label and ends in
// "[+N more]", keeping the hints on the right.
func (m Model) sessionStrip(strip []agentsapi.Session, sel, w int, prefix, hints string) string {
	labels := make([]string, len(strip))
	for i, s := range strip {
		name := ansi.Truncate(sessionName(s), stripNameCells, "…")
		text := "[" + itoa(i+1) + " " + m.agentGlyph(s.Provider) + " " + name + "]"
		if i == sel {
			text = lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent).Render(text)
		}
		labels[i] = text
	}
	hints = m.agentsStripStatus(hints)
	hintText := dimText(hints)
	avail := w - ansi.StringWidth(prefix) - ansi.StringWidth(hints) - 2

	// fitCount is how many labels from start fit, reserving room for the more
	// marker whenever some remain hidden.
	fitCount := func(start int) int {
		used, n := 0, 0
		for i := start; i < len(labels); i++ {
			add := ansi.StringWidth(labels[i])
			if n > 0 {
				add++
			}
			hidden := len(labels) - (i + 1) + start
			reserve := 0
			if hidden > 0 {
				reserve = 1 + ansi.StringWidth(moreLabel(hidden))
			}
			if used+add+reserve > avail {
				break
			}
			used += add
			n++
		}
		return n
	}
	start := 0
	n := fitCount(0)
	for sel >= start+n && start < sel {
		start++
		n = fitCount(start)
	}
	if n == 0 {
		n = 1 // the selected label stays visible even if it must be cut
		start = sel
	}
	parts := append([]string(nil), labels[start:start+n]...)
	if hidden := len(labels) - n; hidden > 0 {
		parts = append(parts, dimText(moreLabel(hidden)))
	}
	return leftRight(prefix+strings.Join(parts, " "), hintText, w)
}

func moreLabel(n int) string { return "[+" + itoa(n) + " more]" }

// fullHeaderTitle: provider · profile · model │ state.
func (m Model) fullHeaderTitle(s agentsapi.Session) string {
	parts := []string{providerName(s.Provider)}
	if s.ProfileName != "" {
		parts = append(parts, s.ProfileName)
	}
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	return m.agentGlyph(s.Provider) + " " + strings.Join(parts, " · ") + " │ " + stateDot(s.State)
}

// compactHeader is the one-row header for panes under 100 cells.
func (m Model) compactHeader(s agentsapi.Session, w int) string {
	left := m.agentGlyph(s.Provider) + " " + s.ProfileName
	if u := m.usageText(s, true); u != "" {
		left += " · " + u
	}
	return leftRight(left, dimText("u: Usage"), w)
}

// usageRow is the second header row: usage text plus the usage hint.
func (m Model) usageRow(s agentsapi.Session, w int) string {
	return leftRight(m.usageText(s, false), dimText("u: Usage"), w)
}

// usageText is the headline for the session's account, dim and marked when the
// last refresh failed. Empty when no accounts backend exists.
func (m Model) usageText(s agentsapi.Session, compact bool) string {
	if m.agents.api.Accounts == nil {
		return ""
	}
	e, ok := m.agents.usage[s.AccountID]
	if !ok || !e.have {
		if ok && e.err != "" {
			return dimText("usage unavailable")
		}
		return dimText("usage loading…")
	}
	var text string
	if compact {
		text = usagefmt.Compact(e.usage.Headline)
	} else {
		text = usagefmt.Headline(e.usage.Headline, time.Now())
	}
	if text == "" {
		text = "no usage data"
	}
	if e.stale {
		return dimText(text + " (stale)")
	}
	return text
}

// placeholderArea stands in for the terminal until it is attached.
func placeholderArea(s agentsapi.Session, w, rows int) []string {
	lines := []string{sessionName(s), dimText(s.State)}
	return centered(lines, w, rows)
}

func emptyArea(branch string, w, rows int) []string {
	lines := []string{"No active agents", ""}
	if branch != "" {
		lines = append(lines, "Start an agent in "+branch, "")
	}
	lines = append(lines, "[n] New agent", "", "[u] View account usage")
	return centered(lines, w, rows)
}

// centered centers lines in a w x rows box (cropping from the bottom).
func centered(lines []string, w, rows int) []string {
	top := (rows - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	out := make([]string, 0, rows)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range lines {
		l = ansi.Truncate(l, w, "…")
		pad := (w - ansi.StringWidth(l)) / 2
		out = append(out, strings.Repeat(" ", max(pad, 0))+l)
	}
	for len(out) < rows {
		out = append(out, "")
	}
	return out[:rows]
}
