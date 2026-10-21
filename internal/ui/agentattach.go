package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/vterm"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// attachState is where the live view of one session stands.
type attachState int

const (
	attachNone           attachState = iota // connecting, or failed (see agentView.err)
	attachObserving                         // attached read-only
	attachControlling                       // this client writes
	attachObservingOther                    // attached for control, another client writes
	attachEnded                             // the session finished; its last screen stays
)

const (
	attachRetryDelay = 500 * time.Millisecond
	// A pump batch stays bounded so one busy agent cannot starve the UI loop.
	maxFrameBatch      = 256
	maxFrameBatchBytes = 4 << 20
)

// agentView is the single live attachment the Agents tab shows.
type agentView struct {
	sessionID string
	// token changes with every new stream: messages from a stream the view has
	// moved on from carry an old one and are dropped.
	token  uint64
	stream agentsapi.Stream
	term   *vterm.Model
	state  attachState
	// replaying lasts until the first status frame, which ends the holder's
	// replay of what happened before this attach.
	replaying bool
	// offset is the end of the last output applied and generation the holder
	// run it belongs to: together they let a re-attach continue the same
	// emulator instead of replaying (and redrawing) everything.
	offset     uint64
	generation string
	opts       agentsapi.AttachOptions // how this view attaches (offset/generation are filled at dial time)
	cols, rows int                     // emulator size, kept in step with the holder's PTY
	attempts   int                     // failed dials so far
	banner     string
	err        string
	retry      bool // enter attaches again; false when retrying cannot help

	// control is terminal mode: the user is typing into the agent. state says
	// whether that is actually possible (controlling) or not (observingOther).
	control bool
	lost    bool // this client wrote and another took over
	hinted  bool // the "another client controls" note was shown
	// inq/sending order writes to the holder: see queueInput.
	inq       [][]byte
	sending   bool
	resizeGen uint64 // debounce generation for window resizes
}

type agentAttachedMsg struct {
	token  uint64
	stream agentsapi.Stream
	err    error
}

type agentRetryMsg struct{ token uint64 }

type agentFramesMsg struct {
	token  uint64
	frames []agentterminal.Frame
	closed bool
}

func attachCmd(ctx context.Context, s agentsapi.Sessions, id string, token uint64, opts agentsapi.AttachOptions) tea.Cmd {
	return func() tea.Msg {
		st, err := s.Attach(ctx, id, opts)
		return agentAttachedMsg{token: token, stream: st, err: err}
	}
}

// waitFramesCmd blocks for one frame, then takes whatever is already queued
// (bounded), so a burst of output costs one render, not one per frame. The
// handler re-issues it; it ends when the stream closes.
func waitFramesCmd(token uint64, st agentsapi.Stream) tea.Cmd {
	return func() tea.Msg {
		ch := st.Frames()
		f, ok := <-ch
		if !ok {
			return agentFramesMsg{token: token, closed: true}
		}
		frames, bytes := []agentterminal.Frame{f}, len(f.Data)
		for len(frames) < maxFrameBatch && bytes < maxFrameBatchBytes {
			select {
			case f, ok := <-ch:
				if !ok {
					return agentFramesMsg{token: token, frames: frames, closed: true}
				}
				frames = append(frames, f)
				bytes += len(f.Data)
			default:
				return agentFramesMsg{token: token, frames: frames}
			}
		}
		return agentFramesMsg{token: token, frames: frames}
	}
}

// releaseCmd closes what a view held. Closing waits for the stream's reader,
// so it runs in a command, never in Update.
func releaseCmd(st agentsapi.Stream, term *vterm.Model) tea.Cmd {
	if st == nil && term == nil {
		return nil
	}
	return func() tea.Msg {
		if st != nil {
			_ = st.Close()
		}
		if term != nil {
			term.Close()
		}
		return nil
	}
}

// quit stops the subscription and releases the live view; the holders keep
// running. Sequenced before tea.Quit so the close runs before the program ends.
func (a agentState) quit() tea.Cmd {
	a.stop()
	if a.view == nil {
		return tea.Quit
	}
	return tea.Sequence(releaseCmd(a.view.stream, a.view.term), tea.Quit)
}

// agentPaneSize is the Agents tab's pane rectangle (inside the border, below
// the tab strip), as View passes it to renderAgentsPane.
func (m Model) agentPaneSize() (w, h int) {
	_, rightW, innerH := m.dims()
	return max(rightW-2, 1), max(innerH-1, 1)
}

// wantedSession is the session the live view should show: the selected one of
// the selected worktree, while the Agents tab is visible.
func (m Model) wantedSession() string {
	if !m.ready || !m.agents.active() || m.rightTab != tabAgents {
		return ""
	}
	wt, ok := m.selectedWorktree()
	if !ok {
		return ""
	}
	s, _, ok := m.selectedSession(cleanPath(wt.Path))
	if !ok {
		return ""
	}
	return s.ID
}

// syncAgentView reconciles the live view with the selection after every
// update: it releases a view the user moved away from and attaches the new
// selection. Doing it in one place covers every trigger (session switch,
// worktree change, tab change, session list change) without per-key code.
func (m *Model) syncAgentView(cmd tea.Cmd) tea.Cmd {
	if !m.agents.active() {
		return cmd
	}
	if id := m.agents.enterAfter; id != "" {
		// The wish holds only while the user stays on the started session.
		wt, ok := m.selectedWorktree()
		if !ok || cleanPath(wt.Path) != m.agents.enterPath || m.agents.selected[m.agents.enterPath] != id {
			m.agents.enterAfter = ""
		}
	}
	want := m.wantedSession()
	v := m.agents.view
	if v != nil && v.sessionID != want {
		cmd = tea.Batch(cmd, releaseCmd(v.stream, v.term))
		m.agents.view, v = nil, nil
	}
	if v == nil && want != "" {
		m.agents.attachSeq++
		m.agents.view = &agentView{sessionID: want, token: m.agents.attachSeq, opts: agentsapi.AttachOptions{ReadOnly: true}}
		if want == m.agents.enterAfter {
			// This client just started it: attach as the writer and type into it.
			m.agents.enterAfter = ""
			m.agents.view.opts.ReadOnly = false
			m.agents.view.control = true
		}
		return tea.Batch(cmd, attachCmd(m.agents.ctx, m.agents.api.Sessions, want, m.agents.attachSeq, m.agents.view.attachOptions()))
	}
	if v != nil && v.term != nil {
		w, rows := m.agentAreaSize()
		v.term.SetViewport(w, rows)
	}
	return cmd
}

// agentAreaSize is the terminal area inside the pane: what the layout leaves
// after the header rows.
func (m Model) agentAreaSize() (w, rows int) {
	if m.layoutKind == LayoutRedesign {
		// The redesign has no Agents tab: the one terminal belongs to the workspace in view.
		return m.terminalAreaSize()
	}
	pw, ph := m.agentPaneSize()
	lay := m.agentsLayout(pw, ph)
	if !lay.ok {
		return pw, 0
	}
	return pw, lay.body
}

// retryAgentAttach drops a failed view so the next sync attaches again.
func (m *Model) retryAgentAttach() tea.Cmd {
	v := m.agents.view
	if v == nil || !v.retry {
		return nil
	}
	m.agents.view = nil
	return m.syncAgentView(releaseCmd(v.stream, v.term))
}

func (m Model) onAgentAttached(msg agentAttachedMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token {
		// The user moved on while the dial ran: do not leak the stream.
		if msg.stream != nil {
			return m, releaseCmd(msg.stream, nil)
		}
		return m, nil
	}
	if msg.err != nil {
		if errors.Is(msg.err, agentsapi.ErrVersion) {
			v.fail("Started by another bonsai version; run bonsai agents attach "+m.viewSessionName(v), false)
			return m, nil
		}
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if v.attempts == 0 {
			v.attempts++
			token := v.token
			return m, tea.Tick(attachRetryDelay, func(time.Time) tea.Msg { return agentRetryMsg{token: token} })
		}
		v.fail("Could not attach to this agent", true)
		return m, nil
	}
	var cmds []tea.Cmd
	st := msg.stream
	v.err, v.banner, v.retry, v.attempts = "", "", false, 0
	cols, rows := st.Size()
	if cols <= 0 || rows <= 0 {
		cols, rows = m.agentPaneSize() // an ended session has no PTY to size from
	}
	if v.term != nil && v.generation != "" && st.Generation() != v.generation {
		// A different holder run: our offset means nothing there and the
		// replay is complete, so the old screen would only corrupt it.
		cmds = append(cmds, releaseCmd(nil, v.term))
		v.term, v.offset = nil, 0
	}
	v.stream, v.generation, v.cols, v.rows = st, st.Generation(), cols, rows
	if v.term == nil {
		v.term = vterm.New(cols, rows)
		cmds = append(cmds, waitInputCmd(v.term))
	} else {
		v.term.ResizeEmulator(cols, rows)
	}
	w, area := m.agentAreaSize()
	v.term.SetViewport(w, area)
	v.term.SetReplaying(true)
	v.replaying, v.state = true, attachObserving
	v.term.SetFocused(false)
	if v.control {
		if st.Writer() {
			v.state = attachControlling
			v.term.SetFocused(true)
			cmds = append(cmds, m.nudge(v))
		} else {
			v.state, v.banner = attachObservingOther, bannerControlledByOther
		}
	}
	cmds = append(cmds, waitFramesCmd(v.token, v.stream))
	return m, tea.Batch(cmds...)
}

// fail records an attach that cannot continue. With an emulator already on
// screen the message goes in the banner so the last screen stays visible.
func (v *agentView) fail(text string, retry bool) {
	v.control, v.retry = false, retry
	if v.term != nil {
		v.banner = text
		if retry {
			v.banner += " · enter: retry"
		}
		return
	}
	v.err = text
}

func (m Model) viewSessionName(v *agentView) string {
	if wt, ok := m.selectedWorktree(); ok {
		if s, ok := m.sessionByID(cleanPath(wt.Path), v.sessionID); ok {
			return sessionName(s)
		}
	}
	return v.sessionID
}

func (m Model) onAgentRetry(msg agentRetryMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token || v.stream != nil {
		return m, nil
	}
	return m, attachCmd(m.agents.ctx, m.agents.api.Sessions, v.sessionID, v.token, v.attachOptions())
}

func (m Model) onAgentFrames(msg agentFramesMsg) (tea.Model, tea.Cmd) {
	v := m.agents.view
	if v == nil || v.token != msg.token || v.term == nil || v.stream == nil {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, f := range msg.frames {
		switch f.Type {
		case "output":
			v.term.Write(f.Data)
			v.offset = f.Offset + uint64(len(f.Data))
		case "gap":
			// Output before f.Offset is gone: stale screen state would corrupt
			// what follows, so start from a blank emulator.
			old := v.term
			v.term = vterm.New(v.cols, v.rows)
			v.offset = f.Offset
			w, area := m.agentAreaSize()
			v.term.SetViewport(w, area)
			v.term.SetReplaying(v.replaying)
			v.term.SetFocused(v.control && v.state == attachControlling)
			cmds = append(cmds, releaseCmd(nil, old), waitInputCmd(v.term))
		case "status":
			if v.replaying {
				v.replaying = false
				v.term.SetReplaying(false)
			}
			if f.Status != nil {
				applyAgentStatus(v, *f.Status)
			}
		}
	}
	if v.state == attachEnded {
		m.leaveTerminalMode(v)
	}
	// The stream's writer flag moves before the frame that announces it, so
	// checking after the batch also covers writer frames.
	if c := m.syncAgentControl(v); c != nil {
		cmds = append(cmds, c)
	}
	// While this client writes, its own size is the truth: the polled holder
	// size lags a resize we just sent and would shrink the emulator back.
	if c, r := v.stream.Size(); v.state != attachControlling && c > 0 && r > 0 && (c != v.cols || r != v.rows) {
		v.cols, v.rows = c, r
		v.term.ResizeEmulator(c, r)
	}
	if msg.closed {
		if v.state != attachEnded {
			v.banner, v.err, v.retry = "disconnected · enter: retry", "disconnected", true
			m.leaveTerminalMode(v)
		}
		return m, tea.Batch(cmds...)
	}
	cmds = append(cmds, waitFramesCmd(v.token, v.stream))
	return m, tea.Batch(cmds...)
}

// applyAgentStatus turns a finished session's summary into the banner.
func applyAgentStatus(v *agentView, s agentterminal.Summary) {
	switch s.State {
	case "exited":
		v.state = attachEnded
		code := ""
		if s.ExitCode != nil {
			code = fmt.Sprintf(" (code %d)", *s.ExitCode)
		}
		v.banner = "exited" + code + " · n: new · a: all"
	case "failed":
		v.state = attachEnded
		// The holder's error is free text; strip escapes and control bytes so
		// it cannot move the cursor or recolor the banner row.
		reason := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, ansi.Strip(s.Error))
		reason = ansi.Truncate(strings.TrimSpace(reason), 120, "…")
		if reason == "" {
			reason = "unknown error"
		}
		v.banner = "failed: " + reason + " · n: new · a: all"
	}
}

// agentTerminalArea is the rows the live view fills: the emulator, an error
// or connecting note, with the banner over the last row.
func (m Model) agentTerminalArea(sel agentsapi.Session, w, rows int) []string {
	v := m.agents.view
	if v == nil || v.sessionID != sel.ID {
		return m.chromeRows(placeholderArea(sel, w, rows))
	}
	var area []string
	switch {
	case v.err != "" && v.term == nil:
		lines := []string{lipgloss.NewStyle().Foreground(theme.Current.Danger).Render(v.err)}
		if v.retry {
			lines = append(lines, dimText("enter: retry"))
		}
		area = m.chromeRows(centered(lines, w, rows))
	case v.term == nil:
		area = m.chromeRows(centered([]string{sessionName(sel), dimText("connecting…")}, w, rows))
	default:
		area = strings.Split(v.term.View(), "\n")
		for len(area) < rows {
			area = append(area, "")
		}
		area = area[:rows]
	}
	if v.banner != "" && rows > 0 {
		style := lipgloss.NewStyle().Foreground(theme.Current.Warning)
		area[rows-1] = fit(m.chrome(style.Render(ansi.Truncate(v.banner, w, "…"))), w)
	}
	return area
}
