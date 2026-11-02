package accounts

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Hint is one key and what it does, for the shell footer and help.
type Hint struct{ Key, Desc string }

// Dirty reports unsaved defaults, so the parent can keep them visible.
func (m Model) Dirty() bool { return m.dirty() }

// Section is the name of the active section.
func (m Model) Section() string { return [tabCount]string{"Overview", "Usage", "Defaults"}[m.tab] }

// Label is "Provider / name" of the selected account, or "" when none is.
func (m Model) Label() string {
	a, ok := m.Current()
	if !ok {
		return ""
	}
	return providerName(a.Provider) + " / " + a.Name
}

// InProfiles reports that the profile list, not an inspector section, has focus.
func (m Model) InProfiles() bool { return m.focus == focusProfiles }

// Show jumps the view to a section and, when id is listed, to that account;
// an empty id keeps the selection. Moving off an account with unsaved
// defaults asks first, so a jump from another workspace never drops a draft.
func (m Model) Show(id agents.AccountID, tab Tab) (Model, tea.Cmd) {
	m.tab = min(max(tab, TabOverview), tabCount-1)
	m.focus = focusProfiles
	m = m.syncFocus()
	if id == "" || id == m.selected {
		return m, nil
	}
	if !m.listed {
		// The first list honors the selection it finds.
		m.selected = id
		return m, nil
	}
	if !m.hasAccount(id) {
		return m, nil
	}
	return m.guard(func(m Model) (Model, tea.Cmd) { return m.selectAccount(id) })
}

func (m Model) hasAccount(id agents.AccountID) bool {
	for _, a := range m.accounts {
		if a.ID == id {
			return true
		}
	}
	return false
}

// Hints are the keys live in the current state, most useful first. Esc is
// described for a workspace body: from the profile list it leaves to the shell.
func (m Model) Hints() []Hint {
	switch {
	case m.dlg != nil:
		return []Hint{{"←/→", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}}
	case m.add.active:
		switch m.add.stage {
		case stageName:
			return []Hint{{"enter", "continue"}, {"esc", "back"}}
		case stageContinue:
			return []Hint{{"enter", "continue in terminal"}, {"esc", "back"}}
		}
		return []Hint{{"↑/↓", "provider"}, {"enter", "continue"}, {"esc", "back"}}
	case m.formEditing():
		return []Hint{{"↑/↓", "choice"}, {"enter", "select"}, {"tab", "section"}, {"esc", "cancel"}}
	case !m.listed:
		return []Hint{{"esc", "workspaces"}}
	case m.listErr != "":
		return []Hint{{"r", "retry"}, {"n", "add"}, {"esc", "workspaces"}}
	case m.selected == "":
		return []Hint{{"n", "add"}, {"esc", "workspaces"}}
	}
	if m.focus == focusProfiles {
		return []Hint{{"↑/↓", "account"}, {"enter", "inspect"}, {"tab", "section"}, {"n", "add"}, {"esc", "workspaces"}}
	}
	switch m.tab {
	case TabDefaults:
		return []Hint{{"↑/↓", "field"}, {"enter", "edit"}, {"ctrl+s", "save"}, {"tab", "section"}, {"esc", "profiles"}}
	case TabUsage:
		h := []Hint{{"r", "refresh"}}
		if m.usageScrollMax() > 0 {
			h = append(h, Hint{"↑/↓", "scroll"})
		}
		return append(h, Hint{"tab", "section"}, Hint{"esc", "profiles"})
	}
	return []Hint{{"←/→", "action"}, {"enter", "select"}, {"tab", "section"}, {"esc", "profiles"}}
}
