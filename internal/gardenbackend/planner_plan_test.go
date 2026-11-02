package gardenbackend

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// startedAttempt approves the Issue and starts one planner attempt.
func startedAttempt(t *testing.T) (*harness, uint64, *stubAgents, plan.Attempt) {
	t.Helper()
	h, _, id, ag, _ := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, _ := h.b.PreviewPlanner(ctx, id)
	v, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	return h, id, ag, v.Attempt
}

func writePlanFile(t *testing.T, a plan.Attempt, text string) {
	t.Helper()
	if err := os.WriteFile(a.PlanPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(a.PlanPath, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestStartPlannerGivesTheSessionOnlyItsPlanFile(t *testing.T) {
	_, _, ag, a := startedAttempt(t)
	l := ag.started[0].Launch
	if a.PlanPath == "" || l.PlanFile != a.PlanPath || !l.ReadOnly || !strings.Contains(l.Prompt, a.PlanPath) {
		t.Fatalf("launch = %+v, attempt plan path %q", l, a.PlanPath)
	}
}

func TestPlannerAttemptsReportLiveActivityAndPlanFileOnlyAsOverlay(t *testing.T) {
	ctx := context.Background()
	h, id, ag, a := startedAttempt(t)
	since := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	ag.status[a.SessionID] = plan.SessionStatus{State: "running", Activity: &agentactivity.Activity{State: agentactivity.Working, Since: since}}
	writePlanFile(t, a, "# Plan\n")
	v, err := h.b.PlannerAttempts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	live := v.Overlay[a.ID]
	if live.Activity == nil || live.Activity.State != agentactivity.Working || live.PlanFile.State != plan.PlanFileWriting {
		t.Fatalf("overlay = %+v", live)
	}
	pv, err := h.b.PreviewPlanner(ctx, id)
	if err != nil || pv.Overlay[a.ID].PlanFile.State != plan.PlanFileWriting {
		t.Fatalf("preview overlay = %+v, %v", pv.Overlay, err)
	}
	ag.status[a.SessionID] = plan.SessionStatus{State: "running", Activity: &agentactivity.Activity{State: agentactivity.Idle, Since: since}}
	if v, _ = h.b.PlannerAttempts(ctx, id); v.Overlay[a.ID].PlanFile.State != plan.PlanFileReady {
		t.Fatalf("idle overlay = %+v", v.Overlay[a.ID])
	}
	raw, _ := readFile(h.b.deps.Plans.Path)
	if strings.Contains(raw, "working") || strings.Contains(raw, "idle") || strings.Contains(raw, "# Plan") {
		t.Fatal("activity or plan text reached the attempt store")
	}
}

func TestPlannerPlanReturnsReadyTextAndRefusesTheRest(t *testing.T) {
	ctx := context.Background()
	h, _, ag, a := startedAttempt(t)
	if _, err := h.b.PlannerPlan(ctx, a.ID); code(err) != gardenapi.CodePlanNotReady {
		t.Fatalf("no file = %v", err)
	}
	if _, err := h.b.PlannerPlan(ctx, "plan-nope"); code(err) != gardenapi.CodeNotFound {
		t.Fatalf("unknown attempt = %v", err)
	}
	writePlanFile(t, a, "# Plan\n1. step\n")
	ag.status[a.SessionID] = plan.SessionStatus{State: "running", Activity: &agentactivity.Activity{State: agentactivity.Idle}}
	v, err := h.b.PlannerPlan(ctx, a.ID)
	if err != nil || v.Text != "# Plan\n1. step\n" || v.AttemptID != a.ID || v.IssueID == 0 || v.File.State != plan.PlanFileReady {
		t.Fatalf("plan = %+v, %v", v, err)
	}
	if err := os.WriteFile(a.PlanPath, []byte(strings.Repeat("x", plan.MaxPlanBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(a.PlanPath, old, old)
	if _, err := h.b.PlannerPlan(ctx, a.ID); code(err) != gardenapi.CodePlanFileInvalid {
		t.Fatalf("oversize = %v", err)
	}
}

func TestLocalWorkFlagsLiveAndUnpublishedReadyPlansWithOneSessionList(t *testing.T) {
	ctx := context.Background()
	h, id, ag, a := startedAttempt(t)
	ag.status[a.SessionID] = plan.SessionStatus{State: "running", Activity: &agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonQuestion}}
	v, err := h.b.LocalWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Stamp.GardenID == 0 {
		t.Fatalf("stamp lacks the repository: %+v", v.Stamp)
	}
	got := v.Issues[id].Planner
	if got == nil || got.AttemptID != a.ID || !got.Live || got.State != "running" || got.Activity == nil || got.Activity.State != agentactivity.Waiting || got.PlanFile != plan.PlanFileNone {
		t.Fatalf("live summary = %+v", got)
	}
	if ag.statusCalls != 1 {
		t.Fatalf("Statuses called %d times, want one", ag.statusCalls)
	}

	// Ended with a ready plan that nobody published: still flagged.
	zero := 0
	writePlanFile(t, a, "# Plan\n")
	ag.status[a.SessionID] = plan.SessionStatus{State: "exited", ExitCode: &zero}
	if v, err = h.b.LocalWork(ctx); err != nil {
		t.Fatal(err)
	}
	got = v.Issues[id].Planner
	if got == nil || got.Live || got.State != "ended" || got.PlanFile != plan.PlanFileReady || got.Activity != nil {
		t.Fatalf("ended summary = %+v", got)
	}

	// Published from here: nothing left to flag.
	svc, _ := h.b.planService()
	if err := svc.MarkPublished(ctx, a.ID, "hash-1"); err != nil {
		t.Fatal(err)
	}
	if v, _ = h.b.LocalWork(ctx); len(v.Issues) != 0 {
		t.Fatalf("published plan still flagged: %+v", v.Issues)
	}
}

func TestLocalWorkIgnoresEndedAttemptsWithoutAReadyPlan(t *testing.T) {
	ctx := context.Background()
	h, _, ag, a := startedAttempt(t)
	one := 1
	ag.status[a.SessionID] = plan.SessionStatus{State: "exited", ExitCode: &one}
	if v, err := h.b.LocalWork(ctx); err != nil || len(v.Issues) != 0 {
		t.Fatalf("failed attempt flagged: %+v, %v", v.Issues, err)
	}
	zero := 0
	ag.status[a.SessionID] = plan.SessionStatus{State: "exited", ExitCode: &zero}
	if v, err := h.b.LocalWork(ctx); err != nil || len(v.Issues) != 0 {
		t.Fatalf("ended attempt without a plan flagged: %+v, %v", v.Issues, err)
	}
}

func TestLocalWorkEmptyReplyStillNamesTheRepository(t *testing.T) {
	h, _, _, _, _ := plannerHarness(t)
	v, err := h.b.LocalWork(context.Background())
	if err != nil || len(v.Issues) != 0 || v.Stamp.GardenID == 0 || v.Stamp.LocalRepoID == "" {
		t.Fatalf("empty LocalWork = %+v, %v", v, err)
	}
	pv, _ := h.b.PlannerAttempts(context.Background(), 1)
	if pv.Stamp.GardenID != v.Stamp.GardenID {
		t.Fatalf("repo id %d differs from PlannerAttempts %d", v.Stamp.GardenID, pv.Stamp.GardenID)
	}
}
