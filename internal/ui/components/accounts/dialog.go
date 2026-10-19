package accounts

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// dialogRows is the body height: up to two message lines, a gap, the buttons.
	dialogRows = 4
)

// dialog is a two-button confirmation drawn over the modal. Focus starts on
// the first (safe) button; only the second runs confirm.
type dialog struct {
	title   string
	text    string
	cancel  string
	confirm string
	focus   int
	then    func(Model) (Model, tea.Cmd)
}

// guard runs act, unless the selected account has unsaved defaults: then it
// asks first. Unsaved edits are never dropped without a yes.
func (m Model) guard(act func(Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	if !m.dirty() {
		return act(m)
	}
	m.dlg = &dialog{
		title:   "Discard changes",
		text:    "Discard unsaved defaults?",
		cancel:  "Keep editing",
		confirm: "Discard",
		then: func(m Model) (Model, tea.Cmd) {
			// The edits go with the form: forget them before acting so the
			// action does not ask again.
			m.df = defaultsState{}
			return act(m)
		},
	}
	return m, nil
}

func (m Model) dialogKey(k tea.KeyMsg) (Model, tea.Cmd) {
	d := *m.dlg
	switch k.String() {
	case "esc":
		m.dlg = nil
	case "left", "h", "shift+tab":
		d.focus = 0
		m.dlg = &d
	case "right", "l", "tab":
		d.focus = 1
		m.dlg = &d
	case "enter":
		m.dlg = nil
		if d.focus == 1 {
			return d.then(m)
		}
	}
	return m, nil
}

// view fills the pane with a confirmation and identifies the affected profile.
func (d dialog) view(profile string, w, h int) string {
	inner := max(ContentWidth(w), 1)
	body := strings.Split(ansi.Wrap(d.text, inner, ""), "\n")
	body = body[:min(len(body), dialogRows-2)]
	for len(body) < dialogRows-1 {
		body = append(body, "")
	}
	body = append(body, button(d.cancel, d.focus == 0, false)+"  "+button(d.confirm, d.focus == 1, false))
	return FrameSingle(d.title+" · "+profile, body, "←/→ Choose  ·  Enter Confirm  ·  Esc Cancel", w, h)
}

// button is "[label]", reversed when focused and dim when unavailable.
func button(label string, focused, disabled bool) string {
	s := lipgloss.NewStyle()
	switch {
	case focused:
		s = s.Reverse(true).Bold(true)
	case disabled:
		s = s.Faint(true)
	}
	return s.Render("[" + label + "]")
}
