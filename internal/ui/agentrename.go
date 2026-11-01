package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// renameNameLimit mirrors agenthost.MaxNameRunes; the backend is the authority
// and this only stops typing past what it would reject.
const renameNameLimit = 80

// sessionsRenameMsg asks the model to save a session's new name.
type sessionsRenameMsg struct{ id, name string }

// agentRenamedMsg is the answer to a rename request.
type agentRenamedMsg struct {
	name string
	err  error
}

// startRename opens the inline name input on the selected session, prefilled
// with its current name.
func (p sessionsPicker) startRename() (sessionsPicker, tea.Cmd) {
	s, ok := p.selected()
	if !ok {
		return p, nil
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = renameNameLimit
	ti.SetValue(sessionName(s))
	ti.CursorEnd()
	p.rename, p.renameID, p.renaming = ti, s.ID, true
	return p, p.rename.Focus()
}

// updateRename handles keys while the name input is open: enter saves, esc
// cancels, everything else edits.
func (p sessionsPicker) updateRename(k tea.KeyMsg) (sessionsPicker, tea.Cmd) {
	switch k.String() {
	case "esc", "ctrl+c":
		p.renaming = false
		p.rename.Blur()
		return p, nil
	case "enter":
		name := strings.TrimSpace(p.rename.Value())
		if name == "" {
			return p, nil
		}
		id := p.renameID
		p.renaming = false
		p.rename.Blur()
		return p, func() tea.Msg { return sessionsRenameMsg{id: id, name: name} }
	}
	var cmd tea.Cmd
	p.rename, cmd = p.rename.Update(k)
	return p, cmd
}

func (p sessionsPicker) renameRow(iw int) string {
	q := p.rename
	q.Width = max(iw-8, 1)
	return lipgloss.NewStyle().Foreground(theme.Current.Accent).Bold(true).Render("Name: ") + q.View()
}

func renameSessionCmd(ctx context.Context, s agentsapi.Sessions, id, name string) tea.Cmd {
	return func() tea.Msg {
		saved, err := s.Rename(ctx, id, name)
		return agentRenamedMsg{name: saved, err: err}
	}
}

// openRenameSelected opens the sessions picker on the Agents tab's selected
// session with the name input already open.
func (m Model) openRenameSelected() (tea.Model, tea.Cmd) {
	if !m.agents.active() {
		m.setStatus(unsupportedNotice, false)
		return m, nil
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	s, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok {
		m.setStatus("agents: no session to rename", false)
		return m, nil
	}
	nm, cmd := m.openAgentsPicker()
	m = nm.(Model)
	o := m.agents.overlay
	if o == nil || o.sessions == nil {
		return m, cmd
	}
	for i, v := range o.sessions.visible() {
		if v.ID == s.ID {
			o.sessions.cursor = i
		}
	}
	o.sessions.sync()
	sp, rcmd := o.sessions.startRename()
	m.setAgentOverlay(agentOverlay{sessions: &sp})
	return m, tea.Batch(cmd, rcmd)
}

func (m Model) onAgentRenamed(msg agentRenamedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus("agents: could not rename: "+sanitizeAgentText(msg.err.Error()), true)
	} else {
		m.setStatus("agents: renamed to "+sanitizeAgentText(msg.name), false)
	}
	return m, m.relistAgents()
}
