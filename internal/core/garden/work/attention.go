package work

import "github.com/Tiago-0liveira/bonsai/internal/core/garden"

type Need int

const (
	NeedNone Need = iota
	NeedBlocker
	NeedInput
	NeedHumanReview
	NeedPlanApproval
	NeedPlanPublish
	NeedStart
)

// Attention requires a proven decision or request: offline and idle data are
// not user requests, and stale local evidence cannot supply one.
func Attention(r Record) (Need, string) {
	if r.Column == Blocked {
		if !r.Placed {
			return NeedBlocker, "Blocked: no proven phase"
		}
		return NeedBlocker, "Blocked: " + r.Reason
	}
	e := r.Evidence
	if !e.Stale {
		if a := e.Implementation; a != nil && a.Live && a.Activity == ActivityNeedsInput {
			return NeedInput, "Implementation needs input"
		}
		if a := e.Planner; a != nil && a.Live && a.Activity == ActivityNeedsInput {
			return NeedInput, "Planner needs input"
		}
	}
	if r.Column == HumanReview || !e.Stale && e.Implementation != nil && e.Implementation.AwaitingReview {
		return NeedHumanReview, "Ready for your review"
	}
	if r.Column == PlanReview && r.Accepted {
		return NeedPlanApproval, "Plan needs approval"
	}
	if !e.Stale && r.Accepted && e.Planner != nil && e.Planner.PlanReady && (r.Column == Approved || r.Column == Planning) {
		return NeedPlanPublish, "Plan ready to publish"
	}
	if r.Column == Ready && r.Accepted && r.Item.Classification.Stage == garden.StageReady && e.Implementation == nil {
		return NeedStart, "Ready to start"
	}
	return NeedNone, ""
}

// NeedsYou preserves the caller's sorted order.
func NeedsYou(rows []Record) []Record {
	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		if need, _ := Attention(r); need != NeedNone {
			out = append(out, r)
		}
	}
	return out
}

type Summary struct{ Active, NeedsYou, Blocked int }

func (c Collection) Summarize() Summary {
	var s Summary
	for _, r := range c.Records {
		e := r.Evidence
		if !e.Stale && (e.Planner != nil && e.Planner.Live || e.Implementation != nil && e.Implementation.Live) {
			s.Active++
		}
		if n, _ := Attention(r); n != NeedNone {
			s.NeedsYou++
		}
		if r.Column == Blocked {
			s.Blocked++
		}
	}
	return s
}

type Group struct {
	Phase   Phase
	Records []Record
}

func GroupByPhase(rows []Record) []Group {
	var groups [PhaseCount + 1][]Record
	for _, r := range rows {
		p := r.Phase
		if !r.Placed {
			p = PhaseCount
		}
		groups[p] = append(groups[p], r)
	}
	out := make([]Group, 0, len(groups))
	for p, records := range groups {
		if len(records) > 0 {
			out = append(out, Group{Phase(p), records})
		}
	}
	return out
}
