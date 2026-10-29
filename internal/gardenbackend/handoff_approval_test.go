package gardenbackend

import (
	"context"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
)

func TestVanishedSpecApprovalMakesAcceptanceStaleNotDone(t *testing.T) {
	r := newHandoffRig(t, false)
	r.linkPR(7, handoffHead)
	r.decide(garden.HandoffAccepted, "", 0)
	if v := r.view(); v.Completion.State != handoff.CompletionAccepted {
		t.Fatalf("setup: want accepted, got %+v", v.Completion)
	}
	fake, ok := r.h.b.deps.Issues.(*gardenfake.Fake)
	if !ok {
		t.Fatalf("unexpected issue reader %T", r.h.b.deps.Issues)
	}
	if !fake.DeleteComment(r.attempt.Repo, garden.IssueID(r.issue), r.attempt.Inputs.SpecApprovalID) {
		t.Fatal("approval not deleted")
	}
	r.prs.detail.State, r.prs.detail.Merged = "merged", true

	read, err := r.h.b.readHandoff(context.Background(), r.issue, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if read.liveOK {
		t.Fatal("approved inputs should be unavailable")
	}
	if read.decision.State != handoff.DecisionStale {
		t.Errorf("decision = %+v, want stale", read.decision)
	}
	if read.completion.State == handoff.CompletionDone {
		t.Errorf("done without a verifiable approved pair: %+v", read.completion)
	}
}
