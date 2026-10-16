package accounts

import (
	"maps"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/launchform"
)

// Update handles keys and backend results.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.onKey(msg)
	case ReloadMsg:
		return m, m.relist(m.selected)
	case listMsg:
		return m.onList(msg)
	case describeMsg:
		return m.onDescribe(msg), nil
	case usageMsg:
		return m.onUsage(msg), nil
	case defaultsMsg:
		return m.onDefaults(msg), nil
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

func (m Model) onList(msg listMsg) (Model, tea.Cmd) {
	if msg.seq != m.latest.list {
		return m, nil
	}
	m.listed = true
	if msg.err != nil {
		m.listErr = sanitize(msg.err)
		return m, nil
	}
	m.listErr = ""
	m.accounts = msg.accounts

	ids := m.orderedIDs()
	has := func(id agents.AccountID) bool { return id != "" && slices.Contains(ids, id) }

	pick := agents.AccountID("")
	switch {
	case m.pendingAdd != nil && m.findNew() != "":
		pick = m.findNew()
	case has(msg.want):
		pick = msg.want
	case has(m.selected):
		pick = m.selected
	case len(ids) > 0:
		pick = ids[0]
	}
	m.pendingAdd = nil

	if pick == "" {
		// Nothing to show but the add form.
		m.resetAccount()
		m.selected = ""
		m.add = newAddState()
		m.add.active = true
		return m, nil
	}
	if pick != m.selected {
		m.resetAccount()
	}
	m.selected = pick
	return m, m.loadSelected()
}

// findNew is the account the last terminal add flow created, or "" when it
// did not get created (the user backed out).
func (m Model) findNew() agents.AccountID {
	for _, a := range m.accounts {
		if m.pendingAdd != nil && a.Provider == m.pendingAdd.provider && a.Name == m.pendingAdd.name {
			return a.ID
		}
	}
	return ""
}

func (m Model) onDescribe(msg describeMsg) Model {
	if msg.id != m.selected || msg.seq != m.latest.describe {
		return m
	}
	m.describing = false
	if msg.err != nil {
		m.describeErr = sanitize(msg.err)
		return m
	}
	m.describeErr = ""
	m.accounts = slices.Clone(m.accounts)
	for i := range m.accounts {
		if m.accounts[i].ID == msg.id {
			m.accounts[i].Info = msg.info
		}
	}
	return m
}

func (m Model) onUsage(msg usageMsg) Model {
	if msg.id != m.selected || msg.seq != m.latest.usage {
		return m
	}
	m.usageLoading = false
	prev, have := m.usage[msg.id]
	if msg.err != nil {
		if !have {
			m.usageErr = sanitize(msg.err)
			return m
		}
		// Keep the last good numbers on screen, marked stale.
		prev.Stale, prev.Err = true, sanitize(msg.err)
		msg.usage = prev
	}
	m.usageErr = ""
	m.usage = maps.Clone(m.usage)
	m.usage[msg.id] = msg.usage
	return m
}

func (m Model) onDefaults(msg defaultsMsg) Model {
	if msg.id != m.selected || msg.seq != m.latest.defaults {
		return m
	}
	if msg.err != nil {
		m.df.err = sanitize(msg.err)
		return m
	}
	m.df.err = ""
	if m.dirty() && !m.df.replace {
		// Unsaved edits win over a background reload.
		return m
	}
	d := msg.defaults
	m.df.data = d
	m.df.form = launchform.New(d.Fields, d.Models, d.Values, m.ascii)
	m.df.loaded, m.df.replace = true, false
	return m.syncFocus()
}

func (m Model) onSaved(msg savedMsg) (Model, tea.Cmd) {
	if msg.id != m.selected || msg.seq != m.latest.save {
		return m, nil
	}
	m.df.saving = false
	if msg.err != nil {
		m.df.saveErr = sanitize(msg.err)
		return m, nil
	}
	m.df.saveErr, m.df.saved, m.df.replace = "", true, true
	return m, tea.Batch(
		defaultsCmd(m.api, msg.id, m.next(&m.latest.defaults)),
		savedClearCmd(msg.id, msg.seq),
	)
}

func (m Model) onRemoved(msg removedMsg) (Model, tea.Cmd) {
	if msg.seq != m.latest.remove {
		return m, nil
	}
	m.ov.removing = false
	if msg.err != nil {
		m.ov.msg = "Couldn't remove: " + sanitize(msg.err)
		return m, nil
	}
	return m, m.relist(msg.neighbor)
}

// onKey routes a key by what is active: a dialog, the add form, an open
// dropdown, then the modal's own keys and the current tab's.
func (m Model) onKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case m.dlg != nil:
		return m.dialogKey(k)
	case m.add.active:
		return m.addKey(k)
	case m.formEditing():
		m.df.form, _ = m.df.form.Update(k)
		return m, nil
	case !m.listed:
		if k.String() == "esc" {
			return m, emit(CloseMsg{})
		}
		return m, nil
	}

	switch key := k.String(); key {
	case "esc":
		return m.guard(func(m Model) (Model, tea.Cmd) { return m, emit(CloseMsg{}) })
	case "j":
		return m.moveAccount(1)
	case "k":
		return m.moveAccount(-1)
	case "tab":
		return m.setTab((m.tab + 1) % tabCount).syncFocus(), nil
	case "shift+tab":
		return m.setTab((m.tab + tabCount - 1) % tabCount).syncFocus(), nil
	case "1", "2", "3":
		return m.setTab(Tab(key[0] - '1')).syncFocus(), nil
	case "ctrl+s":
		if m.tab == TabDefaults {
			return m.save()
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

// moveAccount steps the selection through the provider groups; past the last
// account it lands on "+ Add account". Leaving the account asks first when
// its defaults have unsaved edits.
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
		return m.guard(func(m Model) (Model, tea.Cmd) { return m.openAdd(), nil })
	}
	id := ids[i]
	return m.guard(func(m Model) (Model, tea.Cmd) { return m.selectAccount(id) })
}

func (m Model) selectAccount(id agents.AccountID) (Model, tea.Cmd) {
	if id != m.selected {
		m.resetAccount()
	}
	m.selected = id
	return m, m.loadSelected()
}
