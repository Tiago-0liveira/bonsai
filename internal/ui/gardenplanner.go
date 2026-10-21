package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// The local planner (plan 08.2). Nothing here starts a planner by itself: opening this screen, refreshing it or
// reading an Issue that changed elsewhere only reads. One explicit Request AI plan starts one read-only agent
// session, and the session body reuses the Agents terminal controller (agentattach.go, agentterm.go) as is:
// there is no second PTY client and no dependency on the Worktrees UI.

// plannerStage is how deep the opened Issue's planner screen is.
type plannerStage int

const (
	plannerNone    plannerStage = iota
	plannerHub                  // facts, uncommitted-changes decision, attempts and Request AI plan
	plannerChoose               // account, model and effort of the planner
	plannerSession              // the planner's terminal, live or retained
)

const (
	plannerHeadRows = 2 // title row and facts row above the terminal
	plannerTailRows = 1 // hint row under it
)

type gardenPlannerState struct {
	stage   plannerStage
	preview *gardenapi.PlannerPreviewView
	sel     int // selected attempt, newest first
	btn     int // 0 Back, 1 Request AI plan

	// includeDirty is the user's decision about reading uncommitted changes. It belongs to the content it was
	// made for (dirtyFor), so a changed checkout asks again.
	includeDirty bool
	dirtyFor     string

	dlg *launchDialog
	// save holds the profile defaults to store once the planner really started.
	save map[string]*string

	attempt       string // the attempt whose session the body shows
	confirmCancel bool

	// seen is the newest stamp of any attempts reply taken in, so a slow background poll cannot overwrite a newer read.
	seen gardenapi.Stamp
}

type (
	gardenPlannerPreviewMsg struct {
		seq  uint64
		view gardenapi.PlannerPreviewView
		err  error
	}
	gardenPlannerStartMsg struct {
		seq  uint64
		view gardenapi.PlannerAttemptView
		err  error
	}
	gardenPlannerAttemptsMsg struct {
		seq  uint64
		view gardenapi.PlannerAttemptsView
		err  error
	}
	gardenPlannerCancelMsg struct {
		seq  uint64
		view gardenapi.PlannerAttemptView
		err  error
	}
)

// plannerActive reports whether the opened Issue shows the planner screen.
func (m Model) plannerActive() bool {
	return m.garden.planner.stage != plannerNone && m.garden.board.opened != 0
}

// display lists the attempts newest first, the order the screen shows them in.
func (p gardenPlannerState) display() []plan.Attempt {
	if p.preview == nil {
		return nil
	}
	out := make([]plan.Attempt, len(p.preview.Attempts))
	for i, a := range p.preview.Attempts {
		out[len(out)-1-i] = a
	}
	return out
}

// selected is the attempt under the cursor.
func (p gardenPlannerState) selected() (plan.Attempt, bool) {
	list := p.display()
	if p.sel < 0 || p.sel >= len(list) {
		return plan.Attempt{}, false
	}
	return list[p.sel], true
}

// attemptByID finds an attempt of the shown history.
func (p gardenPlannerState) attemptByID(id string) (plan.Attempt, bool) {
	if p.preview == nil {
		return plan.Attempt{}, false
	}
	for _, a := range p.preview.Attempts {
		if a.ID == id {
			return a, true
		}
	}
	return plan.Attempt{}, false
}

// replaceAttempts swaps the shown history for a newer read and keeps the cursor on the same attempt.
func (p *gardenPlannerState) replaceAttempts(list []plan.Attempt) {
	if p.preview == nil {
		return
	}
	var keep string
	if a, ok := p.selected(); ok {
		keep = a.ID
	}
	p.preview.Attempts = list
	p.sel = 0
	for i, a := range p.display() {
		if a.ID == keep {
			p.sel = i
		}
	}
}

// dirtyDecision reports whether the user's choice about uncommitted changes applies to the base in view.
func (p gardenPlannerState) dirtyDecision() bool {
	return p.preview != nil && p.preview.Base.Dirty && p.includeDirty && p.dirtyFor == p.preview.Base.DirtyDigest
}

// --- reads and writes ---

// gardenOpenPlanner is the p key on an opened Issue: it verifies gate one and inspects the checkout, and starts
// nothing.
func (m Model) gardenOpenPlanner() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	return m.gardenLoadPlanner()
}

func (m Model) gardenLoadPlanner() (Model, tea.Cmd) {
	g := &m.garden
	issue := uint64(g.board.opened)
	ctx, seq := g.begin(busyPlanner)
	api := g.api
	return m, func() tea.Msg {
		v, err := api.PreviewPlanner(ctx, issue)
		return gardenPlannerPreviewMsg{seq: seq, view: v, err: err}
	}
}

// gardenPlannerRefresh re-reads the attempts of the Issue, which also reconciles them with their sessions.
func (m Model) gardenPlannerRefresh() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	issue := uint64(g.board.opened)
	ctx, seq := g.begin(busyPlanner)
	api := g.api
	return m, func() tea.Msg {
		v, err := api.PlannerAttempts(ctx, issue)
		return gardenPlannerAttemptsMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenPlannerPreview(msg gardenPlannerPreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyPlanner, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != uint64(g.board.opened) {
		g.err = staleReply
		return m, nil
	}
	p := &g.planner
	v := msg.view
	if p.stage == plannerNone {
		p.stage = plannerHub
	}
	p.seen = msg.view.Stamp
	p.preview = &v
	if !p.dirtyDecision() {
		p.includeDirty = false
	}
	p.sel = min(p.sel, max(len(v.Attempts)-1, 0))
	if p.stage == plannerHub {
		p.btn = 0
		if v.CanRequest() {
			p.btn = 1
		}
	}
	return m, nil
}

func (m Model) onGardenPlannerAttempts(msg gardenPlannerAttemptsMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		g.err = fail
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != uint64(g.board.opened) || g.planner.preview == nil {
		g.err = staleReply
		return m, nil
	}
	if !g.planner.fresh(msg.view.Stamp) {
		return m, nil // a background poll already showed something newer
	}
	g.planner.replaceAttempts(msg.view.Attempts)
	g.planner.preview.Overlay = msg.view.Overlay
	return m, nil
}

// gardenPlannerRequest is Request AI plan on the hub: it opens the account choice. It still starts nothing.
func (m Model) gardenPlannerRequest() (Model, tea.Cmd) {
	p := &m.garden.planner
	v := p.preview
	switch {
	case v == nil:
		return m, nil
	case !v.CanRequest():
		m.garden.notice = v.Unavailable
		return m, nil
	case v.Base.Dirty && !p.dirtyDecision():
		m.garden.notice = "The checkout has uncommitted changes. Press x to let the planner read them, or commit or stash them and refresh."
		return m, nil
	case m.agents.api.Accounts == nil || !m.agents.active():
		m.garden.notice = "Agent accounts and terminal sessions are unavailable here, so no planner can run."
		return m, nil
	}
	m.agents.launchSeq++
	d := newLaunchDialog(m.agents.launchSeq, m.agents.api.Accounts, launchTarget{}, "", m.agents.usage, m.agentGlyphsASCII())
	d.planner = true
	d.SetSize(m.width, m.gardenBoardRows())
	p.dlg, p.stage = &d, plannerChoose
	m.shellFocus = shellInput
	return m, d.Init()
}

// onPlannerSubmit is the explicit start. The write is never cancelled: abandoning it halfway could leave a session
// nobody recorded.
func (m Model) onPlannerSubmit(msg launchSubmitMsg) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planner
	if p.stage != plannerChoose || p.dlg == nil || p.dlg.id != msg.dlg || p.preview == nil || g.busy != busyNone {
		return m, nil
	}
	cols, rows := m.plannerAreaSize()
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24 // the terminal resizes to the real area when it is focused
	}
	req := gardenapi.PlannerStartRequest{Preview: *p.preview, AccountID: msg.account, Model: msg.req.Launch.Model, Effort: msg.req.Launch.Effort,
		Name: msg.req.Name, IncludeDirty: p.dirtyDecision(), AcceptReadOnly: true, Cols: cols, Rows: rows}
	p.save = msg.save
	_, seq := g.begin(busyPlannerStart)
	api := g.api
	return m, func() tea.Msg {
		v, err := api.StartPlanner(context.Background(), req)
		return gardenPlannerStartMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenPlannerStart(msg gardenPlannerStartMsg) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planner
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		switch fail.Code {
		case gardenapi.CodeNotApproved, gardenapi.CodeSpecChanged, gardenapi.CodeRepoChanged, gardenapi.CodeNotFound, gardenapi.CodeAttemptActive:
			// What the preview showed is no longer true: nothing started. Read it again and decide again.
			p.dlg, p.stage = nil, plannerHub
			m.shellFocus = shellBody
			m, cmd := m.gardenLoadPlanner()
			m.garden.notice = fail.Message + " " + fail.Next
			return m, cmd
		}
		if p.dlg != nil {
			d := p.dlg.failed(failText(fail))
			p.dlg = &d
			return m, nil
		}
		g.err = fail
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) {
		// The planner did start: say so rather than dropping the news with a stale stamp.
		g.notice = "A planner attempt was started; refresh to see it."
		p.dlg, p.stage = nil, plannerHub
		m.shellFocus = shellBody
		return m, nil
	}
	a := msg.view.Attempt
	var cmds []tea.Cmd
	if p.save != nil && m.agents.api.Accounts != nil {
		cmds = append(cmds, saveDefaultsCmd(m.agents.api.Accounts, a.AccountID, p.save))
	}
	p.save, p.dlg = nil, nil
	if p.preview != nil {
		p.replaceAttempts(append(append([]plan.Attempt(nil), p.preview.Attempts...), a))
		p.setOverlay(a.ID, msg.view.Overlay)
		p.preview.Unavailable = "A planner attempt is still running for this issue. Open it or cancel it first."
	}
	p.stage, p.attempt, p.confirmCancel = plannerSession, a.ID, false
	m.shellFocus = shellBody
	cmds = append(cmds, m.plannerAttach(a.SessionID, true))
	m, local := m.gardenLoadLocal()
	cmds = append(cmds, local)
	return m, tea.Batch(cmds...)
}

// failText turns a safe backend error into the one line the dialog shows.
func failText(f *gardenapi.Error) error {
	text := f.Message
	if f.Next != "" {
		text += " " + f.Next
	}
	return plannerError(text)
}

type plannerError string

func (e plannerError) Error() string { return string(e) }

// gardenPlannerCancel stops the attempt under the cursor, or the one the session body shows. The write is never
// cancelled; the attempt and its history stay.
func (m Model) gardenPlannerCancel(id string) (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() {
		return m, nil
	}
	_, seq := g.begin(busyPlannerCancel)
	api := g.api
	g.planner.confirmCancel = false
	return m, func() tea.Msg {
		v, err := api.CancelPlanner(context.Background(), id)
		return gardenPlannerCancelMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenPlannerCancel(msg gardenPlannerCancelMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		g.err = fail
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) || g.planner.preview == nil {
		g.err = staleReply
		return m, nil
	}
	// The cancel only asks the session to end: the attempt follows what the session reports, so refresh.
	list := append([]plan.Attempt(nil), g.planner.preview.Attempts...)
	for i := range list {
		if list[i].ID == msg.view.Attempt.ID {
			list[i] = msg.view.Attempt
		}
	}
	g.planner.replaceAttempts(list)
	g.planner.setOverlay(msg.view.Attempt.ID, msg.view.Overlay)
	m, local := m.gardenLoadLocal()
	m, refresh := m.gardenPlannerRefresh()
	return m, tea.Batch(local, refresh)
}

// --- the session body: the Agents terminal controller, attached to the planner's session ---

// plannerAttach points the one live agent attachment at the planner's session. write attaches as the writer and
// focuses the terminal, as a fresh start does; otherwise the session is only observed until the user focuses it.
func (m *Model) plannerAttach(sessionID string, write bool) tea.Cmd {
	if !m.agents.active() || sessionID == "" {
		m.garden.notice = "Terminal sessions are unavailable here, so the session cannot be shown."
		return nil
	}
	var release tea.Cmd
	if v := m.agents.view; v != nil {
		release = releaseCmd(v.stream, v.term)
	}
	m.agents.attachSeq++
	v := &agentView{sessionID: sessionID, token: m.agents.attachSeq, opts: agentsapi.AttachOptions{ReadOnly: !write}, control: write}
	m.agents.view = v
	return tea.Batch(release, attachCmd(m.agents.ctx, m.agents.api.Sessions, sessionID, v.token, v.attachOptions()))
}

// plannerRelease drops the attachment. The holder, and the planner in it, keep running.
func (m *Model) plannerRelease() tea.Cmd { return m.terminalRelease() }

// plannerSessionShown reports whether the Garden terminal is the body in view: the planner's, or an
// implementation attempt's. Both are the one Agents controller attached to one session.
func (m Model) plannerSessionShown() bool {
	if m.launchSessionShown() {
		return true
	}
	return m.layoutKind == LayoutRedesign && m.workspace == wsGarden && m.plannerActive() && m.garden.planner.stage == plannerSession &&
		m.garden.hasStatus && m.garden.sub != subDrafts && m.agents.view != nil
}

// plannerTerminalMode is true while the user types into the planner. Every key but Ctrl+] then belongs to the
// agent, so no Garden or shell key can fire by accident; a modal still owns the keyboard first.
func (m Model) plannerTerminalMode() bool {
	return m.plannerSessionShown() && m.modal == nil && m.agents.view.control
}

// plannerAreaSize is the terminal area inside the Garden body.
func (m Model) plannerAreaSize() (w, rows int) {
	return max(m.width, 1), max(m.gardenBoardRows()-plannerHeadRows-plannerTailRows, 0)
}

// plannerTerminalMsg routes the controller's messages. The handlers are the Agents tab's own; the planner only
// adds what the Garden screen needs to learn from them.
func (m Model) plannerTerminalMsg(msg tea.Msg) (Model, tea.Cmd) {
	var nm tea.Model
	var cmd tea.Cmd
	var changed bool
	switch msg := msg.(type) {
	case agentAttachedMsg:
		nm, cmd = m.onAgentAttached(msg)
		m = nm.(Model)
		// A fresh attach is when the attempt's state is worth reading again.
		changed = m.agents.view != nil && m.agents.view.stream != nil
	case agentRetryMsg:
		nm, cmd = m.onAgentRetry(msg)
		m = nm.(Model)
	case agentFramesMsg:
		was := m.agents.view != nil && m.agents.view.state == attachEnded
		nm, cmd = m.onAgentFrames(msg)
		m = nm.(Model)
		if v := m.agents.view; v != nil && v.state == attachEnded {
			// The legacy banner points at keys the planner does not have.
			back := " · esc: attempts"
			if m.garden.launch.stage == launchStageSession {
				back = " · esc: attempt"
			}
			v.banner = strings.TrimSuffix(v.banner, " · n: new · a: all") + back
			changed = !was
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
	if changed && m.garden.planner.stage == plannerSession && m.garden.busy == busyNone {
		var refresh tea.Cmd
		m, refresh = m.gardenPlannerRefresh()
		cmd = tea.Batch(cmd, refresh)
	}
	if changed {
		var refresh tea.Cmd
		m, refresh = m.launchTerminalChanged()
		cmd = tea.Batch(cmd, refresh)
	}
	return m, cmd
}

// gardenPlannerForward feeds the account choice the results of its own backend calls. The message continues to
// the rest of the shell afterwards: the choice's accounts browser ignores what is not its own.
func (m Model) gardenPlannerForward(msg tea.Msg) (Model, tea.Cmd) {
	p := &m.garden.planner
	if p.dlg == nil || p.stage != plannerChoose {
		return m, nil
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		return m, nil
	}
	if _, done := msg.(agentExecDoneMsg); done {
		// A sign-in finished in the real terminal: read the accounts again.
		msg = accounts.ReloadMsg{}
	}
	d, cmd := p.dlg.Update(msg)
	p.dlg = &d
	return m, cmd
}

// gardenPlannerClose leaves the planner screen for the Issue document. The session, if any, keeps running.
func (m Model) gardenPlannerClose() (Model, tea.Cmd) {
	rel := m.plannerRelease()
	if m.garden.planner.liveAttempt() {
		m.garden.notice = keepsRunning
	}
	m.garden.planner = gardenPlannerState{}
	if m.shellFocus == shellInput {
		m.shellFocus = shellBody
	}
	m, local := m.gardenLoadLocal() // the badge reflects what was just seen, not the last tick
	return m, tea.Batch(rel, local)
}

// setOverlay records the live view of one attempt.
func (p *gardenPlannerState) setOverlay(id string, v gardenapi.PlannerLiveView) {
	if p.preview == nil {
		return
	}
	next := make(map[string]gardenapi.PlannerLiveView, len(p.preview.Overlay)+1)
	for k, o := range p.preview.Overlay {
		next[k] = o
	}
	next[id] = v
	p.preview.Overlay = next
}

// --- keys ---

func (m Model) gardenPlannerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.garden.planner
	switch p.stage {
	case plannerChoose:
		return m.gardenPlannerChooseKey(msg)
	case plannerSession:
		return m.gardenPlannerSessionKey(msg)
	}
	return m.gardenPlannerHubKey(msg)
}

func (m Model) gardenPlannerChooseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.garden.planner
	if p.dlg == nil {
		return m, nil
	}
	d, cmd := p.dlg.Update(msg)
	p.dlg = &d
	return m, cmd
}

// gardenPlannerChooseClose backs out of the account choice without starting anything.
func (m Model) gardenPlannerChooseClose() Model {
	p := &m.garden.planner
	p.dlg, p.save, p.stage = nil, nil, plannerHub
	m.shellFocus = shellBody
	return m
}

func (m Model) gardenPlannerHubKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	p := &g.planner
	k := msg.String()
	if p.confirmCancel {
		if k == "y" {
			if a, ok := p.selected(); ok && a.State.Live() {
				return m.gardenPlannerCancel(a.ID)
			}
		}
		p.confirmCancel = false
		return m, nil
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenLoadPlanner()
	}
	list := p.display()
	v := p.preview
	canRequest := v != nil && v.CanRequest()
	switch k {
	case "esc", "backspace":
		return m.gardenPlannerClose()
	case "up", "k":
		p.sel = max(p.sel-1, 0)
	case "down", "j":
		p.sel = max(min(p.sel+1, len(list)-1), 0)
	case "left", "h", "shift+tab":
		p.btn = 0
	case "right", "l", "tab":
		if canRequest {
			p.btn = 1
		}
	case "x":
		if v != nil && v.Base.Dirty {
			p.includeDirty = !p.includeDirty
			p.dirtyFor = v.Base.DirtyDigest
		}
	case "u":
		return m.gardenPlannerPublish()
	case "r":
		return m.gardenOpenPlanReview()
	case "o":
		if a, ok := p.selected(); ok && a.SessionID != "" {
			p.stage, p.attempt, p.confirmCancel = plannerSession, a.ID, false
			return m, m.plannerAttach(a.SessionID, false)
		}
	case "c":
		if a, ok := p.selected(); ok && a.State.Live() {
			p.confirmCancel = true
		}
	case "enter":
		if p.btn == 0 || !canRequest {
			return m.gardenPlannerClose()
		}
		return m.gardenPlannerRequest()
	}
	return m, nil
}

func (m Model) gardenPlannerSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	p := &g.planner
	k := msg.String()
	if p.confirmCancel {
		if k == "y" {
			if a, ok := p.attemptByID(p.attempt); ok && a.State.Live() {
				return m.gardenPlannerCancel(a.ID)
			}
		}
		p.confirmCancel = false
		return m, nil
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenPlannerRefresh()
	}
	switch k {
	case "esc", "backspace":
		// Back to the attempts: the attachment is released, the session is not stopped.
		rel := m.plannerRelease()
		p.stage, p.attempt = plannerHub, ""
		m, cmd := m.gardenLoadPlanner()
		return m, tea.Batch(rel, cmd)
	case "enter":
		_, cmd := m.enterTerminalMode()
		return m, cmd
	case "c":
		if a, ok := p.attemptByID(p.attempt); ok && a.State.Live() {
			p.confirmCancel = true
		}
	}
	return m, nil
}

// gardenPlannerHints are the footer keys of the planner screen.
func (m Model) gardenPlannerHints() []shellHint {
	p := m.garden.planner
	refresh := shellHint{m.keys.Refresh.Help().Key, "refresh"}
	switch p.stage {
	case plannerChoose:
		return []shellHint{{"↑/↓", "choose"}, {"tab", "next field"}, {"ctrl+s", "accept and request"}, {"esc", "back"}}
	case plannerSession:
		if m.plannerTerminalMode() {
			return []shellHint{{"ctrl+]", "unfocus"}}
		}
		if p.confirmCancel {
			return []shellHint{{"y", "cancel the attempt"}, {"any", "keep it"}}
		}
		return []shellHint{{"enter", "focus terminal"}, {"c", "cancel attempt"}, refresh, {"esc", "attempts"}}
	}
	if p.confirmCancel {
		return []shellHint{{"y", "cancel the attempt"}, {"any", "keep it"}}
	}
	h := []shellHint{{"↑/↓", "attempt"}, {"o", "open session"}, {"u", "publish plan"}, {"r", "review plans"}, {"c", "cancel attempt"}}
	if p.preview != nil && p.preview.Base.Dirty {
		h = append(h, shellHint{"x", "uncommitted changes"})
	}
	return append(h, shellHint{"enter", "activate"}, refresh, shellHint{"esc", "back"})
}
