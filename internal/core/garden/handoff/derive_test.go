package handoff

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func hx(c string, n int) string { return strings.Repeat(c, n) }

func codeID() Identity {
	return Identity{AttemptID: "att-1", Deliverable: garden.DeliverableCode, SpecRevision: hx("a", 64), SpecApprovalID: 3, PlanRevision: hx("b", 64),
		PlanDecisionID: 5, BaseSHA: hx("1", 40), FinalHead: hx("2", 40), FinalTree: hx("3", 40), ReviewPass: "rp-" + hx("c", 32), ReviewPassRevision: 2}
}

func logOf(entries ...garden.HandoffEntry) garden.HandoffLog {
	return garden.HandoffLog{Entries: entries}
}

func entry(id int64, rec garden.HandoffRecord) garden.HandoffEntry {
	return garden.HandoffEntry{CommentID: id, AuthorID: "me", Record: rec}
}

func TestCurrent(t *testing.T) {
	id := codeID()
	acc := entry(10, id.Record(garden.HandoffAccepted))
	for _, tc := range []struct {
		name  string
		log   garden.HandoffLog
		want  Identity
		state DecisionState
		stale []StaleReason
	}{
		{"none", logOf(), id, DecisionNone, nil},
		{"other attempt", logOf(entry(10, func() garden.HandoffRecord { r := id.Record(garden.HandoffAccepted); r.AttemptID = "x"; return r }())), id, DecisionNone, nil},
		{"current", logOf(acc), id, DecisionCurrent, nil},
		{"head changed", logOf(acc), func() Identity { i := id; i.FinalHead = hx("9", 40); return i }(), DecisionStale, []StaleReason{StaleHead}},
		{"head and tree changed", logOf(acc), func() Identity { i := id; i.FinalHead, i.FinalTree = hx("9", 40), hx("8", 40); return i }(), DecisionStale, []StaleReason{StaleHead, StaleTree}},
		{"tree changed", logOf(acc), func() Identity { i := id; i.FinalTree = hx("8", 40); return i }(), DecisionStale, []StaleReason{StaleTree}},
		{"spec changed", logOf(acc), func() Identity { i := id; i.SpecRevision = hx("f", 64); return i }(), DecisionStale, []StaleReason{StaleSpec}},
		{"spec reapproved", logOf(acc), func() Identity { i := id; i.SpecApprovalID = 30; return i }(), DecisionStale, []StaleReason{StaleSpec}},
		{"plan changed", logOf(acc), func() Identity { i := id; i.PlanDecisionID = 50; return i }(), DecisionStale, []StaleReason{StalePlan}},
		{"review changed", logOf(acc), func() Identity { i := id; i.ReviewPassRevision = 3; return i }(), DecisionStale, []StaleReason{StaleReview}},
		{"base changed", logOf(acc), func() Identity { i := id; i.BaseSHA = hx("7", 40); return i }(), DecisionStale, []StaleReason{StaleAttempt}},
		{"latest wins", logOf(acc, entry(11, id.Record(garden.HandoffChangesRequested))), id, DecisionCurrent, nil},
		{"revoked", logOf(func() garden.HandoffEntry { e := acc; e.Revoked, e.RevokedBy = true, 12; return e }(), entry(12, garden.HandoffRecord{Decision: garden.HandoffRevoked})), id, DecisionRevoked, nil},
		{"accepted again after revoke", logOf(func() garden.HandoffEntry { e := acc; e.Revoked, e.RevokedBy = true, 12; return e }(), entry(12, garden.HandoffRecord{Decision: garden.HandoffRevoked}), entry(13, id.Record(garden.HandoffAccepted))), id, DecisionCurrent, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Current(tc.log, tc.want)
			if got.State != tc.state || !reflect.DeepEqual(got.Stale, tc.stale) {
				t.Fatalf("got %s %v, want %s %v", got.State, got.Stale, tc.state, tc.stale)
			}
		})
	}
}

func TestCurrentNonCodeWithoutAttempt(t *testing.T) {
	id := Identity{Deliverable: garden.DeliverableNonCode, SpecRevision: hx("a", 64), SpecApprovalID: 3, PlanRevision: hx("b", 64), PlanDecisionID: 5}
	done := entry(10, id.Record(garden.HandoffCompleted))
	if got := Current(logOf(entry(9, codeID().Record(garden.HandoffAccepted)), done), id); got.State != DecisionCurrent || got.Entry.CommentID != 10 {
		t.Fatalf("got %+v", got)
	}
}

var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func merged() *PRObservation {
	at := now.Add(-time.Hour)
	return &PRObservation{Number: 7, Merged: true, MergedAt: &at, MergeCommitSHA: hx("5", 40), HeadSHA: hx("2", 40), FetchedAt: now}
}

// bound is an acceptance recorded against pull request number.
func bound(id Identity, number int64) DecisionStatus {
	id.PRNumber, id.PRHeadSHA = number, id.FinalHead
	return DecisionStatus{State: DecisionCurrent, Entry: &garden.HandoffEntry{CommentID: 10, Record: id.Record(garden.HandoffAccepted)}}
}

func TestDeriveCode(t *testing.T) {
	id := codeID()
	accepted := DecisionStatus{State: DecisionCurrent, Entry: &garden.HandoffEntry{CommentID: 10, Record: id.Record(garden.HandoffAccepted)}}
	linked := &LinkedPR{Number: 7, HeadSHA: id.FinalHead}
	base := Input{Identity: id, AttemptState: AwaitingReviewState, Decision: accepted, PR: linked, Observed: merged(), Now: now}
	with := func(f func(*Input)) Input { in := base; f(&in); return in }
	for _, tc := range []struct {
		name    string
		in      Input
		state   CompletionState
		reasons []Reason
	}{
		{"squash merge is done", base, CompletionDone, nil},
		{"dirty", with(func(i *Input) { i.Dirty = true }), CompletionNotReady, []Reason{ReasonDirtyWorktree}},
		{"attempt running", with(func(i *Input) { i.AttemptState = "running" }), CompletionNotReady, []Reason{ReasonAttemptNotDone}},
		{"head unknown", with(func(i *Input) { i.Identity.FinalHead = "" }), CompletionNotReady, []Reason{ReasonHeadUnknown}},
		{"no decision", with(func(i *Input) { i.Decision = DecisionStatus{State: DecisionNone} }), CompletionAwaitingDecision, nil},
		{"revoked", with(func(i *Input) { i.Decision = DecisionStatus{State: DecisionRevoked} }), CompletionAwaitingDecision, []Reason{ReasonAcceptanceRevoked}},
		{"stale", with(func(i *Input) {
			i.Decision = DecisionStatus{State: DecisionStale, Stale: []StaleReason{StaleHead, StaleSpec}}
		}), CompletionStale,
			[]Reason{ReasonAcceptanceStale, "head-changed", "spec-changed"}},
		{"changes requested", with(func(i *Input) {
			i.Decision = DecisionStatus{State: DecisionCurrent, Entry: &garden.HandoffEntry{Record: id.Record(garden.HandoffChangesRequested)}}
		}), CompletionChangesRequested, nil},
		{"no PR yet", with(func(i *Input) { i.PR, i.Observed = nil, nil }), CompletionAccepted, []Reason{ReasonPRNotPublished}},
		{"offline", with(func(i *Input) { i.Observed, i.ReadErr = nil, ReadOffline }), CompletionUnknown, []Reason{ReasonPROffline}},
		{"read failed with stale observation", with(func(i *Input) { i.ReadErr = ReadUnavailable }), CompletionUnknown, []Reason{ReasonPRReadFailed}},
		{"not found", with(func(i *Input) { i.Observed, i.ReadErr = nil, ReadNotFound }), CompletionUnknown, []Reason{ReasonPRNotFound}},
		{"no observation", with(func(i *Input) { i.Observed = nil }), CompletionUnknown, []Reason{ReasonPRReadFailed}},
		{"wrong PR", with(func(i *Input) { o := *merged(); o.Number = 8; i.Observed = &o }), CompletionBlocked, []Reason{ReasonWrongPR}},
		{"closed unmerged", with(func(i *Input) { i.Observed = &PRObservation{FetchedAt: now, Number: 7, HeadSHA: id.FinalHead} }), CompletionBlocked, []Reason{ReasonPRClosedUnmerged}},
		{"open", with(func(i *Input) {
			i.Observed = &PRObservation{FetchedAt: now, Number: 7, Open: true, HeadSHA: id.FinalHead}
		}), CompletionAccepted, []Reason{ReasonPRNotMerged}},
		{"open with pushed head", with(func(i *Input) {
			i.Observed = &PRObservation{FetchedAt: now, Number: 7, Open: true, HeadSHA: hx("9", 40)}
		}), CompletionStale, []Reason{ReasonPRHeadChanged}},
		{"open with other linked head", with(func(i *Input) {
			i.PR = &LinkedPR{Number: 7, HeadSHA: hx("9", 40)}
			i.Observed = &PRObservation{FetchedAt: now, Number: 7, Open: true, HeadSHA: id.FinalHead}
		}), CompletionStale, []Reason{ReasonPRHeadChanged}},
		{"merged other head", with(func(i *Input) { o := *merged(); o.HeadSHA = hx("9", 40); i.Observed = &o }), CompletionBlocked, []Reason{ReasonMergedHeadDiffers}},
		{"linked head differs", with(func(i *Input) { i.PR = &LinkedPR{Number: 7, HeadSHA: hx("9", 40)} }), CompletionBlocked, []Reason{ReasonMergedHeadDiffers}},
		{"unfetched observation", with(func(i *Input) { o := *merged(); o.FetchedAt = time.Time{}; i.Observed = &o }), CompletionUnknown, []Reason{ReasonPRReadStale}},
		{"old observation", with(func(i *Input) {
			o := *merged()
			o.FetchedAt = now.Add(-MaxPRObservationAge - time.Second)
			i.Observed = &o
		}), CompletionUnknown, []Reason{ReasonPRReadStale}},
		{"observation at the limit", with(func(i *Input) { o := *merged(); o.FetchedAt = now.Add(-MaxPRObservationAge); i.Observed = &o }), CompletionDone, nil},
		{"merged into other base", with(func(i *Input) {
			i.PR = &LinkedPR{Number: 7, HeadSHA: id.FinalHead, BaseRef: "main"}
			o := *merged()
			o.BaseRef = "release"
			i.Observed = &o
		}), CompletionBlocked, []Reason{ReasonWrongBase}},
		{"merged into linked base", with(func(i *Input) {
			i.PR = &LinkedPR{Number: 7, HeadSHA: id.FinalHead, BaseRef: "main"}
			o := *merged()
			o.BaseRef = "main"
			i.Observed = &o
		}), CompletionDone, nil},
		{"base unknown on one side", with(func(i *Input) {
			i.PR = &LinkedPR{Number: 7, HeadSHA: id.FinalHead, BaseRef: "main"}
		}), CompletionDone, nil},
		{"accepted on PR 7 merged PR 8", with(func(i *Input) {
			i.Decision = bound(id, 7)
			i.PR = &LinkedPR{Number: 8, HeadSHA: id.FinalHead}
			o := *merged()
			o.Number = 8
			i.Observed = &o
		}), CompletionBlocked, []Reason{ReasonWrongPR}},
		{"bound PR merged with other head", with(func(i *Input) {
			d := bound(id, 7)
			d.Entry.Record.PRHeadSHA = hx("9", 40)
			i.Decision = d
		}), CompletionBlocked, []Reason{ReasonMergedHeadDiffers}},
		{"bound PR merged", with(func(i *Input) { i.Decision = bound(id, 7) }), CompletionDone, nil},
		{"accepted before PR then merged with same head", with(func(i *Input) { i.Decision = accepted }), CompletionDone, nil},
		{"merge commit equals head", with(func(i *Input) { o := *merged(); o.MergeCommitSHA = id.FinalHead; i.Observed = &o }), CompletionDone, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.in)
			if got.State != tc.state || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %s %v, want %s %v", got.State, got.Reasons, tc.state, tc.reasons)
			}
			if got.State == CompletionDone && (got.Evidence.Source != "github-pr" || got.Evidence.PRNumber != 7 || got.Evidence.MergeCommitSHA == "" || got.Evidence.MergedAt == nil || got.Evidence.CommentID != 10) {
				t.Fatalf("evidence = %+v", got.Evidence)
			}
		})
	}
}

func TestDeriveNonCode(t *testing.T) {
	id := Identity{Deliverable: garden.DeliverableNonCode, SpecRevision: hx("a", 64), SpecApprovalID: 3, PlanRevision: hx("b", 64), PlanDecisionID: 5}
	completed := DecisionStatus{State: DecisionCurrent, Entry: &garden.HandoffEntry{CommentID: 20, Record: id.Record(garden.HandoffCompleted)}}
	for _, tc := range []struct {
		name    string
		in      Input
		state   CompletionState
		reasons []Reason
	}{
		{"completed without PR", Input{Identity: id, Decision: completed, Now: now}, CompletionDone, nil},
		{"completed with attempt at base", Input{Identity: func() Identity { i := id; i.BaseSHA, i.FinalHead = hx("1", 40), hx("1", 40); return i }(), Decision: completed, Now: now}, CompletionDone, nil},
		{"attempt with both commits equal", Input{Identity: func() Identity {
			i := id
			i.AttemptID, i.BaseSHA, i.FinalHead = "a1", hx("1", 40), hx("1", 40)
			return i
		}(), Decision: completed, Now: now}, CompletionDone, nil},
		{"attempt with unobserved head", Input{Identity: func() Identity { i := id; i.AttemptID, i.BaseSHA = "a1", hx("1", 40); return i }(), Decision: completed, Now: now}, CompletionUnknown, []Reason{ReasonHeadUnknown}},
		{"attempt with unobserved base", Input{Identity: func() Identity { i := id; i.AttemptID, i.FinalHead = "a1", hx("1", 40); return i }(), Decision: completed, Now: now}, CompletionUnknown, []Reason{ReasonHeadUnknown}},
		{"attempt with nothing observed", Input{Identity: func() Identity { i := id; i.AttemptID = "a1"; return i }(), Decision: completed, Now: now}, CompletionUnknown, []Reason{ReasonHeadUnknown}},
		{"code changed", Input{Identity: func() Identity { i := id; i.BaseSHA, i.FinalHead = hx("1", 40), hx("2", 40); return i }(), Decision: completed, Now: now}, CompletionBlocked, []Reason{ReasonHasCodeChanges}},
		{"dirty", Input{Identity: id, Dirty: true, Decision: completed, Now: now}, CompletionBlocked, []Reason{ReasonHasCodeChanges}},
		{"no completion", Input{Identity: id, Decision: DecisionStatus{State: DecisionNone}, Now: now}, CompletionAwaitingDecision, nil},
		{"stale completion", Input{Identity: id, Decision: DecisionStatus{State: DecisionStale, Stale: []StaleReason{StalePlan}}, Now: now}, CompletionStale, []Reason{ReasonAcceptanceStale, "plan-changed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.in)
			if got.State != tc.state || !reflect.DeepEqual(got.Reasons, tc.reasons) {
				t.Fatalf("got %s %v, want %s %v", got.State, got.Reasons, tc.state, tc.reasons)
			}
			if got.State == CompletionDone && (got.Evidence.Source != "handoff-record" || got.Evidence.CommentID != 20 || got.Evidence.PRNumber != 0) {
				t.Fatalf("evidence = %+v", got.Evidence)
			}
		})
	}
}

func TestCurrentPRBinding(t *testing.T) {
	id := codeID()
	pr := func(n int64) Identity { i := id; i.PRNumber, i.PRHeadSHA = n, id.FinalHead; return i }
	for _, tc := range []struct {
		name  string
		rec   Identity
		want  Identity
		state DecisionState
		stale []StaleReason
	}{
		{"bound and same", pr(7), pr(7), DecisionCurrent, nil},
		{"bound to another PR", pr(7), pr(8), DecisionStale, []StaleReason{StalePR}},
		{"bound PR head moved", pr(7), func() Identity { i := pr(7); i.PRHeadSHA = hx("9", 40); return i }(), DecisionStale, []StaleReason{StalePR}},
		{"bound PR vanished", pr(7), id, DecisionStale, []StaleReason{StalePR}},
		{"recorded before any PR", id, pr(7), DecisionCurrent, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Current(logOf(entry(10, tc.rec.Record(garden.HandoffAccepted))), tc.want)
			if got.State != tc.state || !reflect.DeepEqual(got.Stale, tc.stale) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
