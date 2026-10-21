package ui

import tea "github.com/charmbracelet/bubbletea"

// The agent terminal controller (agentattach.go, agentterm.go) is one client with one live attachment,
// m.agents.view. Garden (planner and launch) and the Playground borrow it, one workspace at a time. These helpers
// are the workspace-neutral boundary: layout, key routing and message routing ask here instead of assuming a body.

// terminalShown reports whether a redesigned workspace is showing the live attachment.
func (m Model) terminalShown() bool {
	return m.plannerSessionShown() || m.pgTerminalShown()
}

// terminalTypingMode is true while the user types into the agent. Every key but Ctrl+] then belongs to it, so no
// shell, palette or workspace key can fire; a modal still owns the keyboard first.
func (m Model) terminalTypingMode() bool {
	return m.plannerTerminalMode() || m.pgTerminalMode()
}

// terminalAreaSize is the terminal area of the workspace that owns the attachment.
func (m Model) terminalAreaSize() (w, rows int) {
	if m.workspace == wsPlayground {
		return m.pgAreaSize()
	}
	return m.plannerAreaSize()
}

// terminalRelease drops the attachment. The holder, and the agent in it, keep running.
func (m *Model) terminalRelease() tea.Cmd {
	v := m.agents.view
	if v == nil {
		return nil
	}
	m.agents.view = nil
	return releaseCmd(v.stream, v.term)
}
