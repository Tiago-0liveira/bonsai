package work

import (
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func TestAttention(t *testing.T) {
	waiting := &RunEvidence{Live: true, Activity: ActivityNeedsInput}
	ready := Record{Column: Ready, Accepted: true}
	ready.Item.Classification.Stage = garden.StageReady
	for _, tc := range []struct {
		name   string
		r      Record
		need   Need
		signal string
	}{
		{"blocker placed", Record{Column: Blocked, Placed: true, Reason: "test"}, NeedBlocker, "Blocked: test"},
		{"blocker unplaced", Record{Column: Blocked}, NeedBlocker, "Blocked: no proven phase"},
		{"blocker before waiting", Record{Column: Blocked, Evidence: Evidence{Implementation: waiting}}, NeedBlocker, "Blocked: no proven phase"},
		{"implementation before planner", Record{Evidence: Evidence{Implementation: waiting, Planner: waiting}}, NeedInput, "Implementation needs input"},
		{"planner waiting", Record{Evidence: Evidence{Planner: waiting}}, NeedInput, "Planner needs input"},
		{"input before review", Record{Column: HumanReview, Evidence: Evidence{Planner: waiting}}, NeedInput, "Planner needs input"},
		{"review stage", Record{Column: HumanReview, Evidence: Evidence{Stale: true}}, NeedHumanReview, "Ready for your review"},
		{"local review", Record{Column: Running, Evidence: Evidence{Implementation: &RunEvidence{AwaitingReview: true}}}, NeedHumanReview, "Ready for your review"},
		{"review before approval", Record{Column: PlanReview, Accepted: true, Evidence: Evidence{Implementation: &RunEvidence{AwaitingReview: true}}}, NeedHumanReview, "Ready for your review"},
		{"approval", Record{Column: PlanReview, Accepted: true}, NeedPlanApproval, "Plan needs approval"},
		{"approval before publish", Record{Column: PlanReview, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{PlanReady: true}}}, NeedPlanApproval, "Plan needs approval"},
		{"publish approved", Record{Column: Approved, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{PlanReady: true}}}, NeedPlanPublish, "Plan ready to publish"},
		{"publish planning", Record{Column: Planning, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{PlanReady: true}}}, NeedPlanPublish, "Plan ready to publish"},
		{"start", ready, NeedStart, "Ready to start"},
		{"approval not current", Record{Column: PlanReview}, NeedNone, ""},
		{"publish not current", Record{Column: Planning, Evidence: Evidence{Planner: &RunEvidence{PlanReady: true}}}, NeedNone, ""},
		{"stale waiting", Record{Evidence: Evidence{Stale: true, Implementation: waiting, Planner: waiting}}, NeedNone, ""},
		{"waiting not live", Record{Evidence: Evidence{Implementation: &RunEvidence{Activity: ActivityNeedsInput}}}, NeedNone, ""},
		{"planner waiting not live", Record{Evidence: Evidence{Planner: &RunEvidence{Activity: ActivityNeedsInput}}}, NeedNone, ""},
		{"stale local review", Record{Evidence: Evidence{Stale: true, Implementation: &RunEvidence{AwaitingReview: true}}}, NeedNone, ""},
		{"stale publish", Record{Column: Planning, Accepted: true, Evidence: Evidence{Stale: true, Planner: &RunEvidence{PlanReady: true}}}, NeedNone, ""},
		{"wrong publish column", Record{Column: Running, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{PlanReady: true}}}, NeedNone, ""},
		{"planner not ready", Record{Column: Planning, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{}}}, NeedNone, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n, s := Attention(tc.r); n != tc.need || s != tc.signal {
				t.Fatalf("got %v %q; want %v %q", n, s, tc.need, tc.signal)
			}
		})
	}
	for _, alter := range []func(*Record){
		func(r *Record) { r.Accepted = false },
		func(r *Record) { r.Item.Classification.Stage = garden.StageInProgress },
		func(r *Record) { r.Evidence.Implementation = &RunEvidence{} },
	} {
		r := ready
		alter(&r)
		if n, _ := Attention(r); n != NeedNone {
			t.Fatal("unproven or previously attempted start", n)
		}
	}
	for _, activity := range []Activity{ActivityUnknown, ActivityWorking, ActivityIdle, ActivityExited, ActivityFailed} {
		for _, col := range []Column{Approved, Planning, Running, Reviewing, Done} {
			for _, implementation := range []bool{false, true} {
				r := Record{Column: col, Accepted: true, Evidence: Evidence{Planner: &RunEvidence{Live: true, Activity: activity}}}
				if implementation {
					r.Evidence.Implementation, r.Evidence.Planner = r.Evidence.Planner, nil
				}
				if n, _ := Attention(r); n != NeedNone {
					t.Fatalf("%v/%v/%t is a request", col, activity, implementation)
				}
			}
		}
	}
}

func TestNeedsYouAndFilteredSummary(t *testing.T) {
	c := Collection{Total: 99, Records: []Record{
		{Column: Blocked, Evidence: Evidence{Planner: &RunEvidence{Live: true}}},
		{Column: Running, Evidence: Evidence{Planner: &RunEvidence{Live: true}, Implementation: &RunEvidence{Live: true}}},
		{Column: HumanReview, Evidence: Evidence{Stale: true, Implementation: &RunEvidence{Live: true}}},
		{Column: Planning, Evidence: Evidence{Stale: true, Planner: &RunEvidence{Live: true, Activity: ActivityNeedsInput}}},
	}}
	for i := range c.Records {
		c.Records[i].Item.Issue.ID = garden.IssueID(i + 1)
	}
	rows := []Record{c.Records[2], c.Records[1], c.Records[0], c.Records[3]}
	if got := NeedsYou(rows); !reflect.DeepEqual(got, []Record{c.Records[2], c.Records[0]}) {
		t.Fatal(got)
	}
	s := c.Summarize()
	if s != (Summary{Active: 2, NeedsYou: 2, Blocked: 1}) || s.NeedsYou != len(NeedsYou(rows)) {
		t.Fatal(s)
	}
	if s := (Collection{}).Summarize(); s != (Summary{}) {
		t.Fatal(s)
	}
}

func TestGroupByPhase(t *testing.T) {
	rows := []Record{
		{Column: Blocked, Phase: Building, Placed: true},
		{Column: Done, Phase: DonePhase, Placed: true},
		{Column: Blocked},
		{Column: Running, Phase: Building, Placed: true},
	}
	for i := range rows {
		rows[i].Item.Issue.ID = garden.IssueID(i + 1)
	}
	want := []Group{
		{Building, []Record{rows[0], rows[3]}},
		{DonePhase, []Record{rows[1]}},
		{PhaseCount, []Record{rows[2]}},
	}
	if got := GroupByPhase(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := GroupByPhase(nil); len(got) != 0 {
		t.Fatal(got)
	}
}
