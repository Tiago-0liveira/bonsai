package gardenbackend

import (
	"context"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const backendPlan = "## Assumptions\nPer project.\n\n## Touched areas\ninternal/budget\n\n## Steps\n1. Add it.\n\n## Tests\nTable test.\n\n## Risks\nRaces.\n\n## Acceptance mapping\nAC-1 -> step 1."

// reviewHarness approves the Issue and records one planner attempt, as a user who asked for a plan.
func reviewHarness(t *testing.T) (*harness, *gardenfake.Fake, uint64, *stubAgents, string) {
	t.Helper()
	h, f, id, ag, _ := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, err := h.b.PreviewPlanner(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	return h, f, id, ag, v.Attempt.ID
}

func commentWrites(f *gardenfake.Fake) int {
	n := 0
	for _, l := range f.WriteLog() {
		if strings.HasPrefix(l, "comment:") {
			n++
		}
	}
	return n
}

func TestPublishReviewAndDecideThroughTheBackend(t *testing.T) {
	h, f, id, ag, attempt := reviewHarness(t)
	ctx := context.Background()
	if rv, err := h.b.PlanReview(ctx, id); err != nil || len(rv.Review.Revisions) != 0 || !rv.Review.CanPublish() || rv.Actor != viewerName {
		t.Fatalf("empty review = %+v, %v", rv, err)
	}
	// Previewing writes nothing and names the exact revision.
	before := commentWrites(f)
	pv, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: backendPlan})
	if err != nil || !pv.CanPublish() || pv.Preview.Ordinal != 1 || pv.Preview.Hash != garden.ContentHash(pv.Preview.Content) || pv.OperationID == "" {
		t.Fatalf("preview = %+v, %v", pv.Preview.Blockers, err)
	}
	if commentWrites(f) != before {
		t.Fatal("previewing wrote")
	}
	res, err := h.b.PublishPlan(ctx, pv)
	if err != nil || res.State != gardenapi.PlanWriteDone || res.Hash != pv.Preview.Hash || res.Ordinal != 1 || commentWrites(f) != before+1 {
		t.Fatalf("publish = %+v, %v", res, err)
	}
	// The board stops flagging a plan once it was published from here.
	if got, err := h.b.deps.Plans.Get(ctx, attempt); err != nil || got.PlanPublishedAt == nil {
		t.Fatalf("attempt not marked published: %+v, %v", got.PlanPublishedAt, err)
	}
	// Publishing the same preview again finds the recorded revision.
	if again, err := h.b.PublishPlan(ctx, pv); err != nil || !again.Replayed || again.CommentID != res.CommentID || commentWrites(f) != before+1 {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	rv, err := h.b.PlanReview(ctx, id)
	if err != nil || rv.Review.Current == nil || rv.Review.Latest.Hash != res.Hash || rv.Review.Approved != nil || rv.Review.Current.BaseCommit != "c0ffee" {
		t.Fatalf("review = %+v, %v", rv.Review.Current, err)
	}
	// Changes requested: a reason is required, and it starts nothing.
	rq := gardenapi.PlanDecisionRequest{IssueID: id, RevisionHash: res.Hash, Fingerprint: pv.Preview.Spec.Fingerprint, Decision: garden.DecisionChangesRequested}
	if _, err := h.b.DecidePlan(ctx, rq); code(err) != gardenapi.CodeInvalidInput {
		t.Fatalf("no reason = %v", err)
	}
	rq.Reason = "Cover the race."
	dv, err := h.b.DecidePlan(ctx, rq)
	if err != nil || dv.State != gardenapi.PlanWriteDone || dv.Ordinal != 1 {
		t.Fatalf("decide = %+v, %v", dv, err)
	}
	if len(ag.started) != 1 {
		t.Fatalf("publishing or deciding started %d extra sessions", len(ag.started)-1)
	}
	rv, _ = h.b.PlanReview(ctx, id)
	if r := rv.Review.Revisions[0]; r.Reason() != "Cover the race." || len(r.Decisions) != 1 {
		t.Fatalf("revision = %+v", r)
	}
	// An approval cannot accept a revision still awaiting requested changes.
	rq.Decision = garden.DecisionApproved
	rq.Stamp, rq.ActorID, rq.SpecHash = rv.Stamp, rv.ActorID, rv.Review.Gate.Head.Hash
	if _, err := h.b.DecidePlan(ctx, rq); code(err) != gardenapi.CodeConflict {
		t.Fatalf("approve = %v", err)
	}
	// An unknown revision, or one about another specification, is refused.
	rq.Decision, rq.RevisionHash = garden.DecisionRejected, strings.Repeat("0", 64)
	if _, err := h.b.DecidePlan(ctx, rq); code(err) != gardenapi.CodeNotFound {
		t.Fatalf("unknown hash = %v", err)
	}
}

func TestRemoteCommentOnlySurfacesAndStartsNothing(t *testing.T) {
	h, f, id, ag, attempt := reviewHarness(t)
	ctx := context.Background()
	pv, _ := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: backendPlan})
	if _, err := h.b.PublishPlan(ctx, pv); err != nil {
		t.Fatal(err)
	}
	f.AppendCommentAs(acmeID, garden.IssueID(id), "reviewer", "9", "Changes requested: this misses the race.")
	rv, err := h.b.PlanReview(ctx, id)
	if err != nil || rv.Review.NewerComments != 1 || rv.Review.Current == nil || !rv.Review.CanPublish() {
		t.Fatalf("review = %+v, %v", rv.Review, err)
	}
	// Reading never starts a run, and a run still needs the local Request AI plan.
	for i := 0; i < 3; i++ {
		if _, err := h.b.PlanReview(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.PreviewPlanner(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if len(ag.started) != 1 {
		t.Fatalf("a remote comment started %d extra planners", len(ag.started)-1)
	}
	p, _ := h.b.PreviewPlanner(ctx, id)
	if !p.CanRequest() && !hasLiveAttempt(p) {
		t.Fatalf("planner unavailable after a remote comment: %q", p.Unavailable)
	}
}

func hasLiveAttempt(p gardenapi.PlannerPreviewView) bool { _, live := p.Live(); return live }

func TestPublishRefusesUnapprovedStaleAndForeign(t *testing.T) {
	h, _, id, _, attempt := reviewHarness(t)
	ctx := context.Background()
	if _, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: "plan-nope", Text: backendPlan}); code(err) != gardenapi.CodeNotFound {
		t.Fatalf("unknown attempt = %v", err)
	}
	if _, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: ""}); code(err) != gardenapi.CodeInvalidInput {
		t.Fatalf("empty = %v", err)
	}
	pv, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: "## Steps\n1. Only this."})
	if err != nil || pv.CanPublish() {
		t.Fatalf("incomplete plan can be published: %+v, %v", pv.Preview.Blockers, err)
	}
	if _, err := h.b.PublishPlan(ctx, pv); code(err) != gardenapi.CodeInvalidInput {
		t.Fatalf("publish incomplete = %v", err)
	}
	// The specification changes after the attempt: its plan answers an older specification.
	d, _ := h.b.Document(ctx, id)
	if _, sv, err := save(t, h, d, completeSpec+"\n\n## Constraints\nNo daemon."); err != nil || sv.State != gardenapi.SaveDone {
		t.Fatalf("second revision: %+v %v", sv, err)
	}
	if _, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: attempt, Text: backendPlan}); code(err) != gardenapi.CodeNotApproved && code(err) != gardenapi.CodeSpecChanged {
		t.Fatalf("stale specification = %v", err)
	}
}

func TestReviewStampsOrderRepliesSoLateOnesAreDropped(t *testing.T) {
	h, _, id, _, _ := reviewHarness(t)
	ctx := context.Background()
	a, err := h.b.PlanReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.b.PlanReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var g gardenapi.Gate
	if !g.Accept(b.Stamp) || g.Accept(a.Stamp) {
		t.Fatal("an older review reply was accepted after a newer one")
	}
}
