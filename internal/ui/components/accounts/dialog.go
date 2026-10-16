package accounts

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	dialogMaxW = 60
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

// view draws the dialog in the modal's frame style, sized for the message.
func (d dialog) view(frameW int) string {
	w := max(min(dialogMaxW, frameW-4), 12)
	inner := max(w-4, 1)
	body := strings.Split(ansi.Wrap(d.text, inner, ""), "\n")
	body = body[:min(len(body), dialogRows-2)]
	for len(body) < dialogRows-1 {
		body = append(body, "")
	}
	body = append(body, button(d.cancel, d.focus == 0, false)+"  "+button(d.confirm, d.focus == 1, false))
	return FrameSingle(d.title, body, "←/→ Choose  ·  Enter Confirm  ·  Esc Cancel", w, dialogRows+chrome)
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

// overlay draws box centered over base. Both are full-width lines; box rows
// replace the middle of base.
func overlay(base, box string, w int) string {
	lines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")
	boxW := 0
	for _, l := range boxLines {
		boxW = max(boxW, ansi.StringWidth(l))
	}
	top := max((len(lines)-len(boxLines))/2, 0)
	left := max((w-boxW)/2, 0)
	for i, bl := range boxLines {
		row := top + i
		if row >= len(lines) {
			break
		}
		lines[row] = ansi.Truncate(lines[row], left, "") + "\x1b[0m" + bl + "\x1b[0m" + ansi.Cut(lines[row], left+boxW, w)
	}
	return strings.Join(lines, "\n")
}
