package ui

import (
	"context"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/usagepane"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

const (
	// usagePaneCheck is how often the pane asks for usage. The backend answers
	// from its snapshot until it is older than agents.UsageMaxAge, so a stale
	// or failed account is retried within a minute of crossing that limit
	// rather than a full period later.
	usagePaneCheck = time.Minute
	// usagePaneMinRows is the terminal height below which the pane would
	// squeeze the worktree list, so it is hidden.
	usagePaneMinRows = 30
	// usagePaneMaxFetches bounds provider calls in flight at once.
	usagePaneMaxFetches = 8
	// usagePaneListMin is the fewest rows the worktree list keeps.
	usagePaneListMin = 3
)

// paneState is the always-on usage pane's model state. Its usage numbers live
// in the shared store (agentState.usage).
type paneState struct {
	accounts []agentsapi.Account
	// listSeq/listApplied number account lists the same way agentState.seq does.
	listSeq, listApplied uint64
	// tickGen makes duplicate timers stale once one re-arms.
	tickGen uint64
	// lim bounds concurrent provider calls.
	lim usagestore.Limiter
}

func newPaneState() paneState {
	return paneState{
		lim:     usagestore.NewLimiter(usagePaneMaxFetches),
		listSeq: 1, // Init issues the first list with seq 1
		tickGen: 1,
	}
}

type paneAccountsMsg struct {
	seq      uint64
	accounts []agentsapi.Account
	err      error
}

type paneTickMsg struct{ gen uint64 }

func tickPane(gen uint64) tea.Cmd {
	return tea.Tick(usagePaneCheck, func(time.Time) tea.Msg { return paneTickMsg{gen: gen} })
}

func listPaneAccountsCmd(ctx context.Context, a agentsapi.Accounts, seq uint64) tea.Cmd {
	return func() tea.Msg {
		list, err := a.List(ctx)
		return paneAccountsMsg{seq: seq, accounts: list, err: err}
	}
}

// paneEnabled: the agent backend is live and can read accounts.
func (m Model) paneEnabled() bool {
	return m.agents.active() && m.agents.api.Accounts != nil
}

// initPane is the Init half: first account list plus the refresh timer.
func (m Model) initPane() tea.Cmd {
	if !m.paneEnabled() {
		return nil
	}
	return tea.Batch(
		listPaneAccountsCmd(m.agents.ctx, m.agents.api.Accounts, m.agents.pane.listSeq),
		tickPane(m.agents.pane.tickGen),
	)
}

func (m Model) onPaneAccounts(msg paneAccountsMsg) (tea.Model, tea.Cmd) {
	p := &m.agents.pane
	if msg.seq <= p.listApplied {
		return m, nil
	}
	p.listApplied = msg.seq
	if msg.err != nil {
		// Keep the last good list; only a successful result replaces it.
		return m, nil
	}
	accounts := append([]agentsapi.Account(nil), msg.accounts...)
	sort.SliceStable(accounts, func(i, j int) bool {
		if ri, rj := providerRank(accounts[i].Provider), providerRank(accounts[j].Provider); ri != rj {
			return ri < rj
		}
		return accounts[i].Name < accounts[j].Name
	})
	p.accounts = accounts
	ids := make([]agents.AccountID, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}
	m.agents.usage = m.agents.usage.Retain(ids)
	var cmds []tea.Cmd
	for _, a := range accounts {
		if !m.agents.usage[a.ID].Have {
			// Paint from disk at once; the fetch below replaces it if older.
			cmds = append(cmds, usagestore.Stored(m.agents.ctx, m.agents.api.Accounts, a.ID))
		}
		cmds = append(cmds, m.fetchUsage(a.ID, p.lim))
	}
	m.layout()
	return m, tea.Batch(cmds...)
}

func (m Model) onPaneTick(msg paneTickMsg) (tea.Model, tea.Cmd) {
	p := &m.agents.pane
	if msg.gen != p.tickGen {
		return m, nil
	}
	p.tickGen++
	next := tickPane(p.tickGen)
	if !m.paneEnabled() {
		return m, next
	}
	p.listSeq++
	// Accounts still inside the window are answered from the stored snapshot
	// without a provider call.
	list := listPaneAccountsCmd(m.agents.ctx, m.agents.api.Accounts, p.listSeq)
	return m, tea.Batch(list, next)
}

// ageLabel describes how long ago usage was fetched.
func ageLabel(fetched, now time.Time, refreshing bool) string {
	if refreshing {
		return "refreshing…"
	}
	if fetched.IsZero() {
		return "not fetched"
	}
	switch age := now.Sub(fetched); {
	case age < 30*time.Second:
		return "<30s"
	case age < time.Minute:
		return "<1m"
	case age < 2*time.Minute:
		return "1m"
	case age < 3*time.Minute:
		return "2m"
	case age < 4*time.Minute:
		return "3m"
	default:
		return "4m+"
	}
}

// ageOverdue: the refresh is late or failing, so the numbers are suspect.
func ageOverdue(label string) bool { return label == "4m+" }

// providerRank orders the pane claude, codex, antigravity, then anything new.
func providerRank(p agents.ProviderID) int {
	switch p {
	case "claude":
		return 0
	case "codex":
		return 1
	case "antigravity":
		return 2
	}
	return 3
}

// paneAccounts builds the pane rows in list order.
func (m Model) paneAccounts() []usagepane.Account {
	p := m.agents.pane
	out := make([]usagepane.Account, 0, len(p.accounts))
	now := agentNow()
	for _, a := range p.accounts {
		e := m.agents.usage[a.ID]
		row := usagepane.Account{Provider: a.Provider, Name: a.Name, Err: e.Message(), Loading: !e.Have && e.Err == ""}
		if e.Have {
			row.Primary, row.Secondary = e.Usage.Headline.Primary, e.Usage.Headline.Secondary
		}
		row.Stale = e.Have && (e.Stale() || ageOverdue(ageLabel(e.Usage.FetchedAt, now, e.Refreshing)))
		out = append(out, row)
	}
	return out
}

// paneHeaderAge is the oldest fetched account's age label, or "refreshing…"
// while any fetch is in flight.
func (m Model) paneHeaderAge() string {
	p := m.agents.pane
	now := agentNow()
	var oldest time.Time
	for _, a := range p.accounts {
		e := m.agents.usage[a.ID]
		if e.Refreshing {
			if m.agentGlyphsASCII() {
				return "refreshing..."
			}
			return "refreshing…"
		}
		if e.Have && (oldest.IsZero() || e.Usage.FetchedAt.Before(oldest)) {
			oldest = e.Usage.FetchedAt
		}
	}
	if oldest.IsZero() {
		return ""
	}
	return ageLabel(oldest, now, false)
}

// usagePaneRows is the pane's outer height under a left column with innerH
// content rows, or 0 when the pane is hidden. It only reads cached state: it
// runs on every layout.
func (m Model) usagePaneRows(innerH int) int {
	if m.height < usagePaneMinRows || !m.paneEnabled() || len(m.agents.pane.accounts) == 0 {
		return 0
	}
	rows := usagepane.Height(m.paneAccounts(), (innerH+2)*40/100)
	if rows == 0 || innerH-rows < usagePaneListMin {
		return 0
	}
	return rows
}
