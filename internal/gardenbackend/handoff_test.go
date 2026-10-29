package gardenbackend

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func hx(c string, n int) string { return strings.Repeat(c, n) }

type fakeHandoffWork struct {
	mu    sync.Mutex
	id    WorkIdentity
	err   error
	calls int
}

func (f *fakeHandoffWork) Inspect(context.Context, string, string) (WorkIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.id, f.err
}

func (f *fakeHandoffWork) set(fn func(*WorkIdentity)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.id)
}

type fakeHandoffPRs struct {
	mu     sync.Mutex
	detail github.PullRequestDetail
	err    error
	calls  int
}

func (f *fakeHandoffPRs) PullRequest(_ context.Context, repo string, n int) (github.PullRequestDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if repo != "acme/my-app" {
		return github.PullRequestDetail{}, fmt.Errorf("unexpected repo %q", repo)
	}
	return f.detail, f.err
}

// countingWorktrees counts the worktree effects handoff must never cause.
type countingWorktrees struct {
	launch.Worktrees
	mu              sync.Mutex
	removes, pushes int
}

func (c *countingWorktrees) RemoveIsolation(ctx context.Context, r domain.IsolationRemoveRequest) error {
	c.mu.Lock()
	c.removes++
	c.mu.Unlock()
	return c.Worktrees.RemoveIsolation(ctx, r)
}

func (c *countingWorktrees) PushIsolation(ctx context.Context, r domain.IsolationPushRequest) (domain.PublishedHead, error) {
	c.mu.Lock()
	c.pushes++
	c.mu.Unlock()
	return c.Worktrees.PushIsolation(ctx, r)
}

type countingWriter struct {
	garden.IssueWriter
	mu                sync.Mutex
	appends, stageOps int
}

func (c *countingWriter) AppendComment(ctx context.Context, repo garden.RepoID, issue garden.IssueID, body string) (garden.IssueComment, error) {
	c.mu.Lock()
	c.appends++
	c.mu.Unlock()
	return c.IssueWriter.AppendComment(ctx, repo, issue, body)
}

func (c *countingWriter) UpdateStage(ctx context.Context, repo garden.RepoID, issue garden.IssueID, labels []string, from, to garden.Stage) error {
	c.mu.Lock()
	c.stageOps++
	c.mu.Unlock()
	return c.IssueWriter.UpdateStage(ctx, repo, issue, labels, from, to)
}

func (c *countingWriter) counts() (appends, stages int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appends, c.stageOps
}

type handoffRig struct {
	t       *testing.T
	h       *harness
	issue   uint64
	runtime *fakeLaunchBackendRuntime
	wt      *countingWorktrees
	writer  *countingWriter
	reviews *fakeLaunchReviewStore
	work    *fakeHandoffWork
	prs     *fakeHandoffPRs
	attempt launch.Attempt
	opened  []string
	stages0 int
}

var handoffHead = hx("d", 40)

// newHandoffRig runs one implementation attempt to awaiting-human-review against fakes.
func newHandoffRig(t *testing.T, review bool) *handoffRig {
	t.Helper()
	h, id, runtime, wt := fakeLaunchBackendHarness(t)
	r := &handoffRig{t: t, h: h, issue: id, runtime: runtime, reviews: trackLaunchReviews(t, h),
		work: &fakeHandoffWork{id: WorkIdentity{Head: handoffHead, Tree: hx("e", 40), Files: []gardenapi.FileChange{{Path: "a.go", Add: 3, Del: 1}}, Totals: gardenapi.ChangeTotals{Files: 1, Add: 3, Del: 1}}},
		prs:  &fakeHandoffPRs{}}
	r.wt = &countingWorktrees{Worktrees: wt}
	r.writer = &countingWriter{IssueWriter: h.b.deps.Writer}
	h.b.deps.LaunchWorktrees, h.b.deps.Writer = r.wt, r.writer
	h.b.deps.HandoffWork, h.b.deps.HandoffPRs = r.work, r.prs
	h.b.deps.OpenURL = func(u string) error { r.opened = append(r.opened, u); return nil }
	if review {
		saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	}
	ctx := context.Background()
	p, err := h.b.PreviewLaunch(ctx, fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartLaunch(ctx, gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	r.finishRun(v.Attempt, 1)
	r.attempt = r.reload()
	if r.attempt.State != launch.StateAwaitingReview {
		t.Fatalf("attempt state = %s", r.attempt.State)
	}
	_, r.stages0 = r.writer.counts()
	return r
}

func (r *handoffRig) finishRun(a launch.Attempt, run int) {
	r.t.Helper()
	zero := 0
	end := fakeLaunchBackendClock{}.Now()
	ident := launch.StartIdentity{OperationID: a.Runs[run-1].OperationID, SessionID: a.Runs[run-1].SessionID}
	r.runtime.observations[ident] = launch.SessionObservation{Identity: ident, State: "exited", Closed: true, Acknowledged: true, ExitCode: &zero, StartedAt: &end, EndedAt: &end}
	if _, err := r.h.b.LaunchAttempts(context.Background(), r.issue); err != nil {
		r.t.Fatal(err)
	}
}

func (r *handoffRig) reload() launch.Attempt {
	r.t.Helper()
	list, err := r.h.b.deps.LaunchStore.List(context.Background(), r.h.b.local, garden.IssueID(r.issue))
	if err != nil || len(list) == 0 {
		r.t.Fatal(err)
	}
	return list[len(list)-1]
}

func (r *handoffRig) linkPR(n int, head string) {
	r.t.Helper()
	_, err := r.h.b.deps.LaunchStore.Update(context.Background(), r.attempt.ID, func(a launch.Attempt) (launch.Attempt, bool, error) {
		a.PR = launch.PRLink{State: launch.PRLinked, Number: n, URL: fmt.Sprintf("https://github.com/acme/my-app/pull/%d", n), HeadSHA: head}
		return a, true, nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.prs.detail = github.PullRequestDetail{PullRequest: github.PullRequest{Number: n, State: "open", HeadSHA: head, Draft: true}}
}

// completePass finishes the attempt's first review pass with a recorded passing check.
func (r *handoffRig) completePass(reviewed, final string) reviewpass.Pass {
	r.t.Helper()
	ctx := context.Background()
	id := launchReviewID(r.attempt.ID)
	end := time.Now().UTC()
	step := func(fn func(*reviewpass.Pass)) reviewpass.Pass {
		p, err := r.reviews.Store.Update(ctx, id, func(p reviewpass.Pass) (reviewpass.Pass, bool, error) { fn(&p); return p, true, nil })
		if err != nil {
			r.t.Fatal(err)
		}
		return p
	}
	step(func(p *reviewpass.Pass) {
		p.ImplementationRun = p.Snapshot.AttemptID + "/start/1"
		p.ReviewedHead, p.FinalHead, p.Report = reviewed, final, "ev-"+hx("e", 32)
		p.Reviewer = &reviewpass.Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "starting"}
		p.State = reviewpass.StateStarting
	})
	step(func(p *reviewpass.Pass) {
		p.State = reviewpass.StateRunning
		p.Reviewer.State, p.Reviewer.Acknowledged = "running", true
	})
	zero := 0
	step(func(p *reviewpass.Pass) {
		p.State = reviewpass.StateChecking
		p.Reviewer.State, p.Reviewer.ExitCode, p.Reviewer.EndedAt = "exited", &zero, &end
		p.Checks = []reviewpass.CheckResult{{CheckID: p.Snapshot.Policy.Checks[0].ID, Outcome: reviewpass.OutcomePassed, ExitCode: &zero, EndedAt: &end}}
	})
	return step(func(p *reviewpass.Pass) { p.State, p.EndedAt = reviewpass.StateAwaitingHumanReview, &end })
}

func (r *handoffRig) view() gardenapi.HandoffView {
	r.t.Helper()
	v, err := r.h.b.Handoff(context.Background(), gardenapi.HandoffRequest{IssueID: r.issue})
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

func (r *handoffRig) decide(d garden.HandoffDecision, text string, target int64) gardenapi.HandoffDecisionView {
	r.t.Helper()
	ctx := context.Background()
	p, err := r.h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: d, Text: text, TargetCommentID: target})
	if err != nil {
		r.t.Fatal(err)
	}
	v, err := r.h.b.DecideHandoff(ctx, p, true)
	if err != nil || v.State != gardenapi.PlanWriteDone {
		r.t.Fatalf("decide %s = %+v, %v", d, v, err)
	}
	return v
}

// noMutations asserts the handoff paths merged, cleaned, pushed and relabeled nothing.
func (r *handoffRig) noMutations() {
	r.t.Helper()
	if r.wt.removes != 0 || r.wt.pushes != 0 || r.runtime.stops != 0 {
		r.t.Fatalf("handoff cleaned/pushed/stopped: removes=%d pushes=%d stops=%d", r.wt.removes, r.wt.pushes, r.runtime.stops)
	}
	if _, stages := r.writer.counts(); stages != r.stages0 {
		r.t.Fatalf("handoff changed labels: %d -> %d", r.stages0, stages)
	}
}

func TestHandoffViewHumanReviewShowsEvidenceAndFixes(t *testing.T) {
	r := newHandoffRig(t, true)
	r.completePass(hx("f", 40), handoffHead)
	r.linkPR(7, handoffHead)
	v := r.view()
	if v.AttemptID != r.attempt.ID || v.Outcome != "Implementation run 1 exited cleanly" || !v.InputsCurrent || !v.Changes.Available || v.Changes.FinalHead != handoffHead || v.Changes.Totals.Files != 1 {
		t.Fatalf("view = %+v", v)
	}
	if v.Review.State != gardenapi.HandoffReviewCompleted || !v.Review.FixesAfterReview || !v.Review.NoSecondReview || !v.ChecksRecorded || len(v.Checks) != 1 || v.Checks[0].Outcome != "passed" {
		t.Fatalf("review = %+v checks=%+v", v.Review, v.Checks)
	}
	if v.ConcernsKnown || v.Completion.State != handoff.CompletionAwaitingDecision || !v.Actions.Accept.Allowed || !v.Actions.OpenPR.Allowed || v.Actions.Rework.Allowed || v.Actions.Revoke.Allowed {
		t.Fatalf("completion=%+v actions=%+v", v.Completion, v.Actions)
	}
	if !v.PR.Linked || v.PR.Observed == nil || !v.PR.Observed.Open || r.prs.calls != 1 {
		t.Fatalf("pr = %+v calls=%d", v.PR, r.prs.calls)
	}
	p, err := r.h.b.PreviewHandoffDecision(context.Background(), gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted})
	if err != nil || len(p.Acknowledgments) != 1 || p.Acknowledgments[0] != acknowledgeFixes {
		t.Fatalf("acks = %+v %v", p.Acknowledgments, err)
	}
	r.noMutations()
}

func TestHandoffNoReviewAndMissingChecksNeedAcknowledgmentRecordedInTheText(t *testing.T) {
	r := newHandoffRig(t, false)
	v := r.view()
	if v.Review.State != gardenapi.HandoffReviewNone || v.ChecksRecorded || len(v.Checks) != 0 {
		t.Fatalf("review = %+v checks=%+v", v.Review, v.Checks)
	}
	p, err := r.h.b.PreviewHandoffDecision(context.Background(), gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted, Text: "Looks fine."})
	if err != nil || strings.Join(p.Acknowledgments, ",") != acknowledgeNoReview+","+acknowledgeChecks || !strings.Contains(p.Body, "Acknowledged: no-ai-review, checks-missing-or-failed.") {
		t.Fatalf("preview = %+v %v", p, err)
	}
	if _, err := r.h.b.DecideHandoff(context.Background(), p, true); err != nil {
		t.Fatal(err)
	}
	v = r.view()
	if v.Decision.Entry == nil || !strings.Contains(v.Decision.Entry.Text, "no-ai-review") || v.Completion.State != handoff.CompletionAccepted {
		t.Fatalf("recorded = %+v completion=%+v", v.Decision, v.Completion)
	}
	r.noMutations()
}

func TestHandoffReviewStateIsUnknownWhenPassesCannotBeRead(t *testing.T) {
	r := newHandoffRig(t, true)
	r.reviews.listErr = fmt.Errorf("store down")
	r.reviews.Store = &failingForAttempt{Store: r.reviews.Store}
	v := r.view()
	if v.Review.State != gardenapi.HandoffReviewUnknown || !v.Review.ReviewUnknown {
		t.Fatalf("review = %+v", v.Review)
	}
}

type failingForAttempt struct{ reviewpass.Store }

func (failingForAttempt) ForAttempt(context.Context, string) ([]reviewpass.Pass, error) {
	return nil, fmt.Errorf("store down")
}

func TestHandoffChangedHeadMakesAcceptanceStale(t *testing.T) {
	r := newHandoffRig(t, false)
	r.decide(garden.HandoffAccepted, "", 0)
	if v := r.view(); v.Completion.State != handoff.CompletionAccepted || v.Decision.State != "current" {
		t.Fatalf("after accept = %+v", v.Completion)
	}
	r.work.set(func(w *WorkIdentity) { w.Head, w.Tree = hx("9", 40), hx("8", 40) })
	v := r.view()
	if v.Completion.State != handoff.CompletionStale || v.Decision.State != "stale" || len(v.Decision.Stale) != 2 || v.Actions.Revoke.Allowed || !v.Actions.Accept.Allowed {
		t.Fatalf("stale view = %+v", v)
	}
	r.noMutations()
}

func TestHandoffPullRequestOutcomes(t *testing.T) {
	merged := func(d *github.PullRequestDetail) {
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		d.State, d.Merged, d.MergedAt, d.MergeCommitSHA = "merged", true, &at, hx("5", 40)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*github.PullRequestDetail)
		state  handoff.CompletionState
		reason handoff.Reason
	}{
		{"squash merge", merged, handoff.CompletionDone, ""},
		{"wrong pull request", func(d *github.PullRequestDetail) { merged(d); d.Number = 8 }, handoff.CompletionBlocked, handoff.ReasonWrongPR},
		{"merged into another base", func(d *github.PullRequestDetail) { merged(d); d.Base = "release" }, handoff.CompletionBlocked, handoff.ReasonWrongBase},
		{"merged into the published base", func(d *github.PullRequestDetail) { merged(d); d.Base = "main" }, handoff.CompletionDone, ""},
		{"closed unmerged", func(d *github.PullRequestDetail) { d.State = "closed" }, handoff.CompletionBlocked, handoff.ReasonPRClosedUnmerged},
		{"merged other head", func(d *github.PullRequestDetail) { merged(d); d.HeadSHA = hx("9", 40) }, handoff.CompletionBlocked, handoff.ReasonMergedHeadDiffers},
		{"open", func(*github.PullRequestDetail) {}, handoff.CompletionAccepted, handoff.ReasonPRNotMerged},
		{"head pushed after acceptance", func(d *github.PullRequestDetail) { d.HeadSHA = hx("9", 40) }, handoff.CompletionStale, handoff.ReasonPRHeadChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newHandoffRig(t, false)
			r.linkPR(7, handoffHead)
			r.decide(garden.HandoffAccepted, "", 0)
			tc.mutate(&r.prs.detail)
			v := r.view()
			if v.Completion.State != tc.state || tc.reason != "" && (len(v.Completion.Reasons) != 1 || v.Completion.Reasons[0] != tc.reason) {
				t.Fatalf("completion = %+v", v.Completion)
			}
			if tc.state == handoff.CompletionDone && (v.Completion.Evidence.Source != "github-pr" || v.Completion.Evidence.MergeCommitSHA != hx("5", 40)) {
				t.Fatalf("evidence = %+v", v.Completion.Evidence)
			}
			r.noMutations()
		})
	}
}

func TestHandoffAcceptanceBindsThePullRequestOnlyOnceItExists(t *testing.T) {
	merge := func(d *github.PullRequestDetail) {
		at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		d.State, d.Merged, d.MergedAt, d.MergeCommitSHA = "merged", true, &at, hx("5", 40)
	}
	t.Run("accepted before any pull request", func(t *testing.T) {
		r := newHandoffRig(t, false)
		r.decide(garden.HandoffAccepted, "", 0)
		r.linkPR(7, handoffHead)
		merge(&r.prs.detail)
		if v := r.view(); v.Completion.State != handoff.CompletionDone || v.Decision.State != "current" {
			t.Fatalf("view = %+v", v)
		}
	})
	t.Run("accepted on a pull request that was replaced", func(t *testing.T) {
		r := newHandoffRig(t, false)
		r.linkPR(7, handoffHead)
		r.decide(garden.HandoffAccepted, "", 0)
		r.linkPR(8, handoffHead)
		merge(&r.prs.detail)
		if v := r.view(); v.Completion.State != handoff.CompletionStale || v.Decision.State != "stale" {
			t.Fatalf("view = %+v", v)
		}
	})
}

func TestHandoffOfflinePullRequestIsUnknownNeverDone(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want handoff.ReadError
	}{
		"offline":     {fmt.Errorf("get: %w", connect.ErrOffline), handoff.ReadOffline},
		"not found":   {domain.E("not_found", "Not Found"), handoff.ReadNotFound},
		"unavailable": {fmt.Errorf("boom"), handoff.ReadUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			r := newHandoffRig(t, false)
			r.linkPR(7, handoffHead)
			r.decide(garden.HandoffAccepted, "", 0)
			r.prs.err = tc.err
			v := r.view()
			if v.Completion.State != handoff.CompletionUnknown || v.PR.ReadErr != tc.want || v.PR.Observed != nil {
				t.Fatalf("completion=%+v pr=%+v", v.Completion, v.PR)
			}
		})
	}
	r := newHandoffRig(t, false)
	r.linkPR(7, handoffHead)
	r.decide(garden.HandoffAccepted, "", 0)
	r.h.b.deps.HandoffPRs = nil
	if v := r.view(); v.Completion.State != handoff.CompletionUnknown || v.PR.ReadErr != handoff.ReadUnavailable {
		t.Fatalf("no seam = %+v", v.Completion)
	}
}

func TestHandoffRevokedAcceptanceAwaitsDecisionAgain(t *testing.T) {
	r := newHandoffRig(t, false)
	acc := r.decide(garden.HandoffAccepted, "", 0)
	v := r.view()
	if !v.Actions.Revoke.Allowed || v.Decision.Entry.CommentID != acc.CommentID {
		t.Fatalf("view = %+v", v.Actions)
	}
	r.decide(garden.HandoffRevoked, "Needs another look.", acc.CommentID)
	v = r.view()
	if v.Decision.State != "revoked" || v.Completion.State != handoff.CompletionAwaitingDecision || len(v.Completion.Reasons) != 1 || v.Completion.Reasons[0] != handoff.ReasonAcceptanceRevoked || v.Actions.Revoke.Allowed {
		t.Fatalf("revoked = %+v %+v", v.Decision, v.Completion)
	}
	r.noMutations()
}

func TestHandoffDirtyWorktreeBlocksAccept(t *testing.T) {
	r := newHandoffRig(t, false)
	r.work.set(func(w *WorkIdentity) { w.Dirty = true })
	v := r.view()
	if v.Actions.Accept.Allowed || v.Actions.Accept.Code != gardenapi.CodeHandoffBlocked || v.Completion.State != handoff.CompletionNotReady || !v.Changes.Dirty {
		t.Fatalf("dirty = %+v %+v", v.Actions.Accept, v.Completion)
	}
	_, err := r.h.b.PreviewHandoffDecision(context.Background(), gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted})
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
	if appends, _ := r.writer.counts(); appends != 0 {
		t.Fatal("blocked accept wrote")
	}
}

func TestHandoffUnreadableWorktreeIsNotReady(t *testing.T) {
	r := newHandoffRig(t, false)
	r.work.err = fmt.Errorf("git failed")
	v := r.view()
	if v.Changes.Available || v.Changes.Reason == "" || v.Completion.State != handoff.CompletionNotReady || v.Actions.Accept.Allowed {
		t.Fatalf("view = %+v %+v", v.Changes, v.Completion)
	}
}

func TestHandoffDigestChangeBetweenPreviewAndDecideWritesNothing(t *testing.T) {
	r := newHandoffRig(t, false)
	ctx := context.Background()
	p, err := r.h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted})
	if err != nil {
		t.Fatal(err)
	}
	r.work.set(func(w *WorkIdentity) { w.Head, w.Tree = hx("9", 40), hx("8", 40) })
	_, err = r.h.b.DecideHandoff(ctx, p, true)
	mustErrCode(t, err, gardenapi.CodeHandoffChanged)
	r.work.set(func(w *WorkIdentity) { w.Head, w.Tree = handoffHead, hx("e", 40) })
	tampered := p
	tampered.Body += "x"
	_, err = r.h.b.DecideHandoff(ctx, tampered, true)
	mustErrCode(t, err, gardenapi.CodeLaunchChanged)
	if _, err = r.h.b.DecideHandoff(ctx, p, false); err == nil {
		t.Fatal("unconfirmed decision was accepted")
	}
	if appends, _ := r.writer.counts(); appends != 0 {
		t.Fatalf("appends = %d", appends)
	}
}

func TestHandoffDecideReplayAppendsOnce(t *testing.T) {
	r := newHandoffRig(t, false)
	ctx := context.Background()
	p, err := r.h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted, Text: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.h.b.DecideHandoff(ctx, p, true)
	if err != nil || first.Replayed {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := r.h.b.DecideHandoff(ctx, p, true)
	if err != nil || !second.Replayed || second.CommentID != first.CommentID {
		t.Fatalf("second = %+v %v", second, err)
	}
	if appends, _ := r.writer.counts(); appends != 1 {
		t.Fatalf("appends = %d", appends)
	}
}

type failingAppend struct {
	garden.IssueWriter
	err error
}

func (f failingAppend) AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error) {
	return garden.IssueComment{}, f.err
}

func TestHandoffUncertainAppendReportsUnknown(t *testing.T) {
	r := newHandoffRig(t, false)
	ctx := context.Background()
	p, err := r.h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffAccepted})
	if err != nil {
		t.Fatal(err)
	}
	r.h.b.handoffs = nil
	r.h.b.deps.Writer = failingAppend{IssueWriter: r.writer, err: garden.ErrOffline}
	v, err := r.h.b.DecideHandoff(ctx, p, true)
	if err != nil || v.State != gardenapi.PlanWriteUnknown || v.Err == nil || v.Err.Code != gardenapi.CodeHandoffUnknown {
		t.Fatalf("view = %+v %v", v, err)
	}
}

func (r *handoffRig) requestChanges(text string) gardenapi.HandoffDecisionView {
	return r.decide(garden.HandoffChangesRequested, text, 0)
}

func TestHandoffReworkNeedsCurrentChangesRequested(t *testing.T) {
	r := newHandoffRig(t, false)
	ctx := context.Background()
	_, err := r.h.b.PreviewRework(ctx, r.issue, "")
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
	r.decide(garden.HandoffAccepted, "", 0)
	_, err = r.h.b.PreviewRework(ctx, r.issue, "")
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
	r.requestChanges("Rename the counter.")
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil || p.Feedback != "Rename the counter." || p.AccountName != "personal" || p.Provider != "codex" || p.AttemptID != r.attempt.ID {
		t.Fatalf("preview = %+v %v", p, err)
	}
	r.work.set(func(w *WorkIdentity) { w.Head, w.Tree = hx("9", 40), hx("8", 40) })
	_, err = r.h.b.PreviewRework(ctx, r.issue, "")
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
	if len(r.runtime.starts) != 1 {
		t.Fatal("preview started work")
	}
}

func TestHandoffReworkDedupesRunAndReviewPass(t *testing.T) {
	r := newHandoffRig(t, true)
	r.completePass(hx("f", 40), handoffHead)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil || !p.Review.Enabled || p.Review.Hash == "" {
		t.Fatalf("preview = %+v %v", p, err)
	}
	if _, err = r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p}); err == nil {
		t.Fatal("unconfirmed rework started")
	}
	req := gardenapi.ReworkStartRequest{Preview: p, Confirmed: true}
	first, err := r.h.b.StartRework(ctx, req)
	if err != nil || len(first.Attempt.Runs) != 2 || len(first.Attempt.Reworks) != 1 || first.Review == nil {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := r.h.b.StartRework(ctx, req)
	if err != nil || len(second.Attempt.Runs) != 2 {
		t.Fatalf("second = %+v %v", second, err)
	}
	if len(r.runtime.starts) != 2 {
		t.Fatalf("starts = %d", len(r.runtime.starts))
	}
	passes, err := r.reviews.Store.ForAttempt(ctx, r.attempt.ID)
	if err != nil || len(passes) != 2 || r.reviews.reserves != 2 {
		t.Fatalf("passes = %d reserves = %d %v", len(passes), r.reviews.reserves, err)
	}
	np := passes[1]
	if np.ID != reworkReviewID(r.attempt.ID, p.FeedbackCommentID, len(r.attempt.Runs)+1) || np.ID == passes[0].ID || np.ImplementationRun != r.attempt.ID+"/start/2" || np.State != reviewpass.StatePending {
		t.Fatalf("new pass = %+v", np)
	}
	r.noMutations()
}

func TestHandoffReworkSupersedesAPassThatNeverStarted(t *testing.T) {
	r := newHandoffRig(t, true)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	old := launchReviewID(r.attempt.ID)
	before, err := r.reviews.Store.Get(ctx, old)
	if err != nil || before.State != reviewpass.StatePending || before.Reviewer != nil {
		t.Fatalf("old pass = %+v %v", before, err)
	}
	req := gardenapi.ReworkStartRequest{Preview: p, Confirmed: true}
	if _, err = r.h.b.StartRework(ctx, req); err != nil {
		t.Fatal(err)
	}
	after, err := r.reviews.Store.Get(ctx, old)
	if err != nil || after.State != reviewpass.StateStopped || after.Code != reviewpass.CodeSuperseded || after.EndedAt == nil || after.Revision != before.Revision+1 {
		t.Fatalf("old pass = %+v %v", after, err)
	}
	fresh, err := r.reviews.Store.Get(ctx, reworkReviewID(r.attempt.ID, p.FeedbackCommentID, len(r.attempt.Runs)+1))
	if err != nil || fresh.State != reviewpass.StatePending {
		t.Fatalf("new pass = %+v %v", fresh, err)
	}
	if _, err = r.h.b.StartRework(ctx, req); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.reviews.Store.Get(ctx, old); again.Revision != after.Revision {
		t.Fatalf("repeat touched the old pass: %+v", again)
	}
	if again, _ := r.reviews.Store.Get(ctx, fresh.ID); again.Revision != fresh.Revision {
		t.Fatalf("repeat touched the new pass: %+v", again)
	}
}

func TestHandoffReworkRefusesWhileAReviewerMayBeRunning(t *testing.T) {
	r := newHandoffRig(t, true)
	ctx := context.Background()
	old := launchReviewID(r.attempt.ID)
	// The decision is taken after the pass state is final for this test, so the record matches what the human saw.
	if _, err := r.reviews.Store.Update(ctx, old, func(pass reviewpass.Pass) (reviewpass.Pass, bool, error) {
		pass.State = reviewpass.StateStarting
		pass.Reviewer = &reviewpass.Session{OperationID: pass.ID + "/review", SessionID: "sess_a", State: "starting"}
		return pass, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.requestChanges("Rename the counter.")
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := r.reviews.Store.Get(ctx, old)
	_, err = r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchBlocked)
	after, _ := r.reviews.Store.Get(ctx, old)
	passes, _ := r.reviews.Store.ForAttempt(ctx, r.attempt.ID)
	if !reflect.DeepEqual(before, after) || len(passes) != 1 || len(r.runtime.starts) != 1 {
		t.Fatalf("changed state: passes=%d starts=%d old=%+v", len(passes), len(r.runtime.starts), after)
	}
}

func TestHandoffReworkRefusesWhileTheOldHolderIsAlive(t *testing.T) {
	r := newHandoffRig(t, false)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	ident := launch.StartIdentity{OperationID: r.attempt.Runs[0].OperationID, SessionID: r.attempt.Runs[0].SessionID}
	r.runtime.observations[ident] = launch.SessionObservation{Identity: ident, State: "running", Alive: true, Acknowledged: true}
	_, err = r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchActive)
	if len(r.runtime.starts) != 1 {
		t.Fatal("second holder started")
	}
}

func TestHandoffReworkRefusesChangedFeedbackOrPolicy(t *testing.T) {
	r := newHandoffRig(t, false)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	_, err = r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchChanged)
	if len(r.runtime.starts) != 1 {
		t.Fatal("changed preview started work")
	}
}

func TestHandoffNonCodeCompletionIsDoneWithoutPullRequest(t *testing.T) {
	h, id, runtime, wt := fakeLaunchBackendHarness(t)
	writer := &countingWriter{IssueWriter: h.b.deps.Writer}
	prs := &fakeHandoffPRs{}
	h.b.deps.Writer, h.b.deps.HandoffPRs, h.b.deps.HandoffWork = writer, prs, &fakeHandoffWork{}
	ctx := context.Background()
	v, err := h.b.Handoff(ctx, gardenapi.HandoffRequest{IssueID: id})
	if err != nil || v.AttemptID != "" || !v.Actions.CompleteNonCode.Allowed || v.Actions.Accept.Allowed || v.Actions.OpenPR.Allowed || v.Completion.State != handoff.CompletionAwaitingDecision {
		t.Fatalf("view = %+v %v", v, err)
	}
	_, err = h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: id, Decision: garden.HandoffCompleted})
	mustErrCode(t, err, gardenapi.CodeInvalidInput)
	p, err := h.b.PreviewHandoffDecision(ctx, gardenapi.HandoffDecisionRequest{IssueID: id, Decision: garden.HandoffCompleted, Text: "Findings are written up in the design doc."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.b.DecideHandoff(ctx, p, true); err != nil {
		t.Fatal(err)
	}
	v, err = h.b.Handoff(ctx, gardenapi.HandoffRequest{IssueID: id})
	if err != nil || v.Completion.State != handoff.CompletionDone || v.Completion.Evidence.Source != "handoff-record" || !strings.Contains(v.Decision.Entry.Text, "design doc") {
		t.Fatalf("done = %+v %v", v.Completion, err)
	}
	if prs.calls != 0 || len(runtime.starts) != 0 || wt.creates != 0 {
		t.Fatal("non-code completion touched a pull request or the launch")
	}
	if _, stages := writer.counts(); stages != 0 {
		t.Fatal("completion changed labels")
	}
}

func TestHandoffOpenPullRequestOnlyOpensTheRepositoryURL(t *testing.T) {
	r := newHandoffRig(t, false)
	ctx := context.Background()
	if err := r.h.b.OpenHandoffPR(ctx, r.issue, ""); err == nil {
		t.Fatal("opened without a linked pull request")
	}
	r.linkPR(7, handoffHead)
	if err := r.h.b.OpenHandoffPR(ctx, r.issue, ""); err != nil || len(r.opened) != 1 || r.opened[0] != "https://github.com/acme/my-app/pull/7" {
		t.Fatalf("opened = %v %v", r.opened, err)
	}
	_, err := r.h.b.deps.LaunchStore.Update(ctx, r.attempt.ID, func(a launch.Attempt) (launch.Attempt, bool, error) {
		a.PR.URL = "https://evil.example/acme/my-app/pull/7"
		return a, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mustErrCode(t, r.h.b.OpenHandoffPR(ctx, r.issue, ""), gardenapi.CodeHandoffBlocked)
	if len(r.opened) != 1 {
		t.Fatalf("opened = %v", r.opened)
	}
}

func TestParseNumstat(t *testing.T) {
	var out strings.Builder
	for i := 0; i < maxHandoffFiles+3; i++ {
		fmt.Fprintf(&out, "1\t2\tf%d.go\x00", i)
	}
	out.WriteString("-\t-\timg\x1b[2J.png\x00")
	files, totals, truncated := parseNumstat(out.String())
	if len(files) != maxHandoffFiles || !truncated || totals.Files != maxHandoffFiles+4 || totals.Add != maxHandoffFiles+3 || totals.Del != 2*(maxHandoffFiles+3) {
		t.Fatalf("files=%d totals=%+v truncated=%v", len(files), totals, truncated)
	}
	_, _, _ = parseNumstat("")
	f, _, _ := parseNumstat("-\t-\timg\x1b[2J.png\x00")
	if len(f) != 1 || !f[0].Binary || strings.ContainsRune(f[0].Path, 0x1b) {
		t.Fatalf("binary = %+v", f)
	}
}

func TestHandoffReworkRefusalLeavesTheOldPassActive(t *testing.T) {
	r := newHandoffRig(t, true)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	ident := launch.StartIdentity{OperationID: r.attempt.Runs[0].OperationID, SessionID: r.attempt.Runs[0].SessionID}
	r.runtime.observations[ident] = launch.SessionObservation{Identity: ident, State: "running", Alive: true, Acknowledged: true}
	_, err = r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchActive)
	passes, err := r.reviews.Store.ForAttempt(ctx, r.attempt.ID)
	if err != nil || len(passes) != 1 || passes[0].State != reviewpass.StatePending {
		t.Fatalf("a refused rework changed the passes: %+v %v", passes, err)
	}
}

// failingHolder refuses to start and proves it: the session is recorded as closed without ever running.
type failingHolder struct{ *fakeLaunchBackendRuntime }

func (f failingHolder) Start(_ context.Context, r launch.StartSession) (launch.SessionObservation, error) {
	f.observations[r.Identity] = launch.SessionObservation{Identity: r.Identity, State: "not-started", Closed: true}
	return launch.SessionObservation{}, fmt.Errorf("the provider refused to start")
}

func TestHandoffReworkThatFailsToStartLeavesNoActivePass(t *testing.T) {
	r := newHandoffRig(t, true)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	r.h.b.mu.Lock()
	r.h.b.deps.LaunchHolder = failingHolder{r.runtime}
	r.h.b.launches = nil
	r.h.b.mu.Unlock()
	v, err := r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	if err == nil || v.Review != nil && v.Review.ID == reworkReviewID(r.attempt.ID, p.FeedbackCommentID, len(r.attempt.Runs)+1) {
		t.Fatalf("failed start returned the new review=%v err=%v", v.Review, err)
	}
	passes, err := r.reviews.Store.ForAttempt(ctx, r.attempt.ID)
	if err != nil || len(passes) != 2 {
		t.Fatalf("passes = %d %v", len(passes), err)
	}
	for _, pass := range passes {
		if pass.State.Active() {
			t.Fatalf("pass %s stays active: %+v", pass.ID, pass)
		}
	}
	if np := passes[1]; np.Code != reviewpass.CodeImplementationNotStarted {
		t.Fatalf("new pass = %+v", np)
	}
}

func TestHandoffReworkWithPolicyNowDisabledStillSupersedesTheOldPass(t *testing.T) {
	r := newHandoffRig(t, true)
	r.requestChanges("Rename the counter.")
	saveLaunchReviewPrefs(t, nil)
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil || p.Review.Hash != "" {
		t.Fatalf("preview = %+v %v", p.Review, err)
	}
	v, err := r.h.b.StartRework(ctx, gardenapi.ReworkStartRequest{Preview: p, Confirmed: true})
	if err != nil || len(v.Attempt.Reworks) != 1 {
		t.Fatalf("rework = %+v %v", v, err)
	}
	passes, err := r.reviews.Store.ForAttempt(ctx, r.attempt.ID)
	if err != nil || len(passes) != 1 || passes[0].State != reviewpass.StateStopped || passes[0].Code != reviewpass.CodeSuperseded {
		t.Fatalf("passes = %+v %v", passes, err)
	}
}

func (r *handoffRig) markCleaned() {
	r.t.Helper()
	_, err := r.h.b.deps.LaunchStore.Update(context.Background(), r.attempt.ID, func(a launch.Attempt) (launch.Attempt, bool, error) {
		a.State = launch.StateCleaned
		return a, true, nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

func TestHandoffDoneSurvivesCleanupAndRefusesEveryDecision(t *testing.T) {
	r := newHandoffRig(t, false)
	r.linkPR(7, handoffHead)
	r.decide(garden.HandoffAccepted, "", 0)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	r.prs.detail.State, r.prs.detail.Merged, r.prs.detail.MergedAt, r.prs.detail.MergeCommitSHA = "merged", true, &at, hx("5", 40)
	if v := r.view(); v.Completion.State != handoff.CompletionDone || v.Actions.Revoke.Allowed || v.Actions.Rework.Allowed || v.Actions.Accept.Allowed || v.Actions.RequestChanges.Allowed {
		t.Fatalf("done view = %+v", v)
	}
	r.markCleaned()
	v := r.view()
	if v.Completion.State != handoff.CompletionDone || v.Changes.Available || v.Changes.Reason != reasonWorktreeCleaned || v.Decision.State != "current" {
		t.Fatalf("cleaned view = %+v", v)
	}
	if v.Actions.Revoke.Allowed || v.Actions.Rework.Allowed || v.Actions.Accept.Allowed || v.Actions.RequestChanges.Allowed || !v.Actions.OpenPR.Allowed {
		t.Fatalf("cleaned actions = %+v", v.Actions)
	}
	if v.Actions.Accept.Message != "This work is already done." {
		t.Fatalf("cleaned done message = %q", v.Actions.Accept.Message)
	}
	_, err := r.h.b.PreviewHandoffDecision(context.Background(), gardenapi.HandoffDecisionRequest{IssueID: r.issue, Decision: garden.HandoffRevoked, TargetCommentID: v.Decision.Entry.CommentID})
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
}

func TestHandoffCleanedAttemptWithoutAcceptanceIsNotDone(t *testing.T) {
	r := newHandoffRig(t, false)
	r.linkPR(7, handoffHead)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	r.prs.detail.State, r.prs.detail.Merged, r.prs.detail.MergedAt, r.prs.detail.MergeCommitSHA = "merged", true, &at, hx("5", 40)
	r.markCleaned()
	if v := r.view(); v.Completion.State == handoff.CompletionDone {
		t.Fatalf("merged PR without an acceptance is done: %+v", v.Completion)
	}
	// An acceptance of other work does not stand in for the worktree either.
	r2 := newHandoffRig(t, false)
	r2.linkPR(7, hx("9", 40))
	r2.decide(garden.HandoffAccepted, "", 0)
	r2.prs.detail.State, r2.prs.detail.Merged, r2.prs.detail.MergedAt, r2.prs.detail.MergeCommitSHA = "merged", true, &at, hx("5", 40)
	r2.markCleaned()
	if v := r2.view(); v.Completion.State == handoff.CompletionDone {
		t.Fatalf("acceptance of another head is done: %+v", v.Completion)
	}
}

func TestHandoffDigestCoversPullRequestStateAndIgnoresNilAcknowledgments(t *testing.T) {
	base := gardenapi.HandoffDecisionPreview{IssueID: 1, AttemptID: "a", PRNumber: 7, PRHeadSHA: hx("a", 40)}
	merged := base
	merged.PRMerged = true
	rebased := base
	rebased.PRBaseRef = "release"
	empty := base
	empty.Acknowledgments = []string{}
	if digestOf(base) == digestOf(merged) || digestOf(base) == digestOf(rebased) || digestOf(base) != digestOf(empty) {
		t.Fatal("digest does not cover the pull request state, or distinguishes nil from empty acknowledgments")
	}
}

func TestHandoffReworkRefeedsAfterDefinitiveStartFailure(t *testing.T) {
	r := newHandoffRig(t, false)
	r.requestChanges("Rename the counter.")
	ctx := context.Background()
	p, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	r.h.b.mu.Lock()
	good := r.h.b.deps.LaunchHolder
	r.h.b.deps.LaunchHolder = failingHolder{r.runtime}
	r.h.b.launches = nil
	r.h.b.mu.Unlock()
	req := gardenapi.ReworkStartRequest{Preview: p, Confirmed: true}
	if _, err = r.h.b.StartRework(ctx, req); err == nil {
		t.Fatal("failed start reported success")
	}
	if v := r.view(); !v.Actions.Rework.Allowed {
		t.Fatalf("rework not offered again: %+v", v.Actions.Rework)
	}
	r.h.b.mu.Lock()
	r.h.b.deps.LaunchHolder = good
	r.h.b.launches = nil
	r.h.b.mu.Unlock()
	p2, err := r.h.b.PreviewRework(ctx, r.issue, "")
	if err != nil {
		t.Fatal(err)
	}
	req.Preview = p2
	v, err := r.h.b.StartRework(ctx, req)
	if err != nil || len(v.Attempt.Runs) != 3 || len(v.Attempt.Reworks) != 1 {
		t.Fatalf("refeed = %+v %v", v, err)
	}
	// The run is now acknowledged: the same feedback is a replay again.
	again, err := r.h.b.StartRework(ctx, req)
	if err != nil || len(again.Attempt.Runs) != 3 {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if len(r.runtime.starts) != 2 {
		t.Fatalf("starts = %d", len(r.runtime.starts))
	}
	if reworkReviewID("a", 1, 2) == reworkReviewID("a", 1, 3) {
		t.Fatal("a refed run reuses the earlier pass id")
	}
}

func TestHandoffReworkPreviewRefusesFeedbackTheAgentCannotTake(t *testing.T) {
	r := newHandoffRig(t, false)
	r.requestChanges("Rename\u202e the counter.")
	_, err := r.h.b.PreviewRework(context.Background(), r.issue, "")
	mustErrCode(t, err, gardenapi.CodeHandoffBlocked)
	if g := r.view().Actions.Rework; g.Allowed || !strings.Contains(g.Message, "cannot pass to the agent") {
		t.Fatalf("gate = %+v", g)
	}
}
