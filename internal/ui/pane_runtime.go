package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/terminal"
)

func newViewTerminals() map[viewID]*terminal.Model {
	out := make(map[viewID]*terminal.Model)
	for _, id := range registeredViews {
		if !terminalBackedView(id) {
			continue
		}
		t := terminal.New()
		t.SetTitle(viewTitle(id))
		out[id] = &t
	}
	return out
}

func (m *Model) initPaneRuntime() {
	if !m.dynamicLayout.valid() {
		m.dynamicLayout = defaultDynamicLayout()
	}
	if m.paneActive == nil {
		m.paneActive = map[paneID]viewID{}
	}
	if m.viewTerms == nil {
		m.viewTerms = newViewTerminals()
	}
	for _, pane := range m.dynamicLayout.panes() {
		active := m.paneActive[pane.ID]
		if !containsView(pane.Views, active) {
			active = pane.Views[0]
		}
		m.paneActive[pane.ID] = active
	}
	panes := m.dynamicLayout.panes()
	if len(panes) == 0 {
		m.dynamicLayout = defaultDynamicLayout()
		panes = m.dynamicLayout.panes()
	}
	if m.focusedPane == "" || !m.hasPane(m.focusedPane) {
		m.focusedPane = panes[0].ID
	}
	m.syncLegacyFocus()
}

func (m Model) hasPane(id paneID) bool {
	for _, pane := range m.dynamicLayout.panes() {
		if pane.ID == id {
			return true
		}
	}
	return false
}

func containsView(views []viewID, target viewID) bool {
	for _, id := range views {
		if id == target {
			return true
		}
	}
	return false
}

func (m Model) paneByID(id paneID) (paneSpec, bool) {
	for _, pane := range m.dynamicLayout.panes() {
		if pane.ID == id {
			return pane, true
		}
	}
	return paneSpec{}, false
}

func (m Model) activeView(id paneID) viewID {
	pane, ok := m.paneByID(id)
	if !ok || len(pane.Views) == 0 {
		return ""
	}
	active := m.paneActive[id]
	if containsView(pane.Views, active) {
		return active
	}
	return pane.Views[0]
}

func (m Model) focusedView() viewID {
	return m.activeView(m.focusedPane)
}

func (m *Model) syncLegacyFocus() {
	active := m.focusedView()
	if active == viewWorktrees {
		m.focus = focusList
		m.list.Focus()
		for _, term := range m.viewTerms {
			term.Blur()
		}
		return
	}

	m.focus = focusTerminal
	m.list.Blur()
	if tab, ok := tabForView(active); ok {
		m.rightTab = tab
	}
	for id, term := range m.viewTerms {
		if id == active {
			term.Focus()
		} else {
			term.Blur()
		}
	}
}

func (m *Model) viewTerm(id viewID) *terminal.Model {
	if m.viewTerms == nil {
		m.viewTerms = newViewTerminals()
	}
	if term, ok := m.viewTerms[id]; ok && term != nil {
		return term
	}
	t := terminal.New()
	t.SetTitle(viewTitle(id))
	m.viewTerms[id] = &t
	return &t
}

func (m Model) viewAvailable(id viewID) bool {
	if id == viewWorktrees || id == viewLog || id == viewProcesses || id == viewInspect {
		return true
	}
	tab, ok := tabForView(id)
	return ok && m.tabVisible(tab)
}

func (m Model) availableViews(pane paneSpec) []viewID {
	out := make([]viewID, 0, len(pane.Views))
	for _, id := range pane.Views {
		if m.viewAvailable(id) {
			out = append(out, id)
		}
	}
	return out
}

func (m *Model) normalizePaneActives() {
	for _, pane := range m.dynamicLayout.panes() {
		available := m.availableViews(pane)
		if len(available) == 0 {
			continue
		}
		active := m.paneActive[pane.ID]
		if !containsView(available, active) {
			m.paneActive[pane.ID] = available[0]
		}
	}
	m.syncLegacyFocus()
}

func (m *Model) focusPane(id paneID) {
	if !m.hasPane(id) {
		return
	}
	m.focusedPane = id
	m.normalizePaneActives()
	m.syncLegacyFocus()
}

func (m *Model) activateView(id viewID) bool {
	pane, ok := m.dynamicLayout.paneContaining(id)
	if !ok {
		return false
	}
	if !m.viewAvailable(id) {
		return false
	}
	m.paneActive[pane.ID] = id
	m.focusedPane = pane.ID
	m.syncLegacyFocus()
	return true
}

func (m *Model) cycleFocusedPaneView() bool {
	pane, ok := m.paneByID(m.focusedPane)
	if !ok {
		return false
	}
	views := m.availableViews(pane)
	if len(views) <= 1 {
		return false
	}
	active := m.activeView(pane.ID)
	idx := 0
	for i, id := range views {
		if id == active {
			idx = i
			break
		}
	}
	m.paneActive[pane.ID] = views[(idx+1)%len(views)]
	m.syncLegacyFocus()
	return true
}

func (m *Model) cyclePaneFocus() {
	panes := m.dynamicLayout.panes()
	if len(panes) <= 1 {
		return
	}
	idx := 0
	for i, pane := range panes {
		if pane.ID == m.focusedPane {
			idx = i
			break
		}
	}
	m.focusedPane = panes[(idx+1)%len(panes)].ID
	m.normalizePaneActives()
	m.syncLegacyFocus()
}

func (m Model) visibleViews() []viewID {
	out := make([]viewID, 0, len(m.dynamicLayout.panes()))
	for _, pane := range m.dynamicLayout.panes() {
		active := m.activeView(pane.ID)
		if active != "" && m.viewAvailable(active) {
			out = append(out, active)
		}
	}
	return out
}

func (m Model) viewVisible(id viewID) bool {
	for _, visible := range m.visibleViews() {
		if visible == id {
			return true
		}
	}
	return false
}

func (m *Model) focusedTerm() *terminal.Model {
	id := m.focusedView()
	if !terminalBackedView(id) {
		id = viewLog
	}
	return m.viewTerm(id)
}

func (m *Model) updateViewTerm(id viewID, msg tea.Msg) tea.Cmd {
	term := m.viewTerm(id)
	next, cmd := term.Update(msg)
	*term = next
	return cmd
}
