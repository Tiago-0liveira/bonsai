package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// shellView renders the redesigned shell: header, empty body, footer. Every
// line is exactly the viewport width and the total never exceeds its height.
func (m Model) shellView() string {
	if !m.ready {
		return "Loading bonsai…"
	}
	regions, ok := shellGeometry(m.width, m.height, 0, 0)
	if !ok {
		return m.shellResizeNotice()
	}
	if m.modal != nil {
		return clampHeight(truncateToWidth(m.modal.View(), m.width), m.height)
	}

	p := theme.Current
	blank := shellRow(regions.width, p.Background, "", "")
	lines := m.shellHeader(regions)
	for i := 0; i < regions.submenu+regions.body+regions.detail; i++ {
		lines = append(lines, blank)
	}
	lines = append(lines, m.shellFooter(regions)...)
	return strings.Join(lines, "\n")
}

// shellResizeNotice is the bounded fallback for a viewport smaller than the
// chrome needs; it fits whatever size it is given.
func (m Model) shellResizeNotice() string {
	lines := []string{
		"bonsai redesign preview",
		fmt.Sprintf("needs %dx%d, have %dx%d", shellMinWidth, shellMinHeight, m.width, m.height),
		"resize the terminal",
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, max(m.width, 1), "")
	}
	return clampHeight(strings.Join(lines, "\n"), m.height)
}

// seg styles one run of text on a background; the background is set on every
// run so resets inside a row never punch holes in the band.
func seg(fg, bg lipgloss.Color, bold bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(bold)
}

// shellRow joins left and right (already styled on bg) into one width-exact
// row, padding or truncating the left side as needed.
func shellRow(width int, bg lipgloss.Color, left, right string) string {
	if lipgloss.Width(right) > width {
		right = ansi.Truncate(right, width, "")
	}
	room := width - lipgloss.Width(right)
	if lipgloss.Width(left) > room {
		left = ansi.Truncate(left, room, "")
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	return left + seg("", bg, false).Render(strings.Repeat(" ", gap)) + right
}

func (m Model) shellHeader(r shellRegions) []string {
	p := theme.Current
	brand := seg(p.Accent, p.Surface, true).Render(" " + m.chrome(workspaceBrandIcon(m.agentGlyphsASCII())+" BONSAI"))
	repo := filepath.Base(m.repoDir)
	right := seg(p.Muted, p.Surface, false).Render(m.chrome(repo+" · preview") + " ")
	rule := seg(p.Border, p.Background, false).Render(m.chrome(strings.Repeat("─", r.width)))
	return []string{
		shellRow(r.width, p.Surface, brand, right),
		shellRow(r.width, p.Surface, m.shellTabs(r.width), ""),
		rule,
	}
}

func workspaceBrandIcon(ascii bool) string {
	if ascii {
		return workspaceInfo[wsGarden].ascii
	}
	return workspaceInfo[wsGarden].icon
}

// shellTabs draws the five workspace tabs: a filled accent block for the
// selected one, quiet muted text for the rest. Narrow widths drop the gaps
// between tabs but never a label.
func (m Model) shellTabs(width int) string {
	p := theme.Current
	gap, margin := "  ", " "
	if width < shellCompactBelow {
		gap = ""
	}
	var b strings.Builder
	b.WriteString(seg("", p.Surface, false).Render(margin))
	for i := workspace(0); i < workspaceCount; i++ {
		info := workspaceInfo[i]
		icon := info.icon
		if m.agentGlyphsASCII() {
			icon = info.ascii
		}
		label := " " + m.chrome(icon+" "+info.name) + " "
		if i == m.workspace {
			b.WriteString(seg(p.Background, p.Accent, true).Render(label))
		} else {
			b.WriteString(seg(p.Muted, p.Surface, false).Render(label))
		}
		if gap != "" && i < workspaceCount-1 {
			b.WriteString(seg("", p.Surface, false).Render(gap))
		}
	}
	return b.String()
}

func (m Model) shellFooter(r shellRegions) []string {
	p := theme.Current
	where := seg(p.Text, p.Surface, true).Render(" " + m.workspace.name())

	// Hints are dropped from the end until the line fits.
	hints := m.shellHints()
	var hintLine string
	for n := len(hints); n >= 0; n-- {
		var parts []string
		for _, h := range hints[:n] {
			parts = append(parts, seg(p.Accent, p.Background, false).Render(m.chrome(h.key))+
				seg(p.Muted, p.Background, false).Render(" "+h.desc))
		}
		hintLine = seg("", p.Background, false).Render(" ") + strings.Join(parts, seg("", p.Background, false).Render("  "))
		if lipgloss.Width(hintLine) <= r.width {
			break
		}
	}

	focus := seg(p.Warning, p.Surface, true).Render(" FOCUS: " + m.workspace.name() + " / " + m.currentShellFocus().String())
	status := "preview · shell only"
	if m.status != "" {
		status = m.status
	}
	statusSeg := seg(p.Muted, p.Surface, false).Render(m.chrome(status) + " ")
	return []string{
		shellRow(r.width, p.Surface, where, ""),
		shellRow(r.width, p.Background, hintLine, ""),
		shellRow(r.width, p.Surface, focus, statusSeg),
	}
}
