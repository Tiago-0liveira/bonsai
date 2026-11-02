package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// The overlay's Plan tab (plan 09). It shows what the Issue's planning really is: the live planner, a plan file
// waiting to be published, the revision awaiting a decision, a recorded changes request that no attempt answers.
// Reading it, reopening the overlay or rereading never starts anything: the only writes are Approve, Send
// feedback & revise plan and Start revision attempt, each behind its own confirmation, and Stop attempt.

// planTabSection is the index of "Plan" in gardenDocumentSections.
const planTabSection = 3

type gardenPlanTabState struct {
	// seq numbers the revise-preview read in flight; gate orders the replies taken in. Both are the tab's own, so a
	// read racing the Work read never makes the board's reply look old.
	seq  uint64
	gate gardenapi.Gate
	// issue is the Issue the preview belongs to.
	issue   uint64
	preview *gardenapi.PlanRevisePreviewView
	// readErr is set while the newest read failed: preview is then the last good one.
	readErr bool

	// intent is what the fresh plan-review read in flight is for ("approve", "changes"); intentHash is the
	// revision the tab showed when the user asked, so a plan that moved meanwhile is refused instead of decided.
	intent     string
	intentHash string

	form *reviseForm
	// forIssue is the Issue the form or the stop confirmation belongs to; for any other Issue they are inert.
	forIssue uint64
	// draft keeps feedback typed for one revision across Back, Esc and tab switches. It is dropped, with a notice,
	// when the revision under review changes.
	draftHash    string
	draftOrdinal int
	draftText    string
	draftDirty   bool

	confirmStop bool
	// accountNames labels the accounts a revision would run with ("provider / name"); the ID stands in until read.
	accountNames map[agents.AccountID]string
	// sentReason is the feedback of the write in flight, kept to show the pending state before the next read.
	sentReason string
	// startErr says why the newest start after a recorded decision failed; startFor is that decision.
	startErr string
	startFor int64
}

// reviseForm is the request-changes form, or with retry the confirmation of Start revision attempt for a
// decision that is already recorded.
type reviseForm struct {
	hash        string
	ordinal     int
	retry       bool
	decisionID  int64
	reason      string // retry: the recorded feedback, shown and not editable
	ta          textarea.Model
	actorID     string
	specHash    string
	fingerprint string
	stamp       gardenapi.Stamp
	base        plan.Base
	// includeDirty is the user's consent to let the planner read uncommitted changes of base.
	includeDirty bool
	settings     *gardenapi.PlanReviseSettings
	// retained is true while settings come from the attempt that wrote the revision.
	retained    bool
	unavailable string
}

type (
	gardenPlanRevisePreviewMsg struct {
		seq   uint64
		issue uint64
		view  gardenapi.PlanRevisePreviewView
		err   error
	}
	gardenPlanTabReviewMsg struct {
		seq    uint64
		intent string
		view   gardenapi.PlanReviewView
		err    error
	}
	gardenPlanAccountsMsg struct {
		names map[agents.AccountID]string
		err   error
	}
	gardenPlanReviseMsg struct {
		seq   uint64
		retry bool
		issue uint64
		view  gardenapi.PlanReviseView
		err   error
	}
)

// planForm is the request-changes form of the opened Issue, nil when none is open.
func (m Model) planForm() *reviseForm {
	if t := m.garden.plantab; t.form != nil && t.forIssue == uint64(m.garden.board.opened) {
		return t.form
	}
	return nil
}

// planStopAsked reports whether the opened Issue's stop confirmation is showing.
func (m Model) planStopAsked() bool {
	t := m.garden.plantab
	return t.confirmStop && t.forIssue == uint64(m.garden.board.opened)
}

// planTabShown reports whether the Plan tab is the one in view.
func (m Model) planTabShown() bool {
	return m.gardenOverlayOpen() && m.garden.board.section == planTabSection
}

// planTabPreview is the newest revise preview of the opened Issue.
func (m Model) planTabPreview() *gardenapi.PlanRevisePreviewView {
	t := m.garden.plantab
	if t.preview == nil || t.issue != uint64(m.garden.board.opened) {
		return nil
	}
	return t.preview
}

// planTabLoad reads the revise preview of the opened Issue. It is a read: it starts and writes nothing.
func (m *Model) planTabLoad() tea.Cmd {
	g := &m.garden
	if g.api == nil || !g.hasStatus || g.board.opened == 0 {
		return nil
	}
	t := &g.plantab
	t.seq++
	seq, issue, api := t.seq, uint64(g.board.opened), g.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
		defer cancel()
		v, err := api.PreviewPlanRevise(ctx, issue)
		return gardenPlanRevisePreviewMsg{seq: seq, issue: issue, view: v, err: err}
	}
}

// planTabReload rereads the preview when the Plan tab is in view.
func (m *Model) planTabReload() tea.Cmd {
	if m.garden.board.opened == 0 || m.garden.board.section != planTabSection {
		return nil
	}
	return m.planTabLoad()
}

func (m Model) onGardenPlanRevisePreview(msg gardenPlanRevisePreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	t := &g.plantab
	if msg.seq != t.seq || msg.issue != uint64(g.board.opened) {
		return m, nil // superseded, or for an Issue that is no longer open
	}
	if msg.err != nil {
		t.readErr = true // the last good preview stays
		return m, nil
	}
	v := msg.view
	s := v.Stamp
	if v.IssueID != msg.issue || s.GardenID != g.status.Target.RepoID || s.LocalRepoID != g.status.Stamp.LocalRepoID || !t.gate.Accept(s) {
		return m, nil
	}
	t.issue, t.preview, t.readErr = msg.issue, &v, false
	var names tea.Cmd
	if v.Settings != nil {
		if _, known := t.accountNames[v.Settings.AccountID]; !known {
			names = m.planTabNames()
		}
	}
	if t.startFor != 0 && (v.Pending == nil || v.Pending.DecisionCommentID != t.startFor || v.Pending.Attempt != nil) {
		t.startErr, t.startFor = "", 0
	}
	m.planTabSync()
	return m, names
}

// planTabNames reads the account names a revision's settings are shown with. It is the fast list, no provider call.
func (m Model) planTabNames() tea.Cmd {
	api := m.agents.api.Accounts
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
		defer cancel()
		list, err := api.List(ctx)
		names := make(map[agents.AccountID]string, len(list))
		for _, a := range list {
			names[a.ID] = string(a.Provider) + " / " + a.Name
		}
		return gardenPlanAccountsMsg{names: names, err: err}
	}
}

func (m Model) onGardenPlanAccounts(msg gardenPlanAccountsMsg) Model {
	if msg.err == nil {
		m.garden.plantab.accountNames = msg.names
	}
	return m
}

// planTabRevisionHash is the plan revision under review as the newest data names it: the revise preview when
// one is held, else the board's verified log.
func (m Model) planTabRevisionHash() string {
	if pv := m.planTabPreview(); pv != nil && pv.Revision.Hash != "" {
		return pv.Revision.Hash
	}
	if r, _, ok := m.garden.board.collection.Find(m.garden.board.opened); ok {
		if cur := planrev.FromVerification(r.Item.Issue, r.Item.Verification).Current; cur != nil {
			return cur.Hash
		}
	}
	return ""
}

// planTabSync keeps the form and the typed feedback bound to the revision under review. Feedback written for a
// revision that was replaced is dropped and the user is told, never silently re-pointed at the new one.
func (m *Model) planTabSync() {
	g := &m.garden
	t := &g.plantab
	cur := m.planTabRevisionHash()
	if cur == "" {
		return
	}
	if f := m.planForm(); f != nil {
		switch pv := m.planTabPreview(); {
		case f.hash != cur:
			t.form = nil
			g.notice = fmt.Sprintf("Plan r%d is no longer the revision under review, so its feedback form was closed.", f.ordinal)
			m.planFormLeft()
		case f.retry && pv != nil && (pv.Pending == nil || pv.Pending.DecisionCommentID != f.decisionID || pv.Pending.Attempt != nil && !planTabRetryableState(pv.Pending.Attempt.State)):
			t.form = nil
			g.notice = "That recorded feedback is already answered by a revision attempt."
			m.planFormLeft()
		case pv != nil && !f.retry:
			if pv.Base.Commit != f.base.Commit || pv.Base.Dirty != f.base.Dirty || pv.Base.DirtyDigest != f.base.DirtyDigest {
				f.base, f.includeDirty = pv.Base, false
				g.notice = "The checkout changed since the form opened. Confirm reading uncommitted changes again."
			}
			if f.retained || f.settings == nil {
				f.settings, f.retained, f.unavailable = pv.Settings, pv.Settings != nil, pv.SettingsUnavailable
			}
		}
	}
	if t.draftHash != "" && t.draftHash != cur {
		if strings.TrimSpace(t.draftText) != "" {
			g.notice = fmt.Sprintf("Your unsent feedback was for plan r%d, which is no longer the revision under review. It was dropped.", t.draftOrdinal)
		}
		t.draftHash, t.draftText, t.draftOrdinal, t.draftDirty = "", "", 0, false
	}
}

// --- the form ---

func planFormTextarea() textarea.Model {
	p := theme.Current
	ta := newTextarea()
	ta.CharLimit = planrev.MaxReasonBytes
	for _, st := range []*textarea.Style{&ta.FocusedStyle, &ta.BlurredStyle} {
		st.Base = st.Base.Background(p.Surface)
		st.CursorLine = st.Base
		st.CursorLineNumber = st.Base
		st.EndOfBuffer = st.Base
		st.LineNumber = st.Base
		st.Prompt = st.Base
		st.Text = st.Base
		st.Placeholder = st.Base.Foreground(p.Muted)
	}
	ta.Placeholder = "What should change in this plan?"
	return ta
}

// planFormOpen builds the form for the revision. The text typed earlier for the same revision comes back.
func (m *Model) planFormOpen(f *reviseForm) {
	t := &m.garden.plantab
	if !f.retry {
		f.ta = planFormTextarea()
		if t.draftHash == f.hash {
			f.ta.SetValue(t.draftText)
			f.includeDirty = t.draftDirty && f.base.Dirty
		}
		f.ta.Focus()
		m.garden.board.overlayFocus = ""
		m.shellFocus = shellInput
	} else {
		m.garden.board.overlayFocus = "form-back"
		m.shellFocus = shellBody
	}
	t.form, t.forIssue = f, uint64(m.garden.board.opened)
	m.garden.board.docScroll = 0
	m.planFormSize()
}

// planFormSize fits the text box to the overlay body.
func (m *Model) planFormSize() {
	f := m.planForm()
	if f == nil || f.retry {
		return
	}
	w, rows := m.gardenOverlayBody()
	f.ta.SetWidth(max(w-2, 4))
	f.ta.SetHeight(max(2, min(rows-planFormFixedRows(f), 8)))
}

// planFormFixedRows is the rows of the form around its text box.
func planFormFixedRows(f *reviseForm) int {
	n := 8 // title, subtitle, blank, blank, access, receives, blank, footer
	if f.base.Dirty {
		n++
	}
	return n
}

// planFormLeft is called whenever the form goes away, so the keyboard returns to the overlay.
func (m *Model) planFormLeft() {
	m.garden.board.overlayFocus = ""
	if m.shellFocus == shellInput {
		m.shellFocus = shellBody
	}
}

// planFormClose keeps what was typed for the revision and leaves the form.
func (m *Model) planFormClose() {
	t := &m.garden.plantab
	f := m.planForm()
	t.form = nil
	if f == nil {
		return
	}
	if !f.retry {
		t.draftHash, t.draftOrdinal, t.draftText, t.draftDirty = f.hash, f.ordinal, f.ta.Value(), f.includeDirty
	}
	m.planFormLeft()
}

// formFocusOrder lists the form's focus stops; the empty id is the text box.
func (m Model) formFocusOrder() []string {
	f := m.planForm()
	if f == nil {
		return nil
	}
	var out []string
	if !f.retry {
		out = append(out, "")
	}
	if f.base.Dirty {
		out = append(out, "form-dirty")
	}
	if f.settings == nil {
		out = append(out, "form-account")
	}
	return append(out, "form-back", "form-send")
}

func (m *Model) planFormFocusStep(dir int) {
	b := &m.garden.board
	order := m.formFocusOrder()
	cur := 0
	for i, id := range order {
		if id == b.overlayFocus {
			cur = i
		}
	}
	b.overlayFocus = order[(cur+dir+len(order))%len(order)]
	f := m.planForm()
	if f.retry {
		return
	}
	if b.overlayFocus == "" {
		f.ta.Focus()
		m.shellFocus = shellInput
	} else {
		f.ta.Blur()
		m.shellFocus = shellBody
	}
}

func (m Model) planFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.planForm()
	b := &m.garden.board
	m.garden.notice = ""
	k := msg.String()
	switch k {
	case "esc":
		return m.issueActivate("form-back")
	case "ctrl+s":
		return m.issueActivate("form-send")
	case "ctrl+y":
		if f.base.Dirty {
			return m.issueActivate("form-dirty")
		}
		return m, nil
	case "ctrl+g":
		if f.settings == nil {
			return m.issueActivate("form-account")
		}
		return m, nil
	case "tab":
		m.planFormFocusStep(1)
		return m, nil
	case "shift+tab":
		m.planFormFocusStep(-1)
		return m, nil
	}
	if b.overlayFocus == "" && !f.retry {
		m.planFormSize()
		var cmd tea.Cmd
		f.ta, cmd = f.ta.Update(msg)
		return m, cmd
	}
	switch k {
	case "enter", " ":
		return m.issueActivate(b.overlayFocus)
	case "down", "j":
		m.issueScroll(1)
	case "up", "k":
		m.issueScroll(-1)
	}
	return m, nil
}

// planFormReady checks what a submit needs and says what is missing. It reads only.
func (m Model) planFormReady() (reason string, problem string) {
	f := m.planForm()
	if f == nil {
		return "", "There is no feedback form open."
	}
	if !f.retry {
		reason = strings.TrimSpace(f.ta.Value())
		switch {
		case reason == "":
			return "", "Feedback needs a reason: say what should change in the plan."
		case len(reason) > planrev.MaxReasonBytes:
			return "", fmt.Sprintf("Feedback is limited to %d bytes.", planrev.MaxReasonBytes)
		}
	}
	switch {
	case f.settings == nil:
		return "", "Choose an account first (ctrl+g)."
	case f.base.Dirty && !f.includeDirty:
		return "", "The checkout has uncommitted changes. Allow the planner to read them (ctrl+y), or commit or stash them first."
	}
	return reason, ""
}

// planReviseRequest is the request the form's submit sends. AcceptReadOnly is set here, from the user's explicit
// submit on a form that shows the read-only access, and nowhere else.
func (m Model) planReviseRequest(reason string, settings gardenapi.PlanReviseSettings) gardenapi.PlanReviseRequest {
	f := m.planForm()
	cols, rows := m.plannerAreaSize()
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24 // the terminal resizes to the real area when it is focused
	}
	return gardenapi.PlanReviseRequest{Stamp: f.stamp, IssueID: uint64(m.garden.board.opened), RevisionHash: f.hash, Fingerprint: f.fingerprint,
		SpecHash: f.specHash, ActorID: f.actorID, Reason: reason, DecisionCommentID: f.decisionID, Settings: settings,
		IncludeDirty: f.base.Dirty && f.includeDirty, AcceptReadOnly: true, Base: f.base, Cols: cols, Rows: rows}
}

// planFormSend is the explicit submit. The write is never cancelled and keys are ignored until it answers, so a
// repeated Enter cannot send twice.
func (m Model) planFormSend() (Model, tea.Cmd) {
	g := &m.garden
	f := m.planForm()
	if g.api == nil || g.busy.writes() || f == nil {
		return m, nil
	}
	reason, problem := m.planFormReady()
	if problem != "" {
		g.notice = problem
		return m, nil
	}
	return m.planFormSubmit(m.planReviseRequest(reason, *f.settings), f.retry)
}

func (m Model) planFormSubmit(req gardenapi.PlanReviseRequest, retry bool) (Model, tea.Cmd) {
	g := &m.garden
	_, seq := g.begin(busyPlanRevise)
	g.plantab.sentReason = req.Reason
	api := g.api
	return m, func() tea.Msg {
		var v gardenapi.PlanReviseView
		var err error
		if retry {
			v, err = api.StartPlanRevise(context.Background(), req)
		} else {
			v, err = api.RevisePlan(context.Background(), req)
		}
		return gardenPlanReviseMsg{seq: seq, retry: retry, issue: req.IssueID, view: v, err: err}
	}
}

// planFormAccount opens the planner's account choice for a revision without retained settings. Its submit sends
// the feedback, so the choice shows the read-only contract the user accepts.
func (m Model) planFormAccount() (Model, tea.Cmd) {
	g := &m.garden
	f := m.planForm()
	if g.api == nil || g.busy.writes() || f == nil {
		return m, nil
	}
	if _, problem := m.planFormReady(); problem != "" && !strings.HasPrefix(problem, "Choose an account") {
		g.notice = problem
		return m, nil
	}
	if m.agents.api.Accounts == nil || !m.agents.active() {
		g.notice = "Agent accounts and terminal sessions are unavailable here, so no planner can run."
		return m, nil
	}
	m.agents.launchSeq++
	d := newLaunchDialog(m.agents.launchSeq, m.agents.api.Accounts, launchTarget{}, "", m.agents.usage, m.agentGlyphsASCII())
	d.planner, d.revise = true, true
	d.issue = fmt.Sprintf("Revise plan r%d", f.ordinal)
	d.SetSize(m.width, m.gardenBoardRows())
	g.planner = gardenPlannerState{stage: plannerChoose, dlg: &d, revise: true}
	m.shellFocus = shellInput
	return m, d.Init()
}

// onReviseSubmit is the account choice's submit when it was opened from the form: it sends the feedback with the
// chosen account, or goes back to the form saying what is missing. The choice itself writes nothing.
func (m Model) onReviseSubmit(msg launchSubmitMsg) (Model, tea.Cmd) {
	g := &m.garden
	if g.planner.stage != plannerChoose || g.planner.dlg == nil || g.planner.dlg.id != msg.dlg || g.busy != busyNone {
		return m, nil
	}
	m = m.planReviseChoiceClose()
	f := m.planForm()
	if f == nil {
		return m, nil
	}
	settings := gardenapi.PlanReviseSettings{AccountID: msg.account, Model: msg.req.Launch.Model, Effort: msg.req.Launch.Effort}
	f.settings, f.retained = &settings, false
	reason, problem := m.planFormReady()
	if problem != "" {
		g.notice = problem
		return m, nil
	}
	return m.planFormSubmit(m.planReviseRequest(reason, settings), f.retry)
}

// planReviseChoiceClose leaves the account choice for the overlay, form and typed text intact.
func (m Model) planReviseChoiceClose() Model {
	m.garden.planner = gardenPlannerState{}
	if f := m.planForm(); f != nil && !f.retry {
		m.garden.board.overlayFocus = ""
		f.ta.Focus()
		m.shellFocus = shellInput
	} else {
		m.shellFocus = shellBody
	}
	return m
}

// --- the write's answer ---

func (m Model) onGardenPlanRevise(msg gardenPlanReviseMsg) (Model, tea.Cmd) {
	g := &m.garden
	t := &g.plantab
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		// Refused or not confirmed. A refusal before the write (spec or checkout moved, consent, account) leaves
		// nothing behind, but a transport error may follow a recorded decision, so everything is read again. The
		// form and its text stay so the next try names what is really there; repeating finds any earlier record.
		cmd := m.planTabAfterWrite(true)
		g.notice = strings.TrimSpace(fail.Message+" "+fail.Next) + " Not confirmed as recorded; the plan state was read again."
		return m, cmd
	}
	v := msg.view
	if !g.gate.Accept(v.Stamp) || v.IssueID != uint64(g.board.opened) || msg.issue != uint64(g.board.opened) {
		// The answer is for another time or Issue: it may still have written, so read instead of guessing.
		cmd := m.planTabAfterWrite(true)
		g.notice = "Feedback may have been recorded; the plan state was read again."
		return m, cmd
	}
	switch {
	case v.Decision.State == gardenapi.PlanWriteUnknown:
		// Starting from an unknown decision would guess: keep the text and let the next read reconcile.
		m.planFormClose()
		cmd := m.planTabAfterWrite(true)
		g.notice = "Feedback may be recorded on the Issue; no attempt was started. Reopen to reconcile."
		if v.Decision.Err != nil {
			g.notice = strings.TrimSpace(v.Decision.Err.Message + " " + v.Decision.Err.Next)
		}
		return m, cmd
	case v.Attempt != nil:
		t.draftHash, t.draftText, t.draftOrdinal, t.draftDirty = "", "", 0, false
		t.form = nil
		m.planFormLeft()
		t.startErr, t.startFor = "", 0
		t.patchPending(m, v.Decision.CommentID, v.Attempt)
		cmd := m.planTabAfterWrite(true)
		g.notice = fmt.Sprintf("Feedback recorded on r%d. A read-only revision attempt is running.", v.Decision.Ordinal)
		if v.Existing {
			g.notice = fmt.Sprintf("That feedback on r%d was already recorded; showing the revision attempt it started.", v.Decision.Ordinal)
		}
		return m, cmd
	}
	// The decision is on the Issue and no attempt started: pending, with the failure and an explicit retry.
	t.draftHash, t.draftText, t.draftOrdinal, t.draftDirty = "", "", 0, false
	t.form = nil
	m.planFormLeft()
	t.startFor = v.Decision.CommentID
	t.startErr = "No revision attempt was started."
	if v.StartErr != nil {
		t.startErr = strings.TrimSpace(v.StartErr.Message + " " + v.StartErr.Next)
	}
	t.patchPending(m, v.Decision.CommentID, nil)
	cmd := m.planTabAfterWrite(true)
	g.notice = fmt.Sprintf("Feedback recorded on r%d, but no revision attempt started. Start it again when ready.", v.Decision.Ordinal)
	return m, cmd
}

// patchPending shows the recorded decision and its attempt before the next read confirms them.
func (t *gardenPlanTabState) patchPending(m Model, decision int64, a *plan.Attempt) {
	pv := m.planTabPreview()
	if pv == nil {
		return
	}
	next := *pv
	next.Pending = &gardenapi.PendingRevise{DecisionCommentID: decision, Reason: t.sentReason, Attempt: a}
	if a != nil && a.State.Live() {
		next.Unavailable = "A planner attempt is running for this Issue."
	}
	t.preview = &next
}

// planTabAfterWrite rereads everything a feedback write can change. refresh is false when nothing was written.
func (m *Model) planTabAfterWrite(refresh bool) tea.Cmd {
	cmds := []tea.Cmd{m.planTabLoad()}
	if refresh {
		var work, local tea.Cmd
		*m, work = m.gardenLoadWork()
		*m, local = m.gardenLoadLocal()
		cmds = append(cmds, work, local)
	}
	return tea.Batch(cmds...)
}

// --- the fresh review read behind Approve and Request changes ---

// planTabReview reads the plan review again before a decision, so the confirmation names what is really there.
func (m Model) planTabReview(intent string) (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	ctx, seq := g.begin(busyPlanReview)
	g.plantab.intent, g.plantab.intentHash = intent, m.planTabRevisionHash()
	issue, api := uint64(g.board.opened), g.api
	return m, func() tea.Msg {
		v, err := api.PlanReview(ctx, issue)
		return gardenPlanTabReviewMsg{seq: seq, intent: intent, view: v, err: err}
	}
}

func (m Model) onGardenPlanTabReview(msg gardenPlanTabReviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	t := &g.plantab
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	want := t.intentHash
	t.intent, t.intentHash = "", ""
	if fail != nil {
		g.notice = strings.TrimSpace(fail.Message + " " + fail.Next)
		return m, nil
	}
	v := msg.view
	if !g.gate.Accept(v.Stamp) || v.IssueID != uint64(g.board.opened) {
		g.notice = staleReply.Message
		return m, nil
	}
	cur := v.Review.Current
	if cur == nil || want != "" && cur.Hash != want {
		// The plan under review moved since the tab was drawn: nothing is decided on a revision nobody looked at.
		cmd := m.planTabAfterWrite(true)
		g.notice = "The plan under review changed. Read it again before deciding."
		return m, cmd
	}
	switch msg.intent {
	case "approve":
		m.garden.planrev = gardenPlanState{drafts: g.planrev.drafts, view: &v, fromOverlay: true}
		m, _ = m.gardenPlanStartDecide(garden.DecisionApproved)
		if m.garden.planrev.stage != planDecide {
			note := m.garden.notice
			m = m.planClose()
			m.garden.notice = note
		}
		return m, nil
	case "changes":
		return m.planTabOpenForm(v)
	}
	return m, nil
}

// planTabOpenForm opens the request-changes form from a fresh review read and the revise preview.
func (m Model) planTabOpenForm(v gardenapi.PlanReviewView) (Model, tea.Cmd) {
	g := &m.garden
	pv := m.planTabPreview()
	cur := v.Review.Current
	switch {
	case g.status.Access < gardenapi.AccessWrite:
		g.notice = "Requesting changes needs Write access to " + g.status.Target.FullName + "."
		return m, nil
	case !v.Review.CanDecide(garden.DecisionChangesRequested) || v.Review.Gate.Head == nil:
		g.notice = decideRefusal(v.Review, garden.DecisionChangesRequested)
		return m, nil
	case pv == nil || pv.Revision.Hash != cur.Hash:
		g.notice = "The plan details are still loading. Try again in a moment."
		return m, m.planTabLoad()
	case pv.Unavailable != "":
		g.notice = pv.Unavailable
		return m, nil
	}
	f := &reviseForm{hash: cur.Hash, ordinal: cur.Ordinal, actorID: v.ActorID, specHash: v.Review.Gate.Head.Hash,
		fingerprint: cur.SourceSpecFingerprint, stamp: v.Stamp, base: pv.Base, settings: pv.Settings, retained: pv.Settings != nil,
		unavailable: pv.SettingsUnavailable}
	m.planFormOpen(f)
	return m, nil
}

// planTabRetry opens the confirmation of Start revision attempt for the recorded decision. It writes nothing.
func (m Model) planTabRetry() (Model, tea.Cmd) {
	g := &m.garden
	pv := m.planTabPreview()
	switch {
	case g.api == nil || g.busy.writes() || pv == nil || pv.Pending == nil:
		return m, nil
	case g.status.Access < gardenapi.AccessWrite:
		g.notice = "Starting a revision attempt needs Write access to " + g.status.Target.FullName + "."
		return m, nil
	case pv.Unavailable != "":
		g.notice = pv.Unavailable
		return m, nil
	}
	rev := pv.Revision
	f := &reviseForm{hash: rev.Hash, ordinal: rev.Ordinal, retry: true, decisionID: pv.Pending.DecisionCommentID, reason: pv.Pending.Reason,
		specHash: rev.SourceSpecHash, fingerprint: rev.SourceSpecFingerprint, stamp: pv.Stamp, base: pv.Base, settings: pv.Settings,
		retained: pv.Settings != nil, unavailable: pv.SettingsUnavailable}
	m.planFormOpen(f)
	return m, nil
}

// --- stop, open, publish ---

// planTabStop asks to confirm stopping the live attempt; the confirmation is drawn in the body.
func (m Model) planTabStop() (Model, tea.Cmd) {
	if run, ok := m.planTabRun(m.garden.board.opened); ok && run.Live && run.AttemptID != "" && !m.garden.busy.writes() {
		m.garden.plantab.confirmStop, m.garden.plantab.forIssue = true, uint64(m.garden.board.opened)
		m.garden.board.docScroll = 0
	}
	return m, nil
}

// planTabKeyStop answers the stop confirmation: y stops, anything else keeps the attempt running.
func (m Model) planTabKeyStop(k string) (Model, tea.Cmd) {
	m.garden.plantab.confirmStop = false
	m.garden.board.overlayFocus = ""
	if k != "y" {
		return m, nil
	}
	if run, ok := m.planTabRun(m.garden.board.opened); ok && run.Live && run.AttemptID != "" {
		return m.gardenPlannerCancel(run.AttemptID)
	}
	return m, nil
}

// planTabOpenPlanner opens the planner screen for the Issue, straight to the live session when there is one.
// Esc from it returns to the overlay.
func (m Model) planTabOpenPlanner() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	g.planner.direct = true
	g.planner.openAttempt = ""
	if run, ok := m.planTabRun(g.board.opened); ok && run.AttemptID != "" {
		g.planner.openAttempt = run.AttemptID
	}
	return m.gardenLoadPlanner()
}

// planTabPublish opens the publish text box for the attempt whose plan file is ready.
func (m Model) planTabPublish() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	g.planner.direct, g.planner.publish = true, true
	return m.gardenLoadPlanner()
}

// planTabRun is the planner run the tab describes: the live one, else the newest ended one with a ready plan.
func (m Model) planTabRun(id garden.IssueID) (gardenapi.LocalRunSummary, bool) {
	s, ok := m.localFor(id)
	if pv := m.planTabPreview(); pv != nil && pv.Pending != nil && pv.Pending.Attempt != nil && (!ok || s.AttemptID != pv.Pending.Attempt.ID) {
		// The attempt the newest revise read knows shows before the next local read does.
		if a := pv.Pending.Attempt; a.State.Live() {
			return summaryOfAttempt(*a), true
		}
	}
	return s, ok
}

func summaryOfAttempt(a plan.Attempt) gardenapi.LocalRunSummary {
	return gardenapi.LocalRunSummary{AttemptID: a.ID, State: string(a.State), Live: a.State.Live(), Updated: a.UpdatedAt, Provider: string(a.Provider),
		AccountName: a.AccountName, Model: a.Model, Effort: a.Effort, RequestedAt: a.RequestedAt, Revises: a.Revises}
}

// onOverlayPlanDecision settles the approval confirmed inside the overlay. Success and refusal both leave the
// plan screens for the overlay and read the Issue again; nothing is retried.
func (m Model) onOverlayPlanDecision(msg gardenPlanDecisionMsg, fail *gardenapi.Error) (Model, tea.Cmd) {
	g := &m.garden
	opened := uint64(g.board.opened)
	m = m.planClose()
	var note string
	switch {
	case fail != nil:
		// Nothing was recorded: say why and read the state, so the next decision names what is really there.
		note = strings.TrimSpace(fail.Message + " " + fail.Next)
	case !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != opened:
		note = "The approval may have been recorded; the plan state was read again."
	case msg.view.State == gardenapi.PlanWriteUnknown && msg.view.Err != nil:
		note = strings.TrimSpace(msg.view.Err.Message + " " + msg.view.Err.Next)
	case msg.view.Replayed:
		note = fmt.Sprintf("That decision on plan r%d was already recorded; nothing was written again.", msg.view.Ordinal)
	default:
		note = fmt.Sprintf("Recorded on plan r%d: %s. No run was started.", msg.view.Ordinal, decisionWords(msg.view.Decision))
	}
	cmd := m.planTabAfterWrite(true)
	m.garden.notice = note
	return m, cmd
}

// planFormHints are the footer keys of the request-changes form and of the retry confirmation.
func (m Model) planFormHints() []shellHint {
	f := m.planForm()
	if f != nil && f.retry {
		return []shellHint{{"tab", "choose"}, {"enter", "activate"}, {"ctrl+s", "start revision attempt"}, {"esc", "back"}}
	}
	h := []shellHint{{"ctrl+s", "send feedback & revise plan"}, {"tab", "next field"}}
	if f != nil && f.base.Dirty {
		h = append(h, shellHint{"ctrl+y", "read uncommitted changes"})
	}
	if f != nil && f.settings == nil {
		h = append(h, shellHint{"ctrl+g", "choose account"})
	}
	return append(h, shellHint{"esc", "back (keeps your text)"})
}
