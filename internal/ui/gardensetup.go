package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// gardenScreen is what the Garden body shows. The association state picks one;
// the setup steps (GS-02 to GS-05) stack on top of GS-01 while the state is
// still none.
type gardenScreen int

const (
	scrUnavailable gardenScreen = iota // no backend injected
	scrLoading                         // first status read in flight
	scrError                           // no status could be read
	scrStart                           // GS-01
	scrTarget                          // GS-02
	scrProjects                        // GS-03
	scrPlan                            // GS-04
	scrConfirm                         // GS-05
	scrIncomplete                      // GS-06
	scrReadOnly                        // GS-07
	scrOff                             // GS-08
	scrBoard                           // accepted-work board and read-only document
	scrDrafts                          // the Drafts inbox, its detail and the capture flow
)

func (m Model) gardenScreen() gardenScreen {
	g := m.garden
	switch {
	case g.api == nil:
		return scrUnavailable
	case !g.hasStatus && g.err != nil && g.busy == busyNone:
		return scrError
	case !g.hasStatus:
		return scrLoading
	}
	st := g.status
	if g.sub == subDrafts && st.Caps.Read {
		return scrDrafts
	}
	if st.State == gardenapi.StateConnected || (g.sub == subBoard && st.Caps.Read) {
		return scrBoard
	}
	switch st.State {
	case gardenapi.StateNone:
		switch g.step {
		case stepTarget:
			return scrTarget
		case stepProjects:
			return scrProjects
		case stepPlan:
			return scrPlan
		case stepConfirm:
			return scrConfirm
		}
		return scrStart
	case gardenapi.StateSkipped:
		return scrOff
	case gardenapi.StateReadOnly:
		return scrReadOnly
	case gardenapi.StatePartialSetup, gardenapi.StateAwaitingManual, gardenapi.StateOperationInProgress:
		return scrIncomplete
	}
	return scrError
}

// gardenModes are the setup choices of GS-01 the capabilities allow. A choice
// the backend would refuse is never drawn.
func (m Model) gardenModes() []gardenapi.Mode {
	var out []gardenapi.Mode
	c := m.garden.status.Caps
	if c.Setup {
		out = append(out, gardenapi.ModeCreate, gardenapi.ModeConnectExisting)
	}
	if c.Skip {
		out = append(out, gardenapi.ModeSkip)
	}
	return out
}

func (m Model) gardenRail() int {
	return max(min(m.garden.rail, len(m.gardenModes())-1), 0)
}

// gardenTarget is the repository to show: the one the association names, else
// the one chosen on GS-02.
func (m Model) gardenTarget() gardenapi.Target {
	if t := m.garden.status.Target; t.RepoID != 0 {
		return t
	}
	return m.garden.chosen
}

// gardenTargetRef is the explicit target every call carries once it is known,
// so a later remote change can never redirect a call.
func (m Model) gardenTargetRef() gardenapi.TargetRef {
	if m.garden.target.RepoID != 0 {
		return m.garden.target
	}
	return gardenapi.TargetRef{RepoID: m.garden.status.Target.RepoID}
}

// gardenOperation is the operation GS-06 shows: the status's, carrying the
// error of the newest Apply or Resume when the status has none.
func (m Model) gardenOperation() (gardenapi.OperationView, bool) {
	g := m.garden
	switch {
	case g.status.Operation != nil:
		op := *g.status.Operation
		if op.Err == nil && g.op != nil && g.op.ID == op.ID {
			op.Err = g.op.Err
		}
		return op, true
	case g.op != nil:
		return *g.op, true
	}
	return gardenapi.OperationView{}, false
}

// gardenEnterBody gives the body the keyboard. It only reads when nothing was
// ever loaded, for instance after the first read was abandoned.
func (m Model) gardenEnterBody() (Model, tea.Cmd) {
	m.shellFocus = shellBody
	if m.gardenTyping() {
		m.shellFocus = shellInput // an unfinished idea keeps the keyboard it had
	}
	if m.garden.api != nil && !m.garden.hasStatus && m.garden.busy == busyNone {
		return m.gardenLoadStatus()
	}
	return m, nil
}

// openGarden is the Settings link: the Garden workspace with the body focused.
func (m Model) openGarden() (Model, tea.Cmd) {
	m, cmd := m.selectWorkspace(wsGarden)
	m.garden.sub = subSetup
	m, extra := m.gardenEnterBody()
	return m, tea.Batch(cmd, extra)
}

// gardenWarm reads the state for Settings when nothing has been read yet, so
// the Garden category never has to guess. It reuses the one status flow.
func (m Model) gardenWarm() (Model, tea.Cmd) {
	m, assistCmd := m.settingsAssistWarm()
	g := m.garden
	if g.api == nil || g.hasStatus || g.busy != busyNone || g.err != nil {
		return m, assistCmd
	}
	if m.workspace != wsSettings || m.curCategory().id != "garden" {
		return m, assistCmd
	}
	m, cmd := m.gardenLoadStatus()
	return m, tea.Batch(assistCmd, cmd)
}

// gardenMsg handles the replies of the Garden calls. handled is false for any
// other message.
func (m Model) gardenMsg(msg tea.Msg) (handled bool, nm Model, cmd tea.Cmd) {
	switch msg := msg.(type) {
	case gardenWorkMsg:
		nm, cmd = m.onGardenWork(msg)
	case assistQuestionsMsg, assistDraftMsg, assistTickMsg:
		return m.gardenAssistMsg(msg)
	case gardenCapturePreviewMsg:
		nm, cmd = m.onGardenCapturePreview(msg)
	case gardenCaptureMsg:
		nm, cmd = m.onGardenCapture(msg)
	case gardenDocumentMsg:
		nm, cmd = m.onGardenDocument(msg)
	case gardenSavePreviewMsg:
		nm, cmd = m.onGardenSavePreview(msg)
	case gardenSaveMsg:
		nm, cmd = m.onGardenSave(msg)
	case gardenValidatePreviewMsg:
		nm, cmd = m.onGardenValidatePreview(msg)
	case gardenValidateMsg:
		nm, cmd = m.onGardenValidate(msg)
	case gardenPlanReviewMsg:
		nm, cmd = m.onGardenPlanReview(msg)
	case gardenPlanPreviewMsg:
		nm, cmd = m.onGardenPlanPreview(msg)
	case gardenPlanPublishMsg:
		nm, cmd = m.onGardenPlanPublish(msg)
	case gardenPlanDecisionMsg:
		nm, cmd = m.onGardenPlanDecision(msg)
	case gardenPlannerPreviewMsg:
		nm, cmd = m.onGardenPlannerPreview(msg)
	case gardenPlannerAttemptsMsg:
		nm, cmd = m.onGardenPlannerAttempts(msg)
	case gardenPlannerStartMsg:
		nm, cmd = m.onGardenPlannerStart(msg)
	case gardenPlannerCancelMsg:
		nm, cmd = m.onGardenPlannerCancel(msg)
	case agentsSubscribedMsg:
		if !m.garden.launchSub {
			return false, m, nil
		}
		nm, cmd = m.onShellAgentsSubscribed(msg)
	case agentsChangedMsg:
		if !m.garden.launchSub {
			return false, m, nil
		}
		nm, cmd = m.onShellAgentsChanged(msg)
	case gardenLaunchResolvedMsg:
		nm, cmd = m.onGardenLaunchResolved(msg)
	case gardenLaunchPreviewMsg:
		nm, cmd = m.onGardenLaunchPreview(msg)
	case gardenLaunchStartMsg:
		nm, cmd = m.onGardenLaunchStart(msg)
	case gardenLaunchAttemptsMsg:
		nm, cmd = m.onGardenLaunchAttempts(msg)
	case gardenLaunchActionMsg:
		nm, cmd = m.onGardenLaunchAction(msg)
	case gardenLaunchCleanupMsg:
		nm, cmd = m.onGardenLaunchCleanup(msg)
	case launchSubmitMsg:
		if l := m.garden.launch; l.dlg != nil && l.dlg.id == msg.dlg {
			nm, cmd = m.onGardenLaunchSubmit(msg)
			break
		}
		if m.garden.planner.dlg == nil {
			return false, m, nil
		}
		nm, cmd = m.onPlannerSubmit(msg)
	case accounts.CloseMsg:
		if m.garden.launch.stage == launchStageChoose {
			nm, cmd = m.gardenLaunchChooseClose()
			break
		}
		if m.garden.planner.stage != plannerChoose {
			return false, m, nil
		}
		nm = m.gardenPlannerChooseClose()
	case agentAttachedMsg, agentRetryMsg, agentFramesMsg, agentInputMsg, agentInputSentMsg, agentTookMsg, agentResizeTickMsg:
		nm, cmd = m.plannerTerminalMsg(msg)
	case gardenViewSavedMsg:
		nm = m.gardenBoardSaved(msg)
	case gardenStatusMsg:
		nm, cmd = m.onGardenStatus(msg)
	case gardenCandidatesMsg:
		nm, cmd = m.onGardenCandidates(msg)
	case gardenProjectsMsg:
		nm, cmd = m.onGardenProjects(msg)
	case gardenPlanMsg:
		nm, cmd = m.onGardenPlan(msg)
	case gardenOpMsg:
		nm, cmd = m.onGardenOp(msg)
	case gardenSkipMsg:
		nm, cmd = m.onGardenSkip(msg)
	default:
		return false, m, nil
	}
	return true, nm, cmd
}

// gardenResetFlow returns the setup flow to its first screen. The rail keeps
// its selection so a returning user finds the same choice.
func (m Model) gardenResetFlow() Model {
	g := &m.garden
	g.step, g.sub, g.pending = stepStart, subSetup, ""
	g.cands, g.candSel = gardenapi.CandidatesView{}, 0
	g.projects, g.projSel = gardenapi.ProjectsView{}, 0
	g.plan, g.planSel, g.confirm = gardenapi.PlanView{}, 0, 0
	g.opSel, g.opBtn = 0, 0
	return m
}

// gardenAdopt makes a status the one both Garden and Settings show. The flow
// resets when the association moved under it; otherwise a user who left and
// came back finds the step where they were.
func (m Model) gardenAdopt(v gardenapi.StatusView, force bool) (Model, tea.Cmd) {
	var release tea.Cmd
	g := &m.garden
	prev, had := g.status, g.hasStatus
	if had && (prev.Stamp.GardenID != v.Stamp.GardenID || prev.Stamp.Epoch != v.Stamp.Epoch) {
		g.board = gardenBoardState{saveGen: g.board.saveGen, memoryLoaded: g.board.memoryLoaded, rememberedView: g.board.rememberedView}
		// A flight and an open assistant belong to the old connection; its sessions stay in memory, inactive.
		g.assist.release()
		// An unfinished idea outlives the change; its preview and results belong to the old Garden.
		g.drafts = gardenDraftsState{form: g.drafts.form, formOpen: g.drafts.formOpen, editors: g.drafts.editors}
		// A planner belongs to the Issue of the old Garden; its session keeps running, only the view goes.
		if g.planner.stage != plannerNone {
			release = m.plannerRelease()
			g.planner = gardenPlannerState{}
		}
		// Launch screens belong to the old Garden's Issue too. Its attempts and any write in flight stay recorded
		// locally and are read again for the new one.
		if g.launch.stage != launchStageNone {
			release = tea.Batch(release, m.launchDetach())
			g.launch = gardenLaunchState{inflight: g.launch.inflight, previewSeq: g.launch.previewSeq + 1, readSeq: g.launch.readSeq, run: -1}
		}
		g.planrev = gardenPlanState{}
	}
	g.status, g.hasStatus = v, true
	if force || !had || prev.State != v.State || prev.Stamp.Epoch != v.Stamp.Epoch {
		m = m.gardenResetFlow()
		g = &m.garden
	}
	if !v.Ambiguous {
		g.target, g.chosen = gardenapi.TargetRef{}, gardenapi.Target{}
	}
	inOp := v.State == gardenapi.StatePartialSetup || v.State == gardenapi.StateAwaitingManual || v.State == gardenapi.StateOperationInProgress
	if g.op != nil && (!inOp || (v.Operation != nil && v.Operation.ID != g.op.ID)) {
		g.op = nil
	}
	if v.State == gardenapi.StateNone && v.Ambiguous && g.target.RepoID == 0 && g.step == stepStart {
		// Several remotes point at different repositories: the target comes first.
		m, cmd := m.gardenLoadCandidates()
		m.garden.pending = ""
		return m, tea.Batch(release, cmd)
	}
	return m, release
}

func (m Model) onGardenStatus(msg gardenStatusMsg) (Model, tea.Cmd) {
	fail, ok := m.garden.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		m.garden.err = fail
		return m, nil
	}
	prior := m.garden.gate.Last()
	if prior.GardenID != 0 && msg.view.Stamp.GardenID != prior.GardenID && msg.view.Stamp.Epoch == prior.Epoch {
		m.garden.err = staleReply
		return m, nil
	}
	if !m.garden.gate.Accept(msg.view.Stamp) {
		m.garden.err = staleReply
		return m, nil
	}
	first := !m.garden.board.memoryLoaded
	m, cmd := m.gardenAdopt(msg.view, false)
	if first {
		m.garden.board.memoryLoaded = true
		m.garden.board.rememberedView = msg.remembered
		if (msg.remembered == "board" || msg.remembered == "table") && m.garden.status.Caps.Read {
			m.garden.sub = subBoard
			if msg.remembered == "table" {
				m.garden.board.layout = layoutTable
			}
		}
	}
	if scr := m.gardenScreen(); scr == scrBoard || scr == scrDrafts {
		var load tea.Cmd
		m, load = m.gardenLoadWork()
		return m, tea.Batch(cmd, load)
	}
	return m, cmd
}

func (m Model) onGardenCandidates(msg gardenCandidatesMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyCandidates, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	cs := msg.view.Candidates
	switch {
	case len(cs) == 0:
		g.err = &gardenapi.Error{Code: gardenapi.CodeNoGitHubRemote, Message: "No remote points at a GitHub repository.", Next: "Add a GitHub remote, then refresh."}
	case !msg.view.Ambiguous && len(cs) == 1:
		g.target, g.chosen = gardenapi.TargetRef{RepoID: cs[0].RepoID}, cs[0]
		return m.gardenAfterPick()
	default:
		g.cands, g.candSel, g.step = msg.view, 0, stepTarget
	}
	return m, nil
}

func (m Model) onGardenProjects(msg gardenProjectsMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyProjects, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	g.projects, g.projSel, g.step = msg.view, 0, stepProjects
	return m, nil
}

func (m Model) onGardenPlan(msg gardenPlanMsg) (Model, tea.Cmd) {
	g := &m.garden
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(busyPlan, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	g.plan, g.planSel, g.confirm, g.step = msg.view, 0, 0, stepPlan
	return m, nil
}

func (m Model) onGardenOp(msg gardenOpMsg) (Model, tea.Cmd) {
	g := &m.garden
	kind := busyApply
	if msg.resume {
		kind = busyResume
	}
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(kind, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	op := msg.view
	g.op = &op
	// The view is the outcome of one call. The state it leaves behind (Caps,
	// target, project) comes from the same status read as everywhere else.
	g.hasStatus = false
	return m.gardenLoadStatus()
}

func (m Model) onGardenSkip(msg gardenSkipMsg) (Model, tea.Cmd) {
	g := &m.garden
	kind := busySkip
	if m.garden.busy == busyUnskip {
		kind = busyUnskip
	}
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		return m.gardenFailed(kind, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	g.op = nil
	return m.gardenAdopt(msg.view, true)
}

// gardenFailed routes a failed call. Codes that mean "the state is not what
// this screen assumed" re-read it or re-preview; the rest show the safe message
// on the current screen.
func (m Model) gardenFailed(kind gardenBusy, f *gardenapi.Error) (Model, tea.Cmd) {
	g := &m.garden
	switch f.Code {
	case gardenapi.CodeTargetAmbiguous:
		var mode gardenapi.Mode
		switch kind {
		case busyPlan:
			mode = g.req.Mode
		case busyProjects:
			mode = gardenapi.ModeConnectExisting
		case busySkip:
			mode = gardenapi.ModeSkip
		}
		if mode != "" {
			m, cmd := m.gardenLoadCandidates()
			m.garden.pending = mode
			return m, cmd
		}
	case gardenapi.CodeStalePlan, gardenapi.CodeProjectConflict:
		if kind == busyApply {
			// GitHub changed since the preview. Nothing was applied: show the new plan.
			m, cmd := m.gardenPreview(g.plan.Request)
			m.garden.notice = "The plan changed on GitHub since you reviewed it. Review it again."
			return m, cmd
		}
	case gardenapi.CodeSkipped, gardenapi.CodeReadOnly, gardenapi.CodeOperationInProgress, gardenapi.CodeConflict:
		// The association is not what this screen assumed (turned off, read-only,
		// already set up, running elsewhere). The state decides the right screen.
		m, cmd := m.gardenLoadStatus()
		m.garden.notice = f.Message
		return m, cmd
	}
	g.err = f
	return m, nil
}

// gardenAfterPick continues the flow that needed the repository choice.
func (m Model) gardenAfterPick() (Model, tea.Cmd) {
	mode := m.garden.pending
	m.garden.pending, m.garden.step = "", stepStart
	if mode == "" {
		return m, nil
	}
	return m.gardenStartMode(mode)
}

// gardenStartMode runs the choice made on GS-01. Preview and project listing
// only read; Skip is the one write here, and it is reversible.
func (m Model) gardenStartMode(mode gardenapi.Mode) (Model, tea.Cmd) {
	g := &m.garden
	if g.status.Ambiguous && g.target.RepoID == 0 {
		m, cmd := m.gardenLoadCandidates()
		m.garden.pending = mode
		return m, cmd
	}
	ref := m.gardenTargetRef()
	switch mode {
	case gardenapi.ModeCreate:
		return m.gardenPreview(gardenapi.PlanRequest{Mode: gardenapi.ModeCreate, Target: ref})
	case gardenapi.ModeConnectExisting:
		return m.gardenLoadProjects(ref)
	case gardenapi.ModeSkip:
		return m.gardenSkip(ref)
	}
	return m, nil
}

// gardenRefreshable says on which screens R re-reads the state. Deeper steps
// keep their own state until the user leaves them.
func gardenRefreshable(s gardenScreen) bool {
	switch s {
	case scrLoading, scrError, scrStart, scrIncomplete, scrReadOnly, scrOff, scrBoard, scrDrafts:
		return true
	}
	return false
}

// gardenKey handles a key while the Garden body owns the keyboard. Selection
// never writes: only GS-05 Apply, GS-06 Resume, Skip and Unskip do.
func (m Model) gardenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	k := msg.String()
	if g.api == nil {
		if k == "esc" {
			m.shellFocus = shellNav
		}
		return m, nil
	}
	if g.busy.writes() {
		return m, nil // a write answers first; a second Enter must not repeat it
	}
	scr := m.gardenScreen()
	if scr == scrBoard {
		return m.gardenBoardKey(msg)
	}
	if scr == scrDrafts {
		return m.gardenDraftsKey(msg)
	}
	if g.busy != busyNone {
		if k == "esc" {
			g.abandon()
			if scr == scrLoading {
				m.shellFocus = shellNav
			}
		}
		return m, nil
	}
	switch {
	case k == "esc":
		return m.gardenBack(scr), nil
	case key.Matches(msg, m.keys.Refresh) && gardenRefreshable(scr):
		return m.gardenLoadStatus()
	case key.Matches(msg, m.keys.Tab):
		m = m.gardenToggleSub()
		if scr := m.gardenScreen(); scr == scrBoard || scr == scrDrafts {
			return m.gardenLoadWork()
		}
		cmd := m.gardenRememberView()
		return m, cmd
	}
	up, down := k == "up" || k == "k", k == "down" || k == "j"
	left, right := k == "left" || k == "h", k == "right" || k == "l"
	enter := k == "enter"
	step := func(sel *int, n int) {
		if n > 0 {
			*sel = max(min(*sel+btoi(down)-btoi(up), n-1), 0)
		}
	}

	switch scr {
	case scrStart:
		modes := m.gardenModes()
		g.rail = m.gardenRail()
		step(&g.rail, len(modes))
		if enter && len(modes) > 0 {
			return m.gardenStartMode(modes[g.rail])
		}
	case scrTarget:
		step(&g.candSel, len(g.cands.Candidates))
		if enter && len(g.cands.Candidates) > 0 {
			c := g.cands.Candidates[g.candSel]
			g.target, g.chosen = gardenapi.TargetRef{RepoID: c.RepoID}, c
			return m.gardenAfterPick()
		}
	case scrProjects:
		step(&g.projSel, len(g.projects.Projects))
		if enter && len(g.projects.Projects) > 0 {
			p := g.projects.Projects[g.projSel]
			return m.gardenPreview(gardenapi.PlanRequest{
				Mode: gardenapi.ModeConnectExisting, Target: m.gardenTargetRef(),
				Project: &gardenapi.ProjectRef{ID: p.ID, Number: p.Number},
			})
		}
	case scrPlan:
		step(&g.planSel, len(g.plan.Rows))
		if enter && g.plan.Summary.CanApply {
			g.step, g.confirm = stepConfirm, 0 // Cancel has the first focus
		}
	case scrConfirm:
		if left || right {
			g.confirm = btoi(right)
		}
		if enter {
			if g.confirm == 1 {
				return m.gardenApply(g.plan)
			}
			g.step = stepPlan
		}
	case scrIncomplete:
		op, _ := m.gardenOperation()
		step(&g.opSel, len(op.Steps)+len(op.AwaitingManual))
		acts := m.gardenIncompleteActions()
		if len(acts) > 0 {
			g.opBtn = max(min(g.opBtn+btoi(right)-btoi(left), len(acts)-1), 0)
			if enter {
				switch acts[g.opBtn] {
				case actResume:
					return m.gardenResume(op.ID)
				case actSkip:
					return m.gardenSkip(gardenapi.TargetRef{RepoID: g.status.Target.RepoID})
				}
			}
		}
	case scrReadOnly:
		if enter && g.status.Caps.Read {
			g.sub = subBoard
			return m.gardenLoadWork()
		}
	case scrOff:
		if enter && g.status.Caps.Unskip {
			return m.gardenUnskip()
		}
	case scrError:
		if enter {
			return m.gardenLoadStatus()
		}
	}
	return m, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GS-06 actions in the order they are offered.
type gardenAction int

const (
	actResume gardenAction = iota
	actSkip
)

func (a gardenAction) label() string {
	if a == actResume {
		return "Resume setup ›"
	}
	return "Skip Garden for now"
}

func (a gardenAction) verb() string {
	if a == actResume {
		return "resume"
	}
	return "skip Garden"
}

// gardenIncompleteActions are the GS-06 actions the capabilities allow. A
// setup that is running elsewhere offers none, and Resume needs an operation.
func (m Model) gardenIncompleteActions() []gardenAction {
	g := m.garden
	if g.status.State == gardenapi.StateOperationInProgress {
		return nil
	}
	var out []gardenAction
	if op, ok := m.gardenOperation(); ok && op.ID != "" && g.status.Caps.Resume {
		out = append(out, actResume)
	}
	if g.status.Caps.Skip {
		out = append(out, actSkip)
	}
	return out
}

// gardenBack is Esc: the parent step with its selection kept, or the shell's
// own navigation from the first screen of a state.
func (m Model) gardenBack(scr gardenScreen) Model {
	g := &m.garden
	g.err, g.notice = nil, ""
	switch scr {
	case scrConfirm:
		g.step = stepPlan
	case scrPlan:
		g.step = stepStart
		if g.plan.Request.Mode == gardenapi.ModeConnectExisting {
			g.step = stepProjects
		}
	case scrProjects:
		g.step = stepStart
	case scrTarget:
		if g.pending == "" {
			m.shellFocus = shellNav // nothing before it to return to
		} else {
			g.step, g.pending = stepStart, ""
		}
	case scrBoard:
		if g.status.State != gardenapi.StateConnected && g.sub == subBoard {
			g.sub = subSetup
		} else {
			m.shellFocus = shellNav
		}
	default:
		m.shellFocus = shellNav
	}
	return m
}

// gardenToggleSub cycles the submenu entries that exist: Setup (until the
// Garden is connected), the work view (Board or Table, whichever is current)
// and Drafts. Drafts is its own inbox: it is never mixed into the work view.
func (m Model) gardenToggleSub() Model {
	g := &m.garden
	if !m.canToggleSub() {
		return m
	}
	var order []gardenSub
	if g.status.State != gardenapi.StateConnected {
		order = append(order, subSetup)
	}
	order = append(order, subBoard, subDrafts)
	cur := g.sub
	if cur == subSetup && g.status.State == gardenapi.StateConnected {
		cur = subBoard
	}
	for i, s := range order {
		if s == cur {
			g.sub = order[(i+1)%len(order)]
			return m
		}
	}
	g.sub = order[0]
	return m
}

// canToggleSub reports whether Tab does anything: a readable Garden has the
// Board and Drafts to move between.
func (m Model) canToggleSub() bool {
	g := m.garden
	return g.hasStatus && g.status.Caps.Read
}

var gardenRailNames = map[gardenapi.Mode]string{
	gardenapi.ModeCreate:          "Create Garden",
	gardenapi.ModeConnectExisting: "Connect existing Project",
	gardenapi.ModeSkip:            "Skip for this repository",
}

// gardenCrumbs is the breadcrumb after the workspace: Garden › Setup › <step>.
func (m Model) gardenCrumbs() []string {
	setup := func(step string) []string { return []string{"Garden", "Setup", step} }
	switch m.gardenScreen() {
	case scrUnavailable:
		return []string{"Garden"}
	case scrDrafts:
		return m.gardenDraftsCrumbs()
	case scrBoard:
		b := m.garden.board
		crumbs := []string{"Garden", "Board", b.selection.Column.String()}
		if b.layout == layoutTable {
			crumbs = []string{"Garden", "Table"}
		}
		if b.opened != 0 {
			if r, _, ok := b.collection.Find(b.opened); ok {
				crumbs = append(crumbs, fmt.Sprintf("#%d", r.Item.Issue.Number))
			}
			switch {
			case m.planActive():
				crumbs = append(crumbs, "Plan review")
			case m.launchActive():
				crumbs = append(crumbs, m.gardenLaunchCrumb())
			case m.plannerActive():
				crumbs = append(crumbs, "Read-only planning")
			}
		}
		return crumbs
	case scrOff:
		return []string{"Garden", "Off"}
	case scrStart:
		if modes := m.gardenModes(); len(modes) > 0 {
			return setup(gardenRailNames[modes[m.gardenRail()]])
		}
	case scrTarget:
		return setup("Choose repository")
	case scrProjects:
		return setup("Connect existing Project")
	case scrPlan:
		return setup("Preview plan")
	case scrConfirm:
		return setup("Confirm setup")
	case scrReadOnly:
		return setup("Read-only access")
	case scrIncomplete:
		switch m.garden.status.State {
		case gardenapi.StateAwaitingManual:
			return setup("Manual step")
		case gardenapi.StateOperationInProgress:
			return setup("Setup running")
		}
		return setup("Incomplete setup")
	}
	return []string{"Garden", "Setup"}
}

// gardenWhere is the footer location.
func (m Model) gardenWhere() string {
	if m.gardenScreen() == scrBoard {
		return m.gardenBoardIdentity()
	}
	if m.gardenScreen() == scrDrafts {
		return m.gardenDraftsIdentity()
	}
	return strings.Join(m.gardenCrumbs(), " / ")
}

// gardenFocusLabel names the focus owner inside the body, as the mockups do:
// "Setup / Create Garden", "Garden / Off".
func (m Model) gardenFocusLabel() string {
	if m.gardenScreen() == scrDrafts {
		return m.gardenDraftsFocusLabel()
	}
	if m.gardenScreen() == scrBoard && m.garden.board.layout == layoutTable && m.garden.board.opened == 0 {
		return "Approved table"
	}
	if m.gardenScreen() == scrBoard && m.planActive() {
		return m.gardenPlanFocusLabel()
	}
	if m.gardenScreen() == scrBoard && m.launchActive() {
		return m.gardenLaunchFocusLabel()
	}
	if m.gardenScreen() == scrBoard && m.plannerActive() {
		return m.gardenPlannerFocusLabel()
	}
	if m.gardenScreen() == scrBoard && m.garden.board.opened != 0 {
		return "Issue / " + gardenDocumentSections[m.garden.board.section]
	}
	c := m.gardenCrumbs()
	if len(c) > 2 {
		c = c[1:]
	}
	return strings.Join(c, " / ")
}

// gardenHints are the footer hints for the screen in view, limited to keys that
// do something there.
func (m Model) gardenHints() []shellHint {
	if m.gardenScreen() == scrBoard {
		return m.gardenBoardHints()
	}
	if m.gardenScreen() == scrDrafts {
		return m.gardenDraftsHints()
	}
	g := m.garden
	if g.api == nil {
		return []shellHint{{"esc", "workspaces"}}
	}
	var h []shellHint
	scr := m.gardenScreen()
	if g.busy != busyNone {
		if !g.busy.writes() {
			h = append(h, shellHint{"esc", "cancel"})
		}
		return h
	}
	switch scr {
	case scrStart:
		verb := "preview plan"
		if modes := m.gardenModes(); len(modes) > 0 {
			switch modes[m.gardenRail()] {
			case gardenapi.ModeConnectExisting:
				verb = "choose Project"
			case gardenapi.ModeSkip:
				verb = "skip"
			}
			h = append(h, shellHint{"↑/↓", "choice"}, shellHint{"enter", verb})
		}
	case scrTarget:
		h = append(h, shellHint{"↑/↓", "repository"}, shellHint{"enter", "choose"})
	case scrProjects:
		h = append(h, shellHint{"↑/↓", "project"}, shellHint{"enter", "select"})
	case scrPlan:
		h = append(h, shellHint{"↑/↓", "step"})
		if g.plan.Summary.CanApply {
			h = append(h, shellHint{"enter", "review and apply"})
		}
	case scrConfirm:
		h = append(h, shellHint{"←/→", "button"}, shellHint{"enter", "choose"})
	case scrIncomplete:
		acts := m.gardenIncompleteActions()
		if len(acts) > 0 {
			h = append(h, shellHint{"enter", acts[max(min(g.opBtn, len(acts)-1), 0)].verb()})
		}
		h = append(h, shellHint{"↑/↓", "step"})
		if len(acts) > 1 {
			h = append(h, shellHint{"←/→", "action"})
		}
	case scrReadOnly:
		if g.status.Caps.Read {
			h = append(h, shellHint{"enter", "open board"})
		}
	case scrOff:
		if g.status.Caps.Unskip {
			h = append(h, shellHint{"enter", "set up Garden"})
		}
	case scrError:
		h = append(h, shellHint{"enter", "retry"})
	}
	back := "parent"
	switch scr {
	case scrPlan, scrConfirm:
		back = "back"
		if scr == scrConfirm {
			back = "cancel"
		}
	}
	h = append(h, shellHint{"esc", back})
	if gardenRefreshable(scr) {
		h = append(h, shellHint{m.keys.Refresh.Help().Key, "refresh"})
	}
	if m.canToggleSub() {
		h = append(h, shellHint{m.keys.Tab.Help().Key, "submenu"})
	}
	return h
}

// gardenStateLabel is the short state name for the header and Settings.
func gardenStateLabel(s gardenapi.State) string {
	switch s {
	case gardenapi.StateNone:
		return "not set up"
	case gardenapi.StateSkipped:
		return "garden off"
	case gardenapi.StateReadOnly:
		return "read-only"
	case gardenapi.StateConnected:
		return "connected"
	case gardenapi.StatePartialSetup:
		return "setup incomplete"
	case gardenapi.StateAwaitingManual:
		return "manual step"
	case gardenapi.StateOperationInProgress:
		return "setting up"
	}
	return "unknown"
}

// gardenStatusLine is the footer's right-hand reminder for the screen in view.
func (m Model) gardenStatusLine() string {
	g := m.garden
	switch m.gardenScreen() {
	case scrUnavailable:
		return "Garden unavailable"
	case scrLoading:
		return "Reading GitHub…"
	case scrError:
		return "Nothing changed"
	case scrStart, scrProjects:
		return "Nothing changes until you confirm"
	case scrTarget:
		return "Credentials are removed from remote URLs"
	case scrPlan:
		if m.width < settingsWideMin {
			return fmt.Sprintf("%d issues imported · nothing written", g.plan.Summary.ImportCount)
		}
		return "Preview only · nothing written"
	case scrConfirm:
		return "Nothing written yet"
	case scrIncomplete:
		if op, ok := m.gardenOperation(); ok && op.Total > 0 {
			return fmt.Sprintf("%d of %d steps done", op.Done, op.Total)
		}
		return "Setup not finished"
	case scrReadOnly:
		return "Read-only · no setup actions"
	case scrOff:
		return "Garden off for this clone"
	case scrBoard:
		return m.gardenBoardStatus()
	case scrDrafts:
		return m.gardenBoardStatus()
	}
	return ""
}
