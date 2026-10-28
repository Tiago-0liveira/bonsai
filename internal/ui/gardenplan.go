package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Plan review (plan 08.3). A plan written in a read-only planner session is published as a content-addressed
// revision on the Issue by an explicit user action keyed to the attempt, and reviewed here. Nothing on this
// screen starts a planner: a decision names one exact revision, "request changes" only records
// a reason, and the next read-only run still needs its own local Request AI plan from the planner screen.

type planStage int

const (
	planNone     planStage = iota
	planReview             // the revisions, the document or comparison, search and decisions
	planCompose            // the plan text the user chose to publish
	planConfirm            // the exact revision publishing would append
	planDecide             // exact approval confirmation or decision reason
	planValidate           // gate-one revalidation, using the existing flow
)

// planMode is what the review body shows.
type planMode int

const (
	planDoc     planMode = iota // the revision as a document
	planUnified                 // comparison with the previous revision, one column (the default comparison)
	planWide                    // the same comparison, side by side, when the width allows
)

// planWideMin is the narrowest body that lays two columns out readably.
const planWideMin = 100

type gardenPlanState struct {
	stage planStage
	view  *gardenapi.PlanReviewView
	// shown is the hash of the revision in view; empty follows the one under review, then the latest.
	shown  string
	mode   planMode
	scroll int

	searching bool
	si        textinput.Model
	query     string
	hit       int

	// drafts keep the text typed for an attempt in memory only, until it is published or the Garden changes.
	drafts  map[string]string
	attempt string
	ta      textarea.Model
	// prefilled is the attempt whose plan file filled the text box; prefillSeq numbers the plan read in flight.
	prefilled  string
	prefillSeq uint64
	preview    *gardenapi.PlanPublishPreviewView
	btn        int // confirm focus: 0 Back to edit, 1 Publish

	decision garden.Decision
	target   *gardenapi.PlanDecisionRequest
	reason   textinput.Model

	// fromOverlay marks the approval confirmation the Plan tab draws inside the overlay: it is the same exact
	// confirmation, and leaving it returns to the overlay instead of the plan review screen.
	fromOverlay bool
}

type (
	gardenPlanReviewMsg struct {
		seq   uint64
		view  gardenapi.PlanReviewView
		note  string
		err   error
		reset bool // a fresh open: show the revision under review from the top
	}
	gardenPlanPreviewMsg struct {
		seq  uint64
		view gardenapi.PlanPublishPreviewView
		err  error
	}
	gardenPlanPublishMsg struct {
		seq  uint64
		view gardenapi.PlanPublishView
		err  error
	}
	gardenPlanDecisionMsg struct {
		seq  uint64
		view gardenapi.PlanDecisionView
		err  error
	}
)

// planActive reports whether the opened Issue shows the plan review screens.
func (m Model) planActive() bool {
	return m.garden.planrev.stage != planNone && m.garden.board.opened != 0
}

// planClose leaves the plan screens. Typed drafts survive; everything read from GitHub is dropped.
func (m Model) planClose() Model {
	m.garden.planrev = gardenPlanState{drafts: m.garden.planrev.drafts}
	if m.shellFocus == shellInput {
		m.shellFocus = shellBody
	}
	return m
}

// current returns the revision in view: the pinned one, else the one under review, else the latest.
func (p gardenPlanState) current() (planrev.Revision, bool) {
	if p.view == nil {
		return planrev.Revision{}, false
	}
	rv := p.view.Review
	if p.shown != "" {
		if r, ok := rv.ByHash(p.shown); ok {
			return r, true
		}
	}
	switch {
	case rv.Current != nil:
		return *rv.Current, true
	case rv.Latest != nil:
		return *rv.Latest, true
	}
	return planrev.Revision{}, false
}

// previous is the revision the shown one is compared with: the one just before it.
func (p gardenPlanState) previous(r planrev.Revision) (planrev.Revision, bool) {
	if p.view == nil || r.Ordinal < 2 {
		return planrev.Revision{}, false
	}
	return p.view.Review.Revisions[r.Ordinal-2], true
}

// --- reads and writes ---

func (m Model) gardenOpenPlanReview() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() || g.board.opened == 0 {
		return m, nil
	}
	g.planrev = gardenPlanState{stage: planReview, drafts: g.planrev.drafts}
	return m.gardenLoadPlanReview("", true)
}

func (m Model) gardenLoadPlanReview(note string, reset bool) (Model, tea.Cmd) {
	g := &m.garden
	issue := uint64(g.board.opened)
	ctx, seq := g.begin(busyPlanReview)
	api := g.api
	return m, func() tea.Msg {
		v, err := api.PlanReview(ctx, issue)
		return gardenPlanReviewMsg{seq: seq, view: v, note: note, err: err, reset: reset}
	}
}

func (m Model) onGardenPlanReview(msg gardenPlanReviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		g.err = fail
		return m, nil
	}
	// A late or older reply must never replace a newer review: the stamp's generation decides.
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != uint64(g.board.opened) {
		g.err = staleReply
		return m, nil
	}
	p := &g.planrev
	v := msg.view
	invalidated := p.stage == planDecide && !planDecisionMatches(p.target, v)
	if invalidated {
		p.stage, p.target = planReview, nil
		m.shellFocus = shellBody
	}
	p.view = &v
	if p.stage == planNone {
		p.stage = planReview
	}
	if _, ok := v.Review.ByHash(p.shown); !ok || msg.reset {
		p.shown = ""
	}
	if msg.reset {
		p.scroll, p.mode = 0, planDoc
	}
	g.notice = msg.note
	if invalidated {
		g.notice = "The plan, specification or actor changed. Review again before deciding."
	}
	m.planClamp()
	return m, nil
}

// gardenPlanOpenCompose is u on the planner screen: it opens the text of the plan the user chooses to publish
// from the selected attempt. Bonsai reads nothing from the attempt's terminal: the user types or pastes it.
func (m Model) gardenPlanOpenCompose() (Model, tea.Cmd) {
	g := &m.garden
	pl := &g.planner
	a, ok := pl.selected()
	switch {
	case g.api == nil || g.busy.writes() || !ok || pl.preview == nil:
		return m, nil
	case g.status.Access < gardenapi.AccessWrite:
		g.notice = "Publishing a plan needs Write access to " + g.status.Target.FullName + "."
		return m, nil
	case !pl.preview.Gate.DownstreamAvailable():
		g.notice = "The specification is not approved, so no plan can be published for it."
		return m, nil
	case a.Spec.Fingerprint != pl.preview.Spec.Fingerprint:
		g.notice = "That attempt answered an older specification. Request a new plan for the current one."
		return m, nil
	}
	p := &g.planrev
	p.stage, p.attempt, p.preview, p.prefilled = planCompose, a.ID, nil, ""
	p.prefillSeq++ // a plan read still in flight belongs to a box that was closed
	p.ta = newTextarea()
	p.ta.Placeholder = "Type or paste the plan to publish (a ready plan file fills this box for you). Required sections: " + strings.Join(planrev.Required, ", ") + "."
	p.ta.SetValue(p.drafts[a.ID])
	p.ta.Focus()
	m.gardenSizePlanEditor()
	m.shellFocus = shellInput
	return m, nil
}

// gardenPlannerPublish is u on the planner screen. It opens the publish text box at once; when the selected attempt's
// plan file is ready and nothing was typed for it yet, the plan text is read in the background and fills the box.
// The user still reviews, edits and previews it, and nothing is published without the existing confirmation.
func (m Model) gardenPlannerPublish() (Model, tea.Cmd) {
	g := &m.garden
	pl := &g.planner
	a, ok := pl.selected()
	m, _ = m.gardenPlanOpenCompose()
	p := &m.garden.planrev
	if !ok || p.stage != planCompose || p.attempt != a.ID || p.ta.Value() != "" {
		return m, nil
	}
	if pl.attemptOverlay(a.ID).PlanFile.State != plan.PlanFileReady {
		return m, nil
	}
	p.prefillSeq++
	seq, id, api := p.prefillSeq, a.ID, g.api
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
		defer cancel()
		v, err := api.PlannerPlan(ctx, id)
		return gardenPlannerPlanMsg{seq: seq, attempt: id, view: v, err: err}
	}
}

func (m Model) onGardenPlannerPlan(msg gardenPlannerPlanMsg) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	if p.stage != planCompose || p.attempt != msg.attempt || p.prefillSeq != msg.seq {
		return m, nil // the box was closed or reopened: this answer is for an older one
	}
	if msg.err != nil {
		// The empty box stays as it was; say why it was not filled.
		f := safeGardenError(msg.err)
		g.notice = strings.TrimSpace(f.Message + " " + f.Next)
		return m, nil
	}
	if msg.view.AttemptID != msg.attempt || msg.view.Stamp.GardenID != g.status.Target.RepoID {
		g.notice = "Ignored an out-of-date plan read; the box stays empty."
		return m, nil
	}
	if p.ta.Value() != "" {
		return m, nil // the user typed meanwhile: never overwrite their text
	}
	p.ta.SetValue(cleanPlanText(msg.view.Text))
	p.prefilled = msg.attempt
	g.notice = ""
	return m, nil
}

func (m *Model) gardenSizePlanEditor() {
	p := &m.garden.planrev
	if p.stage != planCompose {
		return
	}
	p.ta.SetWidth(max(10, m.width-3))
	p.ta.SetHeight(max(3, m.gardenBoardRows()-5))
}

func (m Model) gardenPlanPreview() (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	p.drafts = withDraft(p.drafts, p.attempt, p.ta.Value())
	ctx, seq := g.begin(busyPlanPreview)
	req := gardenapi.PlanPublishRequest{IssueID: uint64(g.board.opened), AttemptID: p.attempt, Text: p.ta.Value()}
	api := g.api
	return m, func() tea.Msg {
		v, err := api.PreviewPlanPublish(ctx, req)
		return gardenPlanPreviewMsg{seq: seq, view: v, err: err}
	}
}

func withDraft(d map[string]string, id, text string) map[string]string {
	if d == nil {
		d = map[string]string{}
	}
	d[id] = text
	return d
}

func (m Model) onGardenPlanPreview(msg gardenPlanPreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		g.err = fail
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != uint64(g.board.opened) {
		g.err = staleReply
		return m, nil
	}
	v := msg.view
	p := &g.planrev
	p.preview, p.stage, p.btn, p.scroll = &v, planConfirm, 0, 0
	if v.CanPublish() {
		p.btn = 1
	}
	m.shellFocus = shellBody
	return m, nil
}

// gardenRunPlanPublish appends the previewed revision. Like every write it is never cancelled.
func (m Model) gardenRunPlanPublish(v gardenapi.PlanPublishPreviewView) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyPlanPublish)
	api := m.garden.api
	return m, func() tea.Msg {
		r, err := api.PublishPlan(context.Background(), v)
		return gardenPlanPublishMsg{seq: seq, view: r, err: err}
	}
}

func (m Model) onGardenPlanPublish(msg gardenPlanPublishMsg) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		switch fail.Code {
		case gardenapi.CodeConflict, gardenapi.CodeSpecChanged, gardenapi.CodeNotApproved, gardenapi.CodeNoChange:
			// What the preview showed is no longer true: nothing was written. Read the current state and decide again.
			p.stage, p.preview = planReview, nil
			m.shellFocus = shellBody
			return m.gardenLoadPlanReview(fail.Message+" "+fail.Next, false)
		}
		g.err = fail
		return m, nil
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.notice = "A plan revision may have been published; refresh to see it."
		p.stage, p.preview = planReview, nil
		m.shellFocus = shellBody
		return m, nil
	}
	if msg.view.State == gardenapi.PlanWriteUnknown && msg.view.Err != nil {
		p.stage, p.preview = planReview, nil
		m.shellFocus = shellBody
		return m.gardenLoadPlanReview(msg.view.Err.Message+" "+msg.view.Err.Next, true)
	}
	delete(p.drafts, p.attempt)
	note := fmt.Sprintf("Published plan r%d (revision %s). It starts nothing; review it and decide.", msg.view.Ordinal, shortHash(msg.view.Hash))
	if msg.view.Replayed {
		note = fmt.Sprintf("Plan r%d was already published (revision %s); nothing was written again.", msg.view.Ordinal, shortHash(msg.view.Hash))
	}
	p.stage, p.preview, p.shown = planReview, nil, msg.view.Hash
	m.shellFocus = shellBody
	// Published plans no longer count as ready to publish: read the badges again.
	m, local := m.gardenLoadLocal()
	m, review := m.gardenLoadPlanReview(note, true)
	return m, tea.Batch(local, review)
}

// gardenPlanStartDecide opens the reason prompt for deciding the revision in view.
func (m Model) gardenPlanStartDecide(d garden.Decision) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	r, ok := p.current()
	switch {
	case !ok || p.view == nil || g.api == nil:
		return m, nil
	case g.status.Access < gardenapi.AccessWrite:
		g.notice = "Deciding on a plan needs Write access to " + g.status.Target.FullName + "."
		return m, nil
	case p.view.Review.Current == nil || p.view.Review.Current.Hash != r.Hash:
		g.notice = "Only the revision under review can be decided. Select it with [ and ], or publish a new revision."
		return m, nil
	case !p.view.Review.CanDecide(d):
		g.notice = decideRefusal(p.view.Review, d)
		return m, nil
	}
	p.stage, p.decision, p.btn = planDecide, d, 0
	p.target = &gardenapi.PlanDecisionRequest{IssueID: uint64(g.board.opened), RevisionHash: r.Hash, Fingerprint: r.SourceSpecFingerprint, Decision: d, Stamp: p.view.Stamp, ActorID: p.view.ActorID, SpecHash: p.view.Review.Gate.Head.Hash}
	p.reason = textinput.New()
	p.reason.Cursor.SetMode(cursor.CursorStatic)
	p.reason.CharLimit = 1000
	p.reason.Placeholder = "Why (shown to everyone who reads the Issue)"
	p.reason.Focus()
	m.shellFocus = shellInput
	if d == garden.DecisionApproved {
		m.shellFocus = shellBody
	}
	return m, nil
}

// planDecisionMatches keeps an open prompt bound to what the reviewer saw.
func planDecisionMatches(req *gardenapi.PlanDecisionRequest, v gardenapi.PlanReviewView) bool {
	return req != nil && v.Review.Current != nil && v.Review.Gate.Head != nil &&
		req.IssueID == v.IssueID && req.RevisionHash == v.Review.Current.Hash &&
		req.SpecHash == v.Review.Gate.Head.Hash && req.Fingerprint == v.Review.Gate.Fingerprint &&
		req.ActorID == v.ActorID && req.Stamp.LocalRepoID == v.Stamp.LocalRepoID &&
		req.Stamp.GardenID == v.Stamp.GardenID && req.Stamp.Epoch == v.Stamp.Epoch && v.Review.CanDecide(req.Decision)
}

// decideRefusal says why the revision under review cannot be decided now.
func decideRefusal(rv planrev.Review, d garden.Decision) string {
	switch {
	case rv.Unavailable != "":
		return rv.Unavailable
	case rv.Current == nil:
		return "There is no revision under review."
	case !rv.Current.Reviewable():
		return "This revision does not verify as a complete plan of yours, so it cannot be decided."
	case rv.Current.State == planrev.StateChangesRequested && d == garden.DecisionChangesRequested:
		return "Changes were already requested on this revision. Publish a new revision, or reject it."
	}
	return "That decision is not available for this revision."
}

func (m Model) gardenRunPlanDecision() (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	if p.stage != planDecide || p.target == nil || p.view == nil || !planDecisionMatches(p.target, *p.view) {
		return m, nil
	}
	reason := strings.TrimSpace(p.reason.Value())
	if p.decision == garden.DecisionChangesRequested && reason == "" {
		g.notice = "Requesting changes needs a reason."
		return m, nil
	}
	req := *p.target
	req.Reason = reason
	_, seq := g.begin(busyPlanDecide)
	api := g.api
	return m, func() tea.Msg {
		v, err := api.DecidePlan(context.Background(), req)
		return gardenPlanDecisionMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenPlanDecision(msg gardenPlanDecisionMsg) (Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if p.fromOverlay {
		return m.onOverlayPlanDecision(msg, fail)
	}
	p.stage = planReview
	m.shellFocus = shellBody
	if fail != nil {
		// Nothing was recorded: read the state again so the next decision names what is really there.
		m2, cmd := m.gardenLoadPlanReview(fail.Message+" "+fail.Next, false)
		return m2, cmd
	}
	if !g.gate.Accept(msg.view.Stamp) || msg.view.IssueID != uint64(g.board.opened) {
		g.err = staleReply
		return m, nil
	}
	note := fmt.Sprintf("Recorded on plan r%d: %s. No run was started.", msg.view.Ordinal, decisionWords(msg.view.Decision))
	if msg.view.Replayed {
		note = fmt.Sprintf("That decision on plan r%d was already recorded; nothing was written again.", msg.view.Ordinal)
	}
	if msg.view.State == gardenapi.PlanWriteUnknown && msg.view.Err != nil {
		note = msg.view.Err.Message + " " + msg.view.Err.Next
	}
	return m.gardenLoadPlanReview(note, false)
}

func decisionWords(d garden.Decision) string {
	switch d {
	case garden.DecisionChangesRequested:
		return "changes requested"
	case garden.DecisionRejected:
		return "rejected, no plan accepted"
	case garden.DecisionApproved:
		return "approved"
	}
	return string(d)
}

// --- keys ---

func (m Model) gardenPlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.garden.notice = "" // a notice explains the last action; the next key moves on
	switch m.garden.planrev.stage {
	case planValidate:
		return m.gardenValidateKey(msg)
	case planCompose:
		return m.gardenPlanComposeKey(msg)
	case planConfirm:
		return m.gardenPlanConfirmKey(msg)
	case planDecide:
		return m.gardenPlanDecideKey(msg)
	}
	return m.gardenPlanReviewKey(msg)
}

func (m Model) gardenPlanComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.garden.planrev
	switch msg.String() {
	case "esc":
		p.drafts = withDraft(p.drafts, p.attempt, p.ta.Value())
		p.stage = planNone
		m.shellFocus = shellBody
		if m.garden.planner.direct {
			return m.gardenPlannerClose() // opened from the overlay's Plan tab: back to the overlay
		}
		return m, nil
	case "ctrl+s":
		if strings.TrimSpace(p.ta.Value()) == "" {
			m.garden.notice = "Paste or type the plan first."
			return m, nil
		}
		return m.gardenPlanPreview()
	}
	var cmd tea.Cmd
	p.ta, cmd = p.ta.Update(msg)
	return m, cmd
}

func (m Model) gardenPlanConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.garden.planrev
	switch msg.String() {
	case "esc", "backspace":
		p.stage, p.preview = planCompose, nil
		p.ta.Focus()
		m.shellFocus = shellInput
	case "left", "h", "shift+tab":
		p.btn = 0
	case "right", "l", "tab":
		if p.preview != nil && p.preview.CanPublish() {
			p.btn = 1
		}
	case "up", "k":
		p.scroll = max(p.scroll-1, 0)
	case "down", "j":
		p.scroll++
	case "enter":
		if p.btn == 0 || p.preview == nil || !p.preview.CanPublish() {
			p.stage, p.preview = planCompose, nil
			p.ta.Focus()
			m.shellFocus = shellInput
			return m, nil
		}
		return m.gardenRunPlanPublish(*p.preview)
	}
	return m, nil
}

func (m Model) gardenPlanDecideKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.garden.planrev
	if p.decision == garden.DecisionApproved {
		leave := func() Model {
			if p.fromOverlay {
				return m.planClose() // the confirmation was the overlay's: back to it, nothing recorded
			}
			p.stage = planReview
			return m
		}
		switch msg.String() {
		case "esc":
			m = leave()
		case "tab", "shift+tab", "left", "right", "h", "l":
			p.btn = 1 - p.btn
		case "enter":
			if p.btn == 1 {
				return m.gardenRunPlanDecision()
			}
			m = leave()
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		p.stage = planReview
		m.shellFocus = shellBody
		return m, nil
	case "enter":
		return m.gardenRunPlanDecision()
	}
	var cmd tea.Cmd
	p.reason, cmd = p.reason.Update(msg)
	return m, cmd
}

func (m Model) gardenPlanReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	p := &g.planrev
	k := msg.String()
	if p.searching {
		switch k {
		case "esc":
			p.searching, p.query, p.hit = false, "", 0
			m.shellFocus = shellBody
		case "enter":
			p.searching = false
			m.shellFocus = shellBody
			m.planGoto(0)
		default:
			var cmd tea.Cmd
			p.si, cmd = p.si.Update(msg)
			p.query, p.hit = p.si.Value(), 0
			m.planGoto(0)
			return m, cmd
		}
		return m, nil
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.gardenLoadPlanReview("", false)
	}
	r, have := p.current()
	switch k {
	case "esc", "backspace":
		if p.query != "" {
			p.query, p.hit = "", 0
			return m, nil
		}
		m = m.planClose()
		if m.garden.planner.direct {
			// The publish box came from the overlay's Plan tab: leave the planner screen too.
			var rel tea.Cmd
			m, rel = m.gardenPlannerClose()
			nm, cmd := m.gardenLoadWork()
			return nm, tea.Batch(rel, cmd)
		}
		return m.gardenLoadWork()
	case "v":
		m.garden.planrev.stage = planValidate
		return m.gardenStartValidate()
	case "o", "y":
		api, id, open := g.api, uint64(g.board.opened), k == "o"
		return m, func() tea.Msg {
			return opDoneMsg{label: "Issue link", err: api.IssueLink(context.Background(), id, open)}
		}
	case "p":
		// The planner screen is where a run is requested, with its own local confirmation.
		m = m.planClose()
		return m.gardenOpenPlanner()
	case "s":
		// A separate local action after Ready: approving started nothing, and this only opens the form.
		switch {
		case !m.launchSupported():
			g.notice = launchUnavailableText
		case p.view == nil || !p.view.Review.Ready:
			g.notice = "A local start needs the exact approved pair: approve a plan first."
		default:
			m = m.planClose()
			return m.gardenLaunchOpen()
		}
		return m, nil
	case "/":
		p.si = textinput.New()
		p.si.Cursor.SetMode(cursor.CursorStatic)
		p.si.CharLimit = 256
		p.si.SetValue(p.query)
		p.si.Focus()
		p.searching = true
		m.shellFocus = shellInput
		return m, nil
	case "n":
		m.planGoto(1)
	case "N":
		m.planGoto(-1)
	case "[", "]":
		if p.view == nil || len(p.view.Review.Revisions) == 0 || !have {
			return m, nil
		}
		i := r.Ordinal - 1
		if k == "[" {
			i--
		} else {
			i++
		}
		i = max(0, min(i, len(p.view.Review.Revisions)-1))
		p.shown, p.scroll = p.view.Review.Revisions[i].Hash, 0
		if _, ok := p.previous(p.view.Review.Revisions[i]); !ok {
			p.mode = planDoc
		}
	case "d":
		switch {
		case !have:
		case p.mode != planDoc:
			p.mode, p.scroll = planDoc, 0
		default:
			if _, ok := p.previous(r); !ok {
				g.notice = fmt.Sprintf("r%d is the first revision: there is nothing to compare it with.", r.Ordinal)
				return m, nil
			}
			p.mode, p.scroll = planUnified, 0
		}
	case "w":
		switch {
		case p.mode == planDoc:
			g.notice = "Press d to compare revisions first."
		case m.width < planWideMin:
			g.notice = fmt.Sprintf("Side by side needs %d columns; the unified comparison stays.", planWideMin)
		case p.mode == planWide:
			p.mode = planUnified
		default:
			p.mode = planWide
		}
	case "left", "h":
		m.planSection(-1)
	case "right", "l":
		m.planSection(1)
	case "up", "k":
		p.scroll = max(p.scroll-1, 0)
	case "down", "j":
		p.scroll++
	case "pgup":
		p.scroll = max(p.scroll-m.planBodyRows(), 0)
	case "pgdown":
		p.scroll += m.planBodyRows()
	case "home":
		p.scroll = 0
	case "end":
		p.scroll = 1 << 20
	case "a":
		return m.gardenPlanStartDecide(garden.DecisionApproved)
	case "c":
		return m.gardenPlanStartDecide(garden.DecisionChangesRequested)
	case "x":
		return m.gardenPlanStartDecide(garden.DecisionRejected)
	}
	m.planClamp()
	return m, nil
}

// planGoto moves to the n-th next search hit (0 stays on the current one) and scrolls it into view.
func (m *Model) planGoto(step int) {
	p := &m.garden.planrev
	r, ok := p.current()
	if !ok || p.query == "" || p.mode != planDoc {
		return
	}
	d := m.planDocument(r, max(m.width, 1))
	if len(d.hits) == 0 {
		return
	}
	p.hit = ((p.hit+step)%len(d.hits) + len(d.hits)) % len(d.hits)
	p.scroll = max(d.hits[p.hit]-m.planBodyRows()/3, 0)
	m.planClamp()
}

// planSection moves the scroll to the start of the previous or next section.
func (m *Model) planSection(step int) {
	p := &m.garden.planrev
	r, ok := p.current()
	if !ok || p.mode != planDoc {
		return
	}
	d := m.planDocument(r, max(m.width, 1))
	at := 0
	for i, s := range d.starts {
		if s <= p.scroll {
			at = i
		}
	}
	if step > 0 && d.starts[at] <= p.scroll && at+1 < len(d.starts) {
		at++
	} else if step < 0 && d.starts[at] == p.scroll && at > 0 {
		at--
	}
	p.scroll = d.starts[at]
	m.planClamp()
}

// planBodyRows is the rows the document or comparison gets under the facts and the notices.
func (m Model) planBodyRows() int {
	notices := 0
	if v := m.garden.planrev.view; v != nil {
		r, have := m.garden.planrev.current()
		notices = len(m.planNotices(max(m.width, 1), v.Review, r, have))
	}
	return max(m.gardenBoardRows()-planHeadRows-notices-planTailRows, 1)
}

// planClamp keeps the scroll inside the content of the revision in view.
func (m *Model) planClamp() {
	p := &m.garden.planrev
	r, ok := p.current()
	if !ok {
		p.scroll = 0
		return
	}
	n := m.planContentRows(r)
	p.scroll = max(0, min(p.scroll, n-m.planBodyRows()))
}

// planHint formats the key hints of the plan screens.
func (m Model) gardenPlanHints() []shellHint {
	p := m.garden.planrev
	refresh := shellHint{m.keys.Refresh.Help().Key, "refresh"}
	switch p.stage {
	case planCompose:
		return []shellHint{{"ctrl+s", "preview"}, {"esc", "keep draft and leave"}}
	case planConfirm:
		return []shellHint{{"tab", "choose"}, {"enter", "activate"}, {"↑/↓", "scroll"}, {"esc", "edit"}}
	case planValidate:
		return []shellHint{{"tab", "choose"}, {"enter", "activate"}, {"esc", "back"}}
	case planDecide:
		if p.decision == garden.DecisionApproved {
			return []shellHint{{"tab", "choose"}, {"enter", "activate"}, {"esc", "cancel"}}
		}
		return []shellHint{{"enter", "record decision"}, {"esc", "cancel"}}
	}
	if p.searching {
		return []shellHint{{"enter", "apply search"}, {"esc", "clear"}}
	}
	h := []shellHint{{"a", "approve plan"}, {"p", "request plan"}}
	if p.view != nil && p.view.Review.Ready && m.launchSupported() {
		h = append(h, shellHint{"s", "start implementation locally…"})
	}
	h = append(h, []shellHint{{"v", "validate spec"}, {"o/y", "Issue link"}, {"[/]", "revision"}, {"d", "compare"}, {"w", "side by side"}, {"/", "search"}, {"←/→", "section"}, {"↑/↓", "scroll"}}...)
	h = append(h, shellHint{"c", "request changes"}, shellHint{"x", "reject"}, refresh, shellHint{"esc", "back"})
	return h
}
