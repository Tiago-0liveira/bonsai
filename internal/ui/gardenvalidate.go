package ui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Gate one: validating a specification. Previewing reads and verifies; only the explicit Validate button
// writes, and it writes the approval of one revision. Nothing here starts a planner, an agent or a worktree.

type (
	gardenValidatePreviewMsg struct {
		seq  uint64
		view gardenapi.ValidatePreviewView
		err  error
	}
	gardenValidateMsg struct {
		seq  uint64
		view gardenapi.ValidateView
		err  error
	}
)

// canValidate says whether v may start: approving records a comment and updates the stage label, which
// needs Write access.
func (m Model) canValidate() (bool, string) {
	g := m.garden
	switch {
	case g.api == nil || !g.hasStatus:
		return false, "Garden is not loaded yet."
	case g.status.Access < gardenapi.AccessWrite:
		return false, "Validating a specification needs Write access to " + g.status.Target.FullName + "."
	}
	return true, ""
}

func (m Model) gardenStartValidate() (Model, tea.Cmd) {
	d := &m.garden.drafts
	if ok, why := m.canValidate(); !ok {
		d.notice = why
		return m, nil
	}
	d.notice = ""
	ctx, seq := m.garden.begin(busyPreviewValidate)
	api, id := m.garden.api, uint64(d.opened)
	if m.garden.planrev.stage == planValidate {
		id = uint64(m.garden.board.opened)
	}
	return m, func() tea.Msg {
		v, err := api.PreviewValidate(ctx, id)
		return gardenValidatePreviewMsg{seq: seq, view: v, err: err}
	}
}

// gardenRunValidate records the previewed approval. Like every write it is never cancelled.
func (m Model) gardenRunValidate(p gardenapi.ValidatePreviewView) (Model, tea.Cmd) {
	_, seq := m.garden.begin(busyValidate)
	api := m.garden.api
	return m, func() tea.Msg {
		v, err := api.Validate(context.Background(), p)
		return gardenValidateMsg{seq: seq, view: v, err: err}
	}
}

func (m Model) onGardenValidatePreview(msg gardenValidatePreviewMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		d.notice = fail.Message
		return m.gardenFailed(busyPreviewValidate, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	p := msg.view
	d.validate, d.validated, d.stage, d.btn = &p, nil, draftsValidate, 0
	m.shellFocus = shellBody
	return m, nil
}

func (m Model) onGardenValidate(msg gardenValidateMsg) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	fail, ok := g.settle(msg.seq, msg.err)
	if !ok {
		return m, nil
	}
	if fail != nil {
		if fail.Code == gardenapi.CodeSpecChanged {
			// The head moved, so nothing was approved: show the Issue again, read afresh.
			d.validate, d.validated, d.stage = nil, nil, draftsDetail
			d.notice = fail.Message + " " + fail.Next
			return m.gardenLoadWork()
		}
		return m.gardenFailed(busyValidate, fail)
	}
	if !g.gate.Accept(msg.view.Stamp) {
		g.err = staleReply
		return m, nil
	}
	v := msg.view
	if v.State != gardenapi.ValidateDone {
		// The approval may or may not be on GitHub: stay here so validating again is one key away.
		d.validated = &v
		return m, nil
	}
	number, ordinal := int64(0), 0
	if d.validate != nil {
		number, ordinal = d.validate.Number, d.validate.Gate.Ordinal
	}
	d.notice = fmt.Sprintf("Validated #%d: specification s%d is approved. The Issue left Drafts and is Approved on the Board.", number, ordinal)
	if g.planrev.stage == planValidate {
		d.validate, d.validated, d.stage = nil, nil, draftsList
		g.planrev.stage = planReview
		return m.gardenLoadPlanReview("Specification revalidated. Review a plan for this exact source before approving.", true)
	}
	d.validate, d.validated, d.opened, d.docScroll, d.stage = nil, nil, 0, 0, draftsList
	m.shellFocus = shellBody
	return m.gardenLoadWork()
}

// gardenValidateKey handles the validation screen: Back has the first focus.
func (m Model) gardenValidateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	can := d.validate != nil && d.validate.CanValidate()
	back := func() (tea.Model, tea.Cmd) {
		if g.planrev.stage == planValidate {
			d.validate, d.validated, d.stage = nil, nil, draftsList
			g.planrev.stage = planReview
			return m.gardenLoadPlanReview("", false)
		}
		reload := d.validated != nil
		d.validate, d.validated, d.stage = nil, nil, draftsDetail
		g.err = nil
		if reload {
			return m.gardenLoadWork()
		}
		return m, nil
	}
	switch msg.String() {
	case "left", "right", "h", "l", "tab", "shift+tab":
		if can {
			d.btn = 1 - d.btn
		}
	case "esc":
		return back()
	case "enter":
		if !can || d.btn == 0 {
			return back()
		}
		return m.gardenRunValidate(*d.validate)
	}
	return m, nil
}

// gateLabel names a gate state for the Issue document and the inspector.
func gateLabel(s approve.State) string {
	switch s {
	case approve.StateFinishing:
		return "approval recorded, not yet applied to the Issue"
	case approve.StateApproved:
		return "approved"
	case approve.StateStale:
		return "approval stale"
	case approve.StateRevoked:
		return "approval revoked"
	case approve.StateUnverified:
		return "approval unverified"
	case approve.StateAttention:
		return "needs attention"
	}
	return "draft, not validated"
}
