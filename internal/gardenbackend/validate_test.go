package gardenbackend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const completeSpec = "## Summary\nCap spend.\n\n## Requirements\nA monthly ceiling.\n\n## Acceptance criteria\nRequests stop at the ceiling.\n\n## Open questions\nNone"

// validateHarness captures a draft and saves a complete specification for it.
func validateHarness(t *testing.T) (*harness, *gardenfake.Fake, uint64) {
	t.Helper()
	h, f, id := editHarness(t)
	d, err := h.b.Document(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, sv, err := save(t, h, d, completeSpec); err != nil || sv.State != gardenapi.SaveDone {
		t.Fatalf("save = %+v, %v", sv, err)
	}
	return h, f, id
}

func gateWrites(f *gardenfake.Fake) []string {
	var out []string
	for _, l := range f.WriteLog() {
		if strings.HasPrefix(l, "metadata:") || strings.HasPrefix(l, "labels:") {
			out = append(out, l)
		}
	}
	return out
}

func TestPreviewValidateWritesNothingAndNamesTheTransition(t *testing.T) {
	h, f, id := validateHarness(t)
	before := len(f.WriteLog())
	p, err := h.b.PreviewValidate(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	g := p.Gate
	if !p.CanValidate() || g.State != approve.StateDraft || g.Head == nil || g.Fingerprint != g.Head.Hash || g.Ordinal != 1 || g.From != garden.StageDraft ||
		len(p.Steps) != 2 || p.Type != garden.TypeFeature || p.Actor != viewerName || p.OperationID == "" || p.Number == 0 {
		t.Fatalf("preview = %+v", p)
	}
	if len(f.WriteLog()) != before {
		t.Fatalf("preview wrote: %v", f.WriteLog()[before:])
	}
}

func TestValidateMovesTheSameIssueFromDraftsToApprovedWork(t *testing.T) {
	h, f, id := validateHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewValidate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.Validate(ctx, p)
	if err != nil || v.State != gardenapi.ValidateDone || v.ApprovalCommentID == 0 || v.RevisionHash != p.Gate.Head.Hash || len(v.Done) != 2 {
		t.Fatalf("validate = %+v, %v", v, err)
	}
	// Nothing but the gate's own writes happened: no Issue, no launch, no extra label.
	if got := gateWrites(f); len(got) != 1 || !strings.Contains(got[0], "bonsai:type/feature") || !strings.Contains(got[0], "bonsai:stage/ready") || strings.Contains(got[0], "stage/draft") {
		t.Fatalf("gate writes = %v", got)
	}
	if creates(f) != 1 {
		t.Fatalf("creates = %d", creates(f))
	}
	w, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, in := work.Drafts(w.Snapshot.Items, work.Query{}).Find(garden.IssueID(id)); in {
		t.Fatal("approved Issue is still in Drafts")
	}
	rec, _, ok := work.Select(w.Snapshot.Items, work.Query{}).Find(garden.IssueID(id))
	if !ok || rec.Column != work.Approved || rec.Item.Classification.Type != garden.TypeFeature {
		t.Fatalf("board record = %+v ok=%v", rec, ok)
	}
	again, err := h.b.PreviewValidate(ctx, id)
	if err != nil || again.Gate.State != approve.StateApproved || again.CanValidate() || again.Gate.Approval == nil || again.Gate.Approval.AuthorID != "fake-authenticated-id" {
		t.Fatalf("approved preview = %+v, %v", again.Gate, err)
	}
}

func TestValidateRefusesAStalePreviewAndAReplayWritesOnce(t *testing.T) {
	h, f, id := validateHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewValidate(ctx, id)
	d, _ := h.b.Document(ctx, id)
	if _, sv, err := save(t, h, d, completeSpec+"\n\n## Constraints\nNo daemon."); err != nil || sv.State != gardenapi.SaveDone {
		t.Fatalf("second revision: %+v %v", sv, err)
	}
	_, err := h.b.Validate(ctx, p)
	var ae *gardenapi.Error
	if !errors.As(err, &ae) || ae.Code != gardenapi.CodeSpecChanged || len(gateWrites(f)) != 0 {
		t.Fatalf("err = %v writes = %v", err, gateWrites(f))
	}
	fresh, _ := h.b.PreviewValidate(ctx, id)
	if _, err := h.b.Validate(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	n := len(f.WriteLog())
	if v, err := h.b.Validate(ctx, fresh); err != nil || v.State != gardenapi.ValidateDone || len(f.WriteLog()) != n {
		t.Fatalf("replay = %+v, %v, wrote %d", v, err, len(f.WriteLog())-n)
	}
}

func TestValidatePartialProjectionIsReportedAndFinishedByAgain(t *testing.T) {
	h, f, id := validateHarness(t)
	ctx := context.Background()
	h.b.deps.Writer = brokenStage{IssueWriter: f}
	h.b.approvals = nil
	p, _ := h.b.PreviewValidate(ctx, id)
	v, err := h.b.Validate(ctx, p)
	if err != nil || v.State != gardenapi.ValidatePartial || v.Err == nil || v.ApprovalCommentID == 0 {
		t.Fatalf("partial = %+v, %v", v, err)
	}
	w, _ := h.b.Work(ctx)
	if _, _, in := work.Drafts(w.Snapshot.Items, work.Query{}).Find(garden.IssueID(id)); !in {
		t.Fatal("a half-approved Issue must remain a draft")
	}
	h.b.deps.Writer = f
	h.b.approvals = nil
	again, _ := h.b.PreviewValidate(ctx, id)
	if again.Gate.State != approve.StateFinishing || len(again.Steps) != 1 {
		t.Fatalf("resume preview = %+v", again)
	}
	if v, err := h.b.Validate(ctx, again); err != nil || v.State != gardenapi.ValidateDone || len(v.Done) != 1 {
		t.Fatalf("resume = %+v, %v", v, err)
	}
}

type brokenStage struct{ garden.IssueWriter }

func (brokenStage) UpdateStage(context.Context, garden.RepoID, garden.IssueID, []string, garden.Stage, garden.Stage) error {
	return garden.ErrOffline
}

func TestValidateIsUnavailableForABlockedSpecificationAndReadOnlyClones(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewValidate(ctx, id) // the body alone is not a specification revision
	if err != nil || p.CanValidate() || len(p.Gate.Blockers) == 0 || p.Gate.Blockers[0].Kind != approve.BlockNoSpec {
		t.Fatalf("preview = %+v, %v", p.Gate, err)
	}
	_, err = h.b.Validate(ctx, p)
	if err == nil {
		t.Fatal("blocked preview validated")
	}
	if len(gateWrites(f)) != 0 {
		t.Fatalf("blocked validate wrote %v", gateWrites(f))
	}

	ro, _, id2 := editHarness(t)
	ro.gh.Mu.Lock()
	for k, r := range ro.gh.Repositories {
		r.Access = connect.AccessRead
		ro.gh.Repositories[k] = r
	}
	ro.gh.Mu.Unlock()
	var ae *gardenapi.Error
	if _, err := ro.b.PreviewValidate(ctx, id2); !errors.As(err, &ae) || ae.Code != gardenapi.CodeReadOnly {
		t.Fatalf("read-only preview err = %v", err)
	}
}
