// Package handoff is the pure domain of human handoff and verified completion (plan 13): what a human decided
// about a finished attempt, whether that decision still applies, and when the work counts as done. It runs no
// Git, process or network code; callers supply observations.
package handoff

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Identity is everything a handoff decision is about. Two identities are the same work only when every field
// is equal.
type Identity struct {
	AttemptID          string
	Deliverable        garden.Deliverable
	SpecRevision       string
	SpecApprovalID     int64
	PlanRevision       string
	PlanDecisionID     int64
	BaseSHA            string
	FinalHead          string
	FinalTree          string
	ReviewPass         string
	ReviewPassRevision uint64
	ReportRef          string
	// PRNumber and PRHeadSHA bind the decision to the published pull request. They stay zero for a decision
	// taken before any pull request existed, which then binds by head identity alone.
	PRNumber  int64
	PRHeadSHA string
}

// Record is the canonical record of decision d about this identity.
func (i Identity) Record(d garden.HandoffDecision) garden.HandoffRecord {
	return garden.HandoffRecord{Decision: d, Deliverable: i.Deliverable, AttemptID: i.AttemptID, SpecRevision: i.SpecRevision,
		SpecApprovalID: i.SpecApprovalID, PlanRevision: i.PlanRevision, PlanDecisionID: i.PlanDecisionID, BaseSHA: i.BaseSHA,
		FinalHead: i.FinalHead, FinalTree: i.FinalTree, ReviewPass: i.ReviewPass, ReviewPassRevision: i.ReviewPassRevision, ReportRef: i.ReportRef,
		PRNumber: i.PRNumber, PRHeadSHA: i.PRHeadSHA}
}

func identityOf(r garden.HandoffRecord) Identity {
	return Identity{AttemptID: r.AttemptID, Deliverable: r.Deliverable, SpecRevision: r.SpecRevision, SpecApprovalID: r.SpecApprovalID,
		PlanRevision: r.PlanRevision, PlanDecisionID: r.PlanDecisionID, BaseSHA: r.BaseSHA, FinalHead: r.FinalHead, FinalTree: r.FinalTree,
		ReviewPass: r.ReviewPass, ReviewPassRevision: r.ReviewPassRevision, ReportRef: r.ReportRef,
		PRNumber: r.PRNumber, PRHeadSHA: r.PRHeadSHA}
}

// DecisionState is how the latest handoff decision relates to the current work.
type DecisionState string

const (
	DecisionNone    DecisionState = "none"
	DecisionCurrent DecisionState = "current"
	DecisionStale   DecisionState = "stale"
	DecisionRevoked DecisionState = "revoked"
)

// StaleReason says which part of the identity moved since the decision.
type StaleReason string

const (
	StaleSpec    StaleReason = "spec-changed"
	StalePlan    StaleReason = "plan-changed"
	StaleHead    StaleReason = "head-changed"
	StaleTree    StaleReason = "tree-changed"
	StaleReview  StaleReason = "review-changed"
	StaleAttempt StaleReason = "attempt-changed"
	StalePR      StaleReason = "pr-changed"
)

// DecisionStatus is the latest verified decision for one piece of work.
type DecisionStatus struct {
	State DecisionState
	Entry *garden.HandoffEntry
	Stale []StaleReason
}

// Current picks the latest decision about want's attempt and compares it with want. A revocation is not a
// decision of its own: it marks the entry it withdraws, and when that entry is the latest the work has none.
func Current(log garden.HandoffLog, want Identity) DecisionStatus {
	var latest *garden.HandoffEntry
	for i := range log.Entries {
		e := &log.Entries[i]
		if e.Record.Decision == garden.HandoffRevoked || e.Record.AttemptID != want.AttemptID {
			continue
		}
		if want.AttemptID == "" && e.Record.Deliverable != garden.DeliverableNonCode {
			continue
		}
		latest = e
	}
	if latest == nil {
		return DecisionStatus{State: DecisionNone}
	}
	if latest.Revoked {
		return DecisionStatus{State: DecisionRevoked, Entry: latest}
	}
	got := identityOf(latest.Record)
	// A decision taken before a pull request existed stays valid once one is published, so the pull request
	// only counts when the record bound one.
	prChanged := got.PRNumber != 0 && (got.PRNumber != want.PRNumber || got.PRHeadSHA != want.PRHeadSHA)
	gotCmp, wantCmp := got, want
	gotCmp.PRNumber, gotCmp.PRHeadSHA, wantCmp.PRNumber, wantCmp.PRHeadSHA = 0, "", 0, ""
	if gotCmp == wantCmp && !prChanged {
		return DecisionStatus{State: DecisionCurrent, Entry: latest}
	}
	var stale []StaleReason
	add := func(changed bool, r StaleReason) {
		if changed {
			stale = append(stale, r)
		}
	}
	add(got.SpecRevision != want.SpecRevision || got.SpecApprovalID != want.SpecApprovalID, StaleSpec)
	add(got.PlanRevision != want.PlanRevision || got.PlanDecisionID != want.PlanDecisionID, StalePlan)
	add(got.FinalHead != want.FinalHead, StaleHead)
	add(got.FinalTree != want.FinalTree, StaleTree)
	add(got.ReviewPass != want.ReviewPass || got.ReviewPassRevision != want.ReviewPassRevision || got.ReportRef != want.ReportRef, StaleReview)
	add(prChanged, StalePR)
	add(got.Deliverable != want.Deliverable || got.BaseSHA != want.BaseSHA, StaleAttempt)
	return DecisionStatus{State: DecisionStale, Entry: latest, Stale: stale}
}
