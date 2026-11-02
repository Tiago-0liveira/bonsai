package gardenbackend

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func publishHarness(t *testing.T) (*harness, *gardenfake.Fake, drafts.Draft) {
	t.Helper()
	h, f := captureHarness(t)
	h.b.deps.Writer = f
	store, err := drafts.New(filepath.Join(t.TempDir(), "drafts", "drafts.json"), time.Now, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.b.deps.PrivateDrafts = store
	d, err := store.Save(context.Background(), drafts.Scope{Local: h.b.local, Repo: acmeID}, drafts.Draft{Title: "Keep sessions", Type: garden.TypeFeature,
		Description: "Because.", Spec: spec.Spec{Summary: "Keep sessions.", CurrentBehavior: "They drop.", DesiredBehavior: "They stay.",
			Requirements: "- keep tokens", AcceptanceCriteria: "- sessions survive"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h, f, d
}

func TestPublishPreviewConfirmAndReplay(t *testing.T) {
	h, f, d := publishHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewPublish(ctx, acmeID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.RepoFullName != "acme/my-app" || p.ActorLogin != viewerName || p.DraftRevision != d.Revision || p.DraftLabel != drafts.DraftLabel(d.ID) || len(p.Labels) != 3 || p.SpecText == "" {
		t.Fatalf("preview = %+v", p)
	}
	if creates(f) != 0 {
		t.Fatal("preview wrote")
	}
	v, err := h.b.Publish(ctx, acmeID, p)
	if err != nil || v.State != gardenapi.PublishDone || v.IssueNumber == 0 || v.IssueURL == "" || v.Err != nil {
		t.Fatalf("publish = %+v, %v", v, err)
	}
	again, err := h.b.Publish(ctx, acmeID, p)
	if err != nil || again.State != gardenapi.PublishDone || again.IssueNumber != v.IssueNumber || creates(f) != 1 {
		t.Fatalf("replay = %+v, %v, creates %d", again, err, creates(f))
	}
	if _, err := h.b.PreviewPublish(ctx, acmeID, d.ID); err == nil {
		t.Fatal("published draft previewed again")
	}
}

func TestPublishNeedsReadyDraftAndMatchingRepository(t *testing.T) {
	h, f, d := publishHarness(t)
	ctx := context.Background()
	blocked := d
	blocked.Spec.AcceptanceCriteria = ""
	store := h.b.deps.PrivateDrafts
	b, err := store.Save(ctx, drafts.Scope{Local: h.b.local, Repo: acmeID}, blocked, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.b.PreviewPublish(ctx, acmeID, b.ID)
	mustErrCode(t, err, gardenapi.CodePublishNotReady)
	_, err = h.b.PreviewPublish(ctx, acmeID+1, b.ID)
	mustErrCode(t, err, gardenapi.CodePublishStale)
	if creates(f) != 0 {
		t.Fatal("wrote")
	}
}

func TestPublishIsStaleWhenAccessOrDraftChanges(t *testing.T) {
	h, f, d := publishHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewPublish(ctx, acmeID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.gh.Mu.Lock()
	h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessRead)
	h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessRead)
	h.gh.Mu.Unlock()
	_, err = h.b.Publish(ctx, acmeID, p)
	mustErrCode(t, err, gardenapi.CodePublishStale)
	h.gh.Mu.Lock()
	h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessWrite)
	h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessWrite)
	h.gh.Mu.Unlock()
	edited := d
	edited.Title = "Changed"
	if _, err := h.b.deps.PrivateDrafts.Save(ctx, drafts.Scope{Local: h.b.local, Repo: acmeID}, edited, d.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = h.b.Publish(ctx, acmeID, p)
	mustErrCode(t, err, gardenapi.CodePublishStale)
	other := p
	other.ActorID = "someone-else"
	_, err = h.b.Publish(ctx, acmeID, other)
	mustErrCode(t, err, gardenapi.CodePublishStale)
	if creates(f) != 0 {
		t.Fatalf("created: %v", f.WriteLog())
	}
}

func TestPublishIncompleteKeepsTheIssueLinkAndResumes(t *testing.T) {
	h, f, d := publishHarness(t)
	ctx := context.Background()
	p, err := h.b.PreviewPublish(ctx, acmeID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The spec append lands; the approval append then fails.
	f.AfterNextAppend(func() { f.BeforeNextAppend(func() { f.FailNext(garden.ErrOffline) }) })
	v, err := h.b.Publish(ctx, acmeID, p)
	if err != nil || v.State != gardenapi.PublishIncomplete || v.IssueNumber == 0 || v.Err == nil || v.Err.Code != gardenapi.CodePublishIncomplete {
		t.Fatalf("publish = %+v, %v", v, err)
	}
	r, err := h.b.ResumePublish(ctx, acmeID, d.ID)
	if err != nil || r.State != gardenapi.PublishDone || r.IssueNumber != v.IssueNumber || creates(f) != 1 {
		t.Fatalf("resume = %+v, %v", r, err)
	}
}

func TestPublishChildCapturesAreNotPendingIdeas(t *testing.T) {
	h, _ := captureHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"publish-abc-issue", "cap-visible"} {
		_, err := h.b.deps.Ops.Update(ctx, id, func(ops.Record, bool) (ops.Record, error) {
			return ops.Record{ID: id, Kind: ops.KindCapture, Local: h.b.local, Repo: acmeID, State: ops.StateUnknown, Owner: "1", ContentHash: "h", CreatedAt: now, UpdatedAt: now}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.b.pendingCaptures(ctx, 1, acmeID)
	if err != nil || len(got) != 1 || got[0].OperationID != "cap-visible" {
		t.Fatalf("pending = %+v, %v", got, err)
	}
}

func TestPublishUnavailableWithoutSeams(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	_, err := h.b.PreviewPublish(context.Background(), acmeID, "draft-x")
	mustErrCode(t, err, gardenapi.CodeFailed)
}
