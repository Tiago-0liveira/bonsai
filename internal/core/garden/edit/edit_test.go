package edit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

const (
	repo   = garden.RepoID(9)
	issue  = garden.IssueID(7)
	viewer = "fake-authenticated-id"
)

type harness struct {
	t    *testing.T
	fake *gardenfake.Fake
	svc  *Service
	tgt  Target
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	f := gardenfake.New()
	body, err := garden.CaptureBody(viewer, strings.Repeat("a", 32), "Teams want limits.\n\n## Requirements\nA ceiling.")
	if err != nil {
		t.Fatal(err)
	}
	f.SeedIssue(garden.Issue{Repo: repo, ID: issue, Number: 7, Title: "Budget", Body: body, Metadata: garden.BodyMetadata(body),
		Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}})
	svc, err := New(Deps{Reader: f, Writer: f})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, fake: f, svc: svc, tgt: Target{Repo: repo, Issue: issue, Viewer: viewer}}
}

func (h *harness) load() Document {
	h.t.Helper()
	d, err := h.svc.Load(context.Background(), h.tgt)
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

// remote posts a spec revision as another account.
func (h *harness) remote(content string, parents ...string) garden.IssueComment {
	h.t.Helper()
	body, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, content, parents)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.fake.AppendCommentAs(repo, issue, "mobile-user", "2002", body)
}

func (h *harness) appends() int {
	n := 0
	for _, l := range h.fake.WriteLog() {
		if strings.HasPrefix(l, "comment:") {
			n++
		}
	}
	return n
}

func (h *harness) save(d Document, edit func(*spec.Spec)) (Result, Outcome, error) {
	h.t.Helper()
	s := spec.Parse(d.Seed)
	edit(&s)
	out, err := h.svc.Preview(context.Background(), h.tgt, d.Base, spec.Render(s))
	if err != nil {
		return Result{}, Outcome{}, err
	}
	if out.Conflict != nil {
		return Result{}, out, nil
	}
	return h.svc.Commit(context.Background(), *out.Save)
}

func TestLoadSeedsFromTheBodyUntilARevisionExists(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	if len(d.Heads) != 0 || d.Base.HeadHash != "" || d.Body != "Teams want limits.\n\n## Requirements\nA ceiling." {
		t.Fatalf("doc = %+v", d)
	}
	s := spec.Parse(d.Seed)
	if s.Summary != "Teams want limits." || s.Requirements != "A ceiling." {
		t.Fatalf("seed = %q", d.Seed)
	}
	if d.Base.BodyHash != garden.ContentHash(d.Body) || d.Base.Content != d.Seed {
		t.Fatal("base does not describe the seed")
	}
}

func TestSaveAppendsOneRevisionAndNeverTouchesBodyTitleOrLabels(t *testing.T) {
	h := newHarness(t)
	before, _ := h.fake.GetIssue(context.Background(), repo, issue)
	d := h.load()
	res, out, err := h.save(d, func(s *spec.Spec) { s.AcceptanceCriteria = "It works." })
	if err != nil || out.Conflict != nil || res.CommentID == 0 || res.Replayed || res.Conflict != nil {
		t.Fatalf("save = %+v %+v, %v", res, out, err)
	}
	if got := h.fake.WriteLog(); len(got) != 1 || !strings.HasPrefix(got[0], "comment:") {
		t.Fatalf("writes = %v", got)
	}
	after, _ := h.fake.GetIssue(context.Background(), repo, issue)
	if after.Body != before.Body || after.Title != before.Title || strings.Join(after.Labels, ",") != strings.Join(before.Labels, ",") {
		t.Fatal("issue changed")
	}
	d2 := h.load()
	if len(d2.Heads) != 1 || d2.Heads[0].CommentID != res.CommentID || d2.Heads[0].Hash != res.Hash {
		t.Fatalf("heads = %+v", d2.Heads)
	}
	if !strings.Contains(d2.Seed, "It works.") || d2.Base.HeadHash != res.Hash {
		t.Fatalf("seed = %q", d2.Seed)
	}
	if len(d2.Heads[0].Author) == 0 || d2.Heads[0].At.IsZero() {
		t.Fatalf("no author data: %+v", d2.Heads[0])
	}
	// The next edit chains on the new head.
	res2, _, err := h.save(d2, func(s *spec.Spec) { s.Constraints = "No daemon." })
	if err != nil || res2.CommentID == 0 || len(h.load().Heads) != 1 {
		t.Fatalf("second save = %+v, %v", res2, err)
	}
}

func TestPreviewDiffNamesChangedSections(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	s := spec.Parse(d.Seed)
	s.Requirements = "A ceiling.\nPer repository."
	s.OpenQuestions = "Who sets it?"
	out, err := h.svc.Preview(context.Background(), h.tgt, d.Base, spec.Render(s))
	if err != nil || out.Save == nil {
		t.Fatalf("%+v, %v", out, err)
	}
	want := map[string][2]int{"Requirements": {1, 0}, "Open questions": {1, 0}}
	if len(out.Save.Diff) != 2 {
		t.Fatalf("diff = %+v", out.Save.Diff)
	}
	for _, sd := range out.Save.Diff {
		if w, ok := want[sd.Section]; !ok || sd.Added != w[0] || sd.Removed != w[1] {
			t.Errorf("diff %+v", sd)
		}
	}
	if h.appends() != 0 {
		t.Fatal("preview wrote")
	}
}

func TestRemoteRevisionBetweenPreviewAndCommitIsAConflictNotAWrite(t *testing.T) {
	h := newHarness(t)
	first, _, err := h.save(h.load(), func(s *spec.Spec) { s.Summary = "Base summary." })
	if err != nil {
		t.Fatal(err)
	}
	d := h.load()
	s := spec.Parse(d.Seed)
	s.Requirements = "Mine."
	out, err := h.svc.Preview(context.Background(), h.tgt, d.Base, spec.Render(s))
	if err != nil || out.Save == nil {
		t.Fatal(err)
	}
	theirs := spec.Parse(d.Seed)
	theirs.Requirements = "Theirs."
	h.remote(spec.Render(theirs), first.Hash)
	before := h.appends()
	res, got, err := h.svc.Commit(context.Background(), *out.Save)
	if err != nil || got.Conflict == nil || res.CommentID != 0 || h.appends() != before {
		t.Fatalf("commit = %+v %+v, %v (appends %d -> %d)", res, got, err, before, h.appends())
	}
	c := got.Conflict
	if len(c.Remote) != 1 || c.Remote[0].Author != "mobile-user" || c.Remote[0].Source != Source || !strings.Contains(c.Remote[0].Content, "Theirs.") || !strings.Contains(c.Yours, "Mine.") {
		t.Fatalf("conflict = %+v", c)
	}
}

func TestRemoteRevisionBetweenCommitReadAndAppendLeavesBothHeadsVisible(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	s := spec.Parse(d.Seed)
	s.Requirements = "Mine."
	out, _ := h.svc.Preview(context.Background(), h.tgt, d.Base, spec.Render(s))
	// A collaborator appends after our verification read but before our append lands.
	h.fake.BeforeNextAppend(func() { h.remote("## Summary\nTheirs, no parent.") })
	res, got, err := h.svc.Commit(context.Background(), *out.Save)
	if err != nil || got.Conflict == nil || !got.Conflict.Saved || got.Conflict.SavedCommentID != res.CommentID || res.Conflict == nil {
		t.Fatalf("commit = %+v %+v, %v", res, got, err)
	}
	if len(got.Conflict.Remote) != 2 {
		t.Fatalf("remote heads = %+v", got.Conflict.Remote)
	}
	if doc := h.load(); len(doc.Heads) != 2 || doc.Seed != "" {
		t.Fatalf("heads = %d, seed %q", len(doc.Heads), doc.Seed)
	}
}

func TestRemoteRevisionBetweenAppendAndRereadIsAConflict(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	out, _ := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nMine.")
	h.fake.AfterNextAppend(func() { h.remote("## Summary\nTheirs.") })
	res, got, err := h.svc.Commit(context.Background(), *out.Save)
	if err != nil || got.Conflict == nil || !got.Conflict.Saved || res.CommentID == 0 {
		t.Fatalf("commit = %+v %+v, %v", res, got, err)
	}
}

func TestResolveSupersedesEveryHeadAndRechecksThem(t *testing.T) {
	h := newHarness(t)
	a := h.remote("## Summary\nA.")
	b := h.remote("## Summary\nB.")
	doc := h.load()
	if len(doc.Heads) != 2 {
		t.Fatalf("heads = %d", len(doc.Heads))
	}
	conflict := Conflict{Base: doc.Base, Yours: "", Remote: doc.Heads, BodyHash: doc.Base.BodyHash}
	// A third head lands after the conflict was shown: the resolution must not be applied.
	c := h.remote("## Summary\nC.")
	before := h.appends()
	res, out, err := h.svc.Resolve(context.Background(), h.tgt, conflict, "## Summary\nMerged.")
	if err != nil || out.Conflict == nil || res.CommentID != 0 || h.appends() != before || len(out.Conflict.Remote) != 3 {
		t.Fatalf("stale resolve = %+v %+v, %v", res, out, err)
	}
	conflict.Remote = out.Conflict.Remote
	res, out, err = h.svc.Resolve(context.Background(), h.tgt, conflict, "## Summary\nMerged.")
	if err != nil || out.Conflict != nil || res.CommentID == 0 {
		t.Fatalf("resolve = %+v %+v, %v", res, out, err)
	}
	after := h.load()
	if len(after.Heads) != 1 || after.Heads[0].CommentID != res.CommentID {
		t.Fatalf("heads = %+v", after.Heads)
	}
	merged := after.Heads[0]
	rev := h.parents(merged.CommentID)
	if len(rev) != 3 {
		t.Fatalf("parents = %v, want %d..%d", rev, a.ID, c.ID)
	}
	_ = b
}

func (h *harness) parents(commentID int64) []string {
	cs, _ := h.fake.ListComments(context.Background(), repo, issue)
	conv := make([]garden.Comment, 0, len(cs))
	for _, c := range cs {
		conv = append(conv, garden.Comment{ID: c.ID, Author: c.Author, AuthorID: c.AuthorID, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	v := garden.Verify(garden.IssueIdentity{Repo: repo, Issue: issue}, conv, garden.ApprovalPolicy{})
	for _, r := range v.Revisions {
		if r.CommentID == commentID {
			return r.Parents
		}
	}
	return nil
}

func TestPreviewResolveReportsParentsAndDiffWithoutWriting(t *testing.T) {
	h := newHarness(t)
	h.remote("## Summary\nA.")
	h.remote("## Summary\nB.")
	doc := h.load()
	out, err := h.svc.PreviewResolve(context.Background(), h.tgt, Conflict{Base: doc.Base, Remote: doc.Heads, BodyHash: doc.Base.BodyHash}, "## Summary\nMerged.\n\n## Constraints\nNone.")
	if err != nil || out.Save == nil || len(out.Save.Parents) != 2 || len(out.Save.Diff) == 0 || h.appends() != 0 {
		t.Fatalf("%+v, %v", out, err)
	}
}

func TestHashCollisionsAreRefusedExceptAnExactReplay(t *testing.T) {
	h := newHarness(t)
	first, _, err := h.save(h.load(), func(s *spec.Spec) { s.Summary = "One." })
	if err != nil {
		t.Fatal(err)
	}
	d1 := h.load()
	if _, _, err = h.save(d1, func(s *spec.Spec) { s.Summary = "Two." }); err != nil {
		t.Fatal(err)
	}
	d2 := h.load()
	// Same text as the current head: nothing to save.
	_, err = h.svc.Preview(context.Background(), h.tgt, d2.Base, d2.Seed)
	if !errors.Is(err, ErrNoChange) {
		t.Fatalf("no-change: %v", err)
	}
	// Text equal to an earlier revision would collide with its hash.
	_, err = h.svc.Preview(context.Background(), h.tgt, d2.Base, d1.Seed)
	var earlier MatchesEarlierError
	if !errors.As(err, &earlier) || earlier.CommentID != first.CommentID || !strings.Contains(err.Error(), "revert is not supported yet") {
		t.Fatalf("earlier: %v", err)
	}
	if h.appends() != 2 {
		t.Fatalf("appends = %d", h.appends())
	}
}

func TestReplayedCommitReturnsTheSameRevisionAndAppendsOnce(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	out, _ := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nOnce.")
	first, _, err := h.svc.Commit(context.Background(), *out.Save)
	if err != nil {
		t.Fatal(err)
	}
	again, got, err := h.svc.Commit(context.Background(), *out.Save)
	if err != nil || got.Conflict != nil || !again.Replayed || again.CommentID != first.CommentID || h.appends() != 1 {
		t.Fatalf("replay = %+v %+v, %v (appends %d)", again, got, err, h.appends())
	}
	// Even after someone else built on top of it, the replay still finds it.
	h.remote("## Summary\nLater.", first.Hash)
	again, _, err = h.svc.Commit(context.Background(), *out.Save)
	if err != nil || !again.Replayed || h.appends() != 1 {
		t.Fatalf("late replay = %+v, %v", again, err)
	}
}

func TestAmbiguousAppendFailureThenRetryDoesNotDuplicate(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	out, _ := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nSlow.")
	// The server stores the comment but the answer is lost.
	failing := &failAfter{Fake: h.fake}
	svc, _ := New(Deps{Reader: h.fake, Writer: failing})
	if _, _, err := svc.Commit(context.Background(), *out.Save); err == nil {
		t.Fatal("append error swallowed")
	}
	again, _, err := svc.Commit(context.Background(), *out.Save)
	if err != nil || !again.Replayed || h.appends() != 1 {
		t.Fatalf("retry = %+v, %v (appends %d)", again, err, h.appends())
	}
}

type failAfter struct {
	*gardenfake.Fake
	done bool
}

func (f *failAfter) AppendComment(ctx context.Context, r garden.RepoID, id garden.IssueID, body string) (garden.IssueComment, error) {
	c, err := f.Fake.AppendComment(ctx, r, id, body)
	if err == nil && !f.done {
		f.done = true
		return garden.IssueComment{}, garden.ErrOffline
	}
	return c, err
}

func TestBodyEditedOnGitHubIsFlaggedAndOnlyBlocksBodySeededEdits(t *testing.T) {
	h := newHarness(t)
	d := h.load() // seeded from the body
	h.fake.SimulateConcurrentEdit(repo, issue, garden.MetadataPrefix+"{} -->\nA phone edit of the body.")
	out, err := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nMine.")
	if err != nil || out.Conflict == nil || len(out.Conflict.Remote) != 1 || out.Conflict.Remote[0].Source != BodySource || !strings.Contains(out.Conflict.Remote[0].Content, "A phone edit") {
		t.Fatalf("body-seeded edit: %+v, %v", out, err)
	}
	// With a revision as the base, the body is not part of the specification: flagged, not blocking.
	h2 := newHarness(t)
	if _, _, err = h2.save(h2.load(), func(s *spec.Spec) { s.Summary = "S." }); err != nil {
		t.Fatal(err)
	}
	d2 := h2.load()
	h2.fake.SimulateConcurrentEdit(repo, issue, garden.MetadataPrefix+"{} -->\nChanged body.")
	out, err = h2.svc.Preview(context.Background(), h2.tgt, d2.Base, "## Summary\nMine.")
	if err != nil || out.Save == nil || !out.Save.BodyChanged {
		t.Fatalf("revision-based edit: %+v, %v", out, err)
	}
}

func TestIncompleteOrBrokenLogsNeverWrite(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	h.fake.FailNext(garden.ErrOffline)
	if _, err := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nx"); !errors.Is(err, garden.ErrOffline) {
		t.Fatalf("offline: %v", err)
	}
	h.fake.AppendCommentAs(repo, issue, "x", "9", garden.MetadataPrefix+"bad")
	h.fake.AppendCommentAs(repo, issue, "x", "9", "<!-- bonsai:garden/v1 {broken} -->\nbody")
	if _, err := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nx"); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("integrity: %v", err)
	}
	if h.appends() != 0 {
		t.Fatal("wrote on a broken log")
	}
}

func TestContentValidation(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	if _, err := h.svc.Preview(context.Background(), h.tgt, d.Base, "  \n"); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
	huge := "## Summary\n" + strings.Repeat("x", MaxSpecBytes)
	if _, err := h.svc.Preview(context.Background(), h.tgt, d.Base, huge); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
	if _, err := New(Deps{}); err == nil {
		t.Fatal("New accepted no seams")
	}
}

func TestConcurrentCommitsOfOnePreviewAppendOnce(t *testing.T) {
	h := newHarness(t)
	d := h.load()
	out, _ := h.svc.Preview(context.Background(), h.tgt, d.Base, "## Summary\nRace.")
	done := make(chan Result, 6)
	for i := 0; i < 6; i++ {
		go func() {
			r, _, _ := h.svc.Commit(context.Background(), *out.Save)
			done <- r
		}()
	}
	var ids = map[int64]bool{}
	for i := 0; i < 6; i++ {
		ids[(<-done).CommentID] = true
	}
	if h.appends() != 1 || len(ids) != 1 {
		t.Fatalf("appends %d, distinct ids %d", h.appends(), len(ids))
	}
}

func TestLineDiffCounts(t *testing.T) {
	for name, tc := range map[string]struct {
		a, b           string
		added, removed int
	}{
		"added":      {"a", "a\nb", 1, 0},
		"removed":    {"a\nb", "a", 0, 1},
		"changed":    {"a\nb\nc", "a\nX\nc", 1, 1},
		"from empty": {"", "a\nb", 2, 0},
		"to empty":   {"a", "", 0, 1},
		"moved":      {"a\nb\nc", "c\nb\na", 2, 2},
	} {
		d := Diff(spec.Spec{Summary: tc.a}, spec.Spec{Summary: tc.b})
		if len(d) != 1 || d[0].Added != tc.added || d[0].Removed != tc.removed {
			t.Errorf("%s: %+v", name, d)
		}
	}
	if d := Diff(spec.Spec{Summary: "same"}, spec.Spec{Summary: "same"}); len(d) != 0 {
		t.Fatalf("diff of equal specs: %+v", d)
	}
	ext := Diff(spec.Spec{}, spec.Spec{Extra: []spec.Section{{Title: "Rollout", Text: "x"}}})
	if len(ext) != 1 || ext[0].Section != "Rollout" {
		t.Fatalf("extra: %+v", ext)
	}
}
