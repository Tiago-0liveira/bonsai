package gardenbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func captureHarness(t *testing.T) (*harness, *gardenfake.Fake) {
	t.Helper()
	h, f := workHarness(t)
	h.b.deps.Creator = f
	h.b.deps.Ops = &ops.Store{Path: filepath.Join(t.TempDir(), "ops.json")}
	return h, f
}

func idea() gardenapi.CaptureRequest {
	return gardenapi.CaptureRequest{Title: "  Add dark mode ", Description: "Because night.", Type: garden.TypeFeature}
}

// newWrites lists mutations other than the seed comments the harness appends while building its fixture.
func newWrites(f *gardenfake.Fake) []string {
	var out []string
	for _, l := range f.WriteLog() {
		if !strings.HasPrefix(l, "comment:") {
			out = append(out, l)
		}
	}
	return out
}

func creates(f *gardenfake.Fake) int {
	n := 0
	for _, l := range f.WriteLog() {
		if l == "create" {
			n++
		}
	}
	return n
}

func TestPreviewCaptureIsPureAndValidates(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewCapture(ctx, idea())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.OperationID, "cap-") || p.Request.Title != "Add dark mode" || p.Target.FullName != "acme/my-app" || len(p.Labels) != 3 || p.Stamp.Generation == 0 {
		t.Fatalf("preview = %+v", p)
	}
	other, _ := h.b.PreviewCapture(ctx, idea())
	if other.OperationID == p.OperationID {
		t.Fatal("operation IDs repeat")
	}
	if len(newWrites(f)) != 0 {
		t.Fatalf("preview wrote: %v", newWrites(f))
	}
	for name, req := range map[string]gardenapi.CaptureRequest{
		"empty title":  {Title: " ", Type: garden.TypeBug},
		"unknown type": {Title: "x", Type: "epic"},
		"long title":   {Title: strings.Repeat("a", 300), Type: garden.TypeBug},
	} {
		if _, err := h.b.PreviewCapture(ctx, req); err == nil {
			t.Errorf("%s accepted", name)
		} else {
			mustErrCode(t, err, gardenapi.CodeInvalidInput)
		}
	}
}

func TestCaptureNeedsWriteAccessAndNeverAttemptsOtherwise(t *testing.T) {
	h, f := captureHarness(t)
	h.gh.Mu.Lock()
	h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessRead)
	h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessRead)
	h.gh.Mu.Unlock()
	_, err := h.b.PreviewCapture(context.Background(), idea())
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	_, err = h.b.Capture(context.Background(), gardenapi.CapturePreview{Stamp: gardenapi.Stamp{LocalRepoID: h.b.local.String()}, OperationID: "cap-x", Request: idea(), Target: gardenapi.Target{RepoID: uint64(acmeID)}})
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	if len(newWrites(f)) != 0 {
		t.Fatalf("wrote with read access: %v", newWrites(f))
	}
}

func TestCaptureCreatesOneDraftAndReplayReturnsIt(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewCapture(ctx, idea())
	v, err := h.b.Capture(ctx, p)
	if err != nil || v.State != gardenapi.CaptureDone || v.Issue == nil || v.Err != nil {
		t.Fatalf("capture = %+v, %v", v, err)
	}
	again, err := h.b.Capture(ctx, p)
	if err != nil || again.Issue == nil || again.Issue.ID != v.Issue.ID || creates(f) != 1 {
		t.Fatalf("replay = %+v, %v, creates %d", again, err, creates(f))
	}
	w, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	drafts := work.Drafts(w.Snapshot.Items, work.Query{})
	if len(drafts.Records) != 1 || drafts.Records[0].Item.Issue.ID != garden.IssueID(v.Issue.ID) || drafts.Records[0].Attention != "" {
		t.Fatalf("drafts = %+v", drafts.Records)
	}
	if got := len(work.Select(w.Snapshot.Items, work.Query{}, nil).Records); got != 1 {
		t.Fatalf("board has %d items; the draft must stay out and only the seeded one remains", got)
	}
	if len(w.PendingCaptures) != 0 {
		t.Fatalf("pending = %+v", w.PendingCaptures)
	}
}

func TestAmbiguousCaptureBlocksRetryUntilReconciled(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewCapture(ctx, idea())
	f.FailNextCreateAfterApply(garden.ErrOffline)
	v, err := h.b.Capture(ctx, p)
	if err != nil || v.State != gardenapi.CaptureUnknown || v.Err == nil || v.Err.Code != gardenapi.CodeCaptureUnknown {
		t.Fatalf("capture = %+v, %v", v, err)
	}
	w, err := h.b.Work(ctx)
	if err != nil || len(w.PendingCaptures) != 1 || w.PendingCaptures[0].OperationID != p.OperationID || w.PendingCaptures[0].State != gardenapi.CaptureUnknown {
		t.Fatalf("pending = %+v, %v", w.PendingCaptures, err)
	}
	retry, err := h.b.Capture(ctx, p)
	if err != nil || retry.State != gardenapi.CaptureUnknown || creates(f) != 1 {
		t.Fatalf("retry = %+v, %v, creates %d", retry, err, creates(f))
	}
	got, err := h.b.ReconcileCapture(ctx, p.OperationID)
	if err != nil || got.State != gardenapi.CaptureDone || got.Issue == nil || creates(f) != 1 {
		t.Fatalf("reconcile = %+v, %v", got, err)
	}
	if w, _ = h.b.Work(ctx); len(w.PendingCaptures) != 0 {
		t.Fatalf("still pending: %+v", w.PendingCaptures)
	}
}

func TestReconcileTooSoonKeepsCaptureUnknown(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewCapture(ctx, idea())
	f.FailNext(garden.ErrOffline)
	if v, _ := h.b.Capture(ctx, p); v.State != gardenapi.CaptureUnknown {
		t.Fatalf("capture = %+v", v)
	}
	v, err := h.b.ReconcileCapture(ctx, p.OperationID)
	if err != nil || v.State != gardenapi.CaptureUnknown || v.Err == nil || creates(f) != 0 {
		t.Fatalf("reconcile = %+v, %v", v, err)
	}
	_, err = h.b.ReconcileCapture(ctx, "cap-missing")
	mustErrCode(t, err, gardenapi.CodeOperationNotFound)
}

func TestDefinitiveCaptureFailureIsRetryable(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewCapture(ctx, idea())
	f.FailNext(garden.ErrRejected)
	v, err := h.b.Capture(ctx, p)
	if err != nil || v.State != gardenapi.CaptureFailed || v.Err == nil || v.Err.Code != gardenapi.CodeValidation {
		t.Fatalf("capture = %+v, %v", v, err)
	}
	v, err = h.b.Capture(ctx, p)
	if err != nil || v.State != gardenapi.CaptureDone || creates(f) != 1 {
		t.Fatalf("retry = %+v, %v", v, err)
	}
}

func TestCaptureRefusesForeignPreviewAndMissingSeams(t *testing.T) {
	h, f := captureHarness(t)
	ctx := context.Background()
	p, _ := h.b.PreviewCapture(ctx, idea())
	p.Stamp.LocalRepoID = "other-clone"
	_, err := h.b.Capture(ctx, p)
	mustErrCode(t, err, gardenapi.CodeConflict)
	p.Stamp.LocalRepoID = h.b.local.String()
	p.Target.RepoID++
	_, err = h.b.Capture(ctx, p)
	mustErrCode(t, err, gardenapi.CodeIdentityChanged)
	h.b.deps.Creator = nil
	h.b.captures = nil
	_, err = h.b.Capture(ctx, p)
	mustErrCode(t, err, gardenapi.CodeFailed)
	if len(newWrites(f)) != 0 {
		t.Fatalf("wrote: %v", newWrites(f))
	}
}

func TestWorkReportsPendingStoreFailureWithoutDiscardingIssueRead(t *testing.T) {
	h, f := captureHarness(t)
	p, err := h.b.PreviewCapture(t.Context(), idea())
	if err != nil {
		t.Fatal(err)
	}
	f.FailNextCreateAfterApply(garden.ErrOffline)
	if v, err := h.b.Capture(t.Context(), p); err != nil || v.State != gardenapi.CaptureUnknown {
		t.Fatalf("capture = %+v, %v", v, err)
	}
	w, err := h.b.Work(t.Context())
	if err != nil || len(w.PendingCaptures) != 1 || w.PendingCapturesErr != nil {
		t.Fatalf("initial read = %+v, %v", w, err)
	}
	path := h.b.deps.Ops.Path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err = h.b.Work(t.Context())
	if err != nil || w.PendingCapturesErr == nil || !w.Snapshot.Complete || len(w.Snapshot.Items) != 2 {
		t.Fatalf("store failure should preserve the Issue read with an explicit pending error: %+v, %v", w, err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	w, err = h.b.Work(t.Context())
	if err != nil || w.PendingCapturesErr != nil || len(w.PendingCaptures) != 1 || w.PendingCaptures[0].OperationID != p.OperationID {
		t.Fatalf("recovery = %+v, %v", w, err)
	}
}
