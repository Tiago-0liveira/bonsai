package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

const (
	peekLines = 10
	// peekTTL is how long a snapshot is reused while the selection comes back to
	// the session; a longer stay refreshes on the next key.
	peekTTL = 5 * time.Second
)

// peekEntry is one session's cached screen tail. Terminal output stays in
// memory only: it is never written to warm-start or browser data.
type peekEntry struct {
	lines   []string
	at      time.Time
	gen     uint64
	loading bool
}

// agentPeekMsg is the answer to a snapshot request. gen drops answers for a
// selection the user has already left.
type agentPeekMsg struct {
	gen   uint64
	id    string
	lines []string
	err   error
}

func peekCmd(ctx context.Context, s agentsapi.Sessions, id string, gen uint64) tea.Cmd {
	return func() tea.Msg {
		lines, err := s.Snapshot(ctx, id, peekLines)
		return agentPeekMsg{gen: gen, id: id, lines: lines, err: err}
	}
}

// peekPicker points the picker's detail pane at the cache and asks for a
// snapshot when the selected running session has none that is fresh. The
// attached session already has a live screen, which agentPeek supplies.
func (m Model) peekPicker(sp *sessionsPicker) (Model, tea.Cmd) {
	if m.agents.peeks == nil {
		m.agents.peeks = map[string]peekEntry{}
	}
	var cmd tea.Cmd
	live := m.agentPeek()
	if s, ok := sp.selected(); ok && s.Active() && live[s.ID] == nil {
		e, have := m.agents.peeks[s.ID]
		if !e.loading && (!have || agentNow().Sub(e.at) > peekTTL) {
			m.agents.peekGen++
			e.loading, e.gen = true, m.agents.peekGen
			m.agents.peeks[s.ID] = e
			cmd = peekCmd(m.agents.ctx, m.agents.api.Sessions, s.ID, e.gen)
		}
	}
	m.syncPeek(sp, live)
	return m, cmd
}

// syncPeek copies the cache into the picker, live screens taking precedence.
func (m Model) syncPeek(sp *sessionsPicker, live map[string][]string) {
	sp.peek, sp.peekLoading = map[string][]string{}, ""
	for id, e := range m.agents.peeks {
		sp.peek[id] = e.lines
		if e.loading && len(e.lines) == 0 {
			sp.peekLoading = id
		}
	}
	for id, lines := range live {
		sp.peek[id] = lines
	}
}

func (m Model) onAgentPeek(msg agentPeekMsg) (tea.Model, tea.Cmd) {
	e, ok := m.agents.peeks[msg.id]
	if !ok || e.gen != msg.gen {
		return m, nil
	}
	if msg.gen != m.agents.peekGen {
		// A later request superseded this one: forget it so the session is
		// asked again when it is selected next.
		delete(m.agents.peeks, msg.id)
		return m, nil
	}
	e.loading, e.at = false, agentNow()
	if msg.err == nil {
		e.lines = msg.lines
	}
	m.agents.peeks[msg.id] = e
	if o := m.agents.overlay; o != nil && o.sessions != nil {
		m.syncPeek(o.sessions, m.agentPeek())
	}
	return m, nil
}
