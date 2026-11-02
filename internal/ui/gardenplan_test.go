package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
)

const planBase = "c0ffee1234567890c0ffee1234567890c0ffee12"

// longPlan is a complete plan whose Steps section is long enough to scroll at every size.
func longPlan(variant string) string {
	var steps strings.Builder
	for i := 1; i <= 30; i++ {
		text := "keeps the budget counter consistent"
		if i == 3 {
			text = "is the only step that differs: " + variant
		}
		fmt.Fprintf(&steps, "%d. Step number %d %s.\n", i, i, text)
	}
	return "## Assumptions\nThe ceiling is per project.\n\n## Touched areas\ninternal/budget\n\n## Steps\n" + steps.String() +
		"\n## Tests\nTable test for the ceiling.\n\n## Risks\nRaces on the counter.\n\n## Acceptance mapping\nAC-1 -> steps 1, 2."
}

// scriptPlan publishes (as the viewer) a plan revision on Issue 1 of the planner environment and returns it.
func scriptPlan(t *testing.T, rig plannerRig, body string) garden.Revision {
	t.Helper()
	it := rig.garden.WorkView.Snapshot.Items[0]
	a := approve.Assess(it.Issue, it.Verification)
	if a.Head == nil {
		t.Fatal("fixture has no specification head")
	}
	doc := planrev.Compose(body, plan.SpecRef{RevisionHash: a.Head.Hash, Ordinal: a.Ordinal, Fingerprint: a.Fingerprint}, plan.Base{Commit: planBase, Branch: "main"})
	return rig.garden.AddPlanRevision(uint64(it.Issue.ID), doc, "1001")
}

func openReview(t *testing.T, m Model) Model {
	t.Helper()
	m = plannerPress(t, m, "enter", "r")
	if m.garden.planrev.stage != planReview || m.garden.planrev.view == nil {
		t.Fatalf("r did not open the plan review: stage %d\n%s", m.garden.planrev.stage, view(m))
	}
	return m
}

func TestPlanReviewShowsCurrentLatestAndApprovedSeparately(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		scriptPlan(t, rig, longPlan("first"))
		scriptPlan(t, rig, longPlan("second"))
		m = openReview(t, m)
		out := view(m)
		mustContain(t, "review", out, "PLAN REVIEW", "r2", "Current r2 awaiting decision", "Latest r2", "Approved none", "r1 superseded", "§ Source specification", "Specification: s1")
		mustFit(t, "review", m)
		mustContain(t, "crumbs", strings.Join(m.gardenCrumbs(), " > "), "Plan review")
		if rig.garden.Writes() != 0 || rig.garden.Count("StartPlanner") != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatalf("reading a review wrote or started something: writes=%d", rig.garden.Writes())
		}
		// r1 is superseded: viewing it never makes it the revision under review.
		m = plannerPress(t, m, "[")
		out = view(m)
		mustContain(t, "r1", out, "PLAN REVIEW / r1", "Current r2 awaiting decision", "Latest r2")
		m = plannerPress(t, m, "c")
		if m.garden.planrev.stage != planReview || rig.garden.Count("DecidePlan") != 0 {
			t.Fatalf("a superseded revision could be decided: stage %d", m.garden.planrev.stage)
		}
		mustContain(t, "refusal", view(m), "Only the revision under review can be decided")
	}
}

func TestPlanReviewWithoutRevisionsExplainsHowOneGetsPublished(t *testing.T) {
	m, _ := plannerEnv(t, 80, 24)
	m = openReview(t, m)
	mustContain(t, "empty", view(m), "No plan revision", "u on that attempt")
	mustFit(t, "empty", m)
}

func TestPlanSectionNavigationAndFocusedSearch(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		scriptPlan(t, rig, longPlan("only"))
		m = openReview(t, m)
		first := view(m)
		m = plannerPress(t, m, "right")
		if m.garden.planrev.scroll == 0 {
			t.Fatalf("right did not move to the next section:\n%s", view(m))
		}
		mustContain(t, "section", view(m), "§ Inspected base")
		for i := 0; i < 3; i++ {
			m = plannerPress(t, m, "right")
		}
		mustContain(t, "later section", view(m), "§ Steps")
		if view(m) == first {
			t.Fatal("the view did not change")
		}
		// Search jumps to the match and counts them, without leaving the document.
		m = plannerPress(t, m, "home", "/")
		m = typed(t, m, "step number 17")
		m = plannerPress(t, m, "enter")
		out := view(m)
		mustContain(t, "search", out, "Step number 17", "search \"step number 17\": 1/1")
		mustFit(t, "search", m)
		m = plannerPress(t, m, "esc")
		mustNotContain(t, "cleared", view(m), "search \"step number 17\"")
		m = plannerPress(t, m, "/")
		m = typed(t, m, "step number 1")
		m = plannerPress(t, m, "enter", "n")
		mustContain(t, "next hit", view(m), "2/")
		m = plannerPress(t, m, "N", "N")
		mustContain(t, "wraps", view(m), "search \"step number 1\"")
		mustFit(t, "search again", m)
	}
}

func TestPlanComparisonIsUnifiedByDefaultAndSideBySideOnlyWhenWide(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		scriptPlan(t, rig, longPlan("first"))
		scriptPlan(t, rig, longPlan("second"))
		m = openReview(t, m)
		m = plannerPress(t, m, "d")
		out := view(m)
		mustContain(t, "unified", out, "r1 → r2 unified", "- ", "+ ")
		mustNotContain(t, "unified", out, "│")
		mustFit(t, "unified", m)
		m = plannerPress(t, m, "w")
		out = view(m)
		if sz[0] >= planWideMin {
			mustContain(t, "wide", out, "side by side", "│")
		} else {
			mustContain(t, "narrow", out, "Side by side needs 100 columns", "r1 → r2 unified")
			mustNotContain(t, "narrow", out, "│")
		}
		mustFit(t, "wide", m)
		// The comparison and the document are the same revision; d returns to the document.
		m = plannerPress(t, m, "d")
		mustContain(t, "document", view(m), "document")
	}
}

func TestComparisonOfTheFirstRevisionHasNothingToCompare(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	scriptPlan(t, rig, longPlan("first"))
	m = plannerPress(t, openReview(t, m), "d")
	mustContain(t, "first", view(m), "r1 is the first revision")
	if m.garden.planrev.mode != planDoc {
		t.Fatal("a comparison opened for the first revision")
	}
}

func TestRequestChangesNeedsAReasonNamesTheExactRevisionAndRunsNothing(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		r1 := scriptPlan(t, rig, longPlan("first"))
		m = openReview(t, m)
		m = plannerPress(t, m, "c")
		if m.garden.planrev.stage != planDecide {
			t.Fatalf("c did not open the decision:\n%s", view(m))
		}
		mustContain(t, "prompt", view(m), "REQUEST CHANGES ON r1", "revision "+shortHash(r1.Hash), "starts nothing", "needs your own confirmation")
		mustFit(t, "prompt", m)
		m = plannerPress(t, m, "enter")
		if rig.garden.Count("DecidePlan") != 0 || m.garden.planrev.stage != planDecide {
			t.Fatalf("a decision without a reason was sent")
		}
		mustContain(t, "reason", view(m), "needs a reason")
		m = typed(t, m, "Cover the counter race.")
		m = plannerPress(t, m, "enter")
		if len(rig.garden.PlanDecided) != 1 {
			t.Fatalf("decisions = %d\n%s", len(rig.garden.PlanDecided), view(m))
		}
		d := rig.garden.PlanDecided[0]
		if d.RevisionHash != r1.Hash || d.Fingerprint != r1.SourceSpecFingerprint || d.Decision != garden.DecisionChangesRequested || d.Reason != "Cover the counter race." {
			t.Fatalf("decision = %+v", d)
		}
		out := view(m)
		mustContain(t, "after", out, "Current r1 changes requested", "Cover the counter race.", "confirmation")
		mustFit(t, "after", m)
		// Nothing ran: no planner was requested and no session started.
		if rig.garden.Count("StartPlanner") != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatalf("requesting changes started a run")
		}
		// Asking again on the same revision is refused; rejecting it is still possible.
		m = plannerPress(t, m, "c")
		mustContain(t, "again", view(m), "already requested")
		if m.garden.planrev.stage != planReview {
			t.Fatal("a second request for changes opened the prompt")
		}
	}
}

func TestRejectReturnsToApprovedWithNoAcceptedPlan(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	scriptPlan(t, rig, longPlan("first"))
	m = plannerPress(t, openReview(t, m), "x")
	mustContain(t, "prompt", view(m), "REJECT r1", "no plan is accepted", "stays Approved")
	m = plannerPress(t, m, "enter")
	if len(rig.garden.PlanDecided) != 1 || rig.garden.PlanDecided[0].Decision != garden.DecisionRejected {
		t.Fatalf("decisions = %+v", rig.garden.PlanDecided)
	}
	out := view(m)
	mustContain(t, "after", out, "Current none", "Approved none", "r1 rejected", "Recorded on plan r1: rejected")
	m = plannerPress(t, m, "R")
	mustContain(t, "state", view(m), "no plan is accepted and the Issue stays Approved")
	// The revision stays in the history and the Issue is still approved work: a new plan is the way forward.
	if m.garden.planrev.view.Review.Gate.State != approve.StateApproved || len(m.garden.planrev.view.Review.Revisions) != 1 {
		t.Fatalf("gate %s revisions %d", m.garden.planrev.view.Review.Gate.State, len(m.garden.planrev.view.Review.Revisions))
	}
	m = plannerPress(t, m, "x")
	mustContain(t, "no current", view(m), "Only the revision under review")
}

func TestRemoteCommentIsAHintAndNeverStartsOrDecides(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	r1 := scriptPlan(t, rig, longPlan("first"))
	rig.garden.PlanComments = map[uint64][]garden.Comment{1: {{ID: r1.CommentID + 1, Author: "reviewer", AuthorID: "9", Body: "Changes requested: please cover the race."}}}
	m = openReview(t, m)
	out := view(m)
	mustContain(t, "hint", out, "Newer on GitHub: 1 comment that Bonsai cannot read as records", "Current r1 awaiting decision")
	mustFit(t, "hint", m)
	for _, k := range []string{"R", "n", "N", "esc"} {
		m = plannerPress(t, m, k)
	}
	if rig.garden.Count("StartPlanner") != 0 || rig.garden.Count("DecidePlan") != 0 || len(rig.agents.Sessions.Starts()) != 0 {
		t.Fatalf("a remote comment triggered something: start=%d decide=%d", rig.garden.Count("StartPlanner"), rig.garden.Count("DecidePlan"))
	}
	// A local run still needs the explicit Request AI plan with its own confirmation.
	m = plannerPress(t, m, "enter", "p")
	if m.garden.planner.stage != plannerHub {
		t.Fatalf("the planner screen did not open: %d", m.garden.planner.stage)
	}
	if rig.garden.Count("StartPlanner") != 0 {
		t.Fatal("opening the planner after a remote request started a run")
	}
}

func TestOtherAuthorsAndTamperedRevisionsCannotBeDecided(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	scriptPlan(t, rig, longPlan("first"))
	it := rig.garden.WorkView.Snapshot.Items[0]
	a := approve.Assess(it.Issue, it.Verification)
	doc := planrev.Compose(longPlan("foreign"), plan.SpecRef{RevisionHash: a.Head.Hash, Ordinal: a.Ordinal, Fingerprint: a.Fingerprint}, plan.Base{Commit: planBase})
	rig.garden.AddPlanRevision(1, doc, "someone-else")
	m = openReview(t, m)
	out := view(m)
	mustContain(t, "foreign", out, "Latest r2", "r2 other author", "Current none", "written by another account")
	m = plannerPress(t, m, "c")
	if rig.garden.Count("DecidePlan") != 0 || m.garden.planrev.stage != planReview {
		t.Fatal("a revision by another account could be decided")
	}
}

func TestLateReviewRepliesAreDropped(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	scriptPlan(t, rig, longPlan("first"))
	m = openReview(t, m)
	old := *m.garden.planrev.view
	// Refresh: a newer reply replaces the view...
	m = plannerPress(t, m, "R")
	newer := *m.garden.planrev.view
	if newer.Stamp.Generation <= old.Stamp.Generation {
		t.Fatalf("generations did not advance: %d -> %d", old.Stamp.Generation, newer.Stamp.Generation)
	}
	// ...and an older one that arrives afterwards, even for the request in flight, never replaces it.
	empty := old
	empty.Review = planrev.Review{}
	m, _ = m.gardenLoadPlanReview("", false)
	seq := m.garden.seq
	m, _ = send(t, m, gardenPlanReviewMsg{seq: seq, view: empty})
	if len(m.garden.planrev.view.Review.Revisions) != 1 || m.garden.err == nil {
		t.Fatalf("a stale reply replaced the review: revisions=%d err=%v", len(m.garden.planrev.view.Review.Revisions), m.garden.err)
	}
	// A reply for a request that was superseded is dropped unread.
	m, _ = send(t, m, gardenPlanReviewMsg{seq: seq - 1, view: newer})
	if len(m.garden.planrev.view.Review.Revisions) != 1 {
		t.Fatal("a superseded reply was applied")
	}
}

func TestPublishPlanFromAnAttemptIsExplicitAndShowsTheExactRevision(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		it := rig.garden.WorkView.Snapshot.Items[0]
		att := plan.Attempt{ID: "plan-fake-9", Local: "x", Repo: it.Issue.Repo, Issue: it.Issue.ID, Number: 101, Role: plan.RolePlanner, State: plan.StateEnded, SessionID: "sess-9",
			Provider: "claude", AccountName: "work", Base: plan.Base{Commit: planBase, Branch: "main"}}
		a := approve.Assess(it.Issue, it.Verification)
		att.Spec = plan.SpecRef{RevisionHash: a.Head.Hash, CommentID: a.Head.CommentID, Ordinal: a.Ordinal, Fingerprint: a.Fingerprint}
		rig.garden.SetAttempt(att)
		m = openPlanner(t, m)
		mustContain(t, "hint", strings.Join(hintKeys(m), " "), "u")
		m = plannerPress(t, m, "u")
		if m.garden.planrev.stage != planCompose {
			t.Fatalf("u did not open the plan text: stage %d\n%s", m.garden.planrev.stage, view(m))
		}
		mustContain(t, "compose", view(m), "PUBLISH PLAN FOR #101", "reads nothing from the planner's")
		mustFit(t, "compose", m)
		// Nothing is published by opening the text, previewing it or leaving it.
		m = typed(t, m, "## Steps\n1. Only this.")
		m = plannerPress(t, m, "ctrl+s")
		if m.garden.planrev.stage != planConfirm {
			t.Fatalf("ctrl+s did not preview: stage %d\n%s", m.garden.planrev.stage, view(m))
		}
		mustContain(t, "incomplete", view(m), "PUBLISH r1 FOR #101", "Missing or empty: Assumptions", "Go back and complete the text")
		mustNotContain(t, "incomplete", view(m), "Publish r1 ")
		mustFit(t, "incomplete", m)
		m = plannerPress(t, m, "enter")
		if m.garden.planrev.stage != planCompose || rig.garden.Count("PublishPlan") != 0 {
			t.Fatalf("Enter on an incomplete preview published or stayed: stage %d", m.garden.planrev.stage)
		}
		// Complete the text. Typing newlines in tests is cheap: set the value as a paste would.
		m.garden.planrev.ta.SetValue(longPlan("pasted"))
		m = plannerPress(t, m, "ctrl+s")
		out := view(m)
		mustContain(t, "preview", out, "PUBLISH r1 FOR #101", "revision", "Inspected base", "c0ffee", "Supersedes", "first revision", "starts no run")
		mustFit(t, "preview", m)
		if rig.garden.Count("PublishPlan") != 0 || rig.garden.Writes() != 0 {
			t.Fatal("previewing published")
		}
		m = plannerPress(t, m, "enter")
		if len(rig.garden.PlanPublished) != 1 || rig.garden.PlanPublished[0].Preview.AttemptID != "plan-fake-9" {
			t.Fatalf("published = %d", len(rig.garden.PlanPublished))
		}
		if m.garden.planrev.stage != planReview {
			t.Fatalf("after publishing the review is not shown: stage %d\n%s", m.garden.planrev.stage, view(m))
		}
		mustContain(t, "published", view(m), "Published plan r1", "Current r1 awaiting decision")
		if rig.garden.Count("StartPlanner") != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatal("publishing started a run")
		}
	}
}

func TestComposeKeepsTheDraftInMemoryWhenLeft(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	it := rig.garden.WorkView.Snapshot.Items[0]
	a := approve.Assess(it.Issue, it.Verification)
	rig.garden.SetAttempt(plan.Attempt{ID: "plan-fake-9", Local: "x", Repo: it.Issue.Repo, Issue: it.Issue.ID, Number: 101, Role: plan.RolePlanner, State: plan.StateEnded,
		Spec: plan.SpecRef{RevisionHash: a.Head.Hash, Ordinal: a.Ordinal, Fingerprint: a.Fingerprint}, Base: plan.Base{Commit: planBase}})
	m = plannerPress(t, openPlanner(t, m), "u")
	m = typed(t, m, "half a plan")
	m = plannerPress(t, m, "esc")
	if m.garden.planrev.stage != planNone || m.garden.planner.stage != plannerHub || rig.garden.Count("PreviewPlanPublish") != 0 {
		t.Fatalf("leaving the text changed the screens or previewed: %d %d", m.garden.planrev.stage, m.garden.planner.stage)
	}
	m = plannerPress(t, m, "u")
	if got := m.garden.planrev.ta.Value(); got != "half a plan" {
		t.Fatalf("draft = %q", got)
	}
}

func TestPublishNeedsAnAttemptOfTheCurrentSpecification(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	it := rig.garden.WorkView.Snapshot.Items[0]
	rig.garden.SetAttempt(plan.Attempt{ID: "plan-old", Local: "x", Repo: it.Issue.Repo, Issue: it.Issue.ID, Number: 101, Role: plan.RolePlanner, State: plan.StateEnded,
		Spec: plan.SpecRef{RevisionHash: "old", Ordinal: 1, Fingerprint: "old"}, Base: plan.Base{Commit: planBase}})
	m = plannerPress(t, openPlanner(t, m), "u")
	if m.garden.planrev.stage != planNone {
		t.Fatal("an attempt of an older specification opened the plan text")
	}
	mustContain(t, "notice", view(m), "older specification")
}

func hintKeys(m Model) []string {
	var keys []string
	for _, h := range m.gardenBoardHints() {
		keys = append(keys, h.key)
	}
	return keys
}
