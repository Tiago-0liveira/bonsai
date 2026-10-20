package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// accountsState is the Accounts workspace body: the shared accounts component
// in embedded mode. It is created once, so selection, scroll, section and
// unsaved defaults survive refreshes and workspace switches; leaving never
// touches sessions, which this workspace does not own.
type accountsState struct {
	model accounts.Model
	// present is false without an accounts backend; the body then says so.
	present bool
	// started: the first load was issued (on first visit, not at startup).
	started bool
}

func newAccountsState(api agentsapi.Accounts, ascii bool, usage usagestore.Store) accountsState {
	if api == nil {
		return accountsState{}
	}
	return accountsState{
		model:   accounts.New(api, accounts.Options{ASCII: ascii, Usage: usage, Embedded: true}),
		present: true,
	}
}

// openAccounts selects the workspace, shows tab and gives it the keyboard.
// The selected account stays: account scope is the local user, not the
// worktree, so no outside selection applies.
func (m Model) openAccounts(tab accounts.Tab) (Model, tea.Cmd) {
	m, cmd := m.selectWorkspace(wsAccounts)
	if !m.acct.present {
		m.shellFocus = shellBody
		return m, cmd
	}
	am, show := m.acct.model.Show("", tab)
	m.acct.model = am
	m.shellFocus = shellBody
	return m, tea.Batch(cmd, show)
}

// accountsKey gives a key to the workspace while its body owns the keyboard.
func (m Model) accountsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.acct.present {
		if msg.String() == "esc" {
			m.shellFocus = shellNav
		}
		return m, nil
	}
	m.acct.model.SetASCII(m.agentGlyphsASCII())
	am, cmd := m.acct.model.Update(msg)
	m.acct.model = am
	return m, cmd
}

// accountsMsg handles what the Accounts component hands to its parent and
// forwards its own results. handled is false for anything else.
func (m Model) accountsMsg(msg tea.Msg) (handled bool, nm Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case accounts.CloseMsg:
		if m.workspace == wsAccounts {
			m.shellFocus = shellNav
		}
		return true, m, nil
	case accounts.ExecMsg:
		if msg.Cmd == nil {
			return true, m, nil
		}
		// Add and sign-in are interactive provider flows: suspend the TUI, give
		// them the terminal, then reload the list. Login is never refreshed
		// behind the user's back.
		return true, m, tea.ExecProcess(msg.Cmd, func(err error) tea.Msg { return agentExecDoneMsg{err: err} })
	case agentExecDoneMsg:
		if msg.err != nil {
			m.status = "accounts: " + sanitizeAgentText(msg.err.Error())
		}
		if !m.acct.present {
			return true, m, nil
		}
		am, c := m.acct.model.Update(accounts.ReloadMsg{})
		m.acct.model = am
		return true, m, c
	case usagestore.Observation:
		m.agents.usage, _ = m.agents.usage.Apply(usagestore.Result(msg))
		return true, m, nil
	}
	if !m.acct.started {
		return false, m, nil
	}
	am, c := m.acct.model.Update(msg)
	m.acct.model = am
	return true, m, c
}

// accountsHints are the footer hints while the body has the keyboard.
func (m Model) accountsHints() []shellHint {
	if !m.acct.present {
		return []shellHint{{"esc", "workspaces"}}
	}
	var out []shellHint
	for _, h := range m.acct.model.Hints() {
		out = append(out, shellHint{h.Key, h.Desc})
	}
	return out
}

// accountsWhere is the footer location: the account and section in view.
func (m Model) accountsWhere() string {
	if !m.acct.present {
		return "Accounts"
	}
	if label := m.acct.model.Label(); label != "" {
		return "Accounts / " + label + " / " + m.acct.model.Section()
	}
	return "Accounts"
}

// accountsFocusLabel names the focus owner inside the workspace.
func (m Model) accountsFocusLabel(f shellFocus) string {
	switch {
	case f == shellInput:
		return "Accounts / Input"
	case f == shellBody && m.acct.present && !m.acct.model.InProfiles():
		return "Accounts / " + m.acct.model.Section()
	case f == shellBody:
		return "Accounts / Profiles"
	}
	return "Accounts / " + f.String()
}

// accountsBody renders exactly rows lines of exactly width cells.
func (m Model) accountsBody(width, rows int) []string {
	p := theme.Current
	var lines []string
	if !m.acct.present {
		dim := seg(p.Muted, p.Background, false)
		lines = []string{
			shellRow(width, p.Background, seg(p.Text, p.Background, true).Render(" ACCOUNTS"), ""),
			blankRow(width, p.Background),
			shellRow(width, p.Background, dim.Render(" Accounts are unavailable: no agent backend is running in this session."), ""),
			shellRow(width, p.Background, dim.Render(" Profiles can still be managed with `bonsai agent account`."), ""),
		}
	} else {
		am := m.acct.model
		am.SetASCII(m.agentGlyphsASCII())
		am.SetSize(width, rows)
		for _, l := range strings.Split(am.View(), "\n") {
			lines = append(lines, onBackground(l, p.Background))
		}
	}
	for len(lines) < rows {
		lines = append(lines, blankRow(width, p.Background))
	}
	return m.settingsASCII(lines[:rows])
}

// onBackground paints bg behind a line whose segments each reset their own
// style, so the component's plain padding matches the shell's bands.
func onBackground(line string, bg lipgloss.Color) string {
	open, _, _ := strings.Cut(lipgloss.NewStyle().Background(bg).Render("x"), "x")
	if open == "" {
		return line
	}
	return open + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+open) + "\x1b[0m"
}
