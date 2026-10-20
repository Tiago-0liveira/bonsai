package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func TestSkipUnskipRoundTripBumpsEpochAndGeneration(t *testing.T) {
	b := New(gardenapi.StateNone)
	ctx := context.Background()
	var gate gardenapi.Gate
	first, _ := b.Status(ctx)
	if !gate.Accept(first.Stamp) || first.State != gardenapi.StateNone {
		t.Fatalf("first = %+v", first)
	}
	skipped, err := b.Skip(ctx, gardenapi.TargetRef{})
	if err != nil || skipped.State != gardenapi.StateSkipped || !skipped.Caps.Unskip || skipped.Stamp.Epoch <= first.Stamp.Epoch {
		t.Fatalf("Skip = %+v, %v", skipped, err)
	}
	if !gate.Accept(skipped.Stamp) {
		t.Fatal("skip reply rejected")
	}
	back, _ := b.Unskip(ctx)
	if back.State != gardenapi.StateNone || back.Stamp.Epoch <= skipped.Stamp.Epoch || !gate.Accept(back.Stamp) {
		t.Fatalf("Unskip = %+v", back)
	}
}

func TestHookMakesRepliesLateAndGateDropsThem(t *testing.T) {
	b := New(gardenapi.StateNone)
	release := make(chan struct{})
	entered := make(chan struct{})
	b.Hook = func(string) {
		select {
		case entered <- struct{}{}:
			<-release // the first call blocks, later ones pass
		default:
		}
	}
	late := make(chan gardenapi.StatusView, 1)
	go func() { v, _ := b.Status(context.Background()); late <- v }()
	<-entered
	fresh, _ := b.Status(context.Background())
	close(release)
	slow := <-late
	var gate gardenapi.Gate
	if !gate.Accept(fresh.Stamp) {
		t.Fatal("fresh reply rejected")
	}
	if slow.Stamp.Generation >= fresh.Stamp.Generation {
		t.Fatalf("blocked call must carry the older generation: %d vs %d", slow.Stamp.Generation, fresh.Stamp.Generation)
	}
	if gate.Accept(slow.Stamp) {
		t.Fatal("late reply accepted")
	}
}

func TestScriptedErrorsAndWriteCounting(t *testing.T) {
	boom := &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub could not be reached."}
	b := New(gardenapi.StatePartialSetup)
	b.ResumeErr = boom
	if _, err := b.Resume(context.Background(), "op-1"); !errors.Is(err, boom) {
		t.Fatalf("Resume err = %v", err)
	}
	if _, err := b.PreviewPlan(context.Background(), gardenapi.PlanRequest{Mode: gardenapi.ModeCreate}); err != nil {
		t.Fatal(err)
	}
	if b.Writes() != 1 || b.Count("PreviewPlan") != 1 {
		t.Fatalf("calls = %v", b.Calls())
	}
}

func TestApplyMovesStatusToReportedState(t *testing.T) {
	b := New(gardenapi.StateNone)
	b.AppliedView = SampleOperation(gardenapi.StatePartialSetup)
	op, err := b.Apply(context.Background(), b.PlanView)
	if err != nil || op.State != gardenapi.StatePartialSetup {
		t.Fatalf("Apply = %+v, %v", op, err)
	}
	st, _ := b.Status(context.Background())
	if st.State != gardenapi.StatePartialSetup || st.Operation == nil {
		t.Fatalf("status after apply = %+v", st)
	}
}

func TestSamplesCoverEveryState(t *testing.T) {
	for _, s := range []gardenapi.State{gardenapi.StateNone, gardenapi.StateSkipped, gardenapi.StateReadOnly, gardenapi.StateConnected, gardenapi.StatePartialSetup, gardenapi.StateAwaitingManual, gardenapi.StateOperationInProgress} {
		v := StatusFor(s)
		if v.State != s || v.Target.RepoID == 0 {
			t.Errorf("%s: %+v", s, v)
		}
		if s == gardenapi.StateReadOnly && (v.Caps.Setup || v.Caps.Resume || v.Caps.Skip || !v.Caps.Read) {
			t.Errorf("read-only sample offers setup actions: %+v", v.Caps)
		}
	}
	if p := SamplePlan(); p.Summary.ImportCount != 0 {
		t.Fatalf("sample plan imports %d issues", p.Summary.ImportCount)
	}
}
