package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

const (
	repo   = garden.RepoID(9)
	viewer = "fake-authenticated-id"
)

type commentStep struct {
	err   error
	apply bool // the comment lands although the call reports err
	// swallow reports success with a comment ID but stores nothing.
	swallow bool
}

// gh wraps the fake with scripted failures. Every other call passes through.
type gh struct {
	*gardenfake.Fake
	mu        sync.Mutex
	comments  []commentStep
	before    func()
	stageErr  error
	stageSkip bool // UpdateStage reports success without moving the label
	listErr   error
}

func (g *gh) AppendComment(ctx context.Context, r garden.RepoID, id garden.IssueID, body string) (garden.IssueComment, error) {
	g.mu.Lock()
	var step *commentStep
	if len(g.comments) > 0 {
		step, g.comments = &g.comments[0], g.comments[1:]
	}
	before := g.before
	if before != nil {
		g.before = nil
	}
	g.mu.Unlock()
	if before != nil {
		before()
	}
	if step != nil && step.swallow {
		return garden.IssueComment{ID: 99999}, nil
	}
	if step != nil && step.err != nil && !step.apply {
		return garden.IssueComment{}, step.err
	}
	c, err := g.Fake.AppendComment(ctx, r, id, body)
	if step != nil && step.err != nil && err == nil {
		return garden.IssueComment{}, step.err
	}
	return c, err
}

func (g *gh) UpdateStage(ctx context.Context, r garden.RepoID, id garden.IssueID, prev []string, from, to garden.Stage) error {
	g.mu.Lock()
	err, skip := g.stageErr, g.stageSkip
	g.stageErr, g.stageSkip = nil, false
	g.mu.Unlock()
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	return g.Fake.UpdateStage(ctx, r, id, prev, from, to)
}

func (g *gh) ListRepositoryIssues(ctx context.Context, r garden.RepoID, cursor string) (garden.IssuePage, error) {
	g.mu.Lock()
	err := g.listErr
	g.mu.Unlock()
	if err != nil {
		return garden.IssuePage{}, err
	}
	return g.Fake.ListRepositoryIssues(ctx, r, cursor)
}

// flakyDrafts fails the next Save whose draft matches.
type flakyDrafts struct {
	*drafts.Store
	mu   sync.Mutex
	fail func(drafts.Draft) bool
}

func (f *flakyDrafts) Save(ctx context.Context, s drafts.Scope, d drafts.Draft, exp uint64) (drafts.Draft, error) {
	f.mu.Lock()
	fail := f.fail != nil && f.fail(d)
	if fail {
		f.fail = nil
	}
	f.mu.Unlock()
	if fail {
		return drafts.Draft{}, errors.New("disk full")
	}
	return f.Store.Save(ctx, s, d, exp)
}

type harness struct {
	t      *testing.T
	fake   *gardenfake.Fake
	gh     *gh
	drafts *flakyDrafts
	ops    *ops.Store
	svc    *Service
	now    time.Time
	n      int
	scope  drafts.Scope
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	f := gardenfake.New()
	h := &harness{t: t, fake: f, gh: &gh{Fake: f}, ops: &ops.Store{Path: filepath.Join(dir, "ops.json")}, now: time.Unix(1_000_000, 0).UTC(),
		scope: drafts.Scope{Local: "local", Repo: repo}}
	st, err := drafts.New(filepath.Join(dir, "drafts", "drafts.json"), func() time.Time { return h.now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.drafts = &flakyDrafts{Store: st}
	h.svc = h.service()
	return h
}

func (h *harness) service() *Service {
	h.t.Helper()
	cap, err := capture.New(capture.Deps{Creator: h.gh, Reader: h.gh, Store: h.ops, Now: func() time.Time { return h.now }, NewNonce: func() string { h.n++; return fmt.Sprintf("%032x", h.n) }})
	if err != nil {
		h.t.Fatal(err)
	}
	ed, err := edit.New(edit.Deps{Reader: h.gh, Writer: h.gh})
	if err != nil {
		h.t.Fatal(err)
	}
	ap, err := approve.New(approve.Deps{Reader: h.gh, Writer: h.gh, Now: func() time.Time { return h.now }})
	if err != nil {
		h.t.Fatal(err)
	}
	svc, err := New(Deps{Drafts: h.drafts, Ops: h.ops, Capture: cap, Edit: ed, Approve: ap, Now: func() time.Time { return h.now }})
	if err != nil {
		h.t.Fatal(err)
	}
	return svc
}

func readyDraft() drafts.Draft {
	return drafts.Draft{Title: "Keep sessions", Type: garden.TypeFeature, Description: "Because switching hurts.", Spec: spec.Spec{
		Summary: "Keep sessions when switching.", CurrentBehavior: "Sessions drop.", DesiredBehavior: "Sessions stay.",
		Requirements: "- keep tokens", AcceptanceCriteria: "- switching keeps the session"}}
}

func (h *harness) draft(mut func(*drafts.Draft)) drafts.Draft {
	h.t.Helper()
	d := readyDraft()
	if mut != nil {
		mut(&d)
	}
	out, err := h.drafts.Save(context.Background(), h.scope, d, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) req(d drafts.Draft) Request {
	return Request{Local: "local", Repo: repo, RepoFullName: "acme/my-app", Viewer: viewer, Login: "fake-authenticated", DraftID: d.ID}
}

func (h *harness) preview(d drafts.Draft) Preview {
	h.t.Helper()
	p, err := h.svc.Preview(context.Background(), h.req(d))
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) read(d drafts.Draft) drafts.Draft {
	h.t.Helper()
	out, err := h.drafts.Read(context.Background(), h.scope, d.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) count(prefix string) int {
	n := 0
	for _, l := range h.fake.WriteLog() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (h *harness) comments(id garden.IssueID) int {
	c, err := h.fake.ListComments(context.Background(), repo, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return len(c)
}

func (h *harness) wantPublished(d drafts.Draft, res Result, err error) drafts.Draft {
	h.t.Helper()
	if err != nil || res.State != Published || res.Issue == nil {
		h.t.Fatalf("result = %+v, %v", res, err)
	}
	cur := h.read(d)
	if cur.Publication.State != drafts.Published || cur.Publication.VerifiedAt.IsZero() || cur.Publication.Issue == nil || *cur.Publication.Issue != *res.Issue {
		h.t.Fatalf("draft publication = %+v", cur.Publication)
	}
	return cur
}

func TestPublishHappyPath(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	if p.SpecText != spec.Render(d.Spec) || len(p.Labels) != 3 || !strings.HasPrefix(p.OperationID, "publish-") {
		t.Fatalf("preview = %+v", p)
	}
	if h.count("create") != 0 || h.read(d).Publication.State != drafts.Unpublished {
		t.Fatal("preview wrote something")
	}
	res, err := h.svc.Publish(context.Background(), p)
	cur := h.wantPublished(d, res, err)
	if h.count("create") != 1 || h.comments(res.Issue.ID) != 2 || res.Issue.URL != "https://github.com/acme/my-app/issues/1" {
		t.Fatalf("writes = %v, issue %+v", h.fake.WriteLog(), res.Issue)
	}
	issue, _ := h.fake.GetIssue(context.Background(), repo, res.Issue.ID)
	if !strings.Contains(strings.Join(issue.Labels, ","), garden.FormatStageLabel(garden.StageReady)) {
		t.Fatalf("labels = %v", issue.Labels)
	}
	if cur.Title != d.Title || cur.Description != d.Description || spec.Render(cur.Spec) != spec.Render(d.Spec) || cur.Publication.OperationID != p.OperationID {
		t.Fatalf("draft text changed: %+v", cur)
	}
	rec, err := h.ops.Get(context.Background(), p.OperationID)
	if err != nil || rec.State != ops.StateDone || rec.ApprovalCommentID == 0 || rec.SpecCommentID == 0 || rec.Draft != string(d.ID) {
		t.Fatalf("op = %+v, %v", rec, err)
	}
}

func TestDuplicateConfirmationSequential(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	first, err := h.svc.Publish(context.Background(), p)
	h.wantPublished(d, first, err)
	again, err := h.svc.Publish(context.Background(), p)
	h.wantPublished(d, again, err)
	other, err := h.service().Publish(context.Background(), p)
	h.wantPublished(d, other, err)
	if h.count("create") != 1 || h.comments(first.Issue.ID) != 2 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}

func TestDuplicateConfirmationConcurrent(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	var wg sync.WaitGroup
	results := make([]error, 8)
	states := make([]State, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := h.svc
			if i%2 == 1 {
				svc = h.service() // another process on the same files
			}
			res, err := svc.Publish(context.Background(), p)
			results[i], states[i] = err, res.State
		}()
	}
	wg.Wait()
	published := 0
	for i, err := range results {
		switch {
		case err == nil && states[i] == Published:
			published++
		case errors.Is(err, ErrInFlight):
		default:
			t.Fatalf("call %d: %v, %v", i, states[i], err)
		}
	}
	if published == 0 || h.count("create") != 1 || h.read(d).Publication.State != drafts.Published {
		t.Fatalf("published %d, writes %v", published, h.fake.WriteLog())
	}
}

func TestPublishRefusedWhileAnotherProcessHoldsTheOperation(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	lock, err := h.ops.TryOperationLock(context.Background(), p.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	if _, err := h.service().Publish(context.Background(), p); !errors.Is(err, ErrInFlight) {
		t.Fatalf("err = %v", err)
	}
	if h.count("create") != 0 || h.read(d).Publication.State != drafts.Unpublished {
		t.Fatal("something was written")
	}
}

func TestEditedDraftAfterPreviewIsStale(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	edited := d
	edited.Title = "Another title"
	if _, err := h.drafts.Save(context.Background(), h.scope, edited, d.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Publish(context.Background(), p); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v", err)
	}
	if h.count("create") != 0 || h.read(d).Publication.State != drafts.Unpublished {
		t.Fatal("something was written")
	}
}

func TestTamperedPreviewIsRefused(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	for name, mut := range map[string]func(*Preview){
		"viewer": func(p *Preview) { p.Viewer = "someone-else" },
		"spec":   func(p *Preview) { p.SpecText += "\n\n## Extra\nsneaky" },
		"title":  func(p *Preview) { p.Title = "Other" },
	} {
		p := h.preview(d)
		mut(&p)
		if _, err := h.svc.Publish(context.Background(), p); !errors.Is(err, ErrMismatch) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if h.count("create") != 0 {
		t.Fatal("created")
	}
}

func TestDefinitiveCreateFailureReturnsToPrivateAndRetries(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.fake.FailNext(garden.ErrRejected)
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Failed || res.Issue != nil || !errors.Is(res.Err, garden.ErrRejected) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	cur := h.read(d)
	if cur.Publication.State != drafts.Unpublished || h.count("create") != 0 {
		t.Fatalf("draft = %+v", cur.Publication)
	}
	res, err = h.svc.Publish(context.Background(), h.preview(cur))
	h.wantPublished(d, res, err)
	if h.count("create") != 1 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}

func TestUnknownCreateIsReconciledByNonce(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.fake.FailNextCreateAfterApply(errors.New("timeout"))
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	h.wantPublished(d, res, err)
	if h.count("create") != 1 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}

func TestUnknownCreateStaysUnknownUntilGitHubCanBeRead(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.fake.FailNextCreateAfterApply(errors.New("timeout"))
	h.gh.listErr = errors.New("offline")
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Unknown || res.Issue != nil || h.count("comment") != 0 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if cur := h.read(d); cur.Publication.State != drafts.Publishing || cur.Publication.Issue != nil {
		t.Fatalf("draft = %+v", cur.Publication)
	}
	if _, err := h.svc.Preview(context.Background(), h.req(d)); !errors.Is(err, ErrPublishing) {
		t.Fatalf("preview err = %v", err)
	}
	h.gh.listErr = nil
	res, err = h.svc.Resume(context.Background(), h.req(d))
	h.wantPublished(d, res, err)
	if h.count("create") != 1 || h.comments(res.Issue.ID) != 2 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}

func TestCrashAfterCreateBeforeDraftUpdateResumesWithTheSameIssue(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.drafts.fail = func(d drafts.Draft) bool {
		return d.Publication.Issue != nil && d.Publication.State == drafts.Publishing
	}
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Incomplete || res.Step != StepIssue || res.Issue == nil {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if cur := h.read(d); cur.Publication.Issue != nil || cur.Publication.State != drafts.Publishing {
		t.Fatalf("draft = %+v", cur.Publication)
	}
	again, err := h.service().Resume(context.Background(), h.req(d))
	h.wantPublished(d, again, err)
	if *again.Issue != *res.Issue || h.count("create") != 1 {
		t.Fatalf("issue %+v vs %+v, writes %v", again.Issue, res.Issue, h.fake.WriteLog())
	}
}

func TestAmbiguousSpecAppendResumesWithOneRevision(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.gh.comments = []commentStep{{err: errors.New("timeout"), apply: true}}
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Incomplete || res.Step != StepSpec || res.Issue == nil || res.Err == nil {
		t.Fatalf("result = %+v, %v", res, err)
	}
	again, err := h.svc.Resume(context.Background(), h.req(d))
	h.wantPublished(d, again, err)
	if h.comments(again.Issue.ID) != 2 {
		t.Fatalf("comments = %d, want one revision and one approval", h.comments(again.Issue.ID))
	}
}

func TestApprovalFailureIsIncompleteThenFinishes(t *testing.T) {
	for name, step := range map[string]commentStep{
		"not applied": {err: errors.New("offline")},
		"applied":     {err: errors.New("timeout"), apply: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			d := h.draft(nil)
			h.gh.comments = []commentStep{{}, step}
			res, err := h.svc.Publish(context.Background(), h.preview(d))
			if err != nil || res.State != Incomplete || res.Step != StepApproval || res.Issue == nil || res.SpecCommentID == 0 {
				t.Fatalf("result = %+v, %v", res, err)
			}
			if cur := h.read(d); cur.Publication.State != drafts.Publishing || cur.Publication.Issue == nil {
				t.Fatalf("draft = %+v", cur.Publication)
			}
			again, err := h.svc.Resume(context.Background(), h.req(d))
			h.wantPublished(d, again, err)
			if h.comments(again.Issue.ID) != 2 || h.count("create") != 1 {
				t.Fatalf("writes = %v", h.fake.WriteLog())
			}
		})
	}
}

func TestLabelProjectionFailureIsIncompleteThenFinishes(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.gh.stageErr = errors.New("labels offline")
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Incomplete || res.Step != StepApproval || !errors.Is(res.Err, approve.ErrProjection) || res.Issue == nil {
		t.Fatalf("result = %+v, %v", res, err)
	}
	again, err := h.svc.Resume(context.Background(), h.req(d))
	h.wantPublished(d, again, err)
	if h.comments(again.Issue.ID) != 2 || h.approvals(again.Issue.ID) != 1 {
		t.Fatalf("comments = %d, approvals = %d, want exactly one approval", h.comments(again.Issue.ID), h.approvals(again.Issue.ID))
	}
}

func (h *harness) approvals(id garden.IssueID) int {
	c, err := h.fake.ListComments(context.Background(), repo, id)
	if err != nil {
		h.t.Fatal(err)
	}
	n := 0
	for _, x := range c {
		if strings.Contains(x.Body, "approved by") {
			n++
		}
	}
	return n
}

func TestForeignSpecRevisionStopsBeforeApproval(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.gh.before = func() {
		body, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: 1}, "## Summary\nsomeone else", nil)
		if err != nil {
			t.Error(err)
			return
		}
		h.fake.AppendCommentAs(repo, 1, "mallory", "999", body)
	}
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Incomplete || res.Step != StepSpec || res.Issue == nil {
		t.Fatalf("result = %+v, %v", res, err)
	}
	again, err := h.svc.Resume(context.Background(), h.req(d))
	if err != nil || again.State != Incomplete {
		t.Fatalf("resume = %+v, %v", again, err)
	}
	if n := h.comments(res.Issue.ID); n != 2 {
		t.Fatalf("comments = %d: an approval was appended", n)
	}
	if h.read(d).Publication.State != drafts.Publishing {
		t.Fatal("draft must stay publishing")
	}
}

func TestPreviewRefusesBlockedDrafts(t *testing.T) {
	h := newHarness(t)
	for name, mut := range map[string]func(*drafts.Draft){
		"no criteria":    func(d *drafts.Draft) { d.Spec.AcceptanceCriteria = "" },
		"open question":  func(d *drafts.Draft) { d.Spec.OpenQuestions = "- which accounts?" },
		"no title":       func(d *drafts.Draft) { d.Title = "" },
		"no requirement": func(d *drafts.Draft) { d.Spec.Requirements = "" },
	} {
		d := h.draft(mut)
		if _, err := h.svc.Preview(context.Background(), h.req(d)); !errors.Is(err, ErrNotReady) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if h.count("create") != 0 {
		t.Fatal("created")
	}
}

func TestPreviewAndResumeStates(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	if _, err := h.svc.Resume(context.Background(), h.req(d)); !errors.Is(err, ErrStale) {
		t.Fatalf("resume of an unpublished draft: %v", err)
	}
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	h.wantPublished(d, res, err)
	if _, err := h.svc.Preview(context.Background(), h.req(d)); !errors.Is(err, ErrAlreadyPublished) {
		t.Fatalf("preview err = %v", err)
	}
	if again, err := h.svc.Resume(context.Background(), h.req(d)); err != nil || again.State != Published {
		t.Fatalf("resume = %+v, %v", again, err)
	}
}

func TestResumeRefusesAnotherAccount(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.gh.comments = []commentStep{{err: errors.New("offline")}}
	if res, err := h.svc.Publish(context.Background(), h.preview(d)); err != nil || res.State != Incomplete {
		t.Fatalf("result = %+v, %v", res, err)
	}
	r := h.req(d)
	r.Viewer = "someone-else"
	if _, err := h.svc.Resume(context.Background(), r); !errors.Is(err, ErrMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeSurvivesARepositoryRename(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	h.gh.comments = []commentStep{{err: errors.New("offline")}}
	res, err := h.svc.Publish(context.Background(), h.preview(d))
	if err != nil || res.State != Incomplete {
		t.Fatalf("result = %+v, %v", res, err)
	}
	r := h.req(d)
	r.RepoFullName = "acme/renamed"
	again, err := h.svc.Resume(context.Background(), r)
	h.wantPublished(d, again, err)
	if again.Issue.URL != res.Issue.URL || h.count("create") != 1 {
		t.Fatalf("issue %+v vs %+v", again.Issue, res.Issue)
	}
}

func TestReservationCrashBeforeDraftSaveIsReusedByRepublish(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	h.drafts.fail = func(d drafts.Draft) bool { return d.Publication.State == drafts.Publishing }
	if _, err := h.svc.Publish(context.Background(), p); err == nil {
		t.Fatal("reservation succeeded")
	}
	first, err := h.ops.Get(context.Background(), p.OperationID)
	if err != nil || first.Draft != string(d.ID) {
		t.Fatalf("op = %+v, %v", first, err)
	}
	res, err := h.svc.Publish(context.Background(), p)
	h.wantPublished(d, res, err)
	again, _ := h.ops.Get(context.Background(), p.OperationID)
	if !again.CreatedAt.Equal(first.CreatedAt) || h.count("create") != 1 {
		t.Fatalf("operation was not reused: %v", h.fake.WriteLog())
	}
}

func TestOperationBoundToAnotherViewerOrRevisionIsRefused(t *testing.T) {
	for name, mut := range map[string]func(*ops.Record){
		"viewer":   func(r *ops.Record) { r.Owner = "someone-else" },
		"revision": func(r *ops.Record) { r.DraftRevision++ },
		"draft":    func(r *ops.Record) { r.Draft = "draft-other" },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			d := h.draft(nil)
			p := h.preview(d)
			rec := ops.Record{ID: p.OperationID, Kind: ops.KindPublish, Local: "local", Repo: repo, State: ops.StatePending, Owner: viewer,
				ContentHash: p.ContentHash, Draft: string(d.ID), DraftRevision: p.DraftRevision, CreatedAt: h.now, UpdatedAt: h.now}
			mut(&rec)
			if _, err := h.ops.Update(context.Background(), rec.ID, func(ops.Record, bool) (ops.Record, error) { return rec, nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := h.svc.Publish(context.Background(), p); !errors.Is(err, ErrMismatch) {
				t.Fatalf("err = %v", err)
			}
			if h.count("create") != 0 || h.read(d).Publication.State != drafts.Unpublished {
				t.Fatal("something was written")
			}
		})
	}
}

func TestReadBackMismatchNeverMarksPublished(t *testing.T) {
	cases := map[string]func(*harness){
		"label not applied":     func(h *harness) { h.gh.stageSkip = true },
		"approval not recorded": func(h *harness) { h.gh.comments = []commentStep{{}, {swallow: true}} },
	}
	for name, arm := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			d := h.draft(nil)
			arm(h)
			res, err := h.svc.Publish(context.Background(), h.preview(d))
			if err != nil || res.State != Incomplete || !errors.Is(res.Err, approve.ErrUnverified) {
				t.Fatalf("result = %+v, %v", res, err)
			}
			if cur := h.read(d); cur.Publication.State != drafts.Publishing {
				t.Fatalf("draft = %+v", cur.Publication)
			}
			again, err := h.svc.Resume(context.Background(), h.req(d))
			h.wantPublished(d, again, err)
			if h.approvals(again.Issue.ID) != 1 || h.count("create") != 1 {
				t.Fatalf("writes = %v", h.fake.WriteLog())
			}
		})
	}
}

func TestVerifyRejectsAnApprovalThatIsNotTheRecordedOne(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	res, err := h.svc.Publish(context.Background(), p)
	h.wantPublished(d, res, err)
	rec, err := h.ops.Get(context.Background(), p.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.verify(context.Background(), p, rec); err != nil {
		t.Fatalf("verify of the recorded result: %v", err)
	}
	rec.ApprovalCommentID++
	if err := h.svc.verify(context.Background(), p, rec); !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v", err)
	}
	rec.ApprovalCommentID--
	rec.SpecCommentID++
	if err := h.svc.verify(context.Background(), p, rec); !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v", err)
	}
}

func TestChildCaptureInFlightIsResumableNotNothingStarted(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	lock, err := h.ops.TryOperationLock(context.Background(), p.OperationID+"-issue")
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.Publish(context.Background(), p)
	lock.Unlock()
	if err != nil || res.State != Unknown || res.Step != StepIssue || !errors.Is(res.Err, ErrInFlight) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if h.read(d).Publication.State != drafts.Publishing || h.count("create") != 0 {
		t.Fatal("draft must be publishing with nothing created")
	}
	again, err := h.svc.Resume(context.Background(), h.req(d))
	h.wantPublished(d, again, err)
	if h.count("create") != 1 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}

func TestFailedRevertLeavesAResumablePublication(t *testing.T) {
	h := newHarness(t)
	d := h.draft(nil)
	p := h.preview(d)
	h.fake.FailNext(garden.ErrRejected)
	h.drafts.fail = func(d drafts.Draft) bool { return d.Publication.State == drafts.Unpublished }
	res, err := h.svc.Publish(context.Background(), p)
	if err != nil || res.State != Unknown || res.Issue != nil || !errors.Is(res.Err, garden.ErrRejected) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if cur := h.read(d); cur.Publication.State != drafts.Publishing || cur.Publication.Issue != nil {
		t.Fatalf("draft = %+v", cur.Publication)
	}
	// A later step that cannot be recorded is resumable too, and still creates nothing.
	// Directory modes do not restrict writes on Windows, and root ignores them.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		dir := filepath.Dir(h.ops.Path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		stuck, err := h.svc.Resume(context.Background(), h.req(d))
		_ = os.Chmod(dir, 0o700)
		if err != nil || stuck.State != Unknown || stuck.Err == nil {
			t.Fatalf("resume with an unwritable store = %+v, %v", stuck, err)
		}
	}
	if h.count("create") != 0 {
		t.Fatal("created while stuck")
	}
	again, err := h.svc.Resume(context.Background(), h.req(d))
	h.wantPublished(d, again, err)
	if h.count("create") != 1 {
		t.Fatalf("writes = %v", h.fake.WriteLog())
	}
}
