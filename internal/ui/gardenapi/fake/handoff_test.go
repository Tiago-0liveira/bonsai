package fake

import (
	"context"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func TestHandoffSamplesMatchTheirStory(t *testing.T) {
	want := map[HandoffSample]handoff.CompletionState{
		HandoffHumanReview: handoff.CompletionAwaitingDecision, HandoffNoReviewNoChecks: handoff.CompletionAwaitingDecision,
		HandoffAcceptedAwaitingMerge: handoff.CompletionAccepted, HandoffDone: handoff.CompletionDone, HandoffBlocked: handoff.CompletionBlocked,
		HandoffStale: handoff.CompletionStale, HandoffOfflinePR: handoff.CompletionUnknown, HandoffChangesRequested: handoff.CompletionChangesRequested,
		HandoffNonCodeDone: handoff.CompletionDone,
	}
	for _, s := range HandoffSamples() {
		if got := SampleHandoff(s).Completion.State; got != want[s] {
			t.Errorf("%s: state %s, want %s", s, got, want[s])
		}
	}
	if v := SampleHandoff(HandoffNoReviewNoChecks); v.ChecksRecorded || len(v.Checks) != 0 {
		t.Fatalf("no checks sample = %+v", v.Checks)
	}
}

func TestHandoffBackendRecordsCallsAndScriptedErrors(t *testing.T) {
	b := NewHandoff(HandoffHumanReview)
	b.Acknowledgments = []string{"no-ai-review"}
	ctx := context.Background()
	v, err := b.Handoff(ctx, gardenapi.HandoffRequest{IssueID: 42})
	if err != nil || v.Stamp.Generation != 1 {
		t.Fatalf("view = %+v %v", v, err)
	}
	p, err := b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: 42, Decision: garden.HandoffAccepted, Text: " ok "})
	if err != nil || p.Text != "ok" || len(p.Acknowledgments) != 1 {
		t.Fatalf("preview = %+v %v", p, err)
	}
	if _, err = b.DecideHandoff(ctx, p, false); err == nil {
		t.Fatal("unconfirmed decision succeeded")
	}
	b.DecideErr = &gardenapi.Error{Code: gardenapi.CodeHandoffChanged}
	if _, err = b.DecideHandoff(ctx, p, true); err == nil || len(b.Decided) != 2 {
		t.Fatalf("err = %v decided=%d", err, len(b.Decided))
	}
	if got := b.Calls(); len(got) != 4 || got[0] != "Handoff" {
		t.Fatalf("calls = %v", got)
	}
}

func TestDoneSamplesAllowNoDecisionButKeepOpenAndCleanup(t *testing.T) {
	for _, s := range []HandoffSample{HandoffDone, HandoffNonCodeDone} {
		a := SampleHandoff(s).Actions
		if a.Accept.Allowed || a.RequestChanges.Allowed || a.Revoke.Allowed || a.Rework.Allowed {
			t.Errorf("%s allows a decision: %+v", s, a)
		}
	}
	if a := SampleHandoff(HandoffDone).Actions; !a.OpenPR.Allowed || !a.Cleanup.Allowed {
		t.Fatalf("done lost open/cleanup: %+v", a)
	}
}
