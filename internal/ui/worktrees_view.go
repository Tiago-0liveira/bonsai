package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func (m Model) worktreesSubmenu(width int) []string {
	p := theme.Current
	crumb := seg(p.Muted, p.Surface, false).Render(" Worktrees  ›  ") + seg(p.Text, p.Surface, true).Render(wtSubNames[m.wt.sub])
	var tabs []string
	for sub := wtSub(0); sub < wtSubCount; sub++ {
		fg, bg := p.Muted, p.Surface
		if sub == m.wt.sub {
			fg, bg = p.Accent, p.Raised
		}
		tabs = append(tabs, seg(fg, bg, sub == m.wt.sub).Render(" "+wtSubNames[sub]+" "))
	}
	return m.settingsASCII([]string{shellRow(width, p.Surface, crumb, ""), shellRow(width, p.Surface, strings.Join(tabs, " "), ""), shellRow(width, p.Background, seg(p.Border, p.Background, false).Render(strings.Repeat("─", width)), "")})
}
func (m Model) worktreesBodyRows() int {
	return max(1, m.height-shellHeaderRows-shellFooterRows-settingsSubmenuRows)
}
func (m Model) worktreesPaneSize() (width, rows int) {
	rows = max(1, m.worktreesBodyRows()-2)
	if m.width >= 100 {
		return max(1, m.width-m.width*35/100-2), rows
	}
	return max(1, m.width), rows
}
func (m *Model) worktreesLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	w, h := m.worktreesPaneSize()
	lw := m.width - 2
	if m.width >= 100 {
		lw = m.width*35/100 - 1
	}
	m.list.SetSize(max(1, lw), h)
	th := h - 1
	if m.rightTab == tabProcs {
		th -= min(m.procFooterHeight(), max(1, h/2))
	}
	m.term.SetSize(w, max(1, th))
	m.sizeAgentOverlay()
	if m.agentPaneModal() {
		m.modal.SetSize(m.agentPaneSize())
	}
}
func wtFitLines(lines []string, width, rows int) []string {
	p := theme.Current
	out := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], width, "")
		}
		out = append(out, shellRow(width, p.Background, onBackground(line, p.Background), ""))
	}
	return out
}
func (m Model) worktreesBody(width, rows int) []string {
	p := theme.Current
	head := " " + strings.ToUpper(wtSubNames[m.wt.sub])
	if m.wt.sub == wtCheckouts {
		head += " / " + m.sort.label()
	}
	if m.wt.searching {
		head = " / " + m.wt.search.View()
	} else if q := m.wt.query[m.wt.sub]; q != "" {
		head += " / " + q
	}
	lines := []string{shellRow(width, p.Background, seg(p.Text, p.Background, true).Render(head), ""), blankRow(width, p.Background)}
	h := max(0, rows-2)
	if width >= 100 {
		lw := width * 35 / 100
		left := m.worktreesList(lw-1, h)
		right := m.worktreesDetail(width-lw-2, h)
		for i := 0; i < h; i++ {
			lines = append(lines, left[i]+seg(p.Border, p.Background, false).Render(" │")+right[i])
		}
	} else if m.focus == focusTerminal {
		lines = append(lines, m.worktreesDetail(width, h)...)
	} else {
		lines = append(lines, m.worktreesList(width, h)...)
	}
	if m.rightTab == tabAgents {
		return wtFitLines(lines, width, rows)
	}
	return m.settingsASCII(wtFitLines(lines, width, rows))
}
func (m Model) worktreesList(width, rows int) []string {
	if m.wt.sub == wtCheckouts {
		if len(m.worktrees) == 0 && m.wt.readErr == "" {
			return wtFitLines([]string{" No checkouts loaded. Refresh to retry."}, width, rows)
		}
		return m.settingsASCII(wtFitLines(strings.Split(m.list.View(), "\n"), width, rows))
	}
	records := m.worktreesRows()
	s := m.wt.sub
	var lines []string
	if len(records) == 0 {
		msg := " No " + strings.ToLower(wtSubNames[s]) + " in this repository."
		if s == wtBranches && !m.wt.branchLoaded {
			msg = " Loading branches…"
		}
		if m.wt.query[s] != "" {
			msg = " No matching rows. Clear the filter to see all."
		}
		return wtFitLines([]string{msg}, width, rows)
	}
	rowH := 3
	if m.width < 100 {
		rowH = 2
	}
	top := max(0, m.wt.index[s]-max(1, rows/rowH)+1)
	for _, r := range records[top:] {
		p := theme.Current
		bg, fg := p.Surface, p.Text
		mark := "  "
		if r.id == m.wt.selected[s] {
			bg, fg = p.Raised, p.Accent
			mark = "▌ "
		}
		lines = append(lines, shellRow(width, bg, seg(fg, bg, r.id == m.wt.selected[s]).Render(mark+pgClean(r.title, width-2)), ""))
		lines = append(lines, shellRow(width, bg, seg(p.Muted, bg, false).Render("  "+pgClean(r.desc, width-2)), ""))
		if rowH == 3 {
			lines = append(lines, blankRow(width, p.Background))
		}
		if len(lines) >= rows {
			break
		}
	}
	return m.settingsASCII(wtFitLines(lines, width, rows))
}
func (m Model) worktreesDetail(width, rows int) []string {
	if m.wt.sub != wtCheckouts {
		r, ok := m.worktreesRow()
		if !ok {
			return wtFitLines(nil, width, rows)
		}
		if r.path == "" {
			return wtFitLines([]string{" " + pgClean(r.title, width-1), "", " No checkout for this row.", " Enter creates one with the existing workflow."}, width, rows)
		}
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return wtFitLines(nil, width, rows)
	}
	var lines []string
	title := pgClean(wt.Branch, width-1)
	if title == "" {
		title = "(detached)"
	}
	if m.rightTab == tabAgents {
		lines = strings.Split(m.renderAgentsPane(width, rows-1), "\n")
	} else {
		lines = strings.Split(m.term.View(), "\n")
		if m.rightTab == tabProcs {
			footer := strings.Split(m.renderProcFooter(), "\n")
			avail := min(len(footer), max(1, (rows-1)/2))
			// Keep the selected process visible even with more processes than rows.
			if len(footer) > avail {
				footer = m.worktreesProcFooter(width, avail)
			}
			lines = append(lines, footer...)
		}
	}
	header := m.chrome(" " + title + " / " + strings.TrimSpace(tabLabel(m.rightTab, true)))
	lines = append([]string{seg(theme.Current.Text, theme.Current.Surface, true).Render(ansi.Truncate(header, width, "…"))}, lines...)
	return wtFitLines(lines, width, rows)
}
func (m Model) worktreesProcFooter(width, rows int) []string {
	var lines []string
	if m.procSearchActive {
		lines = append(lines, "/ "+m.procSearchInput.View())
	} else {
		lines = append(lines, m.procScrollState())
	}
	if p, ok := m.activeProcess(m.currentPath()); ok {
		lines = append(lines, procLine(p, m.procs.LastURL(p.ID)))
	}
	return wtFitLines(lines, width, rows)
}
func (m Model) worktreesWhere() string {
	where := "Worktrees / " + wtSubNames[m.wt.sub]
	if m.wt.sub != wtCheckouts {
		if r, ok := m.worktreesRow(); ok {
			where += " / " + pgClean(r.title, 70)
		}
	} else if wt, ok := m.selectedWorktree(); ok {
		where += " / " + pgClean(wt.Branch, 70)
	}
	if m.focus == focusTerminal {
		where += " / " + strings.TrimSpace(tabLabel(m.rightTab, true))
	}
	return where
}
func (m Model) worktreesStatusLine() string {
	if m.wt.readErr != "" {
		return "Checkouts stale: " + pgClean(m.wt.readErr, 90)
	}
	if m.wt.sub == wtBranches && m.wt.branchErr != "" {
		return "Branches stale: " + pgClean(m.wt.branchErr, 90)
	}
	if m.wt.sub == wtPRs && m.wt.prErr != "" {
		return "PRs stale: " + pgClean(m.wt.prErr, 90)
	}
	return fmt.Sprintf("Local repository · %d checkouts", len(m.worktrees))
}
func (m Model) worktreesHints() []shellHint {
	if m.worktreesCapturing() {
		return []shellHint{{"enter/esc", "finish/cancel"}}
	}
	if m.wt.menu {
		return []shellHint{{"tab/shift+tab", "submenu"}, {"enter", "rows"}, {"esc", "workspaces"}}
	}
	if m.focus == focusTerminal {
		if m.rightTab == tabAgents {
			return []shellHint{{"enter", "control"}, {m.keys.AgentPlayground.Help().Key, "Playground (observe)"}, {m.keys.AgentAll.Help().Key, "all"}, {m.keys.AgentNew.Help().Key, "new"}, {m.keys.AgentAccounts.Help().Key, "account"}, {m.keys.AgentUsage.Help().Key, "usage"}, {"esc", "list"}}
		}
		if m.rightTab == tabProcs {
			return []shellHint{{"↑/↓", "process"}, {"j/k", "scroll"}, {"/", "output search"}, {"G", "follow"}, {m.keys.Kill.Help().Key, "kill"}, {"esc", "list"}}
		}
		return []shellHint{{"↑/↓", "scroll"}, {"shift+tab", "detail tab"}, {"esc", "list"}}
	}
	return []shellHint{{"↑/↓", "select"}, {"enter", "inspect"}, {m.keys.Create.Help().Key, "new"}, {m.keys.Filter.Help().Key, "filter"}, {"tab", "submenu"}, {"esc", "parent"}}
}
