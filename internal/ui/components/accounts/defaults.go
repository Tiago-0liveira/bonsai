package accounts

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
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
	data   agentsapi.Defaults
	form   launchform.Form
	zone   int
	// cur mirrors the form's cursor: launchform does not expose it and the
	// modal must know when "down" has run past the last field.
	cur     int
	saving  bool
	saveErr string
	saved   bool // "Saved" is showing for a few seconds
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
	return m, saveCmd(m.api, m.selected, m.next(&m.latest.save), m.df.form.Changes())
}

func (m Model) defaultsView(rw, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	switch {
	case m.df.err != "" && !m.df.loaded:
		return []string{dim.Render("Defaults unavailable"), dim.Render(m.df.err)}
	case !m.df.loaded:
		return []string{dim.Render("Loading defaults…")}
	}

	slot := ""
	switch {
	case m.df.saveErr != "":
		slot = lipgloss.NewStyle().Foreground(th.Danger).Render("Couldn't save: " + m.df.saveErr)
	case m.df.saving:
		slot = dim.Render("Saving…")
	}
	buttons := button("Reset", m.df.zone == zoneReset, false) + "  " + button("Save defaults", m.df.zone == zoneSave, false)
	tail := []string{
		dim.Render("These defaults apply to new sessions."),
		dim.Render("Running agents are not affected."),
		slot,
		strings.Repeat(" ", max(rw-lipgloss.Width(buttons), 0)) + buttons,
	}
	form := strings.Split(m.df.form.View(rw, max(rows-len(tail), 0)), "\n")
	if rows <= len(tail) {
		form = nil
	}
	return append(form, tail...)
}
