// Package terminal is the right-pane viewport showing process output or git log.
package terminal

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model wraps a scrollable viewport.
type Model struct {
	vp      viewport.Model
	title   string
	focused bool
}

// New builds an empty terminal pane.
func New() Model {
	return Model{vp: viewport.New(0, 0), title: "Output"}
}

// SetSize resizes the viewport.
func (m *Model) SetSize(w, h int) {
	m.vp.Width = w
	m.vp.Height = h
}

// SetContent replaces the displayed text, hard-wrapping to the viewport width so
// long process/log lines cannot overflow into neighboring panes. Keeps the view
// pinned to the bottom when already there (tail-follow behavior).
func (m *Model) SetContent(s string) {
	if m.vp.Width > 0 {
		s = lipgloss.NewStyle().Width(m.vp.Width).Render(s)
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(s)
	if atBottom {
		m.vp.GotoBottom()
	}
}

// SetTitle sets the pane header.
func (m *Model) SetTitle(t string) { m.title = t }

// Title returns the pane header.
func (m Model) Title() string { return m.title }

// Focus / Blur toggle focus.
func (m *Model) Focus() { m.focused = true }
func (m *Model) Blur()  { m.focused = false }

// Focused reports focus state.
func (m Model) Focused() bool { return m.focused }

// Update forwards scroll messages to the viewport when focused.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// View renders the viewport.
func (m Model) View() string { return m.vp.View() }
