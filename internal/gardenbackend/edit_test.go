package gardenbackend

import (
	"context"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// editHarness captures one draft through the backend and returns its Issue ID.
func editHarness(t *testing.T) (*harness, *gardenfake.Fake, uint64) {
	t.Helper()
	h, f := captureHarness(t)
	h.b.deps.Writer = f
	ctx := context.Background()
	p, err := h.b.PreviewCapture(ctx, gardenapi.CaptureRequest{Title: "Budget limits", Description: "Teams want a ceiling.\n\n## Requirements\nPer repo.", Type: garden.TypeFeature})
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.Capture(ctx, p)
	if err != nil || v.Issue == nil {
		t.Fatalf("capture = %+v, %v", v, err)
	}
	return h, f, v.Issue.ID
}

// comments counts the revision comments written since the fixture, which seeds two of its own.
func comments(f *gardenfake.Fake) int {
	n := 0
	for _, l := range f.WriteLog() {
		if strings.HasPrefix(l, "comment:") {
			n++
		}
	}
	return n - 2
}

func save(t *testing.T, h *harness, d gardenapi.DocumentView, content string) (gardenapi.SavePreviewView, gardenapi.SaveView, error) {
	t.Helper()
	ctx := context.Background()
	pv, err := h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: d.IssueID, Base: d.Base, Content: content})
	if err != nil || pv.Conflict != nil {
		return pv, gardenapi.SaveView{}, err
	}
	sv, err := h.b.Save(ctx, pv)
	return pv, sv, err
}

func TestDocumentSeedsFromTheBodyAndPreviewWritesNothing(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	d, err := h.b.Document(ctx, id)
	if err != nil || d.Number == 0 || d.Type != garden.TypeFeature || d.Base.HeadHash != "" || d.Conflict != nil {
		t.Fatalf("document = %+v, %v", d, err)
	}
	if !strings.Contains(d.Seed, "## Summary\nTeams want a ceiling.") || !strings.Contains(d.Seed, "## Requirements\nPer repo.") || strings.Contains(d.Body, "bonsai:garden") {
		t.Fatalf("seed = %q, body = %q", d.Seed, d.Body)
	}
	pv, err := h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: d.Seed + "\n\n## Constraints\nNo daemon."})
	if err != nil || pv.NewHash == "" || len(pv.Diff) != 1 || pv.Diff[0].Section != "Constraints" || pv.OperationID == "" || len(pv.Parents) != 0 {
		t.Fatalf("preview = %+v, %v", pv, err)
	}
	if comments(f) != 0 {
		t.Fatal("preview wrote")
	}
}

func TestSaveAppendsOneRevisionAndLeavesStageLabelsAndBodyAlone(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	before, _ := f.GetIssue(ctx, acmeID, garden.IssueID(id))
	d, _ := h.b.Document(ctx, id)
	pv, sv, err := save(t, h, d, d.Seed+"\n\n## Constraints\nNo daemon.")
	if err != nil || sv.State != gardenapi.SaveDone || sv.CommentID == 0 || sv.Hash != pv.NewHash {
		t.Fatalf("save = %+v, %v", sv, err)
	}
	for _, l := range f.WriteLog() {
		if l != "create" && !strings.HasPrefix(l, "comment:") {
			t.Fatalf("unexpected write %q", l)
		}
	}
	after, _ := f.GetIssue(ctx, acmeID, garden.IssueID(id))
	if after.Body != before.Body || after.Title != before.Title || strings.Join(after.Labels, ",") != strings.Join(before.Labels, ",") {
		t.Fatal("issue changed")
	}
	w, err := h.b.Work(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dr := work.Drafts(w.Snapshot.Items, work.Query{})
	rec, _, ok := dr.Find(garden.IssueID(id))
	if !ok || rec.Attention != "" || rec.Item.Classification.Stage != garden.StageDraft || len(rec.Item.Verification.Heads) != 1 {
		t.Fatalf("draft after save: %+v", rec)
	}
	d2, _ := h.b.Document(ctx, id)
	if d2.Base.HeadHash != sv.Hash || len(d2.Revisions) != 1 || !strings.Contains(d2.Seed, "No daemon.") {
		t.Fatalf("document after save = %+v", d2)
	}
	if rev := d2.Revisions[0]; rev.Author == "" || rev.At.IsZero() {
		t.Fatalf("no author data: %+v", rev)
	}
}

func TestSaveReplayAppendsOnce(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	d, _ := h.b.Document(ctx, id)
	pv, first, err := save(t, h, d, d.Seed+"\n\n## Constraints\nOnce.")
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.b.Save(ctx, pv)
	if err != nil || again.State != gardenapi.SaveDone || !again.Replayed || again.CommentID != first.CommentID || comments(f) != 1 {
		t.Fatalf("replay = %+v, %v (comments %d)", again, err, comments(f))
	}
}

func TestSaveConflictWhenARemoteRevisionLandsAndResolve(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	d, _ := h.b.Document(ctx, id)
	_, first, err := save(t, h, d, d.Seed+"\n\n## Constraints\nBase.")
	if err != nil {
		t.Fatal(err)
	}
	d = mustDoc(t, h, id)
	pv, err := h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: strings.Replace(d.Seed, "Base.", "Mine.", 1)})
	if err != nil || pv.Conflict != nil {
		t.Fatalf("preview = %+v, %v", pv, err)
	}
	// A phone edit appends a competing revision on the same head.
	theirs, _ := garden.NewSpecRevision(garden.IssueIdentity{Repo: acmeID, Issue: garden.IssueID(id)}, strings.Replace(d.Seed, "Base.", "Theirs.", 1), []string{first.Hash})
	f.AppendCommentAs(acmeID, garden.IssueID(id), "phone-user", "2002", theirs)
	before := comments(f)
	sv, err := h.b.Save(ctx, pv)
	if err != nil || sv.State != gardenapi.SaveConflict || sv.Conflict == nil || comments(f) != before {
		t.Fatalf("save = %+v, %v", sv, err)
	}
	c := sv.Conflict
	if len(c.Remote) != 1 || c.Remote[0].Author != "phone-user" || !strings.Contains(c.Remote[0].Content, "Theirs.") || !strings.Contains(c.Yours, "Mine.") {
		t.Fatalf("conflict = %+v", c)
	}
	rp, err := h.b.PreviewResolve(ctx, gardenapi.ResolveRequest{IssueID: id, Conflict: *c, Result: strings.Replace(d.Seed, "Base.", "Both.", 1)})
	if err != nil || rp.Conflict != nil || len(rp.Parents) != 1 || rp.Resolution == nil {
		t.Fatalf("resolve preview = %+v, %v", rp, err)
	}
	rv, err := h.b.Resolve(ctx, rp)
	if err != nil || rv.State != gardenapi.SaveDone || comments(f) != before+1 {
		t.Fatalf("resolve = %+v, %v", rv, err)
	}
	if got := mustDoc(t, h, id); got.Conflict != nil || !strings.Contains(got.Seed, "Both.") {
		t.Fatalf("after resolve = %+v", got)
	}
}

func mustDoc(t *testing.T, h *harness, id uint64) gardenapi.DocumentView {
	t.Helper()
	d, err := h.b.Document(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDocumentReportsCompetingHeadsAsAConflict(t *testing.T) {
	h, f, id := editHarness(t)
	for _, text := range []string{"## Summary\nA.", "## Summary\nB."} {
		body, _ := garden.NewSpecRevision(garden.IssueIdentity{Repo: acmeID, Issue: garden.IssueID(id)}, text, nil)
		f.AppendCommentAs(acmeID, garden.IssueID(id), "someone", "2002", body)
	}
	d := mustDoc(t, h, id)
	if d.Conflict == nil || len(d.Conflict.Remote) != 2 || d.Seed != "" {
		t.Fatalf("document = %+v", d)
	}
}

type lossyWriter struct {
	*gardenfake.Fake
	lose bool
}

func (w *lossyWriter) AppendComment(ctx context.Context, r garden.RepoID, id garden.IssueID, body string) (garden.IssueComment, error) {
	c, err := w.Fake.AppendComment(ctx, r, id, body)
	if err == nil && w.lose {
		w.lose = false
		return garden.IssueComment{}, garden.ErrOffline
	}
	return c, err
}

func TestAmbiguousSaveReportsUnknownAndRetryDoesNotDuplicate(t *testing.T) {
	h, f, id := editHarness(t)
	w := &lossyWriter{Fake: f, lose: true}
	h.b.deps.Writer = w
	h.b.edits = nil
	ctx := context.Background()
	d := mustDoc(t, h, id)
	pv, sv, err := save(t, h, d, d.Seed+"\n\n## Constraints\nUnsure.")
	if err != nil || sv.State != gardenapi.SaveUnknown || sv.Err == nil || sv.Err.Code != gardenapi.CodeSaveUnknown {
		t.Fatalf("save = %+v, %v", sv, err)
	}
	again, err := h.b.Save(ctx, pv)
	if err != nil || again.State != gardenapi.SaveDone || !again.Replayed || comments(f) != 1 {
		t.Fatalf("retry = %+v, %v (comments %d)", again, err, comments(f))
	}
}

func TestEditErrorsAreSafeAndWriteNothing(t *testing.T) {
	h, f, id := editHarness(t)
	ctx := context.Background()
	d := mustDoc(t, h, id)
	_, err := h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: d.Seed})
	mustErrCode(t, err, gardenapi.CodeNoChange)
	_, err = h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: "  "})
	mustErrCode(t, err, gardenapi.CodeInvalidInput)
	f.FailNext(garden.ErrOffline)
	_, err = h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: d.Seed + "x"})
	mustErrCode(t, err, gardenapi.CodeOffline)
	f.AppendCommentAs(acmeID, garden.IssueID(id), "x", "9", "<!-- bonsai:garden/v1 {broken} -->\nbody")
	_, err = h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: d.Seed + "x"})
	mustErrCode(t, err, gardenapi.CodeDocumentIncomplete)
	_, err = h.b.Document(ctx, 999999)
	mustErrCode(t, err, gardenapi.CodeNotFound)
	if comments(f) != 0 {
		t.Fatal("an error path wrote")
	}
}

func TestEditNeedsWriteAccessButReadingDoesNot(t *testing.T) {
	h, f, id := editHarness(t)
	h.gh.Mu.Lock()
	h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessRead)
	h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessRead)
	h.gh.Mu.Unlock()
	ctx := context.Background()
	d, err := h.b.Document(ctx, id)
	if err != nil {
		t.Fatalf("read-only Document: %v", err)
	}
	_, err = h.b.PreviewSave(ctx, gardenapi.SaveRequest{IssueID: id, Base: d.Base, Content: d.Seed + "x"})
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	_, err = h.b.Save(ctx, gardenapi.SavePreviewView{Stamp: gardenapi.Stamp{LocalRepoID: h.b.local.String(), GardenID: uint64(acmeID)}, IssueID: id, Content: "## Summary\nx"})
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	if comments(f) != 0 {
		t.Fatal("read-only user wrote")
	}
}
