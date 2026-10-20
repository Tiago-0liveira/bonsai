package ui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// WithGarden injects the Garden backend. Without it the Garden workspace has
// nothing to ask, and its screens say Garden is unavailable.
func WithGarden(b gardenapi.Backend) Option {
	return func(m *Model) { m.garden.api = b }
}

// gardenStep is how deep the user is in the setup flow. The screen at stepStart
// is the one the association state asks for; the others are the pre-Garden
// steps that stack on top of it.
type gardenStep int

const (
	stepStart    gardenStep = iota // GS-01, or the screen of the current state
	stepTarget                     // GS-02
	stepProjects                   // GS-03
	stepPlan                       // GS-04
	stepConfirm                    // GS-05
)

// gardenBusy names the backend call in flight. Reads can be abandoned with Esc;
// writes never are, and ignore every key until they answer.
type gardenBusy int

const (
	busyNone gardenBusy = iota
	busyStatus
	busyWork
	busyCandidates
	busyProjects
	busyPlan
	busyPreviewCapture
	busyDocument
	busyPreviewSave
	busyPreviewValidate
	busyPlanner     // PreviewPlanner and PlannerAttempts: reads
	busyPlanReview  // PlanReview: a read
	busyPlanPreview // PreviewPlanPublish: a read
	busyApply
	busyResume
	busySkip
	busyUnskip
	busyCapture
	busyReconcile
	busySave
	busyValidate
	busyPlannerStart
	busyPlannerCancel
	busyPlanPublish
	busyPlanDecide
)

func (b gardenBusy) writes() bool { return b >= busyApply }

// gardenSub is the Garden submenu entry in view. Table is a layout of the Board.
type gardenSub int

const (
	subSetup gardenSub = iota
	subBoard
	subDrafts
)

// gardenState is the TUI's side of the Garden backend. Every call goes through
// api as a tea.Cmd; Update and View never wait on it. Settings reads status
// from here too, so the two screens can never disagree.
type gardenState struct {
	board   gardenBoardState
	drafts  gardenDraftsState
	planner gardenPlannerState
	planrev gardenPlanState
	api     gardenapi.Backend // nil when Garden is unavailable

	// seq numbers requests: a reply carrying any other value was superseded or
	// abandoned, so it is dropped before it can move focus. gate is the second
	// line, rejecting replies of another clone or an older generation.
	seq    uint64
	gate   gardenapi.Gate
	cancel context.CancelFunc // cancels the read in flight; nil for writes
	busy   gardenBusy
	// initCmd is the first status load when the shell opens on Garden. Init has
	// a value receiver, so New records the request and Init only returns it.
	initCmd tea.Cmd

	status    gardenapi.StatusView
	hasStatus bool
	err       *gardenapi.Error // last failure, shown on the current screen
	notice    string           // why the screen changed under the user (plan changed, ...)

	sub  gardenSub
	step gardenStep
	rail int // selected mode on GS-01

	target  gardenapi.TargetRef // the repository chosen on GS-02, zero otherwise
	chosen  gardenapi.Target
	pending gardenapi.Mode // setup mode waiting for the repository choice; "" just chooses

	cands    gardenapi.CandidatesView
	candSel  int
	projects gardenapi.ProjectsView
	projSel  int
	req      gardenapi.PlanRequest // the preview in flight, kept to retry it
	plan     gardenapi.PlanView
	planSel  int
	confirm  int // GS-05 focus: 0 Cancel, 1 Apply

	op    *gardenapi.OperationView // newest Apply or Resume result, which carries Err
	opSel int
	opBtn int // GS-06 focus among the offered actions
}

// begin starts a request: it supersedes the read in flight and returns the
// context and sequence number the reply must carry. Writes get a context that
// is never cancelled, because abandoning one halfway is worse than waiting.
func (g *gardenState) begin(kind gardenBusy) (context.Context, uint64) {
	g.abandon()
	g.seq++
	g.busy = kind
	g.err, g.notice = nil, ""
	if kind.writes() {
		return context.Background(), g.seq
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	return ctx, g.seq
}

// abandon drops the read in flight: its reply, if it still arrives, is stale.
func (g *gardenState) abandon() {
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	if !g.busy.writes() {
		g.seq++
		g.busy = busyNone
	}
}

// settle matches a reply to the request in flight. ok is false for a late or
// abandoned reply, which must be dropped unread; fail is the safe error of a
// failed call.
func (g *gardenState) settle(seq uint64, err error) (fail *gardenapi.Error, ok bool) {
	if seq != g.seq {
		return nil, false
	}
	g.busy, g.cancel = busyNone, nil
	if errors.Is(err, context.Canceled) {
		return nil, false
	}
	if err != nil {
		return safeGardenError(err), true
	}
	return nil, true
}

// safeGardenError keeps what the backend marked safe to show. Anything else
// becomes a generic failure: a raw error string could carry a URL or a token.
func safeGardenError(err error) *gardenapi.Error {
	var ge *gardenapi.Error
	if errors.As(err, &ge) && ge != nil {
		return ge
	}
	return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Garden could not complete that request.", Next: "Try again, or refresh the state."}
}

// staleReply is shown when a reply for the request in flight is rejected by
// the gate, so the screen never waits for an answer that was dropped.
var staleReply = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Ignored an out-of-date Garden reply.", Next: "Refresh to read the current state."}

// Replies. Each carries the sequence number of its request.
type (
	gardenStatusMsg struct {
		seq        uint64
		view       gardenapi.StatusView
		remembered string
		err        error
	}
	gardenCandidatesMsg struct {
		seq  uint64
		view gardenapi.CandidatesView
		err  error
	}
	gardenProjectsMsg struct {
		seq  uint64
		view gardenapi.ProjectsView
		err  error
	}
	gardenPlanMsg struct {
		seq  uint64
		view gardenapi.PlanView
		err  error
	}
	// gardenOpMsg is an Apply or Resume answer. An operation that stopped halfway
	// arrives as a view with Err set and no error.
	gardenOpMsg struct {
		seq    uint64
		resume bool
		view   gardenapi.OperationView
		err    error
	}
	// gardenSkipMsg is the status after Skip or Unskip.
	gardenSkipMsg struct {
		seq  uint64
		view gardenapi.StatusView
		err  error
	}
)

// The commands below are the only place the Garden screens reach the backend.

func (m Model) gardenLoadStatus() (Model, tea.Cmd) {
	g := &m.garden
	if g.api == nil || g.busy.writes() {
		return m, nil
	}
	ctx, seq := g.begin(busyStatus)
	api := g.api
	st, dir := m.state, m.repoDir
	return m, func() tea.Msg {
		v, err := api.Status(ctx)
		remembered := ""
		if st != nil {
			remembered = st.GardenView(dir)
		}
		return gardenStatusMsg{seq: seq, view: v, remembered: remembered, err: err}
	}
}

func (m Model) gardenLoadCandidates() (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyCandidates)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Candidates(ctx)
		return gardenCandidatesMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenLoadProjects(t gardenapi.TargetRef) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyProjects)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Projects(ctx, t)
		return gardenProjectsMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenPreview(req gardenapi.PlanRequest) (Model, tea.Cmd) {
	ctx, seq := m.garden.begin(busyPlan)
	m.garden.req = req
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.PreviewPlan(ctx, req)
		return gardenPlanMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenApply(plan gardenapi.PlanView) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyApply)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Apply(context.Background(), plan)
		return gardenOpMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenResume(opID string) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyResume)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Resume(context.Background(), opID)
		return gardenOpMsg{seq: seq, resume: true, view: v, err: err}
	}
}

func (m Model) gardenSkip(t gardenapi.TargetRef) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busySkip)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Skip(context.Background(), t)
		return gardenSkipMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) gardenUnskip() (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyUnskip)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Unskip(context.Background())
		return gardenSkipMsg{seq: seq, view: v, err: err}
	}
}
