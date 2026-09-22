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
	// follow keeps the view pinned to the bottom on new content (tail behavior).
	// When false, the scroll offset is preserved across content updates so the
	// view stays where the user left it (and starts at the top).
	follow bool
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
// long process/log lines cannot overflow into neighboring panes. In follow mode
// the view tail-follows the bottom; otherwise the scroll offset is preserved
// (defaulting to the top for fresh content).
func (m *Model) SetContent(s string) {
	if m.vp.Width > 0 {
		s = lipgloss.NewStyle().Width(m.vp.Width).Render(s)
	}
	if m.follow {
		atBottom := m.vp.AtBottom()
		m.vp.SetContent(s)
		if atBottom {
			m.vp.GotoBottom()
		}
		return
	}
	off := m.vp.YOffset
	m.vp.SetContent(s)
	m.vp.SetYOffset(off)
}

// SetFollow toggles tail-follow: true pins new content to the bottom (process
// output), false preserves the scroll position and starts at the top.
func (m *Model) SetFollow(f bool) { m.follow = f }

// EnsureVisible scrolls the viewport the minimum amount so that content line
// (0-indexed) is within the visible window.
func (m *Model) EnsureVisible(line int) {
	if line < 0 {
		line = 0
	}
	top := m.vp.YOffset
	if top < 0 {
		top = 0
	}
	bottom := top + m.vp.Height - 1
	switch {
	case line < top:
		m.vp.SetYOffset(line)
	case line > bottom:
		newOffset := line - m.vp.Height + 1
		if newOffset < 0 {
			newOffset = 0
		}
		m.vp.SetYOffset(newOffset)
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
