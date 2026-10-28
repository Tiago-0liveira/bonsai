package ui

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
)

func TestApproveExactPlanConfirmationAndReady(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		rev := scriptPlan(t, rig, longPlan("first"))
		m = openReview(t, m)
		m = plannerPress(t, m, "a")
		mustContain(t, "approval", flat(view(m)), "APPROVE PLAN r1?", rev.Hash, rev.SourceSpecFingerprint, "Actor:", "GitHub ID 1001", "No agent, worktree or PR starts.", "Launch unavailable", "Cancel")
		mustFit(t, "approval", m)
		if m.garden.planrev.btn != 0 || m.shellFocus == shellInput {
			t.Fatal("approval must start on Cancel without text focus")
		}
		m = plannerPress(t, m, "enter")
		if rig.garden.Count("DecidePlan") != 0 || m.garden.planrev.stage != planReview {
			t.Fatal("default Enter approved")
		}
		m = plannerPress(t, m, "a", "tab", "enter")
		rv := m.garden.planrev.view.Review
		if !rv.Ready || rv.Approved == nil || rv.Approved.Hash != rev.Hash {
			t.Fatalf("not Ready: %+v", rv)
		}
		mustContain(t, "Ready", view(m), "READY", "Approved r1", "local", "verified", "Launch unavailable")
		mustFit(t, "Ready", m)
		req := rig.garden.PlanDecided[0]
		if req.Decision != garden.DecisionApproved || req.RevisionHash != rev.Hash || req.Fingerprint != rev.SourceSpecFingerprint || req.SpecHash == "" || req.ActorID != "1001" {
			t.Fatalf("request=%+v", req)
		}
		if rig.garden.Count("StartPlanner") != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatal("approval started an agent")
		}
		m = plannerPress(t, m, "esc")
		if m.garden.board.collection.Records[0].Column != work.Ready {
			t.Fatal("Board did not derive Ready")
		}
	}
}

func TestExternalReadyAndStaleGateRendering(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		rev := scriptPlan(t, rig, longPlan("first"))
		rig.garden.AddPlanDecision(1, rev, garden.DecisionApproved, "")
		m = openReview(t, m)
		mustContain(t, "external", view(m), "READY", "external", "verified", "Approved r1")
		scriptPlan(t, rig, longPlan("second"))
		m = plannerPress(t, m, "R")
		if m.garden.planrev.view.Review.Ready || !m.garden.planrev.view.Review.StaleApproval {
			t.Fatal("approval reused for next plan")
		}
		mustContain(t, "stale", view(m), "APPROVAL STALE", "Approved r1", "snapshot is retained", "Downstream disabled")
		mustFit(t, "stale", m)
		m = plannerPress(t, m, "o", "y")
		if rig.garden.Count("OpenIssueLink") != 1 || rig.garden.Count("CopyIssueLink") != 1 || rig.garden.Count("StartPlanner") != 0 {
			t.Fatal("Issue links missing or started a run")
		}
	}
}

func TestApprovalActionsAreDistinctAndLaunchUnavailable(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	scriptPlan(t, rig, longPlan("first"))
	m = openReview(t, m)
	keys := hintKeys(m)
	for _, key := range []string{"v", "p", "a"} {
		found := 0
		for _, got := range keys {
			if got == key {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("key %s occurs %d times: %v", key, found, keys)
		}
	}
	mustContain(t, "launch", view(m), "Launch unavailable")
	m = plannerPress(t, m, "v")
	if m.garden.planrev.stage != planValidate || m.garden.drafts.validate == nil {
		t.Fatal("gate one is not reachable from accepted work")
	}
	mustContain(t, "validate", view(m), "SPECIFICATION")
	m = plannerPress(t, m, "esc")
	if m.garden.planrev.stage != planReview {
		t.Fatal("revalidation back lost accepted work")
	}
}

func TestLateReadyReplyCannotOverwriteNewerStaleReview(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rev := scriptPlan(t, rig, longPlan("first"))
	rig.garden.AddPlanDecision(1, rev, garden.DecisionApproved, "")
	m = openReview(t, m)
	ready := *m.garden.planrev.view
	scriptPlan(t, rig, longPlan("second"))
	m = plannerPress(t, m, "R")
	m, _ = m.gardenLoadPlanReview("", false)
	m, _ = send(t, m, gardenPlanReviewMsg{seq: m.garden.seq, view: ready})
	if m.garden.planrev.view.Review.Ready || !m.garden.planrev.view.Review.StaleApproval {
		t.Fatal("late Ready overwrote stale gate")
	}
	// An old decision response must not change the revision in view or cause a read.
	seq := m.garden.seq
	count := rig.garden.Count("PlanReview")
	m, cmd := m.onGardenPlanDecision(gardenPlanDecisionMsg{seq: seq - 1})
	if cmd != nil || rig.garden.Count("PlanReview") != count || m.garden.planrev.stage != planReview {
		t.Fatal("old decision event landed")
	}
}

func TestPlansSectionUsesRevisionSummaries(t *testing.T) {
	m, rig := plannerEnv(t, 80, 24)
	scriptPlan(t, rig, longPlan("first"))
	m = plannerPress(t, m, "R", "enter")
	m.garden.board.section = 3
	text := strings.Join(m.gardenDocument(80), "\n")
	mustContain(t, "summary", text, "PLAN r1 / NEEDS YOUR APPROVAL", "Latest r1", "awaiting decision")
	mustNotContain(t, "summary", text, "Step number 17")
	// Review still carries the full document for focused navigation.
	m = plannerPress(t, m, "r")
	if r, _ := m.garden.planrev.current(); len(planrev.Parse(r.Content)) < 6 {
		t.Fatal("full plan lost")
	}
}

func TestValidRefreshInvalidatesOpenPlanDecision(t *testing.T) {
	for _, key := range []string{"a", "c", "x"} {
		for _, changed := range []string{"plan", "spec", "fingerprint", "actor"} {
			t.Run(key+"/"+changed, func(t *testing.T) {
				m, rig := plannerEnv(t, 120, 35)
				scriptPlan(t, rig, longPlan("first"))
				m = openReview(t, m)
				// Start a refresh, enter a decision, then deliver that valid newer reply.
				m, cmd := send(t, m, keyOf("R"))
				if cmd == nil {
					t.Fatal("no refresh")
				}
				m, _ = send(t, m, keyOf(key))
				if m.garden.planrev.stage != planDecide {
					t.Fatal("decision did not open")
				}
				v := *m.garden.planrev.view
				v.Stamp.Generation++
				switch changed {
				case "plan":
					scriptPlan(t, rig, longPlan("second"))
					v, _ = rig.garden.PlanReview(t.Context(), v.IssueID)
				case "spec":
					head := *v.Review.Gate.Head
					head.Hash = strings.Repeat("f", 64)
					v.Review.Gate.Head = &head
				case "fingerprint":
					v.Review.Gate.Fingerprint = strings.Repeat("f", 64)
				case "actor":
					v.ActorID, v.Actor = "999", "other"
				}
				m, _ = send(t, m, gardenPlanReviewMsg{seq: m.garden.seq, view: v})
				if m.garden.planrev.stage != planReview {
					t.Fatal("changed prerequisites left confirmation open")
				}
				m, cmd = m.gardenRunPlanDecision()
				if cmd != nil || rig.garden.Count("DecidePlan") != 0 {
					t.Fatal("invalidated confirmation submitted")
				}
			})
		}
	}
}

func TestUnchangedRefreshKeepsOriginalDecisionBinding(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rev := scriptPlan(t, rig, longPlan("first"))
	m = openReview(t, m)
	original := m.garden.planrev.view.Stamp
	m, _ = send(t, m, keyOf("R"))
	m, _ = send(t, m, keyOf("a"))
	v := *m.garden.planrev.view
	v.Stamp.Generation++
	m, _ = send(t, m, gardenPlanReviewMsg{seq: m.garden.seq, view: v})
	if m.garden.planrev.stage != planDecide {
		t.Fatal("unchanged target invalidated")
	}
	m = plannerPress(t, m, "tab", "enter")
	if len(rig.garden.PlanDecided) != 1 {
		t.Fatal("decision missing")
	}
	req := rig.garden.PlanDecided[0]
	if req.Stamp != original || req.RevisionHash != rev.Hash || req.ActorID != "1001" {
		t.Fatalf("confirmation binding changed: %+v", req)
	}
}
