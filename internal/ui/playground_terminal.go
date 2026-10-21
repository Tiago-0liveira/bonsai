package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// The Playground's terminal (plan 10.2). It is the Agents terminal controller as is: one attachment
// (m.agents.view) that the Playground owns while it is the workspace in view. Selecting a session observes it
// read-only after a short debounce; Enter asks for control, with a confirmation before taking the writer role from
// another client; Ctrl+] only returns the keyboard. Observing never resizes, takes or writes: the emulator is only
// cropped, so another client's PTY is never touched.

const (
	// pgAttachDebounce lets a scroll through the list settle, so only the row the cursor rests on is dialed.
	pgAttachDebounce = 150 * time.Millisecond
	// pgTermHeadRows are the state row and the identity row above the terminal area.
	pgTermHeadRows = 2
)

// pgAttachTickMsg ends one debounce; a tick of an older generation is ignored.
type pgAttachTickMsg struct{ gen uint64 }

func (m Model) pgDebounce() time.Duration {
	if m.pg.debounce > 0 {
		return m.pg.debounce
	}
	return pgAttachDebounce
}

func pgEnded(s agentsapi.Session) bool { return s.State == "exited" || s.State == "failed" }

// pgAttachable: a session without an ID, or one that is still starting, has no holder to dial yet.
func pgAttachable(s agentsapi.Session) bool { return s.ID != "" && s.State != "starting" }

// pgAreaSize is the terminal area of the Playground body: the full width under the state and identity rows. It
// is the same whether the terminal fills the body or only observes in the background, so opening never recrops.
func (m Model) pgAreaSize() (w, rows int) {
	body := m.height - shellHeaderRows - shellFooterRows - settingsSubmenuRows
	return max(m.width, 1), max(body-pgTermHeadRows, 0)
}

// pgTerminalShown: the Playground holds the live attachment.
func (m Model) pgTerminalShown() bool {
	return m.layoutKind == LayoutRedesign && m.workspace == wsPlayground && m.pg.own && m.agents.view != nil
}

// pgTerminalMode is true while the user types into the agent.
func (m Model) pgTerminalMode() bool {
	return m.pgTerminalShown() && m.pg.open && m.modal == nil && m.agents.view.control && m.agents.active()
}

// pgWanted is the session the live view should observe: the selected one, while the list has the keyboard.
func (m Model) pgWanted() string {
	if m.layoutKind != LayoutRedesign || m.workspace != wsPlayground || !m.agents.active() || m.shellFocus == shellNav || m.pg.sub == pgSubUsage {
		return ""
	}
	r, ok := m.playgroundSelected()
	if !ok || !pgAttachable(r.Rec.Session) {
		return ""
	}
	return r.Rec.Session.ID
}

// pgSync reconciles the live view with the selection after every update. A different selection releases the
// previous view at once (it never lingers, and writer capture never carries over) and observes the next one once
// the debounce settles.
func (m *Model) pgSync() tea.Cmd {
	if !m.agents.active() {
		return nil
	}
	v := m.agents.view
	owned := m.pg.own && v != nil
	if owned && m.workspace == wsPlayground && v.sessionID == m.pg.sel && m.shellFocus != shellNav && m.pg.sub != pgSubUsage {
		m.pg.pending = "" // already on the selection: the common path of every frame
		return nil
	}
	want := m.pgWanted()
	if owned && v.sessionID == want {
		m.pg.pending = ""
		return nil
	}
	var cmds []tea.Cmd
	if owned {
		cmds = append(cmds, m.pgRelease())
	} else {
		m.pg.own, m.pg.open = false, false
	}
	if want == "" {
		m.pg.pending = ""
		m.pg.gen++
	} else if m.pg.pending != want {
		m.pg.gen++
		m.pg.pending = want
		gen := m.pg.gen
		cmds = append(cmds, tea.Tick(m.pgDebounce(), func(time.Time) tea.Msg { return pgAttachTickMsg{gen: gen} }))
	}
	return tea.Batch(cmds...)
}

// pgRelease drops the Playground's attachment, if it holds one. The holder keeps running.
func (m *Model) pgRelease() tea.Cmd {
	own := m.pg.own
	m.pg.own, m.pg.open, m.pg.takeFor, m.pg.pending = false, false, "", ""
	m.pg.gen++
	if !own {
		return nil
	}
	return m.terminalRelease()
}

// pgTakeOver makes the Playground the owner on entering it: an attachment left by another workspace is released
// (its holder keeps running), and a Garden screen that showed it falls back to its attempts.
func (m *Model) pgTakeOver() tea.Cmd {
	if m.agents.view == nil || m.pg.own {
		return nil
	}
	if p := &m.garden.planner; p.stage == plannerSession {
		p.stage, p.attempt = plannerHub, ""
	}
	return m.terminalRelease()
}

// pgAttach points the one live attachment at a session, replacing whatever it held. write asks for control; the
// default observes read-only.
func (m *Model) pgAttach(id string, write bool) tea.Cmd {
	var release tea.Cmd
	if v := m.agents.view; v != nil {
		release = releaseCmd(v.stream, v.term)
	}
	m.agents.attachSeq++
	v := &agentView{sessionID: id, token: m.agents.attachSeq, opts: agentsapi.AttachOptions{ReadOnly: !write}, control: write}
	m.agents.view = v
	m.pg.own, m.pg.pending, m.pg.takeFor = true, "", ""
	m.pg.gen++
	return tea.Batch(release, attachCmd(m.agents.ctx, m.agents.api.Sessions, id, v.token, v.attachOptions()))
}

// pgEnter is Enter on the selected session: it requests control.
func (m Model) pgEnter() (Model, tea.Cmd) {
	if !m.agents.active() {
		m.status = "Agent terminals are unavailable on this host"
		return m, nil
	}
	r, ok := m.playgroundSelected()
	if !ok || m.pg.sub == pgSubUsage {
		return m, nil
	}
	s := r.Rec.Session
	switch {
	case pgEnded(s):
		m.status = "This agent has ended and accepts no input"
		return m, nil
	case !pgAttachable(s):
		m.status = "This session is still starting; it cannot be attached yet"
		return m, nil
	}
	v := m.agents.view
	if !m.pg.own || v == nil || v.sessionID != s.ID {
		m.pg.open = true
		return m, m.pgAttach(s.ID, true)
	}
	switch {
	case v.state == attachEnded:
		m.status = "This agent has ended and accepts no input"
		return m, nil
	case v.err != "" || (v.stream == nil && v.banner != ""):
		m.pg.open = true
		if v.retry {
			return m, m.pgAttach(s.ID, true)
		}
		return m, nil // retrying cannot help: the message stays on screen
	case v.stream == nil:
		m.status = "Still connecting to the agent"
		return m, nil
	case v.state == attachObservingOther:
		return m.pgOpenTakeover(s.ID), nil
	}
	m.pg.open = true
	_, cmd := m.enterTerminalMode()
	return m, cmd
}

// pgObserve opens the terminal read-only over the whole body, without asking for control.
func (m Model) pgObserve() (Model, tea.Cmd) {
	if !m.agents.active() {
		m.status = "Agent terminals are unavailable on this host"
		return m, nil
	}
	r, ok := m.playgroundSelected()
	if !ok || m.pg.sub == pgSubUsage {
		return m, nil
	}
	s := r.Rec.Session
	if !pgAttachable(s) {
		m.status = "This session is still starting; it cannot be attached yet"
		return m, nil
	}
	m.pg.open = true
	if v := m.agents.view; m.pg.own && v != nil && v.sessionID == s.ID {
		return m, nil
	}
	return m, m.pgAttach(s.ID, false)
}

// pgOpenKey handles a key while the terminal fills the body but the keyboard stays with Bonsai.
func (m Model) pgOpenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pg.open = false
	case "enter":
		nm, cmd := m.pgEnter()
		return nm, cmd
	}
	return m, nil
}

// pgOpenTakeover asks before taking the writer role from another client.
func (m Model) pgOpenTakeover(id string) Model {
	m.pg.takeFor = id
	modal := modals.NewConfirm(modals.KindPlaygroundTakeover, "Take control from another client?")
	modal.SetBody("Another client is typing into this agent. Taking control makes it read-only there and sizes the agent's terminal to this window.")
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

// pgTakeConfirmed runs after the user approved the takeover: only now is the existing Take called.
func (m Model) pgTakeConfirmed() (Model, tea.Cmd) {
	id := m.pg.takeFor
	m.pg.takeFor = ""
	v := m.agents.view
	if !m.pg.own || v == nil || v.sessionID != id || v.state != attachObservingOther {
		m.status = "Control was not taken: the session changed"
		return m, nil
	}
	m.pg.open = true
	_, cmd := m.enterTerminalMode()
	return m, cmd
}

// pgTerminalMsg routes the controller's messages. The handlers are the Agents tab's own; the Playground only adds
// what its screen needs: the takeover question and the retained-output wording.
func (m Model) pgTerminalMsg(msg tea.Msg) (Model, tea.Cmd) {
	var nm tea.Model
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case agentAttachedMsg:
		nm, cmd = m.onAgentAttached(msg)
		m = nm.(Model)
		if v := m.agents.view; v != nil && v.token == msg.token && m.pg.open && v.control && v.state == attachObservingOther {
			// Control was requested but another client writes: stay read-only until the user approves taking it.
			v.control, v.banner = false, ""
			if v.term != nil {
				v.term.SetFocused(false)
			}
			m = m.pgOpenTakeover(v.sessionID)
		}
	case agentRetryMsg:
		nm, cmd = m.onAgentRetry(msg)
		m = nm.(Model)
	case agentFramesMsg:
		nm, cmd = m.onAgentFrames(msg)
		m = nm.(Model)
		if v := m.agents.view; v != nil && v.state == attachEnded {
			// The legacy banner points at keys this screen does not have, and the output is a bounded tail.
			const tail = " · n: new · a: all"
			note := fmt.Sprintf(" · retained output is bounded (last %d MiB)", agentterminal.OutputLimit>>20)
			if b := strings.TrimSuffix(v.banner, tail); !strings.HasSuffix(b, note) {
				v.banner = b + note
			}
		}
	case agentInputMsg:
		nm, cmd = m.onAgentInput(msg)
		m = nm.(Model)
	case agentInputSentMsg:
		nm, cmd = m.onAgentInputSent(msg)
		m = nm.(Model)
	case agentTookMsg:
		nm, cmd = m.onAgentTook(msg)
		m = nm.(Model)
	case agentResizeTickMsg:
		nm, cmd = m.onAgentResizeTick(msg)
		m = nm.(Model)
	}
	return m, cmd
}

// --- what the screen says about the terminal ---

type pgTerm int

const (
	termNone          pgTerm = iota // nothing attached to the selection (yet)
	termConnecting                  // a dial is in flight
	termObserve                     // attached read-only
	termObserveWriter               // this client still writes, but the keyboard is back with Bonsai
	termControl                     // keys go to the agent
	termOther                       // another client writes
	termLost                        // this client wrote and another took over
	termEnded
	termFailed
)

func (m Model) pgTermState() pgTerm {
	v := m.agents.view
	if !m.pg.own || v == nil {
		return termNone
	}
	switch {
	case v.state == attachEnded:
		return termEnded
	case v.err != "" || (v.stream == nil && v.banner != ""):
		return termFailed
	case v.stream == nil:
		return termConnecting
	case v.state == attachControlling && v.control:
		return termControl
	case v.state == attachControlling:
		return termObserveWriter
	case v.state == attachObservingOther && v.lost:
		return termLost
	case v.state == attachObservingOther:
		return termOther
	}
	return termObserve
}

// pgTermChip is the input owner, named as in the design: CONTROL / keys go to agent, OBSERVE / read-only.
func (m Model) pgTermChip() (text string, fg lipgloss.Color) {
	p := theme.Current
	play, eye := "▶", "◎"
	if m.agentGlyphsASCII() {
		play, eye = ">", "o"
	}
	switch m.pgTermState() {
	case termControl:
		return play + " CONTROL / keys go to agent", p.Warning
	case termObserveWriter:
		return eye + " OBSERVE / input stays with Bonsai", p.Muted
	case termOther:
		return eye + " READ-ONLY, controlled by another client", p.Warning
	case termLost:
		return eye + " READ-ONLY, controlled by another client", p.Warning
	case termConnecting:
		return eye + " CONNECTING / keys are ignored until attached", p.Muted
	case termEnded:
		return eye + " ENDED / read-only, no input", p.Muted
	case termFailed:
		return eye + " NOT CONNECTED / read-only", p.Danger
	}
	return eye + " OBSERVE / read-only", p.Muted
}

// pgTermNote is what the user can do next, truthful for the state.
func (m Model) pgTermNote() string {
	v := m.agents.view
	switch m.pgTermState() {
	case termOther:
		return hintOtherControls
	case termLost:
		return hintControlLost
	case termFailed:
		if v != nil && v.retry {
			return "Enter retries"
		}
	}
	return ""
}

// pgTermStatusLine is the one terminal line of the list's detail pane.
func (m Model) pgTermStatusLine(s agentsapi.Session) string {
	switch {
	case !pgAttachable(s):
		return "Terminal: starting, nothing to attach to yet"
	case m.pgTermState() == termNone:
		return "Terminal: observed read-only once the list has focus"
	}
	switch m.pgTermState() {
	case termConnecting:
		return "Terminal: connecting…"
	case termEnded:
		return "Terminal: ended; retained output is bounded"
	case termFailed:
		if v := m.agents.view; v != nil && v.err != "" {
			return "Terminal: " + pgClean(v.err, 90)
		}
		return "Terminal: could not attach"
	case termOther, termLost:
		return "Terminal: another client controls it"
	}
	return "Terminal: observing read-only"
}

// pgTerminalLines draws the terminal over the whole body: state row, identity row, terminal area.
func (m Model) pgTerminalLines(width, rows int) []string {
	p := theme.Current
	r, ok := m.playgroundSelected()
	if !ok {
		return []string{gText(width, p.Muted, false, "No session selected.")}
	}
	s := r.Rec.Session
	chip, fg := m.pgTermChip()
	head := seg(fg, p.Raised, true).Render(" " + chip + " ")
	if note := m.pgTermNote(); note != "" {
		head += seg(p.Warning, p.Background, false).Render("  " + note)
	}
	pfg, isColor := theme.ProviderStyle(s.Provider).GetForeground().(lipgloss.Color)
	if !isColor {
		pfg = p.Muted
	}
	identity := seg(pfg, p.Background, true).Render(" "+providerName(s.Provider)+" / "+r.Account) +
		seg(p.Dim, p.Background, false).Render("  /  "+pgBranchLine(r)) +
		seg(pgFactColor(r.Fact.Key), p.Background, false).Render("  ·  "+r.Fact.Label)
	out := []string{shellRow(width, p.Background, head, ""), shellRow(width, p.Background, identity, "")}
	if area := rows - pgTermHeadRows; area > 0 {
		for _, l := range m.agentTerminalArea(s, width, area) {
			out = append(out, fit(l, width))
		}
	}
	return out
}

// pgTermHints are the footer keys while the terminal fills the body.
func (m Model) pgTermHints() []shellHint {
	esc := shellHint{"esc", "sessions"}
	typing := m.pgTerminalMode()
	switch m.pgTermState() {
	case termControl:
		return []shellHint{{"Ctrl+]", "return to Bonsai · every other key goes to the agent"}}
	case termOther, termLost:
		if typing {
			return []shellHint{{"Ctrl+]", "return to Bonsai, then Enter to take control"}}
		}
		return []shellHint{{"enter", "take control"}, esc}
	case termConnecting:
		if typing {
			return []shellHint{{"Ctrl+]", "return to Bonsai"}}
		}
	case termFailed:
		if v := m.agents.view; v != nil && v.retry {
			return []shellHint{{"enter", "retry"}, esc}
		}
	case termObserve, termObserveWriter:
		return []shellHint{{"enter", "control"}, esc}
	}
	return []shellHint{esc}
}
