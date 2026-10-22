package accounts

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// Update handles keys and backend results.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.onKey(msg)
	case ReloadMsg, listMsg, describeMsg, sidebarInfoMsg, usagestore.Result, defaultsMsg:
		return m.onBrowser(msg)
	case savedMsg:
		return m.onSaved(msg)
	case removedMsg:
		return m.onRemoved(msg)
	case savedClearMsg:
		if msg.id == m.selected && msg.seq == m.latest.save {
			m.df.saved = false
		}
	}
	return m, nil
}

// onBrowser lets the shared Browser apply a result, then updates what only
// this modal owns: the per-account view state and the Defaults form.
func (m Model) onBrowser(msg tea.Msg) (Model, tea.Cmd) {
	prev := m.selected
	var cmd tea.Cmd
	var ch Change
	m.Browser, cmd, ch = m.Browser.Update(msg)
	if m.selected != prev {
		m.resetAccount()
	}
	if ch&ChangedList != 0 && m.listErr == "" && len(m.accounts) == 0 && !m.embedded {
		// Nothing to show but the add form.
		m.add = newAddState()
		m.add.active = true
	}
	if ch&ChangedDefaults != 0 {
		m = m.onDefaults()
	}
	return m, cmd
}

func (m Model) onDefaults() Model {
	if m.defaultsErr != "" {
		m.df.err = m.defaultsErr
		return m
	}
	m.df.err = ""
	if m.dirty() && !m.df.replace {
		// Unsaved edits win over a background reload.
		return m
	}
	d := m.defaults
	form := launchform.New(d.Fields, d.Models, d.Values, m.ascii)
	if m.df.replace && m.df.hasSent {
		form = form.Carry(m.df.form, m.df.sent)
	}
	m.df.form = form
	m.df.loaded, m.df.replace, m.df.hasSent = true, false, false
	return m.syncFocus()
}

func (m Model) onSaved(msg savedMsg) (Model, tea.Cmd) {
	if msg.id != m.selected || msg.seq != m.latest.save {
		return m, nil
	}
	m.df.saving = false
	if msg.err != nil {
		m.df.saveErr = usagestore.Sanitize(msg.err)
		return m, nil
	}
	m.df.saveErr, m.df.saved, m.df.replace = "", true, true
	var reload tea.Cmd
	m.Browser, reload = m.Browser.ReloadDefaults()
	return m, tea.Batch(reload, savedClearCmd(msg.id, msg.seq))
}

func (m Model) onRemoved(msg removedMsg) (Model, tea.Cmd) {
	if msg.seq != m.latest.remove {
		return m, nil
	}
	m.ov.removing = false
	if msg.err != nil {
		m.ov.msg = "Couldn't remove: " + usagestore.Sanitize(msg.err)
		return m, nil
	}
	return m, m.Relist(msg.neighbor)
}

// onKey routes a key by what is active: a dialog, the add form, an open
// dropdown, then the modal's own keys and the current tab's.
func (m Model) onKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case m.dlg != nil:
		return m.dialogKey(k)
	case m.add.active:
		return m.addKey(k)
	case m.formEditing() && (k.String() == "tab" || k.String() == "shift+tab"):
		m.df.form, _ = m.df.form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	case m.formEditing():
		m.df.form, _ = m.df.form.Update(k)
		return m, nil
	}

	key := k.String()
	switch key {
	case "esc":
		if m.focus != focusProfiles {
			m.focus = focusProfiles
			return m.syncFocus(), nil
		}
		if m.embedded {
			// Leaving the workspace keeps the draft on this model, so there is
			// nothing to discard.
			return m, emit(CloseMsg{})
		}
		return m.guard(func(m Model) (Model, tea.Cmd) { return m, emit(CloseMsg{}) })
	case "tab", "shift+tab":
		delta := Tab(1)
		if key == "shift+tab" {
			delta = tabCount - 1
		}
		return m.setTab((m.tab + delta) % tabCount).syncFocus(), nil
	case "1", "2", "3":
		m.focus = focusContent
		return m.setTab(Tab(key[0] - '1')).syncFocus(), nil
	case "n":
		return m.guard(func(m Model) (Model, tea.Cmd) { return m.openAdd(), nil })
	case "ctrl+s":
		if m.tab == TabDefaults {
			return m.save()
		}
		return m, nil
	case "r":
		if m.listErr != "" {
			return m, m.Relist(m.selected)
		}
		if m.tab == TabUsage {
			return m.usageKey(k)
		}
	}
	if !m.listed {
		return m, nil
	}
	if m.focus == focusProfiles {
		switch key {
		case "j", "down":
			return m.moveAccount(1)
		case "k", "up":
			return m.moveAccount(-1)
		case "enter", "right":
			m.focus = focusContent
			return m.syncFocus(), nil
		}
		return m, nil
	}
	if m.selected == "" {
		return m, nil
	}
	switch m.tab {
	case TabUsage:
		return m.usageKey(k)
	case TabDefaults:
		return m.defaultsKey(k)
	}
	return m.overviewKey(k)
}

func (m Model) setTab(t Tab) Model {
	m.tab = t
	return m
}

// moveAccount steps through provider groups. Unsaved defaults require confirmation.
func (m Model) moveAccount(delta int) (Model, tea.Cmd) {
	ids := m.orderedIDs()
	if len(ids) == 0 {
		return m, nil
	}
	i := slices.Index(ids, m.selected) + delta
	switch {
	case i < 0:
		return m, nil
	case i >= len(ids):
		return m, nil
	}
	id := ids[i]
	return m.guard(func(m Model) (Model, tea.Cmd) { return m.selectAccount(id) })
}

func (m Model) selectAccount(id agents.AccountID) (Model, tea.Cmd) {
	if id != m.selected {
		m.resetAccount()
	}
	var cmd tea.Cmd
	m.Browser, cmd = m.Browser.Select(id)
	return m, cmd
}
