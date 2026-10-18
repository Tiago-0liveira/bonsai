package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/vterm"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// resizeDebounce lets a drag of the window edge settle so the agent gets
	// one SIGWINCH for the final size, not one per intermediate size.
	resizeDebounce = 100 * time.Millisecond
	// maxInputQueue bounds keys waiting for a stalled stream; beyond it new
	// input is dropped rather than growing without limit.
	maxInputQueue = 1024

	bannerControlledByOther = "Controlled by another client · Ctrl+] then Enter to take control"
	hintOtherControls       = "READ-ONLY · Ctrl+] Unfocus, then Enter to take control"
	hintControlLost         = "Control taken by another client  ·  Ctrl+] then Enter Take back"
	hintAgentFocused        = "FOCUSED · Input goes to agent · Ctrl+] Unfocus"
	observingOtherNote      = "Another client controls this agent: Ctrl+] then Enter takes control"
	inputDroppedNote        = "agents: input dropped, the agent is not reading"
	hintConnecting          = "Connecting to the agent…  ·  keys are ignored until attached  ·  Ctrl+] Unfocus"
)

// agentInputMsg carries one chunk the emulator wants sent to the agent. It
// names the vterm it came from, so chunks of a terminal the view replaced are
// ignored.
type agentInputMsg struct {
	term *vterm.Model
	data []byte
}

// agentInputSentMsg reports that the in-flight write batch finished.
type agentInputSentMsg struct{ token uint64 }

type agentTookMsg struct {
	token uint64
	err   error
}

type agentResizeTickMsg struct {
	token, gen uint64
}

// agentTerminalMode: the user asked to type into the agent on this tab. It is
// a view property (not a state) so that every key route can check it cheaply.
func (m Model) agentTerminalMode() bool {
	v := m.agents.view
	return v != nil && v.control && m.agents.active() && m.rightTab == tabAgents
}

// agentOverlayOpen is true while something owns the keyboard on top of the tab:
// preferences, any modal (this includes the stop confirmation, whose
// pendingConfirm only exists with its modal) and the agent overlays. They all
// win over terminal mode, so a confirmation can be answered even though the
// agent view stays attached underneath.
func (m Model) agentOverlayOpen() bool {
	return m.prefs != nil || m.modal != nil || m.agents.overlay != nil
}

// waitInputCmd blocks for the next chunk of the terminal's input. The handler
// re-issues it, so exactly one is outstanding per vterm; it ends (nil message)
// when the vterm closes.
func waitInputCmd(term *vterm.Model) tea.Cmd {
	return func() tea.Msg {
		d, ok := <-term.Input()
		if !ok {
			return nil
		}
		return agentInputMsg{term: term, data: d}
	}
}

// sendInputCmd writes a batch to the holder in order. It may block on the
// socket, so it never runs in Update.
func sendInputCmd(token uint64, st agentsapi.Stream, batch [][]byte) tea.Cmd {
	return func() tea.Msg {
		for _, p := range batch {
			_ = st.Input(p) // a lost writer or closed stream drops input by design
		}
		return agentInputSentMsg{token: token}
	}
}

// resizeCmd resizes the holder's PTY through the given sizes in order; the
// nudge is two sizes in one command so they cannot be reordered.
func resizeCmd(st agentsapi.Stream, sizes ...[2]int) tea.Cmd {
	return func() tea.Msg {
		for _, s := range sizes {
			_ = st.Resize(s[0], s[1])
		}
		return nil
	}
}

func takeCmd(token uint64, st agentsapi.Stream) tea.Cmd {
	return func() tea.Msg { return agentTookMsg{token: token, err: st.Take()} }
}

// canSend: input reaches the agent only from the writer, and never while the
// holder replays history (replies to queries from the past are stale).
func (v *agentView) canSend() bool {
	return v.stream != nil && v.state == attachControlling && !v.replaying
}

// queueInput is the only path to Stream.Input. Chunks join one FIFO and one
// write batch is in flight at a time, so bytes reach the holder in the order
// Update saw them even though the writes run in commands. The holder rejects a
// chunk over InputLimit, so a large paste is split. dropped reports input that
// was lost because the queue is full (a stalled stream); input that is not
// meant to be sent (not the writer, replaying) is not "dropped".
func (v *agentView) queueInput(data []byte) (cmd tea.Cmd, dropped bool) {
	if !v.canSend() {
		return nil, false
	}
	chunks := (len(data) + agentterminal.InputLimit - 1) / agentterminal.InputLimit
	if len(v.inq)+chunks > maxInputQueue {
		return nil, true
	}
	for len(data) > 0 {
		n := min(len(data), agentterminal.InputLimit)
		v.inq = append(v.inq, data[:n:n])
		data = data[n:]
	}
	return v.flushInput(), false
}

func (v *agentView) flushInput() tea.Cmd {
	if v.sending || len(v.inq) == 0 || v.stream == nil {
		return nil
	}
	batch := v.inq
	v.inq, v.sending = nil, true
	return sendInputCmd(v.token, v.stream, batch)
}

func (m Model) onAgentInput(msg agentInputMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.term != msg.term {
		return m, nil
	}
	// Dropped unless controlling: this is also what keeps terminal replies
	// (focus reports, cursor queries) away from an agent we do not control.
	send, dropped := v.queueInput(msg.data)
	if dropped {
		m.status = inputDroppedNote
	}
	return m, tea.Batch(send, waitInputCmd(v.term))
}

func (m Model) onAgentInputSent(msg agentInputSentMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token {
		return m, nil
	}
	v.sending = false
	return m, v.flushInput()
}

func (m Model) onAgentTook(msg agentTookMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token {
		return m, nil
	}
	if msg.err != nil {
		m.status = "agents: could not take control"
		return m, nil
	}
	// The writer frame normally decides; checking now covers a holder that
	// already answered.
	return m, m.syncAgentControl(v)
}

// terminalSize is the terminal area in terminal-mode layout, which is the size
// the PTY gets while this client writes.
func (m Model) terminalSize() (cols, rows int) { return m.agentAreaSize() }

// nudge tells the PTY the area it now fills. Going one row short first makes
// the agent redraw fully (same trick as `bonsai agents attach`), since a
// same-size resize is not a SIGWINCH.
func (m Model) nudge(v *agentView) tea.Cmd {
	cols, rows := m.terminalSize()
	if cols <= 0 || rows <= 0 || v.stream == nil {
		return nil
	}
	v.cols, v.rows = cols, rows
	v.term.ResizeEmulator(cols, rows)
	v.term.SetViewport(cols, rows)
	if rows < 2 {
		return resizeCmd(v.stream, [2]int{cols, rows})
	}
	return resizeCmd(v.stream, [2]int{cols, rows - 1}, [2]int{cols, rows})
}

// syncAgentControl follows the stream's writer flag: gaining it starts a
// controlling session (nudge, focus), losing it demotes to observing.
func (m *Model) syncAgentControl(v *agentView) tea.Cmd {
	if v.stream == nil || v.term == nil {
		return nil
	}
	w := v.stream.Writer()
	switch {
	case v.state == attachControlling && !w:
		v.state, v.lost, v.banner = attachObservingOther, true, ""
		v.term.SetFocused(false)
	case v.state == attachObservingOther && w:
		v.state, v.lost, v.banner = attachControlling, false, ""
		if v.control {
			v.term.SetFocused(true)
			return m.nudge(v)
		}
	}
	return nil
}

// enterTerminalMode starts typing into the selected session. handled is false
// when there is nothing for enter to do on this tab.
func (m *Model) enterTerminalMode() (handled bool, cmd tea.Cmd) {
	v := m.agents.view
	if v == nil {
		return false, nil
	}
	if v.state == attachEnded {
		m.status = "This agent has ended"
		return true, nil
	}
	if v.term == nil || v.stream == nil {
		return true, nil // still connecting
	}
	v.control = true
	switch v.state {
	case attachControlling:
		// A stream kept from an earlier visit: it still writes, no re-attach.
		v.term.SetFocused(true)
		return true, m.nudge(v)
	case attachObservingOther:
		v.term.SetFocused(false)
		return true, takeCmd(v.token, v.stream)
	}
	return true, m.reattachAgent(v, false)
}

// leaveTerminalMode returns to navigation keys but keeps the stream attached
// and rendering: interrupting the agent is never a side effect of opening an
// overlay.
func (m *Model) leaveTerminalMode(v *agentView) {
	v.control = false
	if v.term != nil {
		v.term.SetFocused(false)
	}
}

// reattachAgent swaps the stream for a new attach at the current offset,
// keeping the emulator so the screen does not flash.
func (m *Model) reattachAgent(v *agentView, readOnly bool) tea.Cmd {
	m.agents.attachSeq++
	old := v.stream
	v.token = m.agents.attachSeq
	v.stream, v.state = nil, attachNone
	v.attempts, v.err, v.retry, v.banner = 0, "", false, ""
	v.inq, v.sending, v.lost = nil, false, false
	v.opts = agentsapi.AttachOptions{ReadOnly: readOnly}
	return tea.Batch(releaseCmd(old, nil), attachCmd(m.agents.ctx, m.agents.api.Sessions, v.sessionID, v.token, v.attachOptions()))
}

// attachOptions resumes where the emulator stopped.
func (v *agentView) attachOptions() agentsapi.AttachOptions {
	o := v.opts
	o.Offset, o.Generation = v.offset, v.generation
	return o
}

// agentResizeDebounce arms the settle timer after a window resize while this
// client writes. Observers only crop (SetViewport, done by syncAgentView).
func (m Model) agentResizeDebounce() tea.Cmd {
	v := m.agents.view
	if v == nil || !v.control || v.state != attachControlling {
		return nil
	}
	v.resizeGen++
	token, gen := v.token, v.resizeGen
	return tea.Tick(resizeDebounce, func(time.Time) tea.Msg { return agentResizeTickMsg{token: token, gen: gen} })
}

func (m Model) onAgentResizeTick(msg agentResizeTickMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token || v.resizeGen != msg.gen || !v.control || v.state != attachControlling {
		return m, nil
	}
	cols, rows := m.terminalSize()
	if cols <= 0 || rows <= 0 || (cols == v.cols && rows == v.rows) {
		return m, nil
	}
	v.cols, v.rows = cols, rows
	v.term.ResizeEmulator(cols, rows)
	return m, resizeCmd(v.stream, [2]int{cols, rows})
}

// agentTerminalKey routes every key while in terminal mode. Only ctrl+] is
// ours; everything else, including q, ctrl+c, esc, tab and ?, belongs to the
// agent. Ctrl+] immediately returns the keyboard to Bonsai.
func (m Model) agentTerminalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if msg.Type == tea.KeyCtrlCloseBracket {
		m.leaveTerminalMode(v)
		return m, nil
	}
	switch v.state {
	case attachControlling:
		if v.replaying {
			return m, nil
		}
		if msg.Paste {
			v.term.SendPaste(string(msg.Runes))
		} else {
			v.term.SendKey(msg)
		}
	case attachObservingOther:
		if !v.hinted {
			v.hinted = true
			m.status = observingOtherNote
		}
	}
	return m, nil
}

// terminalLayout is the pane in terminal mode: a two-row header so the agent
// gets the height, and a hint row closing the pane.
func (m Model) terminalLayout(w, h int, strip []agentsapi.Session, sel agentsapi.Session, selIdx int, rule string) agentsLayout {
	var head string
	focusLabel := "FOCUSED"
	if v := m.agents.view; v != nil {
		if v.stream == nil {
			focusLabel = "CONNECTING"
		} else if v.state == attachObservingOther {
			focusLabel = "READ-ONLY"
		}
	}
	if w >= agentsWideMin {
		parts := []string{theme.ProviderStyle(sel.Provider).Render(providerName(sel.Provider))}
		if sel.ProfileName != "" {
			parts = append(parts, sel.ProfileName)
		}
		left := m.agentGlyph(sel.Provider) + " " + strings.Join(parts, " · ")
		if u := m.usageText(sel, false); u != "" {
			left += " │ " + u
		}
		head = leftRight(left, dimText(focusLabel), w)
	} else {
		head = m.compactHeaderHint(sel, w, focusLabel)
	}
	fixed := []string{m.sessionStrip(strip, selIdx, w, "", ""), fit(head, w), rule}
	tail := []string{rule, fit(m.terminalHint(), w)}
	if h-len(fixed)-len(tail) < minTerminalRows {
		return agentsLayout{}
	}
	return agentsLayout{fixed: fixed, tail: tail, body: h - len(fixed) - len(tail), ok: true}
}

func (m Model) terminalHint() string {
	v := m.agents.view
	warn := lipgloss.NewStyle().Foreground(theme.Current.Warning)
	switch {
	case v.stream == nil:
		// A re-attach is in flight (take-over, detach, retry): keys are
		// swallowed until it lands, so say so instead of looking frozen.
		return warn.Render(hintConnecting)
	case v.state == attachObservingOther && v.lost:
		return warn.Render(hintControlLost)
	case v.state == attachObservingOther:
		return warn.Render(hintOtherControls)
	}
	return dimText(hintAgentFocused)
}

// agentNavHints are the state-specific hints ahead of the standing ones in
// navigation mode: what the selected session lets you do right now.
func (m Model) agentNavHints(pair func(k, d string) string, descSt lipgloss.Style) []string {
	wt, ok := m.selectedWorktree()
	if !ok {
		return nil
	}
	sel, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok {
		return nil
	}
	v := m.agents.view
	switch {
	case v == nil || v.sessionID != sel.ID || (v.stream == nil && v.err == ""):
		return []string{descSt.Render("connecting…")}
	case v.err != "" && v.retry:
		return []string{pair(m.keys.AgentAttach.Help().Key, "retry")}
	case v.state == attachEnded:
		return nil
	}
	label := "focus agent"
	if v.state == attachObservingOther {
		label = "take control"
	}
	return []string{pair(m.keys.AgentAttach.Help().Key, label)}
}
