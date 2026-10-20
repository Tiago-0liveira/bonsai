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
	submenu := 0
	if m.workspace == wsSettings || m.workspace == wsGarden {
		submenu = settingsSubmenuRows
	}
	regions, ok := shellGeometry(m.width, m.height, submenu, 0)
	if !ok {
		return m.shellResizeNotice()
	}
	if m.modal != nil {
		return clampHeight(truncateToWidth(m.modal.View(), m.width), m.height)
	}

	p := theme.Current
	blank := shellRow(regions.width, p.Background, "", "")
	lines := m.shellHeader(regions)
	if m.workspace == wsSettings {
		lines = append(lines, m.settingsSubmenu(regions.width)...)
		lines = append(lines, m.settingsBody(regions.width, regions.body)...)
	} else if m.workspace == wsAccounts {
		lines = append(lines, m.accountsBody(regions.width, regions.body)...)
	} else if m.workspace == wsGarden {
		lines = append(lines, m.gardenSubmenu(regions.width)...)
		lines = append(lines, m.gardenBody(regions.width, regions.body)...)
	} else {
		for i := 0; i < regions.submenu+regions.body+regions.detail; i++ {
			lines = append(lines, blank)
		}
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
	if m.workspace == wsGarden {
		right = m.gardenHeaderRight()
	}
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
	where := seg(p.Text, p.Surface, true).Render(" " + m.shellWhere())

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

	focus := seg(p.Warning, p.Surface, true).Render(" FOCUS: " + m.shellFocusLabel())
	status := "preview · shell only"
	switch m.workspace {
	case wsSettings:
		status = "Local preferences"
	case wsAccounts:
		status = "Local profiles"
	case wsGarden:
		status = m.gardenStatusLine()
	}
	statusStyle := seg(p.Muted, p.Surface, false)
	if m.status != "" {
		status = m.status
		if strings.HasPrefix(status, "Save failed") {
			statusStyle = seg(p.Danger, p.Surface, true)
		}
	}
	statusSeg := statusStyle.Render(m.chrome(status) + " ")
	return m.settingsASCII([]string{
		shellRow(r.width, p.Surface, where, ""),
		shellRow(r.width, p.Background, hintLine, ""),
		shellRow(r.width, p.Surface, focus, statusSeg),
	})
}

// shellWhere is the footer's location: the workspace, and for Settings the
// category and section the cursor is in.
func (m Model) shellWhere() string {
	if m.workspace == wsAccounts {
		return m.accountsWhere()
	}
	if m.workspace == wsGarden {
		return m.gardenWhere()
	}
	if m.workspace != wsSettings {
		return m.workspace.name()
	}
	return "Settings / " + m.curCategory().name + " / " + m.curSection().name
}

// shellFocusLabel names the focus owner. Inside Settings it names the category
// and marks a text edit, matching the design's "Settings / Appearance".
func (m Model) shellFocusLabel() string {
	f := m.currentShellFocus()
	if m.workspace == wsAccounts {
		return m.accountsFocusLabel(f)
	}
	if m.workspace == wsGarden && m.garden.api != nil && f == shellBody {
		return m.gardenFocusLabel()
	}
	if m.workspace == wsSettings {
		switch f {
		case shellBody:
			return "Settings / " + m.curCategory().name
		case shellInput:
			return "Settings / " + m.curCategory().name + " / Input"
		}
	}
	return m.workspace.name() + " / " + f.String()
}
