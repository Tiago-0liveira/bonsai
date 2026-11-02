package gardenbackend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const feedbackText = "Cover the counter race.\x00"

type reviseFixture struct {
	h       *harness
	f       *gardenfake.Fake
	id      uint64
	ag      *stubAgents
	co      *stubCheckout
	attempt string
	revHash string
}

// reviseSetup approves the Issue, runs one planner attempt to the end and publishes its plan: the state in which
// the reviewer reads revision r1.
func reviseSetup(t *testing.T) *reviseFixture {
	t.Helper()
	h, f, id, ag, co := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, err := h.b.PreviewPlanner(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	ag.running = true
	rv := publishForApproval(t, h, id, v.Attempt.ID, backendPlan)
	ag.status[v.Attempt.SessionID] = plan.SessionStatus{State: "exited", ExitCode: new(int)}
	return &reviseFixture{h: h, f: f, id: id, ag: ag, co: co, attempt: v.Attempt.ID, revHash: rv.Review.Current.Hash}
}

func (x *reviseFixture) preview(t *testing.T) gardenapi.PlanRevisePreviewView {
	t.Helper()
	pv, err := x.h.b.PreviewPlanRevise(context.Background(), x.id)
	if err != nil {
		t.Fatal(err)
	}
	return pv
}

// request is the revise request a UI would build from the preview and the retained settings.
func (x *reviseFixture) request(t *testing.T, pv gardenapi.PlanRevisePreviewView) gardenapi.PlanReviseRequest {
	rv, err := x.h.b.PlanReview(context.Background(), x.id)
	if err != nil {
		t.Fatal(err)
	}
	req := gardenapi.PlanReviseRequest{Stamp: pv.Stamp, IssueID: x.id, RevisionHash: pv.Revision.Hash, Fingerprint: pv.Revision.SourceSpecFingerprint,
		SpecHash: pv.Revision.SourceSpecHash, ActorID: rv.ActorID, Reason: feedbackText, AcceptReadOnly: true, Base: pv.Base, Cols: 100, Rows: 30}
	if pv.Settings != nil {
		req.Settings = *pv.Settings
	}
	if pv.Pending != nil {
		req.DecisionCommentID = pv.Pending.DecisionCommentID
	}
	return req
}

func TestPreviewPlanReviseRetainsSettingsAndStartsNothing(t *testing.T) {
	x := reviseSetup(t)
	before, writes := len(x.ag.started), commentWrites(x.f)
	for i := 0; i < 3; i++ {
		pv := x.preview(t)
		if pv.Unavailable != "" || pv.Revision.Hash != x.revHash || pv.Pending != nil || pv.SettingsUnavailable != "" || pv.SettingsSource != x.attempt {
			t.Fatalf("preview = %+v", pv)
		}
		if pv.Settings == nil || *pv.Settings != (gardenapi.PlanReviseSettings{AccountID: "claude-acct", Model: "opus"}) || pv.Base.Commit != "c0ffee" {
			t.Fatalf("settings = %+v base = %+v", pv.Settings, pv.Base)
		}
	}
	if len(x.ag.started) != before || commentWrites(x.f) != writes {
		t.Fatal("previewing started a run or wrote")
	}
}

func TestRevisePlanRecordsDecisionThenStartsOneAttempt(t *testing.T) {
	x := reviseSetup(t)
	ctx := context.Background()
	pv := x.preview(t)
	writes := commentWrites(x.f)
	v, err := x.h.b.RevisePlan(ctx, x.request(t, pv))
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision.State != gardenapi.PlanWriteDone || v.Decision.CommentID == 0 || v.StartErr != nil || v.Existing || v.Attempt == nil || commentWrites(x.f) != writes+1 {
		t.Fatalf("view = %+v", v)
	}
	if r := v.Attempt.Revises; r == nil || r.RevisionHash != x.revHash || r.DecisionCommentID != v.Decision.CommentID || r.Ordinal != 1 {
		t.Fatalf("revises = %+v", r)
	}
	if len(x.ag.started) != 2 {
		t.Fatalf("sessions = %d", len(x.ag.started))
	}
	l := x.ag.started[1].Launch
	if !l.ReadOnly || l.Model != "opus" || !strings.Contains(l.Prompt, "Cover the counter race.") || !strings.Contains(l.Prompt, "Predecessor plan r1:") ||
		!strings.Contains(l.Prompt, "> 1. Add it.") || strings.ContainsRune(l.Prompt, '\x00') {
		t.Fatalf("launch = %+v", l)
	}
	// The pending decision now points at the running attempt, and reading it again starts nothing.
	for i := 0; i < 2; i++ {
		got := x.preview(t)
		if got.Pending == nil || got.Pending.Attempt == nil || got.Pending.Attempt.ID != v.Attempt.ID || !strings.HasPrefix(got.Pending.Reason, "Cover the counter race.") || got.Unavailable == "" {
			t.Fatalf("pending preview = %+v", got)
		}
	}
	if len(x.ag.started) != 2 {
		t.Fatal("preview started another attempt")
	}
	// The local-work summary names what the attempt revises and who runs it, without prompts or output.
	lw, err := x.h.b.LocalWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := lw.Issues[x.id].Planner
	if s == nil || s.Revises == nil || s.Revises.DecisionCommentID != v.Decision.CommentID || s.Provider != "claude" || s.AccountName != "work" || s.Model != "opus" || s.RequestedAt.IsZero() {
		t.Fatalf("summary = %+v", s)
	}
}

func TestRevisePlanTwiceReturnsTheExistingAttempt(t *testing.T) {
	x := reviseSetup(t)
	ctx := context.Background()
	req := x.request(t, x.preview(t))
	first, err := x.h.b.RevisePlan(ctx, req)
	if err != nil || first.Attempt == nil {
		t.Fatalf("first = %+v, %v", first, err)
	}
	writes := commentWrites(x.f)
	again, err := x.h.b.RevisePlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Existing || again.Attempt == nil || again.Attempt.ID != first.Attempt.ID || !again.Decision.Replayed || again.StartErr != nil {
		t.Fatalf("again = %+v", again)
	}
	if len(x.ag.started) != 2 || commentWrites(x.f) != writes {
		t.Fatal("a repeated request wrote or started again")
	}
}

func TestRevisePlanStartFailureKeepsDecisionAndAllowsExplicitRetry(t *testing.T) {
	x := reviseSetup(t)
	ctx := context.Background()
	req := x.request(t, x.preview(t))
	x.ag.startErr = errors.New("holder down")
	v, err := x.h.b.RevisePlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision.State != gardenapi.PlanWriteDone || v.Attempt != nil || v.StartErr == nil || v.StartErr.Code != gardenapi.CodeFailed {
		t.Fatalf("view = %+v", v)
	}
	// The decision is not rolled back and is shown as pending; looking never starts anything.
	x.ag.startErr = nil
	for i := 0; i < 2; i++ {
		pv := x.preview(t)
		if pv.Pending == nil || pv.Pending.DecisionCommentID != v.Decision.CommentID || pv.Pending.Attempt == nil || pv.Pending.Attempt.State != plan.StateFailed {
			t.Fatalf("pending = %+v", pv.Pending)
		}
	}
	if len(x.ag.started) != 1 {
		t.Fatal("refresh started an attempt")
	}
	pv := x.preview(t)
	retry := x.request(t, pv)
	retry.DecisionCommentID++
	if _, err := x.h.b.StartPlanRevise(ctx, retry); code(err) != gardenapi.CodeConflict {
		t.Fatalf("wrong decision = %v", err)
	}
	retry.DecisionCommentID--
	writes := commentWrites(x.f)
	got, err := x.h.b.StartPlanRevise(ctx, retry)
	if err != nil || got.Attempt == nil || got.StartErr != nil || got.Existing || got.Decision.CommentID != v.Decision.CommentID || commentWrites(x.f) != writes {
		t.Fatalf("retry = %+v, %v", got, err)
	}
	if again, err := x.h.b.StartPlanRevise(ctx, retry); err != nil || !again.Existing || again.Attempt.ID != got.Attempt.ID || len(x.ag.started) != 2 {
		t.Fatalf("second retry = %+v, %v", again, err)
	}
}

func TestStartPlanReviseNeedsAPendingDecision(t *testing.T) {
	x := reviseSetup(t)
	req := x.request(t, x.preview(t))
	req.DecisionCommentID = 1
	if _, err := x.h.b.StartPlanRevise(context.Background(), req); code(err) != gardenapi.CodeConflict {
		t.Fatalf("no decision recorded = %v", err)
	}
	if len(x.ag.started) != 1 {
		t.Fatal("started without a recorded decision")
	}
}

func TestRevisePlanUnknownDecisionStartsNothing(t *testing.T) {
	x := reviseSetup(t)
	req := x.request(t, x.preview(t))
	// The comment reaches GitHub but reading it back fails: the outcome is unknown.
	x.f.AfterNextAppend(func() { x.f.FailNext(errors.New("network down")) })
	v, err := x.h.b.RevisePlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if v.Decision.State != gardenapi.PlanWriteUnknown || v.Decision.Err == nil || v.Attempt != nil || v.StartErr != nil || len(x.ag.started) != 1 {
		t.Fatalf("view = %+v", v)
	}
	// Reopening finds the recorded request pending, and nothing started by itself.
	if pv := x.preview(t); pv.Pending == nil || pv.Pending.Attempt != nil || len(x.ag.started) != 1 {
		t.Fatalf("pending = %+v", pv.Pending)
	}
}

func TestRevisePlanRefusesBeforeWritingWhatTheStartWouldRefuse(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(x *reviseFixture, req *gardenapi.PlanReviseRequest)
		want   gardenapi.Code
	}{
		"spec fingerprint changed": {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.Fingerprint = "other" }, gardenapi.CodeSpecChanged},
		"spec hash changed":        {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.SpecHash = "other" }, gardenapi.CodeSpecChanged},
		"checkout moved":           {func(x *reviseFixture, _ *gardenapi.PlanReviseRequest) { x.co.base.Commit = "beef" }, gardenapi.CodeRepoChanged},
		"dirty without consent": {func(x *reviseFixture, r *gardenapi.PlanReviseRequest) {
			x.co.base = plan.Base{Commit: "c0ffee", Dirty: true, DirtyFiles: 1, DirtyDigest: "d"}
			r.Base = x.co.base
		}, gardenapi.CodeDirtyContext},
		"permissions not accepted": {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.AcceptReadOnly = false }, gardenapi.CodeInvalidInput},
		"account cannot plan":      {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.Settings.AccountID = "agy-acct" }, gardenapi.CodeReadOnlyUnsupported},
		"account deleted": {func(x *reviseFixture, _ *gardenapi.PlanReviseRequest) {
			x.ag.gone = map[agents.AccountID]bool{"claude-acct": true}
		}, gardenapi.CodeNotFound},
		"other clone": {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.Stamp.LocalRepoID = "elsewhere" }, gardenapi.CodeNotFound},
		"no revision": {func(_ *reviseFixture, r *gardenapi.PlanReviseRequest) { r.RevisionHash = "" }, gardenapi.CodeNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			x := reviseSetup(t)
			req := x.request(t, x.preview(t))
			tc.mutate(x, &req)
			writes := commentWrites(x.f)
			if _, err := x.h.b.RevisePlan(context.Background(), req); code(err) != tc.want {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if commentWrites(x.f) != writes || len(x.ag.started) != 1 {
				t.Fatal("a refused request wrote a decision or started a run")
			}
		})
	}
}

func TestRevisePlanRefusesWhenTheHeadMovedSincePreview(t *testing.T) {
	x := reviseSetup(t)
	ctx := context.Background()
	req := x.request(t, x.preview(t))
	// A newer revision supersedes the one the reviewer looked at.
	pp, err := x.h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: x.id, AttemptID: x.attempt, Text: backendPlan + "\n\nAlso handle retries."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.h.b.PublishPlan(ctx, pp); err != nil {
		t.Fatal(err)
	}
	writes := commentWrites(x.f)
	if _, err := x.h.b.RevisePlan(ctx, req); code(err) != gardenapi.CodeConflict {
		t.Fatalf("moved head = %v", err)
	}
	if commentWrites(x.f) != writes || len(x.ag.started) != 1 {
		t.Fatal("moved head wrote or started")
	}
}

func TestRevisePlanFeedbackIsRefusedOnceItIsNoLongerCurrent(t *testing.T) {
	x := reviseSetup(t)
	ctx := context.Background()
	req := x.request(t, x.preview(t))
	x.ag.startErr = errors.New("holder down")
	if _, err := x.h.b.RevisePlan(ctx, req); err != nil {
		t.Fatal(err)
	}
	x.ag.startErr = nil
	retry := x.request(t, x.preview(t))
	// Someone publishes a newer revision before the explicit retry: the old feedback must not start anything.
	pp, err := x.h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: x.id, AttemptID: x.attempt, Text: backendPlan + "\n\nAlso handle retries."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.h.b.PublishPlan(ctx, pp); err != nil {
		t.Fatal(err)
	}
	if _, err := x.h.b.StartPlanRevise(ctx, retry); code(err) != gardenapi.CodeConflict {
		t.Fatalf("stale feedback = %v", err)
	}
	if len(x.ag.started) != 1 {
		t.Fatal("stale feedback started a run")
	}
}

func TestPreviewPlanReviseSettingsUnavailable(t *testing.T) {
	for name, setup := range map[string]func(x *reviseFixture){
		"account deleted": func(x *reviseFixture) { x.ag.gone = map[agents.AccountID]bool{"claude-acct": true} },
	} {
		t.Run(name, func(t *testing.T) {
			x := reviseSetup(t)
			setup(x)
			pv := x.preview(t)
			if pv.Settings != nil || pv.SettingsSource != "" || !strings.Contains(pv.SettingsUnavailable, "no longer exists") || pv.Unavailable != "" || pv.Revision.Hash != x.revHash {
				t.Fatalf("preview = %+v", pv)
			}
		})
	}
	t.Run("no attempt published this revision", func(t *testing.T) {
		x := reviseSetup(t)
		// A revision from someone's other machine: no local attempt names its hash.
		pp, err := x.h.b.PreviewPlanPublish(context.Background(), gardenapi.PlanPublishRequest{IssueID: x.id, AttemptID: x.attempt, Text: backendPlan + "\n\nMore."})
		if err != nil {
			t.Fatal(err)
		}
		res, err := x.h.b.PublishPlan(context.Background(), pp)
		if err != nil {
			t.Fatal(err)
		}
		// The store keeps the first hash, so the new revision has no source attempt.
		if got, _ := x.h.b.deps.Plans.Get(context.Background(), x.attempt); got.PublishedRevision != x.revHash {
			t.Fatalf("published revision overwritten: %q", got.PublishedRevision)
		}
		pv := x.preview(t)
		if pv.Revision.Hash != res.Hash || pv.Settings != nil || pv.SettingsUnavailable == "" {
			t.Fatalf("preview = %+v", pv)
		}
	})
}

func TestPlanReviseRepliesAreStampedInOrder(t *testing.T) {
	x := reviseSetup(t)
	a, b := x.preview(t), x.preview(t)
	var g gardenapi.Gate
	if !g.Accept(b.Stamp) || g.Accept(a.Stamp) {
		t.Fatal("an older preview was accepted after a newer one")
	}
	v, err := x.h.b.RevisePlan(context.Background(), x.request(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if v.Stamp.Generation <= b.Stamp.Generation || v.Decision.Stamp.Generation != v.Stamp.Generation {
		t.Fatalf("stamps preview=%+v revise=%+v decision=%+v", b.Stamp, v.Stamp, v.Decision.Stamp)
	}
}

func TestRevisePlanWithALiveAttemptWritesNothing(t *testing.T) {
	ctx := context.Background()
	t.Run("unrelated running attempt", func(t *testing.T) {
		x := reviseSetup(t)
		req := x.request(t, x.preview(t))
		// Another plain planner run is live for the Issue.
		p, err := x.h.b.PreviewPlanner(ctx, x.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := x.h.b.StartPlanner(ctx, startReq(p, "claude-acct")); err != nil {
			t.Fatal(err)
		}
		writes := commentWrites(x.f)
		if _, err := x.h.b.RevisePlan(ctx, req); code(err) != gardenapi.CodeAttemptActive {
			t.Fatalf("err = %v", err)
		}
		if commentWrites(x.f) != writes || len(x.ag.started) != 2 {
			t.Fatal("refused request wrote a decision or started a run")
		}
	})
	t.Run("this request's own running attempt", func(t *testing.T) {
		x := reviseSetup(t)
		req := x.request(t, x.preview(t))
		first, err := x.h.b.RevisePlan(ctx, req)
		if err != nil || first.Attempt == nil {
			t.Fatalf("first = %+v, %v", first, err)
		}
		writes := commentWrites(x.f)
		again, err := x.h.b.RevisePlan(ctx, req)
		if err != nil || !again.Existing || again.Attempt.ID != first.Attempt.ID || again.Decision.CommentID != first.Decision.CommentID {
			t.Fatalf("again = %+v, %v", again, err)
		}
		if commentWrites(x.f) != writes || len(x.ag.started) != 2 {
			t.Fatal("duplicate wrote or started")
		}
	})
}

func TestStartPlanReviseAfterCancelledAttempt(t *testing.T) {
	ctx := context.Background()
	x := reviseSetup(t)
	first, err := x.h.b.RevisePlan(ctx, x.request(t, x.preview(t)))
	if err != nil || first.Attempt == nil {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := x.h.b.CancelPlanner(ctx, first.Attempt.ID); err != nil {
		t.Fatal(err)
	}
	x.ag.status[first.Attempt.SessionID] = plan.SessionStatus{State: "exited", ExitCode: new(int)}
	pv := x.preview(t)
	if pv.Pending == nil || pv.Pending.Attempt == nil || pv.Pending.Attempt.State != plan.StateCancelled {
		t.Fatalf("pending = %+v", pv.Pending)
	}
	got, err := x.h.b.StartPlanRevise(ctx, x.request(t, pv))
	if err != nil || got.Attempt == nil || got.Existing || got.Attempt.ID == first.Attempt.ID || len(x.ag.started) != 3 {
		t.Fatalf("retry = %+v, %v", got, err)
	}
}
