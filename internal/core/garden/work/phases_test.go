package work

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

func exactPair(t *testing.T) gardensync.Item {
	t.Helper()
	it := accepted(t, 1, garden.StageReady, false)
	v := &it.Verification
	spec := v.Heads[0]
	text := planrev.Compose("## Assumptions\nKnown\n## Touched areas\nCore\n## Steps\nImplement\n## Risks\nNone\n## Tests\nRun tests\n## Acceptance mapping\nAC-1: implement", plan.SpecRef{RevisionHash: spec.Hash, CommentID: spec.CommentID, Ordinal: 1, Fingerprint: v.CurrentSpecFingerprint}, plan.Base{Commit: strings.Repeat("a", 40)})
	rev := garden.Revision{Kind: garden.RevisionPlan, Hash: garden.ContentHash(text), Content: text, CommentID: 3, AuthorID: "1001", SourceSpecFingerprint: v.CurrentSpecFingerprint}
	v.Revisions = append(slices.Clone(v.Revisions), rev)
	v.Heads = append(slices.Clone(v.Heads), rev)
	v.PlanDecisions = []garden.PlanDecision{{CommentID: 4, AuthorID: "1001", RevisionHash: rev.Hash, RevisionCommentID: 3, SourceSpecFingerprint: rev.SourceSpecFingerprint, Decision: garden.DecisionApproved}}
	if !planrev.FromVerification(it.Issue, *v).Ready {
		t.Fatalf("fixture is not exact Ready: %+v", planrev.FromVerification(it.Issue, *v))
	}
	return it
}

func TestColumnPhaseMapping(t *testing.T) {
	for c, want := range []Phase{Backlog, PlanningPhase, PlanningPhase, ReadyPhase, Building, Review, Review, DonePhase, PhaseCount} {
		if got := Column(c).Phase(); got != want {
			t.Errorf("%s: %s want %s", Column(c), got, want)
		}
	}
	if got := (Collection{}).LaneTitles(ModePhases); !slices.Equal(got, []string{"Backlog", "Planning", "Ready", "Building", "Review", "Done"}) {
		t.Fatal(got)
	}
}
func TestPlannerOnlyUpgradesApproved(t *testing.T) {
	for _, e := range []RunEvidence{{Live: true}, {PlanReady: true}, {Activity: ActivityWorking}, {}} {
		for _, tc := range []struct {
			stage garden.Stage
			plan  bool
			col   Column
		}{{garden.StageReady, false, Approved}, {garden.StageReady, true, PlanReview}, {garden.StageInProgress, false, Running}, {garden.StageReview, false, HumanReview}, {garden.StageDone, false, Done}} {
			it := accepted(t, 1, tc.stage, tc.plan)
			r := Select([]gardensync.Item{it}, Query{}, map[garden.IssueID]Evidence{1: {Planner: &e}}).Records[0]
			want := tc.col
			if want == Approved && (e.Live || e.PlanReady) {
				want = Planning
			}
			if r.Column != want {
				t.Errorf("%+v/%+v: %s", tc, e, r.Column)
			}
		}
		it := exactPair(t)
		if r := Select([]gardensync.Item{it}, Query{}, map[garden.IssueID]Evidence{1: {Planner: &e}}).Records[0]; r.Column != Ready {
			t.Fatal("local evidence displaced verified Ready")
		}
	}
}
func TestBlockerProof(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		evidence                       Evidence
		plan, ready, retained, revoked bool
		phase                          Phase
		reason                         string
	}{
		{name: "implementation", evidence: Evidence{Implementation: &RunEvidence{State: "failed"}}, ready: true, phase: Building, reason: "implementation attempt failed"},
		{name: "handoff", evidence: Evidence{Implementation: &RunEvidence{State: "awaiting-human-review", AwaitingReview: true}}, phase: Review, reason: "implementation attempt awaiting-human-review"},
		{name: "approved pair", ready: true, evidence: Evidence{Planner: &RunEvidence{}}, phase: ReadyPhase, reason: "plan approved"},
		{name: "revision", plan: true, evidence: Evidence{Planner: &RunEvidence{}}, phase: PlanningPhase, reason: "plan revision published"},
		{name: "planner", evidence: Evidence{Planner: &RunEvidence{}}, phase: PlanningPhase, reason: "local planner attempt"},
		{name: "acceptance", phase: Backlog, reason: "specification accepted"},
		{name: "moved acceptance", retained: true, phase: PhaseCount, reason: "no proven phase"},
		{name: "retained with evidence", retained: true, evidence: Evidence{Implementation: &RunEvidence{State: "failed"}}, phase: Building, reason: "implementation attempt failed; approval not current"},
		{name: "revoked current acceptance with prior snapshot", retained: true, revoked: true, phase: PhaseCount, reason: "no proven phase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := accepted(t, 1, garden.StageBlocked, tc.plan)
			if tc.ready {
				it = exactPair(t)
				it.Issue.Labels[2] = garden.FormatStageLabel(garden.StageBlocked)
			}
			if tc.retained {
				it.Verification.SupersededApprovals = slices.Clone(it.Verification.ValidApprovals)
				it.Verification.ValidApprovals = nil
				it.Verification.AcceptedSpecHash = ""
			}
			if tc.revoked {
				it.Verification.RevokedApprovals = []garden.Approval{{Revoked: true}}
			}
			c := Select([]gardensync.Item{it}, Query{}, map[garden.IssueID]Evidence{1: tc.evidence})
			if len(c.Records) != 1 {
				t.Fatalf("excluded retained blocker: %+v", c)
			}
			r := c.Records[0]
			if r.Accepted == tc.retained {
				t.Fatalf("acceptance %t, retained %t", r.Accepted, tc.retained)
			}
			if r.Column != Blocked || r.Phase != tc.phase || r.Reason != tc.reason || r.Placed != (tc.phase != PhaseCount) {
				t.Fatalf("%+v", r)
			}
			if len(c.Columns[Blocked]) != 1 {
				t.Fatal("detailed lost blocker")
			}
			if r.Placed {
				if len(c.Phases[r.Phase]) != 1 {
					t.Fatal("phase lost blocker")
				}
			} else if len(c.Unplaced) != 1 {
				t.Fatal("unplaced lost blocker")
			}
		})
	}
}
func TestRetainedApprovedNeverUpgradesToPlanning(t *testing.T) {
	it := accepted(t, 1, garden.StageReady, false)
	it.Verification.SupersededApprovals = slices.Clone(it.Verification.ValidApprovals)
	it.Verification.ValidApprovals = nil
	it.Verification.AcceptedSpecHash = ""
	c := Select([]gardensync.Item{it}, Query{}, map[garden.IssueID]Evidence{1: {Planner: &RunEvidence{Live: true}}})
	if len(c.Records) != 1 || c.Records[0].Column == Planning {
		t.Fatalf("retained approval upgraded: %+v", c.Records)
	}
}
func TestLaneTitleOutOfRange(t *testing.T) {
	var c Collection
	if got := c.LaneTitle(ModePhases, Column(6)); got != "" {
		t.Fatalf("title %q", got)
	}
	if got := c.LaneTitle(ModePhases, Column(1)); got != "Planning" {
		t.Fatalf("title %q", got)
	}
}
func TestEvidenceCannotCreateOrAdmitWork(t *testing.T) {
	it := accepted(t, 1, garden.StageReady, false)
	deleted := accepted(t, 2, garden.StageReady, false)
	deleted.Deleted = true
	excluded := accepted(t, 3, garden.StageDraft, false)
	e := map[garden.IssueID]Evidence{}
	for _, id := range []garden.IssueID{1, 2, 3, 99} {
		e[id] = Evidence{Planner: &RunEvidence{Live: true, PlanReady: true}, Implementation: &RunEvidence{AwaitingReview: true}}
	}
	for _, q := range []Query{{}, {Type: garden.TypeBug}} {
		items := []gardensync.Item{it, deleted, excluded}
		a, b := Select(items, q, nil), Select(items, q, e)
		if a.Total != b.Total || a.Excluded != b.Excluded || len(a.Records) != len(b.Records) {
			t.Fatal("evidence manufactured eligibility")
		}
		for _, r := range b.Records {
			if r.Item.Issue.ID != 1 || r.Column == Ready {
				t.Fatal("evidence manufactured Ready or an Issue")
			}
		}
	}
}
func TestLaneCountAndSelectionMemory(t *testing.T) {
	c := Select([]gardensync.Item{accepted(t, 1, garden.StageDone, false)}, Query{}, nil)
	for _, mode := range []Mode{ModePhases, ModeDetailed} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			s := Selection{Mode: mode}
			s.Reconcile(c)
			s.EnsureVisible(c, 1, 3)
			before := s.Clone()
			extra := Record{Column: Blocked, Item: gardensync.Item{Issue: garden.Issue{ID: 2}}}
			more := c
			more.Unplaced = []Record{extra}
			more.Columns[Blocked] = []Record{extra}
			if mode == ModePhases && len(more.Lanes(mode)) != 7 {
				t.Fatal("unplaced not appended")
			}
			if mode == ModeDetailed && len(more.Lanes(mode)) != 9 {
				t.Fatal("detailed lane count")
			}
			if !reflect.DeepEqual(c.Lanes(ModePhases), more.Lanes(ModePhases)[:6]) {
				t.Fatal("unplaced shifted phases")
			}
			s.Reconcile(more)
			s.EnsureVisible(more, 1, 3)
			if s.ID != 1 || s.Column != before.Column {
				t.Fatal("new lane stole focus")
			}
			if !s.Focus(more, 2) {
				t.Fatal("cannot focus blocker")
			}
			s.EnsureVisible(more, 1, 3)
			s.Reconcile(c)
			s.EnsureVisible(c, 1, 3)
			if s.ID != 1 || int(s.Column) < s.Window || int(s.Column) >= s.Window+3 || len(s.Rows) != len(c.Lanes(mode)) {
				t.Fatalf("fallback %+v", s)
			}
			s.Move(c, -int(s.Column), 0)
			if s.ID != 0 || s.Reconcile(c) != "" {
				t.Fatal("empty lane lost focus")
			}
		})
	}
}
