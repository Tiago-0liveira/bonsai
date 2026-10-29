package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Human handoff (plan 13). The panel reads the evidence of the newest attempt and offers the human's decisions.
// Reading, refreshing and navigating only call Handoff; every write is a preview, a cancel-first confirmation of
// that exact preview, and one call that is never repeated on its own. Nothing here merges, cleans or pushes.

// handoffTick is how often the visible panel reads again. GitHub is part of the read, so it is slower than the
// supervision tick.
const handoffTick = 15 * time.Second

type handoffMode int

const (
	handoffIdle       handoffMode = iota
	handoffText                   // the human's words for a decision
	handoffPreviewing             // waiting for the preview of a decision or rework
	handoffDecide                 // the confirmation of one decision preview
	handoffRework                 // the confirmation of one rework preview
)

type gardenHandoffState struct {
	issue uint64
	// requested is false until the first read of this open; the attempt is then resolved by the backend.
	requested bool
	seq       uint64 // the newest read: an older reply is obsolete
	readGen   uint64 // generation of the newest accepted view
	loading   bool
	// seen is the attempt as it was when the newest read was requested: a later state or revision of the same
	// attempt (a rework that came back to review) is a reason to read again.
	seenID    string
	seenRev   uint64
	seenState launch.State
	view      *gardenapi.HandoffView // last good view; a failed read never replaces it
	readErr   string

	tickGen     uint64
	tickPending bool

	mode     handoffMode
	decision garden.HandoffDecision
	// text is a pointer because the Model is copied by value on every message and a textarea is large.
	text   *textarea.Model
	drafts map[garden.HandoffDecision]string
	// prevSeq makes an answer of a canceled or superseded preview obsolete.
	prevSeq  uint64
	preview  *gardenapi.HandoffDecisionPreview
	rework   *gardenapi.ReworkPreview
	acks     []bool
	scroll   int
	inflight bool
	// writeSeq numbers the writes. It survives handoffReset so a late result of a write sent before an Issue switch
	// can never match a write of the same Issue opened again.
	writeSeq uint64
	// note is the warning or result of the last write, shown until the next key.
	note string
}

type (
	gardenHandoffReadMsg struct {
		seq     uint64
		issue   uint64
		attempt string // what was asked; empty means the newest
		view    gardenapi.HandoffView
		err     error
	}
	gardenHandoffTickMsg struct {
		gen   uint64
		issue uint64
	}
	gardenHandoffPreviewMsg struct {
		seq     uint64
		issue   uint64
		attempt string
		view    gardenapi.HandoffDecisionPreview
		err     error
	}
	gardenHandoffReworkPreviewMsg struct {
		seq     uint64
		issue   uint64
		attempt string
		view    gardenapi.ReworkPreview
		err     error
	}
	gardenHandoffDecideMsg struct {
		wseq    uint64
		issue   uint64
		attempt string
		view    gardenapi.HandoffDecisionView
		err     error
	}
	gardenHandoffReworkMsg struct {
		wseq    uint64
		issue   uint64
		attempt string
		scope   launchScope
		view    gardenapi.LaunchAttemptView
		err     error
	}
	gardenHandoffOpenMsg struct {
		issue uint64
		err   error
	}
)

// handoffBackend is the optional human handoff of the Garden backend.
func (m Model) handoffBackend() (gardenapi.HandoffBackend, bool) {
	hb, ok := m.garden.api.(gardenapi.HandoffBackend)
	return hb, ok
}

func (m Model) handoffSupported() bool {
	_, ok := m.handoffBackend()
	return ok
}

// handoffNewest is the newest attempt of the opened Issue, the one the handoff is about.
func (m Model) handoffNewest() (launch.Attempt, bool) {
	for _, a := range m.garden.launch.display() {
		if uint64(a.Issue) == uint64(m.garden.board.opened) {
			return a, true
		}
	}
	return launch.Attempt{}, false
}

// handoffView is the last good view when it describes the newest attempt.
func (m Model) handoffView() (gardenapi.HandoffView, bool) {
	h := m.garden.handoff
	if h.view == nil || h.issue != uint64(m.garden.board.opened) {
		return gardenapi.HandoffView{}, false
	}
	if a, has := m.handoffNewest(); has && h.view.AttemptID != a.ID {
		return gardenapi.HandoffView{}, false
	}
	return *h.view, true
}

// handoffShown reports whether the Overview draws the handoff panel instead of the supervision or Ready text.
func (m Model) handoffShown() bool {
	if !m.handoffSupported() || !m.gardenOverlayOpen() || m.garden.board.section != 0 {
		return false
	}
	a, has := m.handoffNewest()
	if has && a.State.Active() {
		return false
	}
	v, ok := m.handoffView()
	switch {
	case !has:
		return ok && (v.Actions.CompleteNonCode.Allowed || v.Completion.State != handoff.CompletionNotReady && v.Completion.State != "")
	case a.State == launch.StateAwaitingReview:
		return true
	case a.State == launch.StateCleaned:
		return ok && (v.Decision.Entry != nil || v.Completion.State != handoff.CompletionNotReady && v.Completion.State != "")
	}
	return ok && v.Completion.State != handoff.CompletionNotReady && v.Completion.State != ""
}

// handoffUnavailableNote says, under the overview, that the review of a finished attempt cannot be recorded here.
func (m Model) handoffUnavailableNote() string {
	if a, ok := m.handoffNewest(); ok && a.State == launch.StateAwaitingReview && !m.handoffSupported() {
		return "\n\nHuman handoff is unavailable: this Garden runtime cannot record your review."
	}
	return ""
}

// handoffDone reports a completion proven by the evidence, for the overlay header.
func (m Model) handoffDone() bool {
	v, ok := m.handoffView()
	return ok && m.handoffShown() && v.Completion.State == handoff.CompletionDone
}

func (m Model) handoffModal() bool {
	return m.garden.handoff.mode != handoffIdle && m.handoffSupported() && m.gardenOverlayOpen()
}

// handoffTyping is whether the text box has the keyboard.
func (m Model) handoffTyping() bool {
	return m.garden.handoff.mode == handoffText && m.handoffModal()
}

// handoffReset forgets everything of the previous Issue. Drafts are kept per open Issue only.
func (m *Model) handoffReset(issue uint64) {
	prev := m.garden.handoff
	m.garden.handoff = gardenHandoffState{issue: issue, seq: prev.seq + 1, tickGen: prev.tickGen + 1, prevSeq: prev.prevSeq + 1, writeSeq: prev.writeSeq}
}

// handoffWanted: the panel could be drawn now, so it is worth reading.
func (m Model) handoffWanted() bool {
	if !m.handoffSupported() || !m.gardenOverlayOpen() || m.garden.board.section != 0 || m.garden.board.opened == 0 {
		return false
	}
	a, has := m.handoffNewest()
	return !has || !a.State.Active()
}

// handoffSync reads when the panel has not been read for the attempt it now describes, and keeps its tick armed.
// It is the only place a read starts without the user asking, and it reads only.
func (m Model) handoffSync() (Model, tea.Cmd) {
	if !m.handoffWanted() {
		m.garden.handoff.tickGen++
		m.garden.handoff.tickPending = false
		return m, nil
	}
	h := &m.garden.handoff
	if h.issue != uint64(m.garden.board.opened) {
		m.handoffReset(uint64(m.garden.board.opened))
	}
	var load tea.Cmd
	a, has := m.handoffNewest()
	switch {
	case !h.requested:
		load = m.handoffLoad("")
	case h.view != nil && !h.loading && has && (h.view.AttemptID != a.ID || h.seenID == a.ID && (h.seenRev != a.Revision || h.seenState != a.State)):
		load = m.handoffLoad(a.ID)
	}
	return m, tea.Batch(load, m.handoffNext())
}

// handoffRefresh reads again now, after a write or a screen that may have changed the evidence.
func (m Model) handoffRefresh() (Model, tea.Cmd) {
	if !m.handoffWanted() {
		return m, nil
	}
	if m.garden.handoff.issue != uint64(m.garden.board.opened) {
		m.handoffReset(uint64(m.garden.board.opened))
	}
	attempt := ""
	if v := m.garden.handoff.view; v != nil {
		attempt = v.AttemptID
	}
	if a, has := m.handoffNewest(); has {
		attempt = a.ID
	}
	return m, tea.Batch(m.handoffLoad(attempt), m.handoffNext())
}

func (m *Model) handoffLoad(attempt string) tea.Cmd {
	hb, ok := m.handoffBackend()
	if !ok {
		return nil
	}
	h := &m.garden.handoff
	h.seq++
	h.loading, h.requested = true, true
	if a, has := m.handoffNewest(); has {
		h.seenID, h.seenRev, h.seenState = a.ID, a.Revision, a.State
	}
	seq, issue := h.seq, uint64(m.garden.board.opened)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		v, err := hb.Handoff(ctx, gardenapi.HandoffRequest{IssueID: issue, AttemptID: attempt})
		return gardenHandoffReadMsg{seq: seq, issue: issue, attempt: attempt, view: v, err: err}
	}
}

func (m Model) onGardenHandoffRead(msg gardenHandoffReadMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	if msg.seq != h.seq || msg.issue != uint64(m.garden.board.opened) || msg.issue != h.issue {
		return m, nil
	}
	h.loading = false
	switch {
	case msg.err != nil:
		h.readErr = launchErrText(msg.err)
	case !m.sameClone(msg.view.Stamp) || msg.view.IssueID != msg.issue || msg.view.Stamp.Generation < h.readGen ||
		msg.attempt != "" && msg.view.AttemptID != msg.attempt:
		h.readErr = staleReply.Message
	default:
		v := msg.view
		h.view, h.readErr, h.readGen = &v, "", v.Stamp.Generation
	}
	return m, m.handoffNext()
}

func (m *Model) handoffNext() tea.Cmd {
	h := &m.garden.handoff
	if !m.handoffWanted() || h.mode != handoffIdle || h.inflight {
		h.tickGen++
		h.tickPending = false
		return nil
	}
	if h.tickPending {
		return nil
	}
	h.tickGen++
	h.tickPending = true
	return handoffTickCmd(gardenHandoffTickMsg{gen: h.tickGen, issue: uint64(m.garden.board.opened)})
}

func handoffTickCmd(msg gardenHandoffTickMsg) tea.Cmd {
	return func() tea.Msg { return tea.Tick(handoffTick, func(time.Time) tea.Msg { return msg })() }
}

func (m Model) onGardenHandoffTick(msg gardenHandoffTickMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	if msg.gen != h.tickGen || msg.issue != uint64(m.garden.board.opened) || !h.tickPending || !m.handoffWanted() {
		return m, nil
	}
	h.tickPending = false
	if h.loading || h.inflight || h.mode != handoffIdle {
		return m, m.handoffNext()
	}
	h.note = "" // a result is only news until the next scheduled read
	return m.handoffRefresh()
}

// --- the decisions ---

// handoffAction is the backend's verdict on one control.
func handoffAction(v gardenapi.HandoffView, id string) gardenapi.HandoffAction {
	switch id {
	case "ho-accept":
		return v.Actions.Accept
	case "ho-changes":
		return v.Actions.RequestChanges
	case "ho-revoke":
		return v.Actions.Revoke
	case "ho-complete":
		return v.Actions.CompleteNonCode
	case "ho-rework":
		return v.Actions.Rework
	case "ho-pr":
		return v.Actions.OpenPR
	case "ho-cleanup":
		return v.Actions.Cleanup
	}
	return gardenapi.HandoffAction{}
}

func (h *gardenHandoffState) textValue() string {
	if h.text == nil {
		return ""
	}
	return h.text.Value()
}

func handoffTextArea() textarea.Model {
	ta := planFormTextarea()
	ta.Placeholder = ""
	return ta
}

// handoffActivate runs a handoff control. A refused one says why instead of staying silent.
func (m Model) handoffActivate(id string) (tea.Model, tea.Cmd) {
	h := &m.garden.handoff
	if h.inflight {
		return m, nil
	}
	if h.mode != handoffIdle {
		return m.handoffModalActivate(id)
	}
	v, ok := m.handoffView()
	if !ok {
		if id == "ho-terminal" || id == "ho-cleanup" {
			return m.handoffAttemptRoute(id)
		}
		m.garden.notice = "The handoff has not been read yet."
		return m, nil
	}
	if act := handoffAction(v, id); id != "ho-terminal" && !act.Allowed {
		m.garden.notice = orDefault(boardSafe(act.Message), "That action is not possible now.")
		return m, nil
	}
	switch id {
	case "ho-accept":
		return m.handoffPreview(garden.HandoffAccepted, "")
	case "ho-revoke":
		return m.handoffPreview(garden.HandoffRevoked, "")
	case "ho-changes":
		return m.handoffOpenText(garden.HandoffChangesRequested)
	case "ho-complete":
		return m.handoffOpenText(garden.HandoffCompleted)
	case "ho-rework":
		return m.handoffPreviewRework()
	case "ho-pr":
		return m.handoffOpenPR(v)
	}
	return m.handoffAttemptRoute(id)
}

// handoffAttemptRoute reuses the launch screens for the attempt's terminal and cleanup preview.
func (m Model) handoffAttemptRoute(id string) (tea.Model, tea.Cmd) {
	a, ok := m.handoffNewest()
	if !ok {
		m.garden.notice = "There is no implementation attempt for this Issue."
		return m, nil
	}
	l := &m.garden.launch
	l.attempt, l.run, l.stage = a.ID, -1, launchStageAttempt
	if id == "ho-cleanup" {
		return m.launchOpenConfirm(launchActCleanup)
	}
	return m.launchOpenTerminal()
}

func (m Model) handoffOpenText(d garden.HandoffDecision) (tea.Model, tea.Cmd) {
	h := &m.garden.handoff
	ta := handoffTextArea()
	ta.CharLimit = planrev.MaxReasonBytes
	ta.SetValue(h.drafts[d])
	ta.Focus()
	h.text, h.decision, h.mode, h.note, h.scroll = &ta, d, handoffText, "", 0
	m.garden.board.overlayFocus = ""
	m.garden.board.docScroll = 0
	m.shellFocus = shellInput
	m.handoffSize()
	return m, nil
}

func (m *Model) handoffSize() {
	h := &m.garden.handoff
	w, rows := m.gardenOverlayBody()
	if h.text == nil {
		return
	}
	// Copy before resizing: earlier Model values share the pointer and must not change.
	ta := *h.text
	ta.SetWidth(max(w-2, 4))
	ta.SetHeight(max(2, min(rows-7, 8)))
	h.text = &ta
}

// handoffPreview asks for the exact record a decision would write. It writes nothing.
func (m Model) handoffPreview(d garden.HandoffDecision, text string) (tea.Model, tea.Cmd) {
	hb, ok := m.handoffBackend()
	v, have := m.handoffView()
	if !ok || !have {
		return m, nil
	}
	h := &m.garden.handoff
	req := gardenapi.HandoffDecisionRequest{IssueID: v.IssueID, AttemptID: v.AttemptID, Decision: d, Text: text}
	if d == garden.HandoffRevoked && v.Decision.Entry != nil {
		req.TargetCommentID = v.Decision.Entry.CommentID
	}
	h.decision, h.mode, h.note = d, handoffPreviewing, ""
	h.prevSeq++
	seq, issue := h.prevSeq, uint64(m.garden.board.opened)
	m.shellFocus = shellBody
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		p, err := hb.PreviewHandoffDecision(ctx, req)
		return gardenHandoffPreviewMsg{seq: seq, issue: issue, attempt: req.AttemptID, view: p, err: err}
	}
}

func (m Model) handoffPreviewRework() (tea.Model, tea.Cmd) {
	hb, ok := m.handoffBackend()
	v, have := m.handoffView()
	if !ok || !have {
		return m, nil
	}
	h := &m.garden.handoff
	h.mode, h.note = handoffPreviewing, ""
	h.decision = ""
	h.prevSeq++
	seq, issue, attempt := h.prevSeq, v.IssueID, v.AttemptID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		p, err := hb.PreviewRework(ctx, issue, attempt)
		return gardenHandoffReworkPreviewMsg{seq: seq, issue: issue, attempt: attempt, view: p, err: err}
	}
}

// handoffFail words a refused call and says what to do; changed and unknown outcomes also read again.
func (m Model) handoffFail(err error) (Model, tea.Cmd) {
	f := safeGardenError(err)
	h := &m.garden.handoff
	switch f.Code {
	case gardenapi.CodeHandoffChanged:
		h.note = "Changed since preview; review again."
	case gardenapi.CodeHandoffUnknown:
		h.note = "The decision may have been recorded; refreshing. Check before deciding again."
	case gardenapi.CodeLaunchActive:
		h.note = "Previous run still active; stop it first."
	default:
		h.note = launchErrText(err)
	}
	if f.Code == gardenapi.CodeHandoffChanged || f.Code == gardenapi.CodeHandoffUnknown {
		return m.handoffRefresh()
	}
	return m, nil
}

func (m Model) handoffBack() Model {
	h := &m.garden.handoff
	h.mode, h.preview, h.rework, h.acks, h.scroll = handoffIdle, nil, nil, nil, 0
	h.prevSeq++
	m.garden.board.overlayFocus = ""
	if m.shellFocus == shellInput {
		m.shellFocus = shellBody
	}
	return m
}

func (m Model) onGardenHandoffPreview(msg gardenHandoffPreviewMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	if msg.seq != h.prevSeq || h.mode != handoffPreviewing || msg.issue != uint64(m.garden.board.opened) {
		return m, nil
	}
	if msg.err != nil {
		d, typed := h.decision, h.drafts[h.decision] != ""
		m = m.handoffBack()
		if typed {
			n, _ := m.handoffOpenText(d)
			m = n.(Model)
		}
		return m.handoffFail(msg.err)
	}
	p := msg.view
	if !m.sameClone(p.Stamp) || p.IssueID != msg.issue || p.AttemptID != msg.attempt || p.Decision != h.decision {
		m = m.handoffBack()
		m.garden.handoff.note = staleReply.Message
		return m, nil
	}
	h.preview, h.rework, h.mode, h.scroll = &p, nil, handoffDecide, 0
	h.acks = make([]bool, len(p.Acknowledgments))
	m.garden.board.overlayFocus = "ho-cancel"
	m.shellFocus = shellBody
	return m, nil
}

func (m Model) onGardenHandoffReworkPreview(msg gardenHandoffReworkPreviewMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	if msg.seq != h.prevSeq || h.mode != handoffPreviewing || msg.issue != uint64(m.garden.board.opened) {
		return m, nil
	}
	if msg.err != nil {
		m = m.handoffBack()
		return m.handoffFail(msg.err)
	}
	p := msg.view
	if !m.sameClone(p.Stamp) || p.IssueID != msg.issue || p.AttemptID != msg.attempt {
		m = m.handoffBack()
		m.garden.handoff.note = staleReply.Message
		return m, nil
	}
	h.rework, h.preview, h.mode, h.scroll = &p, nil, handoffRework, 0
	h.acks = make([]bool, len(handoffReworkAcks(p)))
	m.garden.board.overlayFocus = "ho-cancel"
	return m, nil
}

// handoffConfirmable is whether Confirm can be pressed: every acknowledgment is checked and no write is running.
func (m Model) handoffConfirmable() bool {
	h := m.garden.handoff
	if h.inflight || h.mode != handoffDecide && h.mode != handoffRework || len(h.acks) != len(m.handoffAcks()) {
		return false
	}
	for _, on := range h.acks {
		if !on {
			return false
		}
	}
	return true
}

func (m Model) handoffModalActivate(id string) (tea.Model, tea.Cmd) {
	h := &m.garden.handoff
	switch {
	case id == "ho-cancel" || id == "ho-back":
		if h.mode == handoffText {
			h.drafts = handoffDraft(h.drafts, h.decision, h.textValue())
		}
		return m.handoffBack(), nil
	case id == "ho-send":
		return m.handoffSend()
	case strings.HasPrefix(id, "ho-ack-"):
		i := int(id[len("ho-ack-")] - '0')
		if i >= 0 && i < len(h.acks) {
			h.acks[i] = !h.acks[i]
			if !m.handoffConfirmable() && m.garden.board.overlayFocus == "ho-confirm" {
				m.garden.board.overlayFocus = "ho-cancel"
			}
		}
		return m, nil
	case id == "ho-confirm":
		if !m.handoffConfirmable() {
			return m, nil
		}
		if h.mode == handoffRework {
			return m.handoffRunRework()
		}
		return m.handoffRunDecision()
	}
	return m, nil
}

func handoffDraft(drafts map[garden.HandoffDecision]string, d garden.HandoffDecision, text string) map[garden.HandoffDecision]string {
	if drafts == nil {
		drafts = map[garden.HandoffDecision]string{}
	}
	drafts[d] = text
	return drafts
}

// handoffSend is the text box's submit: it only asks for the preview.
func (m Model) handoffSend() (tea.Model, tea.Cmd) {
	h := &m.garden.handoff
	text := strings.TrimSpace(h.textValue())
	if text == "" {
		h.note = "Text is required: say what should change, or what completes the work."
		return m, nil
	}
	h.drafts = handoffDraft(h.drafts, h.decision, h.textValue())
	return m.handoffPreview(h.decision, text)
}

func (m Model) handoffRunDecision() (tea.Model, tea.Cmd) {
	hb, ok := m.handoffBackend()
	h := &m.garden.handoff
	if !ok || h.preview == nil || h.inflight {
		return m, nil
	}
	// The exact preview the human confirmed, unchanged: the backend verifies it against fresh data.
	p := *h.preview
	h.inflight = true
	h.writeSeq++
	wseq, issue := h.writeSeq, uint64(m.garden.board.opened)
	return m, func() tea.Msg {
		v, err := hb.DecideHandoff(context.Background(), p, true)
		return gardenHandoffDecideMsg{wseq: wseq, issue: issue, attempt: p.AttemptID, view: v, err: err}
	}
}

func (m Model) onGardenHandoffDecide(msg gardenHandoffDecideMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	// A result that is not of the write in flight is dropped before it touches any state, inflight included.
	if !h.inflight || msg.wseq != h.writeSeq || msg.issue != uint64(m.garden.board.opened) || h.mode != handoffDecide ||
		h.preview == nil || h.preview.AttemptID != msg.attempt {
		return m, nil
	}
	h.inflight = false
	err := msg.err
	if err == nil && msg.view.Err != nil {
		err = msg.view.Err
	}
	decision := h.decision
	m = m.handoffBack()
	h = &m.garden.handoff
	switch {
	case err != nil:
		return m.handoffFail(err)
	case msg.view.State == gardenapi.PlanWriteDone:
		delete(h.drafts, decision)
		h.note = "Recorded on GitHub. Merging remains a separate action."
		if decision == garden.HandoffChangesRequested {
			h.note = "Recorded on GitHub. Rework does not start automatically."
		}
		return m.handoffRefresh()
	}
	h.note = "The decision may have been recorded; refreshing. Check before deciding again."
	return m.handoffRefresh()
}

func (m Model) handoffRunRework() (tea.Model, tea.Cmd) {
	hb, ok := m.handoffBackend()
	h := &m.garden.handoff
	a, have := m.handoffNewest()
	if !ok || h.rework == nil || h.inflight || !have {
		return m, nil
	}
	req := gardenapi.ReworkStartRequest{Preview: *h.rework, Confirmed: true}
	for i, ack := range handoffReworkAcks(*h.rework) {
		if ack.code == "implementer" {
			req.EscalationConfirmed = h.acks[i]
		} else {
			req.ReviewEscalationConfirmed = h.acks[i]
		}
	}
	h.inflight = true
	h.writeSeq++
	wseq, issue, scope := h.writeSeq, uint64(m.garden.board.opened), scopeOfAttempt(a)
	return m, func() tea.Msg {
		v, err := hb.StartRework(context.Background(), req)
		return gardenHandoffReworkMsg{wseq: wseq, issue: issue, attempt: req.Preview.AttemptID, scope: scope, view: v, err: err}
	}
}

func (m Model) onGardenHandoffRework(msg gardenHandoffReworkMsg) (Model, tea.Cmd) {
	h := &m.garden.handoff
	if !h.inflight || msg.wseq != h.writeSeq || msg.issue != uint64(m.garden.board.opened) || h.mode != handoffRework ||
		h.rework == nil || h.rework.AttemptID != msg.attempt {
		return m, nil
	}
	h.inflight = false
	m = m.handoffBack()
	if msg.err != nil {
		return m.handoffFail(msg.err)
	}
	l := &m.garden.launch
	a := msg.view.Attempt
	if a.ID != "" && msg.scope.valid(a, msg.view.Stamp) {
		l.merge(a)
		l.keepReview(a.ID, msg.view.Review)
		l.setReviewUnknown(a.ID, msg.view.ReviewUnknown)
	}
	// The rework is an active run of the same attempt: the Overview supervises it from the next read on.
	m, read := m.launchLoad(msg.issue)
	m, sync := m.handoffSync()
	m.garden.handoff.note = ""
	return m, tea.Batch(read, sync, m.launchOverviewNext(), m.launchSubscribe())
}

func (m Model) handoffOpenPR(v gardenapi.HandoffView) (tea.Model, tea.Cmd) {
	hb, ok := m.handoffBackend()
	if !ok {
		return m, nil
	}
	if !v.PR.Linked {
		// Without a linked pull request no screen shows the diff from here: the changed files are listed in the panel.
		m.garden.notice = "No pull request is linked; the changed files are listed in this panel."
		return m, nil
	}
	issue, attempt := v.IssueID, v.AttemptID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), launchReadTimeout)
		defer cancel()
		return gardenHandoffOpenMsg{issue: issue, err: hb.OpenHandoffPR(ctx, issue, attempt)}
	}
}

func (m Model) onGardenHandoffOpen(msg gardenHandoffOpenMsg) (Model, tea.Cmd) {
	if msg.err != nil && msg.issue == uint64(m.garden.board.opened) {
		m.garden.notice = launchErrText(msg.err)
	}
	return m, nil
}

// --- keys ---

// handoffKeyIDs are the panel's shortcuts. They only exist while the panel is drawn.
var handoffKeyIDs = map[string]string{"a": "ho-accept", "c": "ho-changes", "u": "ho-revoke", "m": "ho-rework", "n": "ho-complete",
	"g": "ho-pr", "w": "ho-cleanup", "o": "ho-terminal"}

// handoffKey owns every key while a flow is open. While a write answers, every key is ignored so a second Enter
// cannot repeat it.
func (m Model) handoffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	h := &m.garden.handoff
	b := &m.garden.board
	if h.inflight {
		return m, nil
	}
	h.note = ""
	k := msg.String()
	if h.mode == handoffText {
		switch k {
		case "esc":
			return m.handoffModalActivate("ho-back")
		case "ctrl+s":
			return m.handoffSend()
		}
		m.handoffSize()
		if h.text == nil {
			return m, nil
		}
		ta, cmd := h.text.Update(msg)
		h.text = &ta
		return m, cmd
	}
	switch {
	case k == "esc":
		return m.handoffModalActivate("ho-cancel")
	case k == "tab":
		b.overlayFocus = m.issueFocusStep(1)
	case k == "shift+tab":
		b.overlayFocus = m.issueFocusStep(-1)
	case k == "enter":
		if h.mode == handoffPreviewing {
			return m, nil
		}
		return m.handoffModalActivate(b.overlayFocus)
	case len(k) == 1 && k[0] >= '1' && k[0] <= '9':
		return m.handoffModalActivate(fmt.Sprintf("ho-ack-%d", int(k[0]-'1')))
	case k == "up", k == "k":
		h.scroll = max(h.scroll-1, 0)
		b.docScroll = h.scroll
	case k == "down", k == "j":
		h.scroll++
		b.docScroll = h.scroll
		m.gardenClampDocument()
		h.scroll = b.docScroll
	}
	return m, nil
}
