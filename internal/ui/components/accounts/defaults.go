package accounts

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
)

// Focus zones of the Defaults tab.
const (
	zoneForm = iota
	zoneReset
	zoneSave
)

type defaultsState struct {
	loaded bool
	err    string
	form   launchform.Form
	zone   int
	// cur mirrors the form's cursor: launchform does not expose it and the
	// modal must know when "down" has run past the last field.
	cur     int
	saving  bool
	saveErr string
	saved   bool // "Saved" is showing for a few seconds
	// sent is the form as it was when Save went out; edits made after it are
	// carried over the reload that follows the save.
	sent    launchform.Form
	hasSent bool
	// replace lets the next Defaults result overwrite the form even though it
	// is dirty, which is the case right after a save.
	replace bool
}

// dirty reports unsaved edits on the selected account's defaults.
func (m Model) dirty() bool { return m.df.loaded && m.df.form.Dirty() }

// syncFocus shows the form's focus highlight only while it owns the keys.
func (m Model) syncFocus() Model {
	m.df.form = m.df.form.Focus(m.focus == focusContent && m.tab == TabDefaults && m.df.zone == zoneForm)
	return m
}

func (m Model) defaultsKey(k tea.KeyMsg) (Model, tea.Cmd) {
	if !m.df.loaded {
		return m, nil
	}
	key := k.String()
	if m.df.zone == zoneForm {
		if key == "down" && m.df.form.AtLast() && !m.df.form.Editing() {
			m.df.zone = zoneSave
			return m.syncFocus(), nil
		}
		m.df.form, _ = m.df.form.Update(k)
		return m, nil
	}

	switch key {
	case "left", "h":
		m.df.zone = zoneReset
	case "right", "l":
		m.df.zone = zoneSave
	case "up":
		m.df.zone = zoneForm
		return m.syncFocus(), nil
	case "enter":
		if m.df.zone == zoneReset {
			m.df.form = m.df.form.ResetAll()
			return m, nil
		}
		return m.save()
	}
	return m, nil
}

// save sends only the fields that changed.
func (m Model) save() (Model, tea.Cmd) {
	if !m.df.loaded || m.df.saving || !m.df.form.Dirty() {
		return m, nil
	}
	m.df.saving, m.df.saveErr = true, ""
	m.df.sent, m.df.hasSent = m.df.form, true
	return m, saveCmd(m.api, m.selected, m.next(&m.latest.save), m.df.form.Changes())
}
